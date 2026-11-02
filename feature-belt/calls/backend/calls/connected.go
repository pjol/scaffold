package calls

// Meeting providers beside the app's own calls: Zoom meetings (Zoom API) and
// Microsoft Teams meetings (Graph online meetings), made with the host's
// own connection (app.Connections, the connectors feature) for scheduling
// features (the calendar's "Video call" through its conferencing seam).
// Google Meet links come from Google Calendar events, so the calendar makes
// those itself. Nothing from the platforms is stored here: a meeting made
// on a platform is named by an opaque id ("zoom:<host>:<connection>:<id>")
// the scheduling feature keeps, and Calls lists meetings on connected
// calendars through SetCalendarMeetings (the app's stitching).
//
// Whitelabel honestly: the app's own calls run embedded; Zoom, Meet and
// Teams meetings open in their own apps or web clients.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Capabilities calls declares (app.CapabilityProvider).
const (
	CapZoomMeetings = "calls.zoom.meetings"
	CapTeams        = "calls.microsoft.teams"

	// Provider ids, shared with the calendar's conferencing options.
	ProviderApp        = "app"
	ProviderZoom       = "zoom"
	ProviderTeams      = "teams"
	ProviderGoogleMeet = "google_meet"
)

// Capabilities implements app.CapabilityProvider: Zoom's granular meeting
// scopes and Graph's OnlineMeetings.ReadWrite (the account's own meetings).
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapZoomMeetings, Module: "calls", Provider: "zoom", Label: "Zoom meetings for calendar events and bookings", Write: true,
			Scopes: []string{"meeting:write:meeting", "meeting:update:meeting", "meeting:delete:meeting"}},
		{Key: CapTeams, Module: "calls", Provider: "microsoft", Label: "Microsoft Teams meetings for calendar events and bookings", Write: true,
			Scopes: []string{"OnlineMeetings.ReadWrite"}},
	}
}

// MeetingProvider is one way to hold a video meeting, for one person.
type MeetingProvider struct {
	ID           string `json:"id"`                      // app | zoom | teams | google_meet
	Label        string `json:"label"`                   // "Video call", "Zoom", "Microsoft Teams"
	Platform     string `json:"platform,omitempty"`      // connections registry id; "" for the app's own
	ConnectionID string `json:"connection_id,omitempty"` // the account it uses
	Account      string `json:"account,omitempty"`
	Status       string `json:"status,omitempty"` // active | needs_reauth | blocked
	FixURL       string `json:"fix_url,omitempty"`
	Embedded     bool   `json:"embedded"` // runs inside the app (the app's own); others open the platform's app
	// Via says where the provider lives when Calls doesn't make it itself
	// ("calendar": Google Meet on Google Calendar events).
	Via string `json:"via,omitempty"`
}

// MeetingProviders is GET /v1/calls/providers.
type MeetingProviders struct {
	Providers []MeetingProvider     `json:"providers"`
	Offers    []app.ConnectionOffer `json:"offers"`
}

var providerCaps = map[string]string{ProviderZoom: CapZoomMeetings, ProviderTeams: CapTeams}
var providerLabels = map[string]string{ProviderZoom: "Zoom", ProviderTeams: "Microsoft Teams", ProviderGoogleMeet: "Google Meet"}

// ProviderLabel is what people call a meeting provider.
func ProviderLabel(id string) string {
	if l, ok := providerLabels[id]; ok {
		return l
	}
	return "Video call"
}

// Providers lists the meeting providers open to userID in orgID (uuid.Nil
// = no workspace): the app's own first when media is configured, then each
// connected Zoom or Teams account (including ones that need reconnecting,
// so screens can say so).
func Providers(ctx context.Context, userID, orgID uuid.UUID) ([]MeetingProvider, error) {
	s := current.Load()
	if s == nil {
		return nil, errNotBuilt
	}
	return s.providers(ctx, userID, orgID)
}

func (s *Service) providers(ctx context.Context, userID, orgID uuid.UUID) ([]MeetingProvider, error) {
	out := []MeetingProvider{}
	if s.media != nil {
		out = append(out, MeetingProvider{ID: ProviderApp, Label: "Video call", Embedded: true, Status: "active"})
	}
	cx := app.FindConnections(s.d.Modules)
	if cx == nil {
		return out, nil
	}
	for _, id := range []string{ProviderZoom, ProviderTeams} {
		list, err := cx.For(ctx, userID, orgID, providerCaps[id])
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			p := MeetingProvider{ID: id, Label: providerLabels[id], Platform: c.Provider, ConnectionID: c.ID, Account: c.Account, Status: c.Status}
			if p.Status == "" {
				p.Status = "active"
			}
			if p.Status == "needs_reauth" {
				p.FixURL = "/settings/connections?capability=" + url.QueryEscape(providerCaps[id])
			}
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Service) providersHandler(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r)
	if err != nil {
		return err
	}
	list, err := s.providers(r.Context(), me.UserID, org)
	if err != nil {
		return err
	}
	out := MeetingProviders{Providers: list, Offers: []app.ConnectionOffer{}}
	if cx := app.FindConnections(s.d.Modules); cx != nil {
		if offers, err := cx.Offers(r.Context(), me.UserID, org, s.Name()); err == nil && offers != nil {
			out.Offers = offers
		}
	}
	httpx.OK(w, out)
	return nil
}

// orgParam reads ?org_id= (optional) and checks the caller belongs to it.
func (s *Service) orgParam(r *http.Request) (uuid.UUID, error) {
	raw := r.URL.Query().Get("org_id")
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.BadRequest("org_id must be an id.")
	}
	me := auth.MustFromContext(r.Context())
	var member bool
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, id, me.UserID).Scan(&member); err != nil {
		return uuid.Nil, err
	}
	if !member {
		return uuid.Nil, httpx.NotFound("Not found.")
	}
	return id, nil
}

// ---- meetings on platforms ------------------------------------------------------

// ProviderMeetingInput describes a meeting to make on a platform.
type ProviderMeetingInput struct {
	HostID       uuid.UUID
	Provider     string // ProviderZoom | ProviderTeams
	ConnectionID string
	OrgID        uuid.UUID // workspace whose connection policy applies (uuid.Nil = none)
	Title        string
	StartsAt     time.Time
	EndsAt       time.Time
}

// ProviderMeeting is a meeting a platform made. ID is opaque: pass it back
// to UpdateProviderMeeting and CancelProviderMeeting.
type ProviderMeeting struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	JoinURL  string `json:"join_url"`
}

// IsProviderMeetingID reports whether id names a platform meeting (as
// opposed to the app's own meeting id).
func IsProviderMeetingID(id string) bool {
	return strings.HasPrefix(id, ProviderZoom+":") || strings.HasPrefix(id, ProviderTeams+":")
}

type providerRef struct {
	provider, connID, meetingID string
	host                        uuid.UUID
}

func (r providerRef) String() string {
	return r.provider + ":" + r.host.String() + ":" + r.connID + ":" + r.meetingID
}

func parseProviderRef(id string) (providerRef, error) {
	parts := strings.SplitN(id, ":", 4)
	if len(parts) != 4 || providerCaps[parts[0]] == "" {
		return providerRef{}, fmt.Errorf("calls: not a platform meeting id: %q", id)
	}
	host, err := uuid.Parse(parts[1])
	if err != nil {
		return providerRef{}, fmt.Errorf("calls: platform meeting id: %w", err)
	}
	return providerRef{provider: parts[0], host: host, connID: parts[2], meetingID: parts[3]}, nil
}

// meetingPlatform is one provider's meeting API.
type meetingPlatform interface {
	create(ctx context.Context, title string, start, end time.Time) (id, joinURL string, err error)
	update(ctx context.Context, id, title string, start, end time.Time) error
	cancel(ctx context.Context, id string) error
}

// platformFor opens the host's connection for a provider.
func (s *Service) platformFor(ctx context.Context, host uuid.UUID, org uuid.UUID, provider, connID string) (meetingPlatform, error) {
	cx := app.FindConnections(s.d.Modules)
	if cx == nil {
		return nil, httpx.NewError(http.StatusServiceUnavailable, app.ConnectionUnavailable, "Connected accounts aren't available in this app.")
	}
	capability := providerCaps[provider]
	if capability == "" {
		return nil, httpx.BadRequest("Meetings can be made on Zoom or Microsoft Teams.")
	}
	if connID == "" {
		list, err := cx.For(ctx, host, org, capability)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			if c.Status == "" || c.Status == "active" {
				connID = c.ID
				break
			}
		}
		if connID == "" {
			e := httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing, "Connect "+providerLabels[provider]+" first.")
			e.Details = map[string]any{"fix_url": "/settings/connections?capability=" + url.QueryEscape(capability), "capability": capability}
			return nil, e
		}
	}
	if org != uuid.Nil {
		ctx = app.WithConnectionOrg(ctx, org)
	}
	client, conn, err := cx.Client(ctx, host, connID, capability)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) && (he.Code == app.ConnectionNeedsAuth || he.Code == app.ConnectionScopeMissing) {
			out := *he
			out.Details = map[string]any{"fix_url": "/settings/connections?capability=" + url.QueryEscape(capability), "capability": capability, "connection_id": connID}
			return nil, &out
		}
		return nil, err
	}
	switch provider {
	case ProviderZoom:
		base := conn.Endpoints["api"]
		if base == "" {
			base = "https://api.zoom.us/v2"
		}
		return &zoomMeetings{c: meetingClient{client: client, name: "Zoom"}, base: strings.TrimRight(base, "/"), connID: conn.ID}, nil
	default:
		base := conn.Endpoints["graph"]
		if base == "" {
			base = conn.Endpoints["api"]
		}
		if base == "" {
			base = "https://graph.microsoft.com/v1.0"
		}
		return &teamsMeetings{c: meetingClient{client: client, name: "Microsoft Teams"}, base: strings.TrimRight(base, "/"), connID: conn.ID}, nil
	}
}

// CreateProviderMeeting makes a meeting on Zoom or Teams with the host's
// connection (the first active one when ConnectionID is empty).
func CreateProviderMeeting(ctx context.Context, in ProviderMeetingInput) (ProviderMeeting, error) {
	s := current.Load()
	if s == nil {
		return ProviderMeeting{}, errNotBuilt
	}
	if !in.EndsAt.After(in.StartsAt) {
		in.EndsAt = in.StartsAt.Add(30 * time.Minute)
	}
	p, err := s.platformFor(ctx, in.HostID, in.OrgID, in.Provider, in.ConnectionID)
	if err != nil {
		return ProviderMeeting{}, err
	}
	id, join, err := p.create(ctx, strings.TrimSpace(in.Title), in.StartsAt, in.EndsAt)
	if err != nil {
		return ProviderMeeting{}, err
	}
	connID := in.ConnectionID
	switch v := p.(type) {
	case *zoomMeetings:
		connID = v.connID
	case *teamsMeetings:
		connID = v.connID
	}
	ref := providerRef{provider: in.Provider, host: in.HostID, connID: connID, meetingID: id}
	return ProviderMeeting{ID: ref.String(), Provider: in.Provider, JoinURL: join}, nil
}

// UpdateProviderMeeting moves or renames a platform meeting.
func UpdateProviderMeeting(ctx context.Context, id, title string, start, end time.Time) (ProviderMeeting, error) {
	s := current.Load()
	if s == nil {
		return ProviderMeeting{}, errNotBuilt
	}
	ref, err := parseProviderRef(id)
	if err != nil {
		return ProviderMeeting{}, err
	}
	p, err := s.platformFor(ctx, ref.host, uuid.Nil, ref.provider, ref.connID)
	if err != nil {
		return ProviderMeeting{}, err
	}
	if err := p.update(ctx, ref.meetingID, strings.TrimSpace(title), start, end); err != nil {
		return ProviderMeeting{}, err
	}
	return ProviderMeeting{ID: id, Provider: ref.provider}, nil
}

// CancelProviderMeeting deletes a platform meeting. One that's already gone,
// or whose connection is, is not an error (there is nothing left to do).
func CancelProviderMeeting(ctx context.Context, id string) error {
	s := current.Load()
	if s == nil {
		return errNotBuilt
	}
	ref, err := parseProviderRef(id)
	if err != nil {
		return nil
	}
	p, err := s.platformFor(ctx, ref.host, uuid.Nil, ref.provider, ref.connID)
	if err != nil {
		switch app.ConnectionErrorCode(err) {
		case app.ConnectionUnavailable, app.ConnectionRateLimited:
			return err
		}
		return nil
	}
	return p.cancel(ctx, ref.meetingID)
}

// ---- platform clients ---------------------------------------------------------

type meetingClient struct {
	client *http.Client
	name   string
}

var errMeetingGone = errors.New("calls: platform meeting gone")

func (m meetingClient) call(ctx context.Context, method, rawURL string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = strings.NewReader(string(b))
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.client.Do(req)
	if err != nil {
		var he *httpx.Error
		if errors.As(err, &he) {
			return he
		}
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Couldn't reach "+m.name+". Try again.")
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return errMeetingGone
	case res.StatusCode == http.StatusUnauthorized:
		return httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+m.name+" to continue.")
	case res.StatusCode == http.StatusTooManyRequests:
		return httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, m.name+" is limiting requests. Try again in a minute.")
	case res.StatusCode == http.StatusForbidden:
		return httpx.NewError(http.StatusForbidden, "connection_forbidden", m.name+" didn't allow that for this account.")
	case res.StatusCode >= 500:
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, m.name+" had a problem. Try again.")
	case res.StatusCode >= 300:
		var e struct {
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Message
		if msg == "" {
			msg = e.Error.Message
		}
		if msg == "" || len(msg) > 200 {
			msg = "the meeting wasn't accepted"
		}
		return httpx.NewError(http.StatusBadRequest, "connection_rejected", m.name+": "+msg)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, m.name+" sent an answer this app couldn't read.")
		}
	}
	return nil
}

// zoomMeetings: POST /users/me/meetings (scheduled, type 2), PATCH and
// DELETE /meetings/{id}. Waiting room on, so guests are let in by the host.
type zoomMeetings struct {
	c      meetingClient
	base   string
	connID string
}

func zoomBody(title string, start, end time.Time) map[string]any {
	if title == "" {
		title = "Meeting"
	}
	mins := int(end.Sub(start) / time.Minute)
	if mins < 1 {
		mins = 30
	}
	return map[string]any{"topic": title, "type": 2, "start_time": start.UTC().Format("2006-01-02T15:04:05Z"), "duration": mins,
		"timezone": "UTC", "settings": map[string]any{"waiting_room": true, "join_before_host": false}}
}

func (z *zoomMeetings) create(ctx context.Context, title string, start, end time.Time) (string, string, error) {
	var res struct {
		ID      json.Number `json:"id"`
		JoinURL string      `json:"join_url"`
	}
	if err := z.c.call(ctx, http.MethodPost, z.base+"/users/me/meetings", zoomBody(title, start, end), &res); err != nil {
		return "", "", err
	}
	if res.ID == "" || res.JoinURL == "" {
		return "", "", httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Zoom didn't return a meeting link.")
	}
	return res.ID.String(), res.JoinURL, nil
}

func (z *zoomMeetings) update(ctx context.Context, id, title string, start, end time.Time) error {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return httpx.BadRequest("Not a Zoom meeting id.")
	}
	return z.c.call(ctx, http.MethodPatch, z.base+"/meetings/"+id, zoomBody(title, start, end), nil)
}

func (z *zoomMeetings) cancel(ctx context.Context, id string) error {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return nil
	}
	err := z.c.call(ctx, http.MethodDelete, z.base+"/meetings/"+id, nil, nil)
	if errors.Is(err, errMeetingGone) {
		return nil
	}
	return err
}

// teamsMeetings: Graph /me/onlineMeetings (create, PATCH, DELETE).
type teamsMeetings struct {
	c      meetingClient
	base   string
	connID string
}

func teamsBody(title string, start, end time.Time) map[string]any {
	if title == "" {
		title = "Meeting"
	}
	return map[string]any{"subject": title, "startDateTime": start.UTC().Format(time.RFC3339), "endDateTime": end.UTC().Format(time.RFC3339)}
}

func (t *teamsMeetings) create(ctx context.Context, title string, start, end time.Time) (string, string, error) {
	var res struct {
		ID         string `json:"id"`
		JoinWebURL string `json:"joinWebUrl"`
	}
	if err := t.c.call(ctx, http.MethodPost, t.base+"/me/onlineMeetings", teamsBody(title, start, end), &res); err != nil {
		return "", "", err
	}
	if res.ID == "" || res.JoinWebURL == "" {
		return "", "", httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Microsoft Teams didn't return a meeting link.")
	}
	return res.ID, res.JoinWebURL, nil
}

func (t *teamsMeetings) update(ctx context.Context, id, title string, start, end time.Time) error {
	return t.c.call(ctx, http.MethodPatch, t.base+"/me/onlineMeetings/"+url.PathEscape(id), teamsBody(title, start, end), nil)
}

func (t *teamsMeetings) cancel(ctx context.Context, id string) error {
	err := t.c.call(ctx, http.MethodDelete, t.base+"/me/onlineMeetings/"+url.PathEscape(id), nil, nil)
	if errors.Is(err, errMeetingGone) {
		return nil
	}
	return err
}

// ---- meetings on connected calendars --------------------------------------------

// CalendarMeeting is a meeting with a platform link (Meet, Teams, Zoom) on
// one of the person's connected calendars, as Calls lists it beside its own.
type CalendarMeeting struct {
	ID         string         `json:"id"` // the calendar's reference for the event
	Title      string         `json:"title"`
	StartsAt   time.Time      `json:"starts_at"`
	EndsAt     time.Time      `json:"ends_at"`
	AllDay     bool           `json:"all_day"`
	Provider   string         `json:"provider"` // google_meet | teams | zoom
	JoinURL    string         `json:"join_url"`
	Organizer  string         `json:"organizer"`
	MyResponse string         `json:"my_response,omitempty"`
	Link       string         `json:"link,omitempty"` // app path of the event (the calendar)
	Source     *app.SourceRef `json:"source"`
}

// CalendarMeetingsFunc lists userID's meetings on connected calendars
// overlapping [from, to), as caller.
type CalendarMeetingsFunc func(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, from, to time.Time) ([]CalendarMeeting, error)

var calendarMeetings atomic.Pointer[CalendarMeetingsFunc]

// SetCalendarMeetings wires where Calls finds meetings on people's connected
// calendars (the calendar feature, in the app's stitching). Without it Calls
// lists only its own meetings.
func SetCalendarMeetings(fn CalendarMeetingsFunc) {
	if fn == nil {
		calendarMeetings.Store(nil)
		return
	}
	calendarMeetings.Store(&fn)
}

// CalendarMeetingsResult is GET /v1/calls/external.
type CalendarMeetingsResult struct {
	Items     []CalendarMeeting `json:"items"`
	Available bool              `json:"available"` // a calendar is wired
}

// externalHandler: GET /calls/external?when=upcoming|recent → meetings with
// platform links on the caller's connected calendars (next 14 days, or the
// last 14), soonest (or latest) first.
func (s *Service) externalHandler(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r)
	if err != nil {
		return err
	}
	out := CalendarMeetingsResult{Items: []CalendarMeeting{}}
	fn := calendarMeetings.Load()
	if fn == nil {
		httpx.OK(w, out)
		return nil
	}
	out.Available = true
	now := time.Now().UTC()
	from, to := now, now.AddDate(0, 0, 14)
	recent := r.URL.Query().Get("when") == "recent"
	if recent {
		from, to = now.AddDate(0, 0, -14), now
	}
	list, err := (*fn)(r.Context(), me, org, from, to)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.JoinURL == "" || m.Provider == "" || m.Provider == ProviderApp {
			continue
		}
		if recent && m.EndsAt.After(now) || !recent && !m.EndsAt.After(now) {
			continue
		}
		out.Items = append(out.Items, m)
	}
	if recent {
		for i, j := 0, len(out.Items)-1; i < j; i, j = i+1, j-1 {
			out.Items[i], out.Items[j] = out.Items[j], out.Items[i]
		}
	}
	httpx.OK(w, out)
	return nil
}
