package calendar

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Location types for booking links.
const (
	LocationVideo    = "video"
	LocationPhone    = "phone"
	LocationInPerson = "in_person"
	LocationCustom   = "custom"
)

// Question is a custom question on the booking form.
type Question struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // short_text | long_text | phone | choice
	Required bool     `json:"required"`
	Options  []string `json:"options"`
}

// BookingLink is a link as its owner manages it.
type BookingLink struct {
	ID                  uuid.UUID       `json:"id"`
	OrgID               *uuid.UUID      `json:"org_id"`
	OrgName             *string         `json:"org_name"`
	Audience            string          `json:"audience"`
	Slug                string          `json:"slug"`
	URL                 string          `json:"url"`
	Title               string          `json:"title"`
	Description         string          `json:"description"`
	Durations           []int           `json:"durations"`
	LocationType        string          `json:"location_type"`
	LocationValue       string          `json:"location_value"`
	Timezone            string          `json:"timezone"`
	WeeklyHours         []HoursInterval `json:"weekly_hours"`
	DateOverrides       []DateOverride  `json:"date_overrides"`
	BufferBefore        int             `json:"buffer_before"`
	BufferAfter         int             `json:"buffer_after"`
	MinNotice           int             `json:"min_notice"`
	MaxDaysAhead        int             `json:"max_days_ahead"`
	MaxPerDay           *int            `json:"max_per_day"`
	SlotInterval        int             `json:"slot_interval"`
	Questions           []Question      `json:"questions"`
	ConfirmationMessage string          `json:"confirmation_message"`
	Color               string          `json:"color"`
	Active              bool            `json:"active"`
	AllowReschedule     bool            `json:"allow_reschedule"`
	AllowCancel         bool            `json:"allow_cancel"`
	ManageCutoff        int             `json:"manage_cutoff"`
	UpcomingCount       int             `json:"upcoming_count"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
	// AddTo is the connected calendar bookings are also written to (the
	// app's own event stays; the copy carries the meeting link), nil = only
	// the app's calendar.
	AddTo *LinkTarget `json:"add_to"`
	// ConferenceProvider is the meeting provider for video bookings
	// (ProviderApp default); ConferenceConnectionID its account.
	ConferenceProvider     string  `json:"conference_provider"`
	ConferenceConnectionID *string `json:"conference_connection_id"`

	ownerID uuid.UUID
}

// LinkTarget names one of the owner's connected calendars.
type LinkTarget struct {
	ConnectionID string `json:"connection_id"`
	CalendarID   string `json:"calendar_id"`
}

const linkCols = `l.id, l.owner_id, l.org_id, o.name, l.audience, l.slug, l.title, l.description, l.durations,
	l.location_type, l.location_value, l.timezone, l.weekly_hours, l.date_overrides, l.buffer_before, l.buffer_after,
	l.min_notice, l.max_days_ahead, l.max_per_day, l.slot_interval, l.questions, l.confirmation_message, l.color,
	l.active, l.allow_reschedule, l.allow_cancel, l.manage_cutoff,
	(SELECT count(*) FROM calendar_bookings b WHERE b.link_id = l.id AND b.status = 'confirmed' AND b.starts_at > now())::int,
	l.created_at, l.updated_at, l.write_connection_id, l.write_calendar_id, l.conference_provider, l.conference_connection_id`

const linkFrom = ` FROM calendar_booking_links l LEFT JOIN orgs o ON o.id = l.org_id `

func (s *Service) scanLink(row pgx.Row) (BookingLink, error) {
	var l BookingLink
	var durations []int32
	var hours, overrides, questions []byte
	var writeConn, writeCal *string
	err := row.Scan(&l.ID, &l.ownerID, &l.OrgID, &l.OrgName, &l.Audience, &l.Slug, &l.Title, &l.Description, &durations,
		&l.LocationType, &l.LocationValue, &l.Timezone, &hours, &overrides, &l.BufferBefore, &l.BufferAfter,
		&l.MinNotice, &l.MaxDaysAhead, &l.MaxPerDay, &l.SlotInterval, &questions, &l.ConfirmationMessage, &l.Color,
		&l.Active, &l.AllowReschedule, &l.AllowCancel, &l.ManageCutoff, &l.UpcomingCount, &l.CreatedAt, &l.UpdatedAt,
		&writeConn, &writeCal, &l.ConferenceProvider, &l.ConferenceConnectionID)
	if err != nil {
		return l, err
	}
	if writeConn != nil && writeCal != nil {
		l.AddTo = &LinkTarget{ConnectionID: *writeConn, CalendarID: *writeCal}
	}
	for _, d := range durations {
		l.Durations = append(l.Durations, int(d))
	}
	l.WeeklyHours, l.DateOverrides, l.Questions = []HoursInterval{}, []DateOverride{}, []Question{}
	_ = json.Unmarshal(hours, &l.WeeklyHours)
	_ = json.Unmarshal(overrides, &l.DateOverrides)
	_ = json.Unmarshal(questions, &l.Questions)
	l.URL = s.webURL("/book/" + l.Slug)
	return l, nil
}

// linkInput is the create/update body; on update only fields sent change.
type linkInput struct {
	Title               *string          `json:"title"`
	Description         *string          `json:"description"`
	Slug                *string          `json:"slug"`
	Audience            *string          `json:"audience"`
	OrgID               json.RawMessage  `json:"org_id"`
	Durations           *[]int           `json:"durations"`
	LocationType        *string          `json:"location_type"`
	LocationValue       *string          `json:"location_value"`
	Timezone            *string          `json:"timezone"`
	WeeklyHours         *[]HoursInterval `json:"weekly_hours"`
	DateOverrides       *[]DateOverride  `json:"date_overrides"`
	BufferBefore        *int             `json:"buffer_before"`
	BufferAfter         *int             `json:"buffer_after"`
	MinNotice           *int             `json:"min_notice"`
	MaxDaysAhead        *int             `json:"max_days_ahead"`
	MaxPerDay           json.RawMessage  `json:"max_per_day"`
	SlotInterval        *int             `json:"slot_interval"`
	Questions           *[]Question      `json:"questions"`
	ConfirmationMessage *string          `json:"confirmation_message"`
	Color               *string          `json:"color"`
	Active              *bool            `json:"active"`
	AllowReschedule     *bool            `json:"allow_reschedule"`
	AllowCancel         *bool            `json:"allow_cancel"`
	ManageCutoff        *int             `json:"manage_cutoff"`
	// AddTo is {connection_id, calendar_id} or null (the app's calendar only).
	AddTo                  json.RawMessage `json:"add_to"`
	ConferenceProvider     *string         `json:"conference_provider"`
	ConferenceConnectionID json.RawMessage `json:"conference_connection_id"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var reservedSlugs = []string{"manage", "rsvp", "new", "edit", "admin", "api", "settings", "links", "bookings"}

// apply merges in into l and validates the result.
func (s *Service) applyLinkInput(ctx context.Context, callerID uuid.UUID, l *BookingLink, in linkInput) error {
	if in.Title != nil {
		l.Title, _ = cleanText(*in.Title, 1000)
	}
	if in.Description != nil {
		l.Description, _ = cleanText(*in.Description, 100000)
	}
	if in.Slug != nil {
		l.Slug = strings.ToLower(strings.TrimSpace(*in.Slug))
	}
	if in.Audience != nil {
		l.Audience = *in.Audience
	}
	if len(in.OrgID) > 0 {
		l.OrgID = nil
		if string(in.OrgID) != "null" {
			var id uuid.UUID
			if err := json.Unmarshal(in.OrgID, &id); err != nil {
				return httpx.BadRequest("org_id must be an id or null.")
			}
			l.OrgID = &id
		}
	}
	if in.Durations != nil {
		l.Durations = slices.Clone(*in.Durations)
	}
	if in.LocationType != nil {
		l.LocationType = *in.LocationType
	}
	if in.LocationValue != nil {
		l.LocationValue, _ = cleanText(*in.LocationValue, 10000)
	}
	if in.Timezone != nil {
		l.Timezone = *in.Timezone
	}
	if in.WeeklyHours != nil {
		l.WeeklyHours = *in.WeeklyHours
	}
	if in.DateOverrides != nil {
		l.DateOverrides = *in.DateOverrides
	}
	setInt := func(dst *int, src *int) {
		if src != nil {
			*dst = *src
		}
	}
	setInt(&l.BufferBefore, in.BufferBefore)
	setInt(&l.BufferAfter, in.BufferAfter)
	setInt(&l.MinNotice, in.MinNotice)
	setInt(&l.MaxDaysAhead, in.MaxDaysAhead)
	setInt(&l.SlotInterval, in.SlotInterval)
	setInt(&l.ManageCutoff, in.ManageCutoff)
	if len(in.MaxPerDay) > 0 {
		l.MaxPerDay = nil
		if string(in.MaxPerDay) != "null" {
			var n int
			if err := json.Unmarshal(in.MaxPerDay, &n); err != nil {
				return httpx.BadRequest("max_per_day must be a number or null.")
			}
			l.MaxPerDay = &n
		}
	}
	if in.Questions != nil {
		l.Questions = *in.Questions
	}
	if in.ConfirmationMessage != nil {
		l.ConfirmationMessage, _ = cleanText(*in.ConfirmationMessage, 100000)
	}
	if in.Color != nil {
		l.Color = *in.Color
	}
	if in.Active != nil {
		l.Active = *in.Active
	}
	if in.AllowReschedule != nil {
		l.AllowReschedule = *in.AllowReschedule
	}
	if in.AllowCancel != nil {
		l.AllowCancel = *in.AllowCancel
	}
	if len(in.AddTo) > 0 {
		l.AddTo = nil
		if string(in.AddTo) != "null" {
			var t LinkTarget
			if err := json.Unmarshal(in.AddTo, &t); err != nil || t.ConnectionID == "" || t.CalendarID == "" || len(t.ConnectionID) > 200 || len(t.CalendarID) > 1024 {
				return httpx.BadRequest("add_to must be {connection_id, calendar_id} or null.")
			}
			l.AddTo = &t
		}
	}
	if in.ConferenceProvider != nil {
		l.ConferenceProvider = trimProvider(*in.ConferenceProvider)
	}
	if len(in.ConferenceConnectionID) > 0 {
		l.ConferenceConnectionID = nil
		if string(in.ConferenceConnectionID) != "null" {
			var v string
			if err := json.Unmarshal(in.ConferenceConnectionID, &v); err != nil || len(v) > 200 {
				return httpx.BadRequest("conference_connection_id must be a connection id or null.")
			}
			if v != "" {
				l.ConferenceConnectionID = &v
			}
		}
	}
	return s.validateLink(ctx, callerID, l)
}

// validateLinkTarget checks a link's connected calendar and meeting provider
// against the owner's connections (live; on save, not on every preview).
func (s *Service) validateLinkTarget(ctx context.Context, callerID uuid.UUID, l *BookingLink) error {
	if l.AddTo == nil && (l.LocationType != LocationVideo || l.ConferenceProvider == ProviderApp) {
		return nil
	}
	org := uuid.Nil
	if l.OrgID != nil {
		org = *l.OrgID
	}
	connID, calID := "", ""
	if l.AddTo != nil {
		connID, calID = l.AddTo.ConnectionID, l.AddTo.CalendarID
	}
	opts, _, err := s.conferenceOptionsFor(ctx, callerID, org, connID, calID)
	if err != nil {
		return err
	}
	if l.LocationType != LocationVideo {
		return nil
	}
	for _, o := range opts {
		if o.ID == l.ConferenceProvider && (l.ConferenceConnectionID == nil || o.ConnectionID == "" || o.ConnectionID == *l.ConferenceConnectionID) {
			if o.ConnectionID != "" && !o.Native {
				id := o.ConnectionID
				l.ConferenceConnectionID = &id
			}
			if o.Native {
				l.ConferenceConnectionID = nil
			}
			return nil
		}
	}
	return httpx.NewError(http.StatusConflict, "video_unavailable", "That meeting provider isn't available for this link's calendar.")
}

func (s *Service) validateLink(ctx context.Context, callerID uuid.UUID, l *BookingLink) error {
	switch n := len([]rune(l.Title)); {
	case n == 0:
		return httpx.BadRequest("Add a title.")
	case n > maxTitle:
		return httpx.BadRequest("Titles are up to 200 characters.")
	}
	if len([]rune(l.Description)) > 2000 {
		return httpx.BadRequest("Descriptions are up to 2000 characters.")
	}
	if len(l.Slug) < 3 || len(l.Slug) > 60 || !slugRe.MatchString(l.Slug) || slices.Contains(reservedSlugs, l.Slug) {
		return httpx.BadRequest("The link name needs 3 to 60 lowercase letters, numbers and dashes.")
	}
	switch l.Audience {
	case "internal":
		if l.OrgID == nil {
			return httpx.BadRequest("Pick the " + orgs.Noun() + " whose members can book.")
		}
	case "external":
	default:
		return httpx.BadRequest("audience must be internal or external.")
	}
	if l.OrgID != nil {
		var member bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, *l.OrgID, callerID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return httpx.BadRequest("You can only use " + orgs.Nouns() + " you belong to.")
		}
	}
	if len(l.Durations) == 0 || len(l.Durations) > 6 {
		return httpx.BadRequest("Offer 1 to 6 meeting lengths.")
	}
	slices.Sort(l.Durations)
	l.Durations = slices.Compact(l.Durations)
	for _, d := range l.Durations {
		if d < 5 || d > 480 {
			return httpx.BadRequest("Meeting lengths are 5 to 480 minutes.")
		}
	}
	if l.ConferenceProvider == "" {
		l.ConferenceProvider = ProviderApp
	}
	switch l.ConferenceProvider {
	case ProviderApp, ProviderZoom, ProviderTeams, ProviderGoogleMeet:
	default:
		return httpx.BadRequest("conference_provider must be app, google_meet, teams or zoom.")
	}
	switch l.LocationType {
	case LocationVideo:
		native := l.AddTo != nil && (l.ConferenceProvider == ProviderGoogleMeet || l.ConferenceProvider == ProviderTeams)
		if l.ConferenceProvider == ProviderGoogleMeet && l.AddTo == nil {
			return httpx.BadRequest("Google Meet links come from Google Calendar: add bookings to a Google calendar first.")
		}
		if !native && !VideoAvailable() {
			return httpx.NewError(http.StatusConflict, "video_unavailable", "Video calls aren't set up in this app.")
		}
		l.LocationValue = ""
	case LocationPhone:
	case LocationInPerson, LocationCustom:
		if l.LocationValue == "" {
			return httpx.BadRequest("Add the address or meeting details.")
		}
	default:
		return httpx.BadRequest("location_type must be video, phone, in_person or custom.")
	}
	if len([]rune(l.LocationValue)) > maxLocation {
		return httpx.BadRequest("Locations are up to 500 characters.")
	}
	if !validZone(l.Timezone) {
		return httpx.BadRequest("Pick a valid time zone.")
	}
	hours, err := validateHours(l.WeeklyHours)
	if err != nil {
		return err
	}
	l.WeeklyHours = hours
	if len(l.DateOverrides) > 200 {
		return httpx.BadRequest("Up to 200 date overrides.")
	}
	seen := map[string]bool{}
	for i, o := range l.DateOverrides {
		if _, err := time.Parse(time.DateOnly, o.Date); err != nil || seen[o.Date] {
			return httpx.BadRequest("Each override needs a different date like 2026-11-27.")
		}
		seen[o.Date] = true
		iv, err := validateIntervals(o.Intervals)
		if err != nil {
			return err
		}
		l.DateOverrides[i].Intervals = iv
	}
	slices.SortFunc(l.DateOverrides, func(a, b DateOverride) int { return strings.Compare(a.Date, b.Date) })
	switch {
	case l.BufferBefore < 0 || l.BufferBefore > 240 || l.BufferAfter < 0 || l.BufferAfter > 240:
		return httpx.BadRequest("Buffers are 0 to 240 minutes.")
	case l.MinNotice < 0 || l.MinNotice > 30*24*60:
		return httpx.BadRequest("Minimum notice is up to 30 days.")
	case l.MaxDaysAhead < 1 || l.MaxDaysAhead > 365:
		return httpx.BadRequest("Bookings open 1 to 365 days ahead.")
	case l.MaxPerDay != nil && (*l.MaxPerDay < 1 || *l.MaxPerDay > 50):
		return httpx.BadRequest("The daily limit is 1 to 50, or none.")
	case l.SlotInterval < 5 || l.SlotInterval > 240:
		return httpx.BadRequest("Start times are 5 to 240 minutes apart.")
	case l.ManageCutoff < 0 || l.ManageCutoff > 7*24*60:
		return httpx.BadRequest("The change cutoff is up to 7 days before the start.")
	case len([]rune(l.ConfirmationMessage)) > 1000:
		return httpx.BadRequest("The confirmation message is up to 1000 characters.")
	case !validColor(l.Color):
		return httpx.BadRequest("Pick one of the calendar colors.")
	}
	if len(l.Questions) > 10 {
		return httpx.BadRequest("Up to 10 questions.")
	}
	ids := map[string]bool{}
	for i := range l.Questions {
		q := &l.Questions[i]
		q.Label, _ = cleanText(q.Label, 1000)
		if q.ID == "" || len(q.ID) > 40 || ids[q.ID] {
			q.ID = randomToken(6)
		}
		ids[q.ID] = true
		if q.Label == "" || len([]rune(q.Label)) > 200 {
			return httpx.BadRequest("Each question needs a label up to 200 characters.")
		}
		switch q.Type {
		case "short_text", "long_text", "phone":
			q.Options = []string{}
		case "choice":
			if len(q.Options) < 2 || len(q.Options) > 20 {
				return httpx.BadRequest("Choice questions have 2 to 20 options.")
			}
			for j, o := range q.Options {
				o, _ = cleanText(o, 1000)
				if o == "" || len([]rune(o)) > 100 {
					return httpx.BadRequest("Options are 1 to 100 characters.")
				}
				q.Options[j] = o
			}
		default:
			return httpx.BadRequest("Question types are short_text, long_text, phone or choice.")
		}
	}
	return nil
}

// slugify makes a link name from text: "Sam's 30 min chat" → "sams-30-min-chat".
func slugify(v string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(v) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r == '\'' || r == '’':
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
	}
	return out
}

func (s *Service) loadOwnLink(ctx context.Context, q db.Querier, ownerID, id uuid.UUID) (BookingLink, error) {
	l, err := s.scanLink(q.QueryRow(ctx, `SELECT `+linkCols+linkFrom+`WHERE l.id = $1 AND l.owner_id = $2 AND l.deleted_at IS NULL`, id, ownerID))
	if db.IsNoRows(err) {
		return l, httpx.NotFound("That booking link doesn't exist.")
	}
	return l, err
}

func (s *Service) listLinks(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var c struct {
		T  time.Time `json:"t"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var cursorT *time.Time
	if !c.T.IsZero() {
		cursorT = &c.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+linkCols+linkFrom+`
		WHERE l.owner_id = $1 AND l.deleted_at IS NULL
		  AND ($2::timestamptz IS NULL OR (l.created_at, l.id) < ($2, $3::uuid))
		ORDER BY l.created_at DESC, l.id DESC LIMIT $4`, id.UserID, cursorT, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []BookingLink{}
	for rows.Next() {
		l, err := s.scanLink(rows)
		if err != nil {
			return err
		}
		list = append(list, l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(l BookingLink) any {
		return map[string]any{"t": l.CreatedAt, "id": l.ID}
	}))
	return nil
}

func (s *Service) getLink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	lid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	l, err := s.loadOwnLink(r.Context(), s.d.Pool, id.UserID, lid)
	if err != nil {
		return err
	}
	httpx.OK(w, l)
	return nil
}

// newLinkDefaults is a fresh link: 30 minutes, weekdays 9–5 (or the owner's
// working hours) in the owner's zone.
func (s *Service) newLinkDefaults(ctx context.Context, userID uuid.UUID) (BookingLink, error) {
	st, err := s.loadSettings(ctx, s.d.Pool, userID)
	if err != nil {
		return BookingLink{}, err
	}
	return BookingLink{
		Audience: "external", Durations: []int{30}, LocationType: LocationPhone, Timezone: st.Timezone,
		WeeklyHours: st.WorkingHours, DateOverrides: []DateOverride{}, MinNotice: 240, MaxDaysAhead: 60,
		SlotInterval: 30, Questions: []Question{}, Color: "primary", Active: true, AllowReschedule: true,
		AllowCancel: true, ManageCutoff: 60, ConferenceProvider: ProviderApp, ownerID: userID,
	}, nil
}

func (s *Service) createLink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var in linkInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var count int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM calendar_booking_links WHERE owner_id = $1 AND deleted_at IS NULL`, id.UserID).Scan(&count); err != nil {
		return err
	}
	if count >= maxLinks {
		return httpx.Conflict("You have 50 booking links. Delete one to add another.")
	}
	l, err := s.newLinkDefaults(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	autoSlug := in.Slug == nil || strings.TrimSpace(*in.Slug) == ""
	if autoSlug {
		title := ""
		if in.Title != nil {
			title = *in.Title
		}
		base := slugify(strings.Split(id.DisplayName, " ")[0] + " " + title)
		if len(base) < 3 {
			base = "meet-" + strings.ToLower(randomToken(3))
		}
		in.Slug = &base
	}
	if err := s.applyLinkInput(r.Context(), id.UserID, &l, in); err != nil {
		return err
	}
	if err := s.validateLinkTarget(r.Context(), id.UserID, &l); err != nil {
		return err
	}
	var newID uuid.UUID
	for attempt := 0; ; attempt++ {
		err := s.insertLink(r.Context(), id.UserID, &l, &newID)
		if err == nil {
			break
		}
		if !db.IsUniqueViolation(err, "calendar_booking_links_slug_key") {
			return err
		}
		if !autoSlug || attempt >= 4 {
			return httpx.Conflict("That link name is taken. Try another.")
		}
		l.Slug = strings.Trim(l.Slug[:min(len(l.Slug), 50)], "-") + "-" + strings.ToLower(strings.NewReplacer("-", "", "_", "").Replace(randomToken(4)))[:4]
	}
	out, err := s.loadOwnLink(r.Context(), s.d.Pool, id.UserID, newID)
	if err != nil {
		return err
	}
	s.changed(r.Context(), "links", id.UserID)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

func (s *Service) insertLink(ctx context.Context, ownerID uuid.UUID, l *BookingLink, newID *uuid.UUID) error {
	hours, _ := json.Marshal(l.WeeklyHours)
	overrides, _ := json.Marshal(l.DateOverrides)
	questions, _ := json.Marshal(l.Questions)
	return s.d.Pool.QueryRow(ctx, `
		INSERT INTO calendar_booking_links (owner_id, org_id, audience, slug, title, description, durations, location_type,
		  location_value, timezone, weekly_hours, date_overrides, buffer_before, buffer_after, min_notice, max_days_ahead,
		  max_per_day, slot_interval, questions, confirmation_message, color, active, allow_reschedule, allow_cancel, manage_cutoff,
		  write_connection_id, write_calendar_id, conference_provider, conference_connection_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25,
		  $26, $27, $28, $29)
		RETURNING id`,
		ownerID, l.OrgID, l.Audience, l.Slug, l.Title, l.Description, toInt32(l.Durations), l.LocationType,
		l.LocationValue, l.Timezone, hours, overrides, l.BufferBefore, l.BufferAfter, l.MinNotice, l.MaxDaysAhead,
		l.MaxPerDay, l.SlotInterval, questions, l.ConfirmationMessage, l.Color, l.Active, l.AllowReschedule, l.AllowCancel,
		l.ManageCutoff, l.addToConn(), l.addToCal(), l.ConferenceProvider, l.ConferenceConnectionID).Scan(newID)
}

func (l *BookingLink) addToConn() *string {
	if l.AddTo == nil {
		return nil
	}
	return &l.AddTo.ConnectionID
}

func (l *BookingLink) addToCal() *string {
	if l.AddTo == nil {
		return nil
	}
	return &l.AddTo.CalendarID
}

func (s *Service) updateLink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	lid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in linkInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	l, err := s.loadOwnLink(r.Context(), s.d.Pool, id.UserID, lid)
	if err != nil {
		return err
	}
	if err := s.applyLinkInput(r.Context(), id.UserID, &l, in); err != nil {
		return err
	}
	if len(in.AddTo) > 0 || in.ConferenceProvider != nil || len(in.ConferenceConnectionID) > 0 || in.LocationType != nil {
		if err := s.validateLinkTarget(r.Context(), id.UserID, &l); err != nil {
			return err
		}
	}
	hours, _ := json.Marshal(l.WeeklyHours)
	overrides, _ := json.Marshal(l.DateOverrides)
	questions, _ := json.Marshal(l.Questions)
	_, err = s.d.Pool.Exec(r.Context(), `
		UPDATE calendar_booking_links SET org_id = $3, audience = $4, slug = $5, title = $6, description = $7, durations = $8,
		  location_type = $9, location_value = $10, timezone = $11, weekly_hours = $12, date_overrides = $13,
		  buffer_before = $14, buffer_after = $15, min_notice = $16, max_days_ahead = $17, max_per_day = $18,
		  slot_interval = $19, questions = $20, confirmation_message = $21, color = $22, active = $23,
		  allow_reschedule = $24, allow_cancel = $25, manage_cutoff = $26, write_connection_id = $27, write_calendar_id = $28,
		  conference_provider = $29, conference_connection_id = $30, updated_at = now()
		WHERE id = $1 AND owner_id = $2`,
		l.ID, id.UserID, l.OrgID, l.Audience, l.Slug, l.Title, l.Description, toInt32(l.Durations), l.LocationType,
		l.LocationValue, l.Timezone, hours, overrides, l.BufferBefore, l.BufferAfter, l.MinNotice, l.MaxDaysAhead,
		l.MaxPerDay, l.SlotInterval, questions, l.ConfirmationMessage, l.Color, l.Active, l.AllowReschedule,
		l.AllowCancel, l.ManageCutoff, l.addToConn(), l.addToCal(), l.ConferenceProvider, l.ConferenceConnectionID)
	if db.IsUniqueViolation(err, "calendar_booking_links_slug_key") {
		return httpx.Conflict("That link name is taken. Try another.")
	}
	if err != nil {
		return err
	}
	out, err := s.loadOwnLink(r.Context(), s.d.Pool, id.UserID, lid)
	if err != nil {
		return err
	}
	s.changed(r.Context(), "links", id.UserID)
	httpx.OK(w, out)
	return nil
}

// deleteLink retires a link: its page stops working and the name frees up
// after a rename; bookings already made stay (cancel them separately).
func (s *Service) deleteLink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	lid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `
		UPDATE calendar_booking_links SET deleted_at = now(), active = false, slug = slug || '~' || substr(md5(id::text), 1, 8), updated_at = now()
		WHERE id = $1 AND owner_id = $2 AND deleted_at IS NULL`, lid, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That booking link doesn't exist.")
	}
	s.changed(r.Context(), "links", id.UserID)
	httpx.NoContent(w)
	return nil
}

// previewLink computes the slots a draft link would offer this month, from
// the owner's real calendar, for the editor's live preview.
func (s *Service) previewLink(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	var in struct {
		Link     linkInput  `json:"link"`
		LinkID   *uuid.UUID `json:"link_id"`
		From     time.Time  `json:"from"`
		To       time.Time  `json:"to"`
		Duration int        `json:"duration"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	var l BookingLink
	var err error
	if in.LinkID != nil {
		l, err = s.loadOwnLink(r.Context(), s.d.Pool, id.UserID, *in.LinkID)
	} else {
		l, err = s.newLinkDefaults(r.Context(), id.UserID)
	}
	if err != nil {
		return err
	}
	// A draft without a name or title still previews: neither changes the slots.
	if in.Link.Slug == nil && l.Slug == "" {
		placeholder := "preview"
		in.Link.Slug = &placeholder
	}
	if (in.Link.Title == nil && l.Title == "") || (in.Link.Title != nil && strings.TrimSpace(*in.Link.Title) == "") {
		placeholder := "Preview"
		in.Link.Title = &placeholder
	}
	l.ownerID = id.UserID
	if err := s.applyLinkInput(r.Context(), id.UserID, &l, in.Link); err != nil {
		return err
	}
	if !in.To.After(in.From) || in.To.Sub(in.From) > 43*24*time.Hour {
		return httpx.BadRequest("from and to must be at most 42 days apart.")
	}
	dur := in.Duration
	if !slices.Contains(l.Durations, dur) {
		dur = l.Durations[0]
	}
	slots, err := s.linkSlots(r.Context(), s.d.Pool, l, in.From.UTC(), in.To.UTC(), dur, uuid.Nil)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"slots": slots, "duration": dur, "timezone": l.Timezone})
	return nil
}

// linkSlots lists bookable starts for a link in [from, to).
func (s *Service) linkSlots(ctx context.Context, q db.Querier, l BookingLink, from, to time.Time, duration int, excludeBooking uuid.UUID) ([]Slot, error) {
	loc := zoneOr(l.Timezone, time.UTC)
	now := time.Now().UTC()
	earliest := now.Add(time.Duration(l.MinNotice) * time.Minute)
	horizon := now.In(loc)
	latest := time.Date(horizon.Year(), horizon.Month(), horizon.Day()+l.MaxDaysAhead+1, 0, 0, 0, 0, loc).UTC()
	lo, hi := from, to
	if earliest.After(lo) {
		lo = earliest
	}
	if latest.Before(hi) {
		hi = latest
	}
	if !hi.After(lo) {
		return []Slot{}, nil
	}
	pad := time.Duration(max(l.BufferBefore, l.BufferAfter)) * time.Minute
	busy, err := s.busyFor(ctx, q, l.ownerID, lo.Add(-pad-time.Duration(duration)*time.Minute), hi.Add(pad+time.Duration(duration)*time.Minute), excludeBooking)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	if l.MaxPerDay != nil && l.ID != uuid.Nil {
		rows, err := q.Query(ctx, `
			SELECT to_char(starts_at AT TIME ZONE $2, 'YYYY-MM-DD'), count(*) FROM calendar_bookings
			WHERE link_id = $1 AND status = 'confirmed' AND id <> $5 AND starts_at >= $3 AND starts_at < $4 GROUP BY 1`,
			l.ID, l.Timezone, lo.Add(-48*time.Hour), hi.Add(48*time.Hour), excludeBooking)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var d string
			var n int
			if err := rows.Scan(&d, &n); err != nil {
				rows.Close()
				return nil, err
			}
			counts[d] = n
		}
		rows.Close()
	}
	overrides := map[string][]TimeInterval{}
	for _, o := range l.DateOverrides {
		overrides[o.Date] = o.Intervals
	}
	rules := slotRules{
		loc: loc, weekly: l.WeeklyHours, overrides: overrides,
		duration: time.Duration(duration) * time.Minute, interval: time.Duration(l.SlotInterval) * time.Minute,
		before: time.Duration(l.BufferBefore) * time.Minute, after: time.Duration(l.BufferAfter) * time.Minute,
		earliest: earliest, latest: latest, dayCounts: counts,
	}
	if l.MaxPerDay != nil {
		rules.maxPerDay = *l.MaxPerDay
	}
	return generateSlots(rules, lo, hi, busy, 2000), nil
}
