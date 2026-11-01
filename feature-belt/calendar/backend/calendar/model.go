package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Response values for attendees.
const (
	ResponseNeedsAction = "needs_action"
	ResponseAccepted    = "accepted"
	ResponseTentative   = "tentative"
	ResponseDeclined    = "declined"
)

// Edit and delete scopes for recurring events.
const (
	ScopeThis      = "this"
	ScopeFollowing = "following"
	ScopeAll       = "all"
)

var colors = []string{"primary", "accent", "success", "warning", "danger", "info", "neutral"}

// Person is a platform user as other people see them: name and picture.
type Person struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
}

// Attendee is one invitee. Email is set for external guests only; platform
// users show by name.
type Attendee struct {
	ID          uuid.UUID  `json:"id"`
	UserID      *uuid.UUID `json:"user_id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	AvatarURL   *string    `json:"avatar_url"`
	Response    string     `json:"response"`
	RespondedAt *time.Time `json:"responded_at"`
}

// Occurrence is one instance of an event in a calendar range. EventID is
// the single event or the series master; OccurrenceStart (series only) is
// the instance's original start and identifies it in edits.
type Occurrence struct {
	EventID         uuid.UUID  `json:"event_id"`
	OccurrenceStart *time.Time `json:"occurrence_start"`
	Recurring       bool       `json:"recurring"`
	Modified        bool       `json:"modified"`
	Title           string     `json:"title"`
	Location        string     `json:"location"`
	ConferenceURL   *string    `json:"conference_url"`
	StartsAt        time.Time  `json:"starts_at"`
	EndsAt          time.Time  `json:"ends_at"`
	AllDay          bool       `json:"all_day"`
	StartDate       *string    `json:"start_date"`
	EndDate         *string    `json:"end_date"`
	Timezone        string     `json:"timezone"`
	Color           string     `json:"color"`
	ShowAs          string     `json:"show_as"`
	Visibility      string     `json:"visibility"`
	Role            string     `json:"role"`
	MyResponse      *string    `json:"my_response"`
	Owner           Person     `json:"owner"`
	AttendeeCount   int        `json:"attendee_count"`
	BookingID       *uuid.UUID `json:"booking_id"`
}

// EventDetail is an occurrence with everything the event dialog shows.
type EventDetail struct {
	Occurrence
	Description string      `json:"description"`
	Recurrence  *Recurrence `json:"recurrence"`
	RRule       string      `json:"rrule"`
	Attendees   []Attendee  `json:"attendees"`
	Reminders   []int       `json:"reminders"`
	CanEdit     bool        `json:"can_edit"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Recurrence is the structured form of the supported RRULE subset.
type Recurrence struct {
	Freq       string   `json:"freq"`         // daily | weekly | monthly | yearly
	Interval   int      `json:"interval"`     // every N periods (1 = every)
	ByDay      []string `json:"by_day"`       // weekly: MO…SU; monthly: 1MO…-1SU
	ByMonthDay []int    `json:"by_month_day"` // monthly: 1…31, -1 = last day
	Count      *int     `json:"count"`        // ends after N occurrences
	Until      *string  `json:"until"`        // ends on this day (YYYY-MM-DD, inclusive) in the event's zone
}

// HoursInterval is a weekly availability window: day 0 = Sunday, start and
// end in minutes from local midnight (end ≤ 1440).
type HoursInterval struct {
	Day   int `json:"day"`
	Start int `json:"start"`
	End   int `json:"end"`
}

// DateOverride replaces a date's weekly hours; no intervals = unavailable.
type DateOverride struct {
	Date      string         `json:"date"`
	Intervals []TimeInterval `json:"intervals"`
}

// TimeInterval is minutes from local midnight.
type TimeInterval struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// eventRow is a calendar_events row joined with its owner.
type eventRow struct {
	ID               uuid.UUID
	OwnerID          uuid.UUID
	RecurringEventID *uuid.UUID
	OriginalStart    *time.Time
	Title            string
	Description      string
	Location         string
	ConferenceURL    *string
	ConferenceID     *string
	StartsAt         time.Time
	EndsAt           time.Time
	Timezone         string
	AllDay           bool
	RRule            *string
	SeriesUntil      *time.Time
	Status           string
	Visibility       string
	ShowAs           string
	Color            string
	Reminders        []int32
	Sequence         int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	OwnerName        string
	OwnerAvatar      *string
}

const eventCols = `e.id, e.owner_id, e.recurring_event_id, e.original_start, e.title, e.description, e.location,
	e.conference_url, e.conference_id, e.starts_at, e.ends_at, e.timezone, e.all_day, e.rrule, e.series_until,
	e.status, e.visibility, e.show_as, e.color, e.reminders, e.sequence, e.created_at, e.updated_at,
	u.display_name, u.avatar_url`

func scanEvent(row pgx.Row) (eventRow, error) {
	var e eventRow
	err := row.Scan(&e.ID, &e.OwnerID, &e.RecurringEventID, &e.OriginalStart, &e.Title, &e.Description, &e.Location,
		&e.ConferenceURL, &e.ConferenceID, &e.StartsAt, &e.EndsAt, &e.Timezone, &e.AllDay, &e.RRule, &e.SeriesUntil,
		&e.Status, &e.Visibility, &e.ShowAs, &e.Color, &e.Reminders, &e.Sequence, &e.CreatedAt, &e.UpdatedAt,
		&e.OwnerName, &e.OwnerAvatar)
	return e, err
}

func (e eventRow) loc() *time.Location { return zoneOr(e.Timezone, time.UTC) }

func (e eventRow) duration() time.Duration { return e.EndsAt.Sub(e.StartsAt) }

func (e eventRow) rule() (Rule, bool) {
	if e.RRule == nil {
		return Rule{}, false
	}
	r, err := ParseRule(*e.RRule, e.loc())
	return r, err == nil
}

func (e eventRow) reminders() []int {
	out := make([]int, len(e.Reminders))
	for i, r := range e.Reminders {
		out[i] = int(r)
	}
	return out
}

// loadEventRow reads one row (any kind) by id.
func loadEventRow(ctx context.Context, q db.Querier, id uuid.UUID) (eventRow, error) {
	e, err := scanEvent(q.QueryRow(ctx, `SELECT `+eventCols+` FROM calendar_events e JOIN users u ON u.id = e.owner_id WHERE e.id = $1`, id))
	if db.IsNoRows(err) {
		return e, errEventNotFound
	}
	return e, err
}

var (
	errEventNotFound = httpx.NotFound("That event doesn't exist or isn't on your calendar.")
	errNotOrganizer  = httpx.Forbidden("Only the organizer can change this event.")
)

// ---- zones, dates and tokens ----------------------------------------------

// zoneOr loads an IANA zone, or returns def.
func zoneOr(name string, def *time.Location) *time.Location {
	if name == "" {
		return def
	}
	if l, err := time.LoadLocation(name); err == nil {
		return l
	}
	return def
}

// validZone reports whether name is an IANA zone (not "Local").
func validZone(name string) bool {
	if name == "" || name == "Local" || len(name) > 64 {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// localDate is t's calendar date in loc as YYYY-MM-DD.
func localDate(t time.Time, loc *time.Location) string { return t.In(loc).Format(time.DateOnly) }

// allDayDates are the first and last (inclusive) dates of an all-day span.
func allDayDates(start, end time.Time, loc *time.Location) (*string, *string) {
	a := localDate(start, loc)
	b := localDate(end.Add(-time.Second), loc)
	return &a, &b
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// randomToken is n random bytes, URL-safe base64.
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// cleanText trims and bounds free text; ok=false when over max runes.
func cleanText(v string, max int) (string, bool) {
	v = strings.TrimSpace(strings.ToValidUTF8(v, ""))
	return v, utf8.RuneCountInString(v) <= max
}

// normalizeEmail returns the lowercase address, or "" when invalid.
func normalizeEmail(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 254 || strings.ContainsAny(v, " \t\r\n<>,;\"") {
		return ""
	}
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || !strings.Contains(v[strings.LastIndex(v, "@"):], ".") {
		return ""
	}
	return strings.ToLower(v)
}

func validColor(c string) bool { return slices.Contains(colors, c) }

// ---- recurrence conversions ------------------------------------------------

var freqs = map[string]string{"daily": "DAILY", "weekly": "WEEKLY", "monthly": "MONTHLY", "yearly": "YEARLY"}

// toRule validates a structured recurrence against the series start and
// returns the rule. Weekly without days repeats on the start's weekday;
// monthly without days repeats on the start's day of month.
func (rc Recurrence) toRule(start time.Time, loc *time.Location) (Rule, error) {
	f, ok := freqs[rc.Freq]
	if !ok {
		return Rule{}, httpx.BadRequest("Repeat must be daily, weekly, monthly or yearly.")
	}
	r := Rule{Freq: f, Interval: max(rc.Interval, 1), WeekStart: time.Monday}
	if r.Interval > 99 {
		return r, httpx.BadRequest("Repeat every 1 to 99 periods.")
	}
	if len(rc.ByDay) > 7 || len(rc.ByMonthDay) > 31 {
		return r, httpx.BadRequest("Too many repeat days.")
	}
	for _, d := range rc.ByDay {
		p, err := ParseRule("FREQ="+f+";BYDAY="+strings.ToUpper(d), loc)
		if err != nil || len(p.ByDay) != 1 {
			return r, httpx.BadRequest("Repeat days must look like MO or 2TU.")
		}
		if p.ByDay[0].N != 0 && f != "MONTHLY" {
			return r, httpx.BadRequest("Numbered repeat days (like 2TU) work with monthly only.")
		}
		r.ByDay = append(r.ByDay, p.ByDay[0])
	}
	if f == "DAILY" && len(rc.ByDay) > 0 {
		return r, httpx.BadRequest("Daily repeats can't pick days; use weekly.")
	}
	if f == "YEARLY" && (len(rc.ByDay) > 0 || len(rc.ByMonthDay) > 0) {
		return r, httpx.BadRequest("Yearly repeats use the start date.")
	}
	for _, d := range rc.ByMonthDay {
		if d == 0 || d < -1 || d > 31 {
			return r, httpx.BadRequest("Repeat dates must be 1 to 31, or -1 for the last day.")
		}
		if f != "MONTHLY" {
			return r, httpx.BadRequest("Repeat dates work with monthly only.")
		}
		r.ByMonthDay = append(r.ByMonthDay, d)
	}
	if rc.Count != nil && rc.Until != nil {
		return r, httpx.BadRequest("Choose an end date or a number of times, not both.")
	}
	if rc.Count != nil {
		if *rc.Count < 1 || *rc.Count > 999 {
			return r, httpx.BadRequest("A series repeats 1 to 999 times.")
		}
		r.Count = *rc.Count
	}
	if rc.Until != nil {
		d, err := time.ParseInLocation(time.DateOnly, *rc.Until, loc)
		if err != nil {
			return r, httpx.BadRequest("The end date must look like 2026-12-31.")
		}
		until := d.AddDate(0, 0, 1).Add(-time.Second).UTC()
		if until.Before(start) {
			return r, httpx.BadRequest("The series must end after it starts.")
		}
		r.Until = &until
	}
	return r, nil
}

// recurrenceOf is the structured form of a stored rule.
func recurrenceOf(r Rule, loc *time.Location) *Recurrence {
	out := &Recurrence{Freq: strings.ToLower(r.Freq), Interval: max(r.Interval, 1), ByDay: []string{}, ByMonthDay: []int{}}
	for _, d := range r.ByDay {
		c := codeOfDay[d.Day]
		if d.N != 0 {
			c = strconv.Itoa(d.N) + c
		}
		out.ByDay = append(out.ByDay, c)
	}
	out.ByMonthDay = append(out.ByMonthDay, r.ByMonthDay...)
	if r.Count > 0 {
		n := r.Count
		out.Count = &n
	}
	if r.Until != nil {
		d := r.Until.In(loc).Format(time.DateOnly)
		out.Until = &d
	}
	return out
}

// seriesUntil is the end of the series' last occurrence, nil when endless.
func seriesUntil(r Rule, start time.Time, loc *time.Location, dur time.Duration) *time.Time {
	last, ok := r.Last(start, loc)
	if !ok {
		return nil
	}
	end := last.Add(dur)
	return &end
}

// ---- settings --------------------------------------------------------------

// Settings are the caller's calendar preferences.
type Settings struct {
	Timezone               string          `json:"timezone"`
	TimezoneSet            bool            `json:"timezone_set"`
	WeekStart              int             `json:"week_start"`
	WorkingHours           []HoursInterval `json:"working_hours"`
	DefaultReminders       []int           `json:"default_reminders"`
	VideoAvailable         bool            `json:"video_available"`
	SubscriptionsAvailable bool            `json:"subscriptions_available"`
}

func defaultWorkingHours() []HoursInterval {
	out := make([]HoursInterval, 0, 5)
	for d := 1; d <= 5; d++ {
		out = append(out, HoursInterval{Day: d, Start: 9 * 60, End: 17 * 60})
	}
	return out
}

// loadSettings returns the user's settings, defaults when none are saved.
func (s *Service) loadSettings(ctx context.Context, q db.Querier, userID uuid.UUID) (Settings, error) {
	st := Settings{Timezone: "UTC", WeekStart: 1, WorkingHours: defaultWorkingHours(), DefaultReminders: []int{10},
		VideoAvailable: VideoAvailable(), SubscriptionsAvailable: s.secretKey != nil}
	var tz *string
	var hours []byte
	var rem []int32
	err := q.QueryRow(ctx, `SELECT timezone, week_start, working_hours, default_reminders FROM calendar_settings WHERE user_id = $1`, userID).
		Scan(&tz, &st.WeekStart, &hours, &rem)
	if db.IsNoRows(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if tz != nil && validZone(*tz) {
		st.Timezone, st.TimezoneSet = *tz, true
	}
	var wh []HoursInterval
	if json.Unmarshal(hours, &wh) == nil && len(wh) > 0 {
		st.WorkingHours = wh
	}
	st.DefaultReminders = make([]int, len(rem))
	for i, r := range rem {
		st.DefaultReminders[i] = int(r)
	}
	return st, nil
}

// UserZone is the time zone a person reads times in: the one in their
// calendar settings (their device's, saved the first time they're signed
// in, or one they chose). ok is false while they have none. Other features
// use it through the app's stitching to word times for a person (a
// meeting invitation), so it never fails: a read error is "no zone".
func UserZone(ctx context.Context, q db.Querier, userID uuid.UUID) (loc *time.Location, ok bool) {
	var tz *string
	if err := q.QueryRow(ctx, `SELECT timezone FROM calendar_settings WHERE user_id = $1`, userID).Scan(&tz); err != nil || tz == nil || !validZone(*tz) {
		return nil, false
	}
	return zoneOr(*tz, time.UTC), true
}

// zoneFor is the zone to word times in for userID: their own, else
// fallback (the event's or booking link's zone, the organizer's). Wording
// always names the zone, so a fallback is still read correctly.
func (s *Service) zoneFor(ctx context.Context, q db.Querier, userID uuid.UUID, fallback *time.Location) *time.Location {
	if loc, ok := UserZone(ctx, q, userID); ok {
		return loc
	}
	if fallback == nil {
		return time.UTC
	}
	return fallback
}

// validateHours checks weekly windows: days 0–6, 0 ≤ start < end ≤ 1440,
// no overlaps on a day. Returned sorted.
func validateHours(in []HoursInterval) ([]HoursInterval, error) {
	if len(in) > 50 {
		return nil, httpx.BadRequest("Too many availability windows.")
	}
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b HoursInterval) int {
		if a.Day != b.Day {
			return a.Day - b.Day
		}
		return a.Start - b.Start
	})
	for i, h := range out {
		if h.Day < 0 || h.Day > 6 || h.Start < 0 || h.End > 1440 || h.Start >= h.End {
			return nil, httpx.BadRequest("Each window needs a day and a start before its end.")
		}
		if i > 0 && out[i-1].Day == h.Day && out[i-1].End > h.Start {
			return nil, httpx.BadRequest("Windows on the same day can't overlap.")
		}
	}
	return out, nil
}

func validateIntervals(in []TimeInterval) ([]TimeInterval, error) {
	out := slices.Clone(in)
	slices.SortFunc(out, func(a, b TimeInterval) int { return a.Start - b.Start })
	for i, h := range out {
		if h.Start < 0 || h.End > 1440 || h.Start >= h.End {
			return nil, httpx.BadRequest("Each window needs a start before its end.")
		}
		if i > 0 && out[i-1].End > h.Start {
			return nil, httpx.BadRequest("Windows on the same day can't overlap.")
		}
	}
	if out == nil {
		out = []TimeInterval{}
	}
	return out, nil
}

func validateReminders(in []int) ([]int, error) {
	if len(in) > maxReminders {
		return nil, httpx.BadRequest("Up to 5 reminders per event.")
	}
	out := []int{}
	for _, m := range in {
		if m < 0 || m > maxReminderMins {
			return nil, httpx.BadRequest("Reminders go out between 0 minutes and 4 weeks before.")
		}
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out, nil
}
