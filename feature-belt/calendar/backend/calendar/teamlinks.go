package calendar

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/mail"
	"scaffoldapp/backend/internal/notify"
)

// Booking link kinds.
const (
	LinkPersonal   = "personal"
	LinkRoundRobin = "round_robin"
	LinkCollective = "collective"
)

const maxHosts = 20

// validateLinkDepth checks what 1.3.0 added to links: team kinds and their
// hosts, single use, redirect, booker reminders.
func (s *Service) validateLinkDepth(ctx context.Context, callerID uuid.UUID, l *BookingLink) error {
	if l.Kind == "" {
		l.Kind = LinkPersonal
	}
	switch l.Kind {
	case LinkPersonal:
		l.TeamID, l.Hosts = nil, []LinkHost{}
	case LinkRoundRobin, LinkCollective:
		if l.TeamID == nil {
			return httpx.BadRequest("Pick the team whose members host this link.")
		}
		var org uuid.UUID
		var member bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT t.org_id, EXISTS (SELECT 1 FROM access_team_members m WHERE m.team_id = t.id AND m.user_id = $2)
			FROM access_teams t WHERE t.id = $1`, *l.TeamID, callerID).Scan(&org, &member); db.IsNoRows(err) {
			return httpx.BadRequest("Pick a team you're on.")
		} else if err != nil {
			return err
		}
		if !member && l.ownerID == callerID {
			return httpx.BadRequest("Pick a team you're on.")
		}
		l.OrgID = &org
		if l.AddTo != nil {
			return httpx.BadRequest("Team links add bookings to each host's calendar here, not to a connected calendar.")
		}
		if l.LocationType == LocationVideo && l.ConferenceProvider != ProviderApp {
			return httpx.BadRequest("Team links use this app's own video calls.")
		}
		if len(l.Hosts) == 0 {
			return httpx.BadRequest("Add at least one host from the team.")
		}
		ids := make([]uuid.UUID, 0, len(l.Hosts))
		for i := range l.Hosts {
			h := &l.Hosts[i]
			if h.Priority < 0 || h.Priority > 4 {
				return httpx.BadRequest("Host priority is 0 (lowest) to 4 (highest).")
			}
			if h.Weight < 1 || h.Weight > 1000 {
				return httpx.BadRequest("Host weight is 1 to 1000.")
			}
			if (h.WeeklyHours == nil) != (h.Timezone == nil) {
				return httpx.BadRequest("A host's own hours need a time zone.")
			}
			if h.WeeklyHours != nil {
				if !validZone(*h.Timezone) {
					return httpx.BadRequest("Pick a valid time zone for each host.")
				}
				hours, err := validateHours(*h.WeeklyHours)
				if err != nil {
					return err
				}
				h.WeeklyHours = &hours
			}
			ids = append(ids, h.User.ID)
		}
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM access_team_members m JOIN users u ON u.id = m.user_id
			WHERE m.team_id = $1 AND m.user_id = ANY($2) AND u.status = 'active'`, *l.TeamID, ids).Scan(&n); err != nil {
			return err
		}
		if n != len(ids) {
			return httpx.BadRequest("Hosts must be active members of the team.")
		}
	default:
		return httpx.BadRequest("kind must be personal, round_robin or collective.")
	}
	if l.RedirectURL != "" {
		u, err := url.Parse(l.RedirectURL)
		dev := s.d.Cfg.IsDevelopment()
		if err != nil || len(l.RedirectURL) > 2000 || u.Host == "" || u.User != nil || !(u.Scheme == "https" || (dev && u.Scheme == "http")) {
			return httpx.BadRequest("The redirect must be a full https:// address.")
		}
	}
	if len(l.BookerReminders) > 3 {
		return httpx.BadRequest("Up to 3 reminders to bookers.")
	}
	slices.Sort(l.BookerReminders)
	l.BookerReminders = slices.Compact(l.BookerReminders)
	for _, m := range l.BookerReminders {
		if m < 10 || m > 7*24*60 {
			return httpx.BadRequest("Reminders go out 10 minutes to 7 days before.")
		}
	}
	return nil
}

// fillLinkAccess sets CanEdit for callerID and loads a team link's hosts.
func (s *Service) fillLinkAccess(ctx context.Context, q db.Querier, callerID uuid.UUID, l *BookingLink) error {
	l.CanEdit = l.ownerID == callerID
	if l.TeamID == nil {
		return nil
	}
	if !l.CanEdit {
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_team_members WHERE team_id = $1 AND user_id = $2 AND role = 'lead')`,
			*l.TeamID, callerID).Scan(&l.CanEdit); err != nil {
			return err
		}
	}
	hosts, err := loadHosts(ctx, q, l.ID)
	if err != nil {
		return err
	}
	l.Hosts = hosts
	return nil
}

// loadHosts reads a link's hosts with their recent bookings on it.
func loadHosts(ctx context.Context, q db.Querier, linkID uuid.UUID) ([]LinkHost, error) {
	rows, err := q.Query(ctx, `
		SELECT h.user_id, u.display_name, u.avatar_url, h.priority, h.weight, h.weekly_hours, h.timezone, h.active,
		       (SELECT max(b.created_at) FROM calendar_bookings b WHERE b.link_id = h.link_id AND b.host_id = h.user_id AND b.status = 'confirmed'),
		       (SELECT count(*) FROM calendar_bookings b WHERE b.link_id = h.link_id AND b.host_id = h.user_id AND b.status = 'confirmed'
		          AND b.created_at > now() - interval '30 days')::int
		FROM calendar_link_hosts h JOIN users u ON u.id = h.user_id
		WHERE h.link_id = $1 ORDER BY h.priority DESC, u.display_name, h.user_id`, linkID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (LinkHost, error) {
		var h LinkHost
		var hours []byte
		err := row.Scan(&h.User.ID, &h.User.DisplayName, &h.User.AvatarURL, &h.Priority, &h.Weight, &hours, &h.Timezone, &h.Active,
			&h.LastBookedAt, &h.RecentCount)
		if err == nil && hours != nil {
			var wh []HoursInterval
			if json.Unmarshal(hours, &wh) == nil {
				h.WeeklyHours = &wh
			}
		}
		return h, err
	})
	if out == nil {
		out = []LinkHost{}
	}
	return out, err
}

func saveHosts(ctx context.Context, tx pgx.Tx, linkID uuid.UUID, hosts []LinkHost) error {
	if _, err := tx.Exec(ctx, `DELETE FROM calendar_link_hosts WHERE link_id = $1`, linkID); err != nil {
		return err
	}
	for _, h := range hosts {
		var hours []byte
		if h.WeeklyHours != nil {
			hours, _ = json.Marshal(*h.WeeklyHours)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO calendar_link_hosts (link_id, user_id, priority, weight, weekly_hours, timezone, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, linkID, h.User.ID, h.Priority, h.Weight, hours, h.Timezone, h.Active); err != nil {
			return err
		}
	}
	return nil
}

// linkWatchers are people whose links list shows l (its team's members).
func (s *Service) linkWatchers(ctx context.Context, l BookingLink) []uuid.UUID {
	if l.TeamID == nil {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM access_team_members WHERE team_id = $1 LIMIT 500`, *l.TeamID)
	if err != nil {
		return nil
	}
	ids, _ := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	return ids
}

// ---- availability across hosts ---------------------------------------------------

// hostAvail is one host's availability for a team link: their busy time
// and, when they keep their own hours, those rules.
type hostAvail struct {
	ID         uuid.UUID
	Priority   int
	Weight     int
	Count      int       // bookings on this link in the last 30 days
	LastBooked time.Time // zero: never
	Rules      *slotRules
	Busy       []busyItem
}

// contains reports whether [start, end) lies inside one of the rules'
// windows on its local date.
func (r slotRules) contains(start, end time.Time) bool {
	day := start.In(r.loc)
	noon := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, r.loc)
	for _, w := range r.windowsOn(noon) {
		if !start.Before(w[0]) && !end.After(w[1]) {
			return true
		}
	}
	return false
}

// free reports whether the host can take [start, end) with the link's buffers.
func (h hostAvail) free(start, end time.Time, before, after time.Duration) bool {
	if h.Rules != nil && !h.Rules.contains(start, end) {
		return false
	}
	return !conflicts(start, end, start.Add(-before), end.Add(after), h.Busy)
}

// freeHosts lists the hosts free for a slot.
func freeHosts(sl Slot, hosts []hostAvail, before, after time.Duration) []hostAvail {
	var out []hostAvail
	for _, h := range hosts {
		if h.free(sl.StartsAt, sl.EndsAt, before, after) {
			out = append(out, h)
		}
	}
	return out
}

// teamSlots keeps the link's candidate starts that a round-robin link can
// staff (any host free) or a collective link can (every host free).
func teamSlots(cands []Slot, hosts []hostAvail, kind string, before, after time.Duration, limit int) []Slot {
	out := []Slot{}
	if len(hosts) == 0 {
		return out
	}
	for _, sl := range cands {
		n := len(freeHosts(sl, hosts, before, after))
		if (kind == LinkCollective && n == len(hosts)) || (kind != LinkCollective && n > 0) {
			out = append(out, sl)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// pickRoundRobin chooses who takes a booking among the free hosts: the
// highest priority, then the fewest recent bookings for their weight (so
// a weight of 200 takes twice the share of 100), then the least recently
// booked, then a stable order.
func pickRoundRobin(free []hostAvail) (hostAvail, bool) {
	if len(free) == 0 {
		return hostAvail{}, false
	}
	best := slices.Clone(free)
	slices.SortStableFunc(best, func(a, b hostAvail) int {
		if a.Priority != b.Priority {
			return b.Priority - a.Priority
		}
		// count_a / weight_a vs count_b / weight_b without division.
		if l, r := a.Count*max(b.Weight, 1), b.Count*max(a.Weight, 1); l != r {
			return l - r
		}
		if c := a.LastBooked.Compare(b.LastBooked); c != 0 {
			return c
		}
		return compareUUID(a.ID, b.ID)
	})
	return best[0], true
}

// hostAvailability loads each active host's busy time over [from, to) and
// own hours (team members with active accounts only). l.onlyHost narrows
// it to one host (rescheduling a round-robin booking keeps its host).
func (s *Service) hostAvailability(ctx context.Context, q db.Querier, l BookingLink, base slotRules, from, to time.Time, exclude uuid.UUID) ([]hostAvail, error) {
	hosts := l.Hosts
	if l.ID != uuid.Nil && len(hosts) == 0 {
		var err error
		if hosts, err = loadHosts(ctx, q, l.ID); err != nil {
			return nil, err
		}
	}
	var ids []uuid.UUID
	for _, h := range hosts {
		ids = append(ids, h.User.ID)
	}
	member := map[uuid.UUID]bool{}
	if l.TeamID != nil && len(ids) > 0 {
		rows, err := q.Query(ctx, `SELECT m.user_id FROM access_team_members m JOIN users u ON u.id = m.user_id
			WHERE m.team_id = $1 AND m.user_id = ANY($2) AND u.status = 'active'`, *l.TeamID, ids)
		if err != nil {
			return nil, err
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return nil, err
		}
		for _, id := range got {
			member[id] = true
		}
	}
	var out []hostAvail
	for _, h := range hosts {
		if !h.Active || !member[h.User.ID] || (l.onlyHost != nil && *l.onlyHost != h.User.ID) {
			continue
		}
		busy, err := s.busyFor(ctx, q, h.User.ID, from, to, exclude)
		if err != nil {
			return nil, err
		}
		ha := hostAvail{ID: h.User.ID, Priority: h.Priority, Weight: h.Weight, Count: h.RecentCount, Busy: busy}
		if h.LastBookedAt != nil {
			ha.LastBooked = *h.LastBookedAt
		}
		if h.WeeklyHours != nil && h.Timezone != nil {
			r := base
			r.loc, r.weekly, r.overrides = zoneOr(*h.Timezone, time.UTC), *h.WeeklyHours, nil
			ha.Rules = &r
		}
		out = append(out, ha)
	}
	if l.Kind == LinkCollective && l.onlyHost == nil && len(out) != countActive(hosts) {
		// A collective link needs everyone: a host who left the team or
		// closed their account stops it until the hosts are fixed.
		return nil, nil
	}
	return out, nil
}

func countActive(hosts []LinkHost) int {
	n := 0
	for _, h := range hosts {
		if h.Active {
			n++
		}
	}
	return n
}

// assignHosts decides who hosts a booking at start: round robin picks one
// free host, collective needs every host. organizer comes first.
func (s *Service) assignHosts(ctx context.Context, q db.Querier, l BookingLink, start time.Time, dur int, exclude uuid.UUID, notUser uuid.UUID) ([]uuid.UUID, error) {
	end := start.Add(time.Duration(dur) * time.Minute)
	before, after := time.Duration(l.BufferBefore)*time.Minute, time.Duration(l.BufferAfter)*time.Minute
	pad := max(before, after) + time.Duration(dur)*time.Minute
	loc := zoneOr(l.Timezone, time.UTC)
	hosts, err := s.hostAvailability(ctx, q, l, slotRules{loc: loc}, start.Add(-pad), end.Add(pad), exclude)
	if err != nil {
		return nil, err
	}
	free := freeHosts(Slot{StartsAt: start, EndsAt: end}, hosts, before, after)
	if l.Kind == LinkCollective {
		if len(hosts) == 0 || len(free) != len(hosts) {
			return nil, errSlotTaken
		}
		for _, h := range free {
			if h.ID == notUser {
				return nil, httpx.BadRequest("You host this link.")
			}
		}
		// The link's maker organizes when they host; else the top host.
		slices.SortStableFunc(free, func(a, b hostAvail) int {
			if (a.ID == l.ownerID) != (b.ID == l.ownerID) {
				if a.ID == l.ownerID {
					return -1
				}
				return 1
			}
			return b.Priority - a.Priority
		})
		ids := make([]uuid.UUID, 0, len(free))
		for _, h := range free {
			ids = append(ids, h.ID)
		}
		return ids, nil
	}
	free = slices.DeleteFunc(free, func(h hostAvail) bool { return h.ID == notUser })
	h, ok := pickRoundRobin(free)
	if !ok {
		return nil, errSlotTaken
	}
	return []uuid.UUID{h.ID}, nil
}

// lockHosts takes each host's booking lock in id order (no deadlocks).
func lockHosts(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	sorted := slices.Clone(ids)
	slices.SortFunc(sorted, compareUUID)
	for _, id := range slices.Compact(sorted) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, jobs.LockKey("calendar.host:"+id.String())); err != nil {
			return err
		}
	}
	return nil
}

// bookingHosts is a booking's co-hosts (collective links).
func bookingCoHosts(ctx context.Context, q db.Querier, bookingID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM calendar_booking_hosts WHERE booking_id = $1`, bookingID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// ---- reassignment -------------------------------------------------------------------

// reassignHandler hands a round-robin booking to another host of its link
// who is free then (the link's maker, a lead of its team, or the current
// host may do it). The event moves to the new host's calendar; the booker
// gets an updated invitation and both hosts are told.
func (s *Service) reassignHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	bid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	b, err := scanBooking(s.d.Pool.QueryRow(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b WHERE b.id = $1`, bid))
	if db.IsNoRows(err) {
		return httpx.NotFound("That booking doesn't exist.")
	}
	if err != nil {
		return err
	}
	l, oldHost, err := s.bookingContext(ctx, s.d.Pool, b)
	if err != nil {
		return err
	}
	if l.ID == uuid.Nil || l.Kind != LinkRoundRobin {
		return httpx.BadRequest("Only round-robin bookings can be handed to another host.")
	}
	if err := s.fillLinkAccess(ctx, s.d.Pool, id.UserID, &l); err != nil {
		return err
	}
	if !l.CanEdit && b.HostID != id.UserID {
		if b.BookerUserID != nil && *b.BookerUserID == id.UserID {
			return httpx.Forbidden("Only the hosts can hand this booking to someone else.")
		}
		return httpx.NotFound("That booking doesn't exist.")
	}
	if b.Status != "confirmed" || !b.StartsAt.After(time.Now()) {
		return httpx.Conflict("Only upcoming bookings can be handed over.")
	}
	if in.UserID == b.HostID {
		return httpx.BadRequest("They already host it.")
	}
	if !slices.ContainsFunc(l.Hosts, func(h LinkHost) bool { return h.User.ID == in.UserID && h.Active }) {
		return httpx.BadRequest("Pick one of this link's hosts.")
	}
	calID, err := s.ensurePrimary(ctx, in.UserID, uuidOr(l.OrgID))
	if err != nil {
		return err
	}
	dur := int(b.EndsAt.Sub(b.StartsAt) / time.Minute)
	only := in.UserID
	target := l
	target.onlyHost = &only
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := lockHosts(ctx, tx, []uuid.UUID{b.HostID, in.UserID}); err != nil {
			return err
		}
		cur, err := scanBooking(tx.QueryRow(ctx, `SELECT `+bookingCols+` FROM calendar_bookings b WHERE b.id = $1 FOR UPDATE`, bid))
		if err != nil {
			return err
		}
		if cur.Status != "confirmed" || cur.HostID != b.HostID {
			return httpx.Conflict("This booking changed. Reload and try again.")
		}
		ids, err := s.assignHosts(withoutConnectedBusy(ctx), tx, target, b.StartsAt, dur, b.ID, uuidOr(b.BookerUserID))
		if err != nil || len(ids) == 0 || ids[0] != in.UserID {
			return httpx.Conflict("They aren't free then.")
		}
		if _, err := tx.Exec(ctx, `UPDATE calendar_bookings SET host_id = $2, assignment = 'reassigned', updated_at = now() WHERE id = $1`, bid, in.UserID); err != nil {
			return err
		}
		if b.EventID != nil {
			if _, err := tx.Exec(ctx, `UPDATE calendar_events SET owner_id = $2, calendar_id = $3, sequence = sequence + 1, updated_at = now() WHERE id = $1`,
				*b.EventID, in.UserID, calID); err != nil {
				return err
			}
		}
		moved := cur
		moved.HostID, moved.Sequence = in.UserID, cur.Sequence+1
		var newHost Person
		if err := tx.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1`, in.UserID).Scan(&newHost.ID, &newHost.DisplayName, &newHost.AvatarURL); err != nil {
			return err
		}
		mt, stable := s.tokenFor("manage", b.ID)
		if !stable {
			if _, err := tx.Exec(ctx, `UPDATE calendar_bookings SET manage_token_hash = $2 WHERE id = $1`, b.ID, hashToken(mt)); err != nil {
				return err
			}
		}
		if err := s.tellBooking(ctx, tx, moved, l, newHost, "new", mt, ""); err != nil {
			return err
		}
		return enqueueNotify(ctx, tx, notify.Notification{UserID: oldHost.ID, Kind: NotifyBooking,
			Title: b.BookerName + "'s booking moved to " + newHost.DisplayName, Body: s.whenFor(ctx, tx, oldHost.ID, b.StartsAt, b.EndsAt, false, zoneOr(l.Timezone, time.UTC)),
			Link: "/calendar/bookings"})
	})
	if err != nil {
		if isExclusionViolation(err) {
			return httpx.Conflict("They aren't free then.")
		}
		return err
	}
	kickOutbox()
	if b.ConferenceID != nil {
		moved := b
		moved.HostID = in.UserID
		l2 := l
		l2.ownerID = in.UserID
		if _, err := s.bookingConference(ctx, l2, moved, b.ConferenceID); err != nil {
			slog.WarnContext(ctx, "calendar: conference host change failed", "err", err)
		}
	}
	s.changed(ctx, "events", b.HostID, in.UserID, uuidOr(b.BookerUserID))
	s.changed(ctx, "bookings", b.HostID, in.UserID, uuidOr(b.BookerUserID))
	b, err = s.loadVisibleBooking(ctx, id.UserID, bid)
	if err != nil {
		httpx.NoContent(w)
		return nil
	}
	v, err := s.bookingView(ctx, b, id.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// bookerGuests validates the guests a booker adds: up to 10 addresses,
// not the booker's own.
func bookerGuests(l BookingLink, in []string, bookerEmail string) ([]string, error) {
	if len(in) == 0 {
		return []string{}, nil
	}
	if !l.AllowGuests {
		return nil, httpx.BadRequest("This link doesn't take guests.")
	}
	if len(in) > 10 {
		return nil, httpx.BadRequest("Up to 10 guests.")
	}
	out := []string{}
	for _, raw := range in {
		e := normalizeEmail(raw)
		if e == "" {
			return nil, httpx.BadRequest(strings.TrimSpace(raw) + " isn't a valid email address.")
		}
		if e != strings.ToLower(bookerEmail) && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out, nil
}

// tellGuests sends a booking's guests (not the booker or hosts) the event's
// invitation, update or cancellation.
func (s *Service) tellGuests(ctx context.Context, q db.Querier, b bookingRow, kind inviteKind) error {
	if b.EventID == nil {
		return nil
	}
	e, err := loadEventRow(ctx, q, *b.EventID)
	if db.IsNoRows(err) || err == errEventNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	atts, err := loadFullAttendees(ctx, q, *b.EventID)
	if err != nil {
		return err
	}
	var guests []fullAttendee
	for _, a := range atts {
		if a.external() && slices.Contains(b.Guests, strings.ToLower(a.Email)) {
			guests = append(guests, a)
		}
	}
	return s.notifyAttendees(ctx, q, e, nil, guests, kind)
}

// tellCoHosts tells a collective booking's other hosts in-app.
func (s *Service) tellCoHosts(ctx context.Context, q db.Querier, b bookingRow, l BookingLink, what string) error {
	ids, err := bookingCoHosts(ctx, q, b.ID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		n := notify.Notification{UserID: id, Kind: NotifyBooking, Body: s.whenFor(ctx, q, id, b.StartsAt, b.EndsAt, false, zoneOr(l.Timezone, time.UTC)), Link: "/calendar/bookings"}
		switch what {
		case "new":
			n.Title = b.BookerName + " booked " + b.Title
		case "rescheduled":
			n.Title = b.BookerName + " moved " + b.Title
		default:
			n.Title = b.Title + " with " + b.BookerName + " is cancelled"
		}
		if err := enqueueNotify(ctx, q, n); err != nil {
			return err
		}
		if what == "new" {
			// Hosts get the invite in email too, like the organizing host.
			var email string
			if err := q.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, id).Scan(&email); err == nil && email != "" {
				text, html := s.mailBody([]string{n.Title + ".", "When: " + n.Body, "Email: " + b.BookerEmail}, nil)
				if err := enqueueMail(ctx, q, mail.Message{To: email, Subject: "New booking: " + b.BookerName + " @ " + n.Body, Text: text, HTML: html, ReplyTo: b.BookerEmail}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// errLinkUsed answers a single-use link that already took its booking.
var errLinkUsed = httpx.NewError(http.StatusGone, "link_used", "This one-time link was already used.")
