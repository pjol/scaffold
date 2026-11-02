package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/legal"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

const (
	bookingsPerEmailPerDay  = 5 // across all links
	upcomingPerEmailPerLink = 3
)

var (
	errLinkNotFound = httpx.NotFound("This booking link doesn't exist.")
	errLinkPaused   = httpx.NewError(http.StatusGone, "link_paused", "This booking link is turned off.")
	errSlotTaken    = httpx.NewError(http.StatusConflict, "slot_taken", "That time is no longer available. Pick another.")
)

// ---- public link view -------------------------------------------------------

// PublicLink is what a booking page shows: never events, only the link.
type PublicLink struct {
	// Access is "open", or "members_only" for an internal link the visitor
	// can't book: then only Slug, OrgName and SignedIn are filled.
	Access        string     `json:"access"`
	SignedIn      bool       `json:"signed_in"`
	Slug          string     `json:"slug"`
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Durations     []int      `json:"durations"`
	LocationType  string     `json:"location_type"`
	LocationLabel string     `json:"location_label"`
	NeedsPhone    bool       `json:"needs_phone"`
	Timezone      string     `json:"timezone"`
	Audience      string     `json:"audience"`
	OrgName       *string    `json:"org_name"`
	Host          Person     `json:"host"`
	Questions     []Question `json:"questions"`
	MaxDaysAhead  int        `json:"max_days_ahead"`
	Color         string     `json:"color"`
	Booker        *Booker    `json:"booker"`
}

// Booker is the signed-in visitor (prefilled, not editable).
type Booker struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func locationLabel(l BookingLink) string {
	switch l.LocationType {
	case LocationVideo:
		switch l.ConferenceProvider {
		case ProviderGoogleMeet:
			return "Google Meet"
		case ProviderTeams:
			return "Microsoft Teams"
		case ProviderZoom:
			return "Zoom"
		}
		return "Video call"
	case LocationPhone:
		if l.LocationValue != "" {
			return "Phone call: you call " + l.LocationValue
		}
		return "Phone call: you'll be called"
	case LocationInPerson:
		return "In person"
	}
	return l.LocationValue
}

// linkForVisitor loads an active link and applies its audience: internal
// links answer 403 members_only (with the org's name and nothing else) to
// signed-out visitors and non-members.
func (s *Service) linkForVisitor(ctx context.Context, q db.Querier, slug string, viewer *auth.Identity) (BookingLink, Person, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" || len(slug) > 60 {
		return BookingLink{}, Person{}, errLinkNotFound
	}
	l, err := s.scanLink(q.QueryRow(ctx, `SELECT `+linkCols+linkFrom+`WHERE l.slug = $1 AND l.deleted_at IS NULL`, slug))
	if db.IsNoRows(err) {
		return l, Person{}, errLinkNotFound
	}
	if err != nil {
		return l, Person{}, err
	}
	var host Person
	var status string
	if err := q.QueryRow(ctx, `SELECT id, display_name, avatar_url, status FROM users WHERE id = $1`, l.ownerID).
		Scan(&host.ID, &host.DisplayName, &host.AvatarURL, &status); err != nil {
		return l, host, err
	}
	if status != "active" {
		return l, host, errLinkNotFound
	}
	if !l.Active {
		return l, host, errLinkPaused
	}
	if l.Audience == "internal" {
		var hostMember bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, l.OrgID, l.ownerID).Scan(&hostMember); err != nil {
			return l, host, err
		}
		if !hostMember || l.OrgName == nil {
			return l, host, errLinkPaused
		}
		member := false
		if viewer != nil {
			if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, l.OrgID, viewer.UserID).Scan(&member); err != nil {
				return l, host, err
			}
		}
		if !member {
			return l, Person{}, &httpx.Error{Status: http.StatusForbidden, Code: "members_only",
				Message: "Only members of " + *l.OrgName + " can book this link.",
				Details: map[string]any{"org_name": *l.OrgName, "signed_in": viewer != nil}}
		}
	}
	return l, host, nil
}

func (s *Service) publicLink(w http.ResponseWriter, r *http.Request) error {
	viewer := auth.FromContext(r.Context())
	l, host, err := s.linkForVisitor(r.Context(), s.d.Pool, chi.URLParam(r, "slug"), viewer)
	var he *httpx.Error
	if errors.As(err, &he) && he.Code == "members_only" {
		// The page needs to say who may book; nothing else about the link or host.
		w.Header().Set("Cache-Control", "private, no-store")
		httpx.OK(w, PublicLink{Access: "members_only", SignedIn: viewer != nil, Slug: l.Slug, OrgName: l.OrgName,
			Durations: []int{}, Questions: []Question{}})
		return nil
	}
	if err != nil {
		return err
	}
	out := PublicLink{
		Access: "open", SignedIn: viewer != nil,
		Slug: l.Slug, Title: l.Title, Description: l.Description, Durations: l.Durations, LocationType: l.LocationType,
		LocationLabel: locationLabel(l), NeedsPhone: l.LocationType == LocationPhone && l.LocationValue == "",
		Timezone: l.Timezone, Audience: l.Audience, Host: host, Questions: l.Questions, MaxDaysAhead: l.MaxDaysAhead, Color: l.Color,
	}
	if l.Audience == "internal" {
		out.OrgName = l.OrgName
	}
	if viewer != nil {
		out.Booker = &Booker{Name: viewer.DisplayName, Email: viewer.Email}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.OK(w, out)
	return nil
}

func parseSlotWindow(r *http.Request) (time.Time, time.Time, error) {
	return parseWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"), 43*24*time.Hour)
}

func (s *Service) publicSlots(w http.ResponseWriter, r *http.Request) error {
	viewer := auth.FromContext(r.Context())
	l, _, err := s.linkForVisitor(r.Context(), s.d.Pool, chi.URLParam(r, "slug"), viewer)
	if err != nil {
		return err
	}
	from, to, err := parseSlotWindow(r)
	if err != nil {
		return err
	}
	dur := l.Durations[0]
	if v := r.URL.Query().Get("duration"); v != "" {
		var n int
		if _, err := fmt.Sscan(v, &n); err != nil || !slices.Contains(l.Durations, n) {
			return httpx.BadRequest("Pick one of the offered meeting lengths.")
		}
		dur = n
	}
	slots, err := s.linkSlots(r.Context(), s.d.Pool, l, from, to, dur, uuid.Nil)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.OK(w, map[string]any{"slots": slots, "duration": dur})
	return nil
}

// slotAvailable checks one start against the link's rules and the host's
// calendar right now.
func (s *Service) slotAvailable(ctx context.Context, q db.Querier, l BookingLink, start time.Time, dur int, exclude uuid.UUID) (bool, error) {
	slots, err := s.linkSlots(ctx, q, l, start, start.Add(time.Minute), dur, exclude)
	if err != nil {
		return false, err
	}
	for _, sl := range slots {
		if sl.StartsAt.Equal(start) {
			return true, nil
		}
	}
	return false, nil
}

// ---- booking ----------------------------------------------------------------

type answerInput struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// Answer is a booker's reply to a custom question.
type Answer struct {
	QuestionID string `json:"question_id"`
	Label      string `json:"label"`
	Value      string `json:"value"`
}

type bookInput struct {
	StartsAt time.Time     `json:"starts_at"`
	Duration int           `json:"duration"`
	Name     string        `json:"name"`
	Email    string        `json:"email"`
	Phone    string        `json:"phone"`
	Timezone string        `json:"timezone"`
	Answers  []answerInput `json:"answers"`
}

// BookingConfirmation answers a successful booking. ManageURL is the
// booker's secret link to reschedule or cancel.
type BookingConfirmation struct {
	Booking             PublicBooking `json:"booking"`
	ManageURL           string        `json:"manage_url"`
	ConfirmationMessage string        `json:"confirmation_message"`
}

func validateAnswers(qs []Question, in []answerInput) ([]Answer, error) {
	byID := map[string]string{}
	for _, a := range in {
		v, ok := cleanText(a.Value, 2000)
		if !ok {
			return nil, httpx.BadRequest("Answers are up to 2000 characters.")
		}
		byID[a.QuestionID] = v
	}
	out := []Answer{}
	for _, q := range qs {
		v := byID[q.ID]
		if v == "" {
			if q.Required {
				return nil, httpx.BadRequest("Answer: " + q.Label)
			}
			continue
		}
		if q.Type == "choice" && !slices.Contains(q.Options, v) {
			return nil, httpx.BadRequest("Pick one of the options for: " + q.Label)
		}
		out = append(out, Answer{QuestionID: q.ID, Label: q.Label, Value: v})
	}
	return out, nil
}

func (s *Service) bookHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	viewer := auth.FromContext(ctx)
	l, host, err := s.linkForVisitor(ctx, s.d.Pool, chi.URLParam(r, "slug"), viewer)
	if err != nil {
		return err
	}
	if viewer != nil {
		if !viewer.Active() {
			return httpx.ErrSuspended
		}
		if !legal.Current(viewer.TermsVersion, viewer.PrivacyVersion) {
			return legal.ErrConsentRequired()
		}
		if viewer.UserID == l.ownerID {
			return httpx.BadRequest("This is your own booking link.")
		}
	}
	var in bookInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if !slices.Contains(l.Durations, in.Duration) {
		return httpx.BadRequest("Pick one of the offered meeting lengths.")
	}
	if in.Timezone == "" || !validZone(in.Timezone) {
		in.Timezone = l.Timezone
	}
	var name, email string
	var bookerID *uuid.UUID
	if viewer != nil {
		name, email = viewer.DisplayName, viewer.Email
		uid := viewer.UserID
		bookerID = &uid
		if name == "" {
			name, _ = cleanText(in.Name, 100)
		}
	} else {
		var ok bool
		if name, ok = cleanText(in.Name, 100); name == "" || !ok {
			return httpx.BadRequest("Enter your name (up to 100 characters).")
		}
		if email = normalizeEmail(in.Email); email == "" {
			return httpx.BadRequest("Enter a valid email address.")
		}
	}
	phone, _ := cleanText(in.Phone, 40)
	if l.LocationType == LocationPhone && l.LocationValue == "" && phone == "" {
		return httpx.BadRequest("Enter the phone number to call.")
	}
	answers, err := validateAnswers(l.Questions, in.Answers)
	if err != nil {
		return err
	}
	var today, upcoming int
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE created_at > now() - interval '24 hours'),
		       count(*) FILTER (WHERE link_id = $2 AND status = 'confirmed' AND starts_at > now())
		FROM calendar_bookings WHERE lower(booker_email) = $1`, email, l.ID).Scan(&today, &upcoming); err != nil {
		return err
	}
	if today >= bookingsPerEmailPerDay {
		return httpx.NewError(http.StatusTooManyRequests, "rate_limited", "You've made several bookings today. Try again tomorrow.")
	}
	if upcoming >= upcomingPerEmailPerLink {
		return httpx.Conflict("You already have 3 upcoming bookings on this link.")
	}
	start := in.StartsAt.UTC()
	dur := time.Duration(in.Duration) * time.Minute
	if ok, err := s.slotAvailable(ctx, s.d.Pool, l, start, in.Duration, uuid.Nil); err != nil {
		return err
	} else if !ok {
		return errSlotTaken
	}

	b := bookingRow{
		ID: uuid.New(), LinkID: &l.ID, HostID: l.ownerID, Title: l.Title, BookerUserID: bookerID, BookerName: name,
		BookerEmail: email, BookerTimezone: in.Timezone, StartsAt: start, EndsAt: start.Add(dur),
		LocationType: l.LocationType, LocationValue: l.LocationValue, Answers: answers, Status: "confirmed",
	}
	if l.LocationType == LocationPhone && l.LocationValue == "" {
		b.LocationValue = phone
	}
	var conf *Conference
	eventID := uuid.New() // the host's event, made below; named now so the room can link to it
	b.EventID = &eventID
	// A link that adds bookings to a connected calendar writes the copy
	// first: a native meeting (Meet, Teams) comes from it. Without a native
	// meeting the copy is a convenience, and the booking goes on without it.
	bcopy, err := s.prepareBookingCopy(ctx, &l)
	if err != nil {
		return err
	}
	if l.LocationType == LocationVideo && !bcopy.native {
		if conf, err = s.bookingConference(ctx, l, b, nil); err != nil {
			return err
		}
		b.ConferenceURL, b.ConferenceID = &conf.JoinURL, &conf.ID
	}
	if bcopy.sess != nil {
		if err := s.writeBookingCopy(ctx, l, &b, bcopy, bookingTitle(l.Title, host.DisplayName, name), bookingNotes(b, phone)); err != nil {
			// Only a native meeting fails the copy: fall back to the app's
			// own rooms when there are any.
			if !VideoAvailable() {
				return err
			}
			slog.WarnContext(ctx, "calendar: native meeting failed; using the app's own call", "err", err)
			l.ConferenceProvider, l.ConferenceConnectionID = ProviderApp, nil
			if conf, err = s.bookingConference(ctx, l, b, nil); err != nil {
				return err
			}
			b.ConferenceURL, b.ConferenceID = &conf.JoinURL, &conf.ID
		}
	}
	if l.LocationType == LocationVideo {
		provider := trimProvider(l.ConferenceProvider)
		b.ConferenceProvider = &provider
	}
	token, _ := s.tokenFor("manage", b.ID)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// One booking at a time per host: the check below and the insert
		// can't interleave with another booking for the same person.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobs.LockKey("calendar.host:"+l.ownerID.String())); err != nil {
			return err
		}
		ok, err := s.slotAvailable(withoutConnectedBusy(ctx), tx, l, start, in.Duration, uuid.Nil)
		if err != nil {
			return err
		}
		if !ok {
			return errSlotTaken
		}
		ev := eventRow{
			ID: eventID, OwnerID: l.ownerID, Title: bookingTitle(l.Title, host.DisplayName, name), StartsAt: b.StartsAt, EndsAt: b.EndsAt,
			Timezone: l.Timezone, Status: "confirmed", Visibility: "busy", ShowAs: "busy", Color: l.Color,
			Location: bookingLocation(b), Description: bookingNotes(b, phone), ConferenceURL: b.ConferenceURL, ConferenceID: b.ConferenceID,
		}
		st, err := s.loadSettings(ctx, tx, l.ownerID)
		if err != nil {
			return err
		}
		ev.Reminders = toInt32(st.DefaultReminders)
		if err := insertEvent(ctx, tx, ev); err != nil {
			return err
		}
		att := resolvedAttendee{userID: bookerID}
		if bookerID == nil {
			att = resolvedAttendee{email: email, name: name}
		}
		if _, err := s.insertAttendees(ctx, tx, ev.ID, []resolvedAttendee{att}, ResponseAccepted); err != nil {
			return err
		}
		b.EventID = &ev.ID
		answersJSON, _ := json.Marshal(answers)
		if _, err := tx.Exec(ctx, `
			INSERT INTO calendar_bookings (id, link_id, host_id, event_id, title, booker_user_id, booker_name, booker_email,
			  booker_timezone, starts_at, ends_at, block, location_type, location_value, conference_url, conference_id,
			  answers, manage_token_hash, ext_connection_id, ext_calendar_id, ext_event_id, conference_provider)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, tstzrange($12, $13), $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`,
			b.ID, b.LinkID, b.HostID, b.EventID, b.Title, b.BookerUserID, b.BookerName, b.BookerEmail, b.BookerTimezone,
			b.StartsAt, b.EndsAt, b.StartsAt.Add(-time.Duration(l.BufferBefore)*time.Minute), b.EndsAt.Add(time.Duration(l.BufferAfter)*time.Minute),
			b.LocationType, b.LocationValue, b.ConferenceURL, b.ConferenceID, answersJSON, hashToken(token),
			b.ExtConnectionID, b.ExtCalendarID, b.ExtEventID, b.ConferenceProvider); err != nil {
			return err
		}
		return s.tellBooking(ctx, tx, b, l, host, "new", token, "")
	})
	if err != nil {
		if conf != nil {
			s.cancelConferenceNow(ctx, conf.ID)
		}
		s.dropBookingCopy(ctx, bcopy, b)
		if isExclusionViolation(err) {
			return errSlotTaken
		}
		return err
	}
	kickOutbox()
	if b.ExtEventID != nil {
		s.changed(ctx, "connected", l.ownerID)
	}
	s.changed(ctx, "events", l.ownerID, uuidOr(bookerID))
	s.changed(ctx, "bookings", l.ownerID, uuidOr(bookerID))
	pb := s.publicBooking(b, l, host)
	httpx.JSON(w, http.StatusCreated, BookingConfirmation{Booking: pb, ManageURL: s.webURL("/book/manage/" + token), ConfirmationMessage: l.ConfirmationMessage})
	return nil
}

func uuidOr(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

func firstName(n string) string {
	if f, _, ok := strings.Cut(strings.TrimSpace(n), " "); ok {
		return f
	}
	return strings.TrimSpace(n)
}

func bookingTitle(title, host, booker string) string {
	t := title + ": " + firstName(host) + " and " + booker
	if len([]rune(t)) > maxTitle {
		t = string([]rune(t)[:maxTitle])
	}
	return t
}

func bookingLocation(b bookingRow) string {
	switch b.LocationType {
	case LocationVideo:
		return ""
	case LocationPhone:
		return "Phone: " + b.LocationValue
	}
	return b.LocationValue
}

func bookingNotes(b bookingRow, phone string) string {
	lines := []string{"Booked by " + b.BookerName + " (" + b.BookerEmail + ")"}
	if phone != "" {
		lines = append(lines, "Phone: "+phone)
	}
	for _, a := range b.Answers {
		lines = append(lines, "", a.Label, a.Value)
	}
	out := strings.Join(lines, "\n")
	if len([]rune(out)) > maxDescription {
		out = string([]rune(out)[:maxDescription])
	}
	return out
}

func (s *Service) bookingConference(ctx context.Context, l BookingLink, b bookingRow, existing *string) (*Conference, error) {
	c := conferencingProvider()
	if c == nil {
		return nil, httpx.NewError(http.StatusConflict, "video_unavailable", "Video calls aren't available for this link right now.")
	}
	req := ConferenceRequest{EventID: uuidOr(b.EventID), HostID: l.ownerID, Title: l.Title, StartsAt: b.StartsAt, EndsAt: b.EndsAt, Audience: AudienceExternal,
		Provider: trimProvider(l.ConferenceProvider), ConnectionID: ptrStr(l.ConferenceConnectionID)}
	if b.BookerUserID == nil && b.BookerEmail != "" {
		req.GuestEmails = []string{b.BookerEmail}
	}
	if l.Audience == "internal" {
		req.Audience, req.OrgID = AudienceInternal, l.OrgID
	}
	if b.BookerUserID != nil {
		req.ParticipantIDs = []uuid.UUID{*b.BookerUserID}
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var conf Conference
	var err error
	if existing != nil {
		conf, err = c.Update(cctx, *existing, req)
	} else {
		conf, err = c.Create(cctx, req)
	}
	if err != nil {
		slog.WarnContext(ctx, "calendar: booking conference failed", "provider", req.Provider, "err", err)
		return nil, httpx.NewError(http.StatusBadGateway, "video_failed", "Couldn't set up the video call. Try again.")
	}
	if conf.Provider == "" {
		conf.Provider = req.Provider
	}
	return &conf, nil
}

// ---- booking rows ---------------------------------------------------------------

type bookingRow struct {
	ID             uuid.UUID
	LinkID         *uuid.UUID
	HostID         uuid.UUID
	EventID        *uuid.UUID
	Title          string
	BookerUserID   *uuid.UUID
	BookerName     string
	BookerEmail    string
	BookerTimezone string
	StartsAt       time.Time
	EndsAt         time.Time
	LocationType   string
	LocationValue  string
	ConferenceURL  *string
	ConferenceID   *string
	Answers        []Answer
	Status         string
	CancelledAt    *time.Time
	CancelledBy    *string
	CancelReason   string
	CreatedAt      time.Time
	Sequence       int

	// The platform copy on a connected calendar (links with AddTo).
	ExtConnectionID    *string
	ExtCalendarID      *string
	ExtEventID         *string
	ConferenceProvider *string
}

const bookingCols = `b.id, b.link_id, b.host_id, b.event_id, b.title, b.booker_user_id, b.booker_name, b.booker_email,
	b.booker_timezone, b.starts_at, b.ends_at, b.location_type, b.location_value, b.conference_url, b.conference_id,
	b.answers, b.status, b.cancelled_at, b.cancelled_by, b.cancel_reason, b.created_at,
	COALESCE((SELECT sequence FROM calendar_events e WHERE e.id = b.event_id), b.reschedule_count),
	b.ext_connection_id, b.ext_calendar_id, b.ext_event_id, b.conference_provider`

func scanBooking(row pgx.Row) (bookingRow, error) {
	var b bookingRow
	var answers []byte
	err := row.Scan(&b.ID, &b.LinkID, &b.HostID, &b.EventID, &b.Title, &b.BookerUserID, &b.BookerName, &b.BookerEmail,
		&b.BookerTimezone, &b.StartsAt, &b.EndsAt, &b.LocationType, &b.LocationValue, &b.ConferenceURL, &b.ConferenceID,
		&answers, &b.Status, &b.CancelledAt, &b.CancelledBy, &b.CancelReason, &b.CreatedAt, &b.Sequence,
		&b.ExtConnectionID, &b.ExtCalendarID, &b.ExtEventID, &b.ConferenceProvider)
	if err == nil {
		b.Answers = []Answer{}
		_ = json.Unmarshal(answers, &b.Answers)
	}
	return b, err
}

// PublicBooking is a booking as the booker sees it (manage page).
type PublicBooking struct {
	ID             uuid.UUID `json:"id"`
	Title          string    `json:"title"`
	Host           Person    `json:"host"`
	BookerName     string    `json:"booker_name"`
	BookerEmail    string    `json:"booker_email"`
	BookerTimezone string    `json:"booker_timezone"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	LocationType   string    `json:"location_type"`
	LocationLabel  string    `json:"location_label"`
	ConferenceURL  *string   `json:"conference_url"`
	Status         string    `json:"status"`
	CancelledBy    *string   `json:"cancelled_by"`
	CancelReason   string    `json:"cancel_reason"`
	CanCancel      bool      `json:"can_cancel"`
	CanReschedule  bool      `json:"can_reschedule"`
	ManageCutoff   int       `json:"manage_cutoff"`
	LinkSlug       *string   `json:"link_slug"`
	Durations      []int     `json:"durations"`
	Timezone       string    `json:"timezone"`
}

func (s *Service) publicBooking(b bookingRow, l BookingLink, host Person) PublicBooking {
	pb := PublicBooking{
		ID: b.ID, Title: b.Title, Host: host, BookerName: b.BookerName, BookerEmail: b.BookerEmail, BookerTimezone: b.BookerTimezone,
		StartsAt: b.StartsAt, EndsAt: b.EndsAt, LocationType: b.LocationType, ConferenceURL: b.ConferenceURL,
		Status: b.Status, CancelledBy: b.CancelledBy, CancelReason: b.CancelReason, ManageCutoff: l.ManageCutoff,
		Durations: []int{int(b.EndsAt.Sub(b.StartsAt) / time.Minute)}, Timezone: l.Timezone,
	}
	pb.LocationLabel = bookingLocation(b)
	if b.LocationType == LocationVideo {
		pb.LocationLabel = providerName(ptrStr(b.ConferenceProvider))
	}
	if l.ID != uuid.Nil && l.Active {
		slug := l.Slug
		pb.LinkSlug = &slug
	}
	open := b.Status == "confirmed" && time.Now().Before(b.StartsAt.Add(-time.Duration(l.ManageCutoff)*time.Minute))
	pb.CanCancel = open && (l.ID == uuid.Nil || l.AllowCancel)
	pb.CanReschedule = open && l.ID != uuid.Nil && l.Active && l.AllowReschedule
	return pb
}

// bookingByToken loads a booking from its manage token with its link
// (zero BookingLink when the link was deleted) and host.
func (s *Service) bookingByToken(ctx context.Context, q db.Querier, token string, lock bool) (bookingRow, BookingLink, Person, error) {
	if token == "" || len(token) > 128 {
		return bookingRow{}, BookingLink{}, Person{}, errBookingNotFound
	}
	sql := `SELECT ` + bookingCols + ` FROM calendar_bookings b WHERE b.manage_token_hash = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	b, err := scanBooking(q.QueryRow(ctx, sql, hashToken(token)))
	if db.IsNoRows(err) {
		return b, BookingLink{}, Person{}, errBookingNotFound
	}
	if err != nil {
		return b, BookingLink{}, Person{}, err
	}
	l, host, err := s.bookingContext(ctx, q, b)
	return b, l, host, err
}

var errBookingNotFound = httpx.NotFound("This booking link doesn't work any more.")

func (s *Service) bookingContext(ctx context.Context, q db.Querier, b bookingRow) (BookingLink, Person, error) {
	var host Person
	if err := q.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1`, b.HostID).
		Scan(&host.ID, &host.DisplayName, &host.AvatarURL); err != nil {
		return BookingLink{}, host, err
	}
	var l BookingLink
	if b.LinkID != nil {
		var err error
		l, err = s.scanLink(q.QueryRow(ctx, `SELECT `+linkCols+linkFrom+`WHERE l.id = $1 AND l.deleted_at IS NULL`, *b.LinkID))
		if err != nil && !db.IsNoRows(err) {
			return l, host, err
		}
		if db.IsNoRows(err) {
			l = BookingLink{}
		}
	}
	if l.ID == uuid.Nil {
		l.ManageCutoff, l.AllowCancel = 0, true
	}
	return l, host, nil
}

func (s *Service) manageGet(w http.ResponseWriter, r *http.Request) error {
	b, l, host, err := s.bookingByToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.OK(w, s.publicBooking(b, l, host))
	return nil
}

func (s *Service) manageICS(w http.ResponseWriter, r *http.Request) error {
	b, _, _, err := s.bookingByToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	host, err := ownerContact(r.Context(), s.d.Pool, b.HostID)
	if err != nil {
		return err
	}
	method := "PUBLISH"
	body := renderICS(method, b.Title, []icsEvent{bookingVEvent(b, &host, nil, b.Status == "cancelled")})
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="booking.ics"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(body)
	return nil
}

func (s *Service) manageSlots(w http.ResponseWriter, r *http.Request) error {
	b, l, _, err := s.bookingByToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	if l.ID == uuid.Nil || !l.Active || !l.AllowReschedule {
		return httpx.NewError(http.StatusConflict, "reschedule_closed", "This booking can't be moved online. Contact the host.")
	}
	from, to, err := parseSlotWindow(r)
	if err != nil {
		return err
	}
	slots, err := s.linkSlots(r.Context(), s.d.Pool, l, from, to, int(b.EndsAt.Sub(b.StartsAt)/time.Minute), b.ID)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, no-store")
	httpx.OK(w, map[string]any{"slots": slots, "duration": int(b.EndsAt.Sub(b.StartsAt) / time.Minute)})
	return nil
}

func (s *Service) manageCancel(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	b, l, host, err := s.bookingByToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	if !s.publicBooking(b, l, host).CanCancel {
		return httpx.NewError(http.StatusConflict, "cancel_closed", "This booking can't be cancelled online any more. Contact the host.")
	}
	reason, _ := cleanText(in.Reason, 500)
	if err := s.cancelBooking(r.Context(), b.ID, "booker", reason); err != nil {
		return err
	}
	b, l, host, err = s.bookingByToken(r.Context(), s.d.Pool, chi.URLParam(r, "token"), false)
	if err != nil {
		return err
	}
	httpx.OK(w, s.publicBooking(b, l, host))
	return nil
}

func (s *Service) manageReschedule(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	var in struct {
		StartsAt time.Time `json:"starts_at"`
		Timezone string    `json:"timezone"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	b, l, host, err := s.bookingByToken(ctx, s.d.Pool, token, false)
	if err != nil {
		return err
	}
	if !s.publicBooking(b, l, host).CanReschedule {
		return httpx.NewError(http.StatusConflict, "reschedule_closed", "This booking can't be moved online any more. Contact the host.")
	}
	dur := int(b.EndsAt.Sub(b.StartsAt) / time.Minute)
	start := in.StartsAt.UTC()
	if ok, err := s.slotAvailable(ctx, s.d.Pool, l, start, dur, b.ID); err != nil {
		return err
	} else if !ok {
		return errSlotTaken
	}
	moved := b
	moved.StartsAt, moved.EndsAt = start, start.Add(time.Duration(dur)*time.Minute)
	if in.Timezone != "" && validZone(in.Timezone) {
		moved.BookerTimezone = in.Timezone
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobs.LockKey("calendar.host:"+b.HostID.String())); err != nil {
			return err
		}
		cur, _, _, err := s.bookingByToken(ctx, tx, token, true)
		if err != nil {
			return err
		}
		if cur.Status != "confirmed" {
			return httpx.Conflict("This booking was cancelled.")
		}
		ok, err := s.slotAvailable(withoutConnectedBusy(ctx), tx, l, start, dur, b.ID)
		if err != nil {
			return err
		}
		if !ok {
			return errSlotTaken
		}
		if _, err := tx.Exec(ctx, `
			UPDATE calendar_bookings SET starts_at = $2, ends_at = $3, booker_timezone = $4,
			  block = tstzrange($2::timestamptz - make_interval(mins => $5), $3::timestamptz + make_interval(mins => $6)),
			  reschedule_count = reschedule_count + 1, updated_at = now()
			WHERE id = $1`, b.ID, moved.StartsAt, moved.EndsAt, moved.BookerTimezone, l.BufferBefore, l.BufferAfter); err != nil {
			return err
		}
		if b.EventID != nil {
			if _, err := tx.Exec(ctx, `UPDATE calendar_events SET starts_at = $2, ends_at = $3, sequence = sequence + 1, updated_at = now()
				WHERE id = $1`, *b.EventID, moved.StartsAt, moved.EndsAt); err != nil {
				return err
			}
			moved.Sequence = cur.Sequence + 1
		}
		if err := enqueueCopyMove(ctx, tx, b, moved.StartsAt, moved.EndsAt, l.Timezone); err != nil {
			return err
		}
		mt, stable := s.tokenFor("manage", b.ID)
		if !stable {
			if _, err := tx.Exec(ctx, `UPDATE calendar_bookings SET manage_token_hash = $2 WHERE id = $1`, b.ID, hashToken(mt)); err != nil {
				return err
			}
		}
		return s.tellBooking(ctx, tx, moved, l, host, "rescheduled", mt, "")
	})
	if err != nil {
		if isExclusionViolation(err) {
			return errSlotTaken
		}
		return err
	}
	kickOutbox()
	if b.ConferenceID != nil {
		if _, err := s.bookingConference(ctx, l, moved, b.ConferenceID); err != nil {
			slog.WarnContext(ctx, "calendar: conference update failed", "err", err)
		}
	}
	s.changed(ctx, "events", b.HostID, uuidOr(b.BookerUserID))
	s.changed(ctx, "bookings", b.HostID, uuidOr(b.BookerUserID))
	b, l, host, err = s.bookingByToken(ctx, s.d.Pool, token, false)
	if err != nil {
		// Without a stable token the old link stops working after a move.
		httpx.OK(w, s.publicBooking(moved, l, host))
		return nil
	}
	httpx.OK(w, s.publicBooking(b, l, host))
	return nil
}

// cancelBooking cancels a confirmed booking (idempotent): the event leaves
// both calendars, the room closes, and the other side is told. by is host,
// booker or system. External bookers cancelled by the host get the reason
// and, while the link is open, a link to book another time.
func (s *Service) cancelBooking(ctx context.Context, id uuid.UUID, by, reason string) error {
	var users []uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		users, err = s.cancelBookingTx(ctx, tx, id, by, reason)
		return err
	})
	if err != nil {
		return err
	}
	kickOutbox()
	s.changed(ctx, "events", users...)
	s.changed(ctx, "bookings", users...)
	return nil
}

// cancelBookingTx is cancelBooking inside the caller's transaction (account
// deletion). It returns the users whose calendars changed.
func (s *Service) cancelBookingTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, by, reason string) ([]uuid.UUID, error) {
	b, err := scanBooking(tx.QueryRow(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b WHERE b.id = $1 FOR UPDATE`, id))
	if db.IsNoRows(err) {
		return nil, errBookingNotFound
	}
	if err != nil {
		return nil, err
	}
	if b.Status != "confirmed" {
		return nil, nil
	}
	l, host, err := s.bookingContext(ctx, tx, b)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE calendar_bookings SET status = 'cancelled', cancelled_at = now(), cancelled_by = $2,
		cancel_reason = $3, updated_at = now() WHERE id = $1`, id, by, reason); err != nil {
		return nil, err
	}
	b.Status, b.CancelledBy, b.CancelReason = "cancelled", &by, reason
	b.Sequence++
	if b.EventID != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM calendar_events WHERE id = $1`, *b.EventID); err != nil {
			return nil, err
		}
	}
	if err := enqueueConferenceCancel(ctx, tx, b.ConferenceID); err != nil {
		return nil, err
	}
	if b.ExtEventID != nil && b.ExtConnectionID != nil && b.ExtCalendarID != nil {
		if err := enqueueConnectedOp(ctx, tx, connectedOp{UserID: b.HostID, ConnectionID: *b.ExtConnectionID, CalendarID: *b.ExtCalendarID,
			EventID: *b.ExtEventID, Op: "delete"}); err != nil {
			return nil, err
		}
	}
	if err := s.tellBooking(ctx, tx, b, l, host, "cancelled", "", by); err != nil {
		return nil, err
	}
	return []uuid.UUID{b.HostID, uuidOr(b.BookerUserID)}, nil
}

// bookingVEvent is the booking as one VEVENT (UID of its calendar event,
// so updates replace the original invite).
func bookingVEvent(b bookingRow, host *icsPerson, booker *icsPerson, cancelled bool) icsEvent {
	basis := b.ID
	if b.EventID != nil {
		basis = *b.EventID
	}
	desc := ""
	place := bookingLocation(b)
	if b.ConferenceURL != nil {
		desc = "Join the video call: " + *b.ConferenceURL
		place = *b.ConferenceURL
	}
	ev := icsEvent{UID: uid(basis), Sequence: b.Sequence, Start: b.StartsAt, End: b.EndsAt, Summary: b.Title,
		Description: desc, Location: place, Organizer: host, Cancelled: cancelled, Created: b.CreatedAt}
	if booker != nil {
		ev.Attendees = []icsPerson{*booker}
	}
	return ev
}

// tellBooking queues the booker's and host's emails (with .ics) and
// in-app notifications for a new, rescheduled or cancelled booking.
func (s *Service) tellBooking(ctx context.Context, q db.Querier, b bookingRow, l BookingLink, host Person, what, manageToken, by string) error {
	hostContact, err := ownerContact(ctx, q, b.HostID)
	if err != nil {
		return err
	}
	booker := icsPerson{Name: b.BookerName, Email: b.BookerEmail, Response: ResponseAccepted}
	bookerZone := zoneOr(b.BookerTimezone, time.UTC)
	hostZone := s.zoneFor(ctx, q, b.HostID, zoneOr(l.Timezone, time.UTC))
	bookerWhen := formatWhen(b.StartsAt, b.EndsAt, false, bookerZone)
	hostWhen := formatWhen(b.StartsAt, b.EndsAt, false, hostZone)
	method := "REQUEST"
	if what == "cancelled" {
		method = "CANCEL"
	}
	ics := renderICS(method, "", []icsEvent{bookingVEvent(b, &hostContact, &booker, what == "cancelled")})
	where := bookingLocation(b)
	var joinLink []mailLink
	if b.ConferenceURL != nil {
		joinLink = []mailLink{{"Join the video call", *b.ConferenceURL}}
	}
	manage := ""
	if manageToken != "" {
		manage = s.webURL("/book/manage/" + manageToken)
	}

	// The booker.
	if !(what == "cancelled" && by == "booker" && b.BookerUserID != nil) {
		var subject string
		var paras []string
		links := joinLink
		switch what {
		case "new":
			subject = "Confirmed: " + b.Title + " with " + host.DisplayName + " @ " + bookerWhen
			paras = []string{"You're booked with " + host.DisplayName + ".", l.ConfirmationMessage, "When: " + bookerWhen}
		case "rescheduled":
			subject = "Rescheduled: " + b.Title + " with " + host.DisplayName + " @ " + bookerWhen
			paras = []string{"Your booking with " + host.DisplayName + " moved.", "When: " + bookerWhen}
		case "cancelled":
			subject = "Cancelled: " + b.Title + " with " + host.DisplayName + " @ " + bookerWhen
			lead := "Your booking with " + host.DisplayName + " is cancelled."
			if by == "host" {
				lead = host.DisplayName + " cancelled your booking."
			}
			paras = []string{lead, "When: " + bookerWhen}
			if b.CancelReason != "" && by != "booker" {
				paras = append(paras, "Reason: "+b.CancelReason)
			}
			links = nil
			if l.ID != uuid.Nil && l.Active && by != "booker" {
				links = []mailLink{{"Book another time", s.webURL("/book/" + l.Slug)}}
			}
		}
		if where != "" && what != "cancelled" {
			paras = append(paras, "Where: "+where)
		}
		if manage != "" && what != "cancelled" {
			links = append(links, mailLink{"Reschedule or cancel", manage})
		}
		text, html := s.mailBody(paras, links)
		if err := enqueueMail(ctx, q, mail.Message{To: b.BookerEmail, Subject: subject, Text: text, HTML: html,
			ReplyTo: hostContact.Email, Attachments: []mail.Attachment{icsAttachment(method, ics)}}); err != nil {
			return err
		}
		if b.BookerUserID != nil && what != "new" {
			n := notify.Notification{UserID: *b.BookerUserID, Kind: NotifyBooking, Body: s.whenFor(ctx, q, *b.BookerUserID, b.StartsAt, b.EndsAt, false, bookerZone), Link: "/calendar"}
			if what == "rescheduled" {
				n.Title, n.Link = b.Title+" moved", eventLink(uuidOr(b.EventID))
			} else {
				n.Title = host.DisplayName + " cancelled " + b.Title
			}
			if err := enqueueNotify(ctx, q, n); err != nil {
				return err
			}
		}
	}

	// The host (skip their own cancellations and their account closing).
	if what == "cancelled" && (by == "host" || by == "system") {
		return nil
	}
	var subject string
	var paras []string
	switch what {
	case "new":
		subject = "New booking: " + b.BookerName + " @ " + hostWhen
		paras = []string{b.BookerName + " booked " + b.Title + ".", "When: " + hostWhen, "Email: " + b.BookerEmail}
	case "rescheduled":
		subject = "Rescheduled: " + b.BookerName + " @ " + hostWhen
		paras = []string{b.BookerName + " moved their booking of " + b.Title + ".", "When: " + hostWhen}
	case "cancelled":
		subject = "Cancelled: " + b.BookerName + " @ " + hostWhen
		paras = []string{b.BookerName + " cancelled " + b.Title + ".", "When: " + hostWhen}
		if b.CancelReason != "" {
			paras = append(paras, "Reason: "+b.CancelReason)
		}
	}
	if what == "new" {
		for _, a := range b.Answers {
			paras = append(paras, a.Label+"\n"+a.Value)
		}
	}
	links := []mailLink{}
	if what != "cancelled" && b.EventID != nil {
		links = append(links, mailLink{"Open in " + appName, s.webURL(eventLink(*b.EventID))})
	}
	text, html := s.mailBody(paras, links)
	hostICS := renderICS(method, "", []icsEvent{bookingVEvent(b, &hostContact, &booker, what == "cancelled")})
	if err := enqueueMail(ctx, q, mail.Message{To: hostContact.Email, Subject: subject, Text: text, HTML: html,
		ReplyTo: b.BookerEmail, Attachments: []mail.Attachment{icsAttachment(method, hostICS)}}); err != nil {
		return err
	}
	n := notify.Notification{UserID: b.HostID, Kind: NotifyBooking, Body: hostWhen, Link: "/calendar/bookings"}
	switch what {
	case "new":
		n.Title = b.BookerName + " booked " + b.Title
	case "rescheduled":
		n.Title = b.BookerName + " moved " + b.Title
	default:
		n.Title = b.BookerName + " cancelled " + b.Title
	}
	return enqueueNotify(ctx, q, n)
}

// ---- host and booker lists ---------------------------------------------------------

// Booking is a booking in the signed-in lists (host or booker side).
type Booking struct {
	ID             uuid.UUID  `json:"id"`
	Role           string     `json:"role"` // host | booker
	LinkID         *uuid.UUID `json:"link_id"`
	EventID        *uuid.UUID `json:"event_id"`
	Title          string     `json:"title"`
	Host           Person     `json:"host"`
	BookerUserID   *uuid.UUID `json:"booker_user_id"`
	BookerName     string     `json:"booker_name"`
	BookerEmail    string     `json:"booker_email"`
	BookerTimezone string     `json:"booker_timezone"`
	StartsAt       time.Time  `json:"starts_at"`
	EndsAt         time.Time  `json:"ends_at"`
	LocationType   string     `json:"location_type"`
	LocationValue  string     `json:"location_value"`
	ConferenceURL  *string    `json:"conference_url"`
	Answers        []Answer   `json:"answers"`
	Status         string     `json:"status"`
	CancelledAt    *time.Time `json:"cancelled_at"`
	CancelledBy    *string    `json:"cancelled_by"`
	CancelReason   string     `json:"cancel_reason"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (s *Service) bookingView(ctx context.Context, b bookingRow, viewer uuid.UUID) (Booking, error) {
	var host Person
	if err := s.d.Pool.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1`, b.HostID).
		Scan(&host.ID, &host.DisplayName, &host.AvatarURL); err != nil {
		return Booking{}, err
	}
	out := Booking{
		ID: b.ID, Role: "booker", LinkID: b.LinkID, EventID: b.EventID, Title: b.Title, Host: host, BookerUserID: b.BookerUserID,
		BookerName: b.BookerName, BookerEmail: b.BookerEmail, BookerTimezone: b.BookerTimezone, StartsAt: b.StartsAt, EndsAt: b.EndsAt,
		LocationType: b.LocationType, LocationValue: b.LocationValue, ConferenceURL: b.ConferenceURL, Answers: b.Answers,
		Status: b.Status, CancelledAt: b.CancelledAt, CancelledBy: b.CancelledBy, CancelReason: b.CancelReason, CreatedAt: b.CreatedAt,
	}
	if b.HostID == viewer {
		out.Role = "host"
	}
	return out, nil
}

func (s *Service) listBookingsFor(ctx context.Context, userID uuid.UUID, role, when string, limit int, cursor string) (httpx.Page[Booking], error) {
	var c struct {
		T  time.Time `json:"t"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(cursor, &c); err != nil {
		return httpx.Page[Booking]{}, err
	}
	var cursorT *time.Time
	if !c.T.IsZero() {
		cursorT = &c.T
	}
	who := "b.host_id = $1"
	if role == "booker" {
		who = "b.booker_user_id = $1"
	}
	var cond, order, cmp string
	switch when {
	case "past":
		cond, order, cmp = "b.status = 'confirmed' AND b.ends_at <= now()", "DESC", "<"
	case "cancelled":
		cond, order, cmp = "b.status = 'cancelled'", "DESC", "<"
	default:
		cond, order, cmp = "b.status = 'confirmed' AND b.ends_at > now()", "ASC", ">"
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b
		WHERE `+who+` AND `+cond+`
		  AND ($2::timestamptz IS NULL OR (b.starts_at, b.id) `+cmp+` ($2, $3::uuid))
		ORDER BY b.starts_at `+order+`, b.id `+order+` LIMIT $4`, userID, cursorT, c.ID, limit+1)
	if err != nil {
		return httpx.Page[Booking]{}, err
	}
	var list []bookingRow
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			rows.Close()
			return httpx.Page[Booking]{}, err
		}
		list = append(list, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return httpx.Page[Booking]{}, err
	}
	out := []Booking{}
	for _, b := range list {
		v, err := s.bookingView(ctx, b, userID)
		if err != nil {
			return httpx.Page[Booking]{}, err
		}
		out = append(out, v)
	}
	return httpx.NewPage(out, limit, func(b Booking) any { return map[string]any{"t": b.StartsAt, "id": b.ID} }), nil
}

func (s *Service) listBookings(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q := r.URL.Query()
	role, when := q.Get("role"), q.Get("when")
	if role != "" && role != "host" && role != "booker" {
		return httpx.BadRequest("role must be host or booker.")
	}
	if when != "" && when != "upcoming" && when != "past" && when != "cancelled" {
		return httpx.BadRequest("when must be upcoming, past or cancelled.")
	}
	p := httpx.ParsePage(r)
	page, err := s.listBookingsFor(r.Context(), id.UserID, role, when, p.Limit, p.Cursor)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) loadVisibleBooking(ctx context.Context, viewer, id uuid.UUID) (bookingRow, error) {
	b, err := scanBooking(s.d.Pool.QueryRow(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b
		WHERE b.id = $1 AND (b.host_id = $2 OR b.booker_user_id = $2)`, id, viewer))
	if db.IsNoRows(err) {
		return b, httpx.NotFound("That booking doesn't exist.")
	}
	return b, err
}

func (s *Service) getBooking(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	bid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	b, err := s.loadVisibleBooking(r.Context(), id.UserID, bid)
	if err != nil {
		return err
	}
	v, err := s.bookingView(r.Context(), b, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// hostCancelBooking cancels as the host (any time), or as a signed-in
// booker within the link's rules.
func (s *Service) hostCancelBooking(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	bid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	b, err := s.loadVisibleBooking(r.Context(), id.UserID, bid)
	if err != nil {
		return err
	}
	if b.Status != "confirmed" {
		return httpx.Conflict("This booking is already cancelled.")
	}
	by := "host"
	if b.HostID != id.UserID {
		l, host, err := s.bookingContext(r.Context(), s.d.Pool, b)
		if err != nil {
			return err
		}
		if !s.publicBooking(b, l, host).CanCancel {
			return httpx.NewError(http.StatusConflict, "cancel_closed", "This booking can't be cancelled online any more. Contact the host.")
		}
		by = "booker"
	}
	reason, _ := cleanText(in.Reason, 500)
	if err := s.cancelBooking(r.Context(), b.ID, by, reason); err != nil {
		return err
	}
	b, err = s.loadVisibleBooking(r.Context(), id.UserID, bid)
	if err != nil {
		return err
	}
	v, err := s.bookingView(r.Context(), b, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}
