package calendar

// Connected calendars: Google Calendar and Outlook (Microsoft Graph) shown
// in the person's own calendar views, read and changed live with their own
// connection (app.Connections, the connectors feature). Nothing from a
// platform is stored here except what the person sets or creates: their
// colour, visibility and availability choices per connected calendar
// (calendar_connected_calendars), the platform copy of a booking
// (calendar_connected_mirrors) and the room made for a connected event
// (calendar_connected_conferences) — references, never content. Events are
// kept briefly in memory per person and connection (connected_cache.go)
// and brought up to date incrementally (Google syncToken, Graph delta).
//
// Rules for every connected route and tool:
//   - A connection is used only by the person it belongs to: every call goes
//     through Connections.Client with the caller's id. Booking pages read the
//     host's free/busy (times only) through the host's connection, as they
//     read the host's own calendar.
//   - Reads need the platform's read capability, changes its write
//     capability (asked for separately, incrementally).
//   - Agents are capped by the calendar scope and the connection's own scope
//     (app.ConnectionScope); writes are separate tools with Source set.
//   - Platform errors pass on with their code and fix link, never as a 401.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Capabilities calendar declares (app.CapabilityProvider). Reading covers
// calendars, events and free/busy; writing adds creating, changing and
// deleting events, RSVP and meeting links on them.
const (
	CapGoogleRead     = "calendar.google.read"
	CapGoogleWrite    = "calendar.google.write"
	CapMicrosoftRead  = "calendar.microsoft.read"
	CapMicrosoftWrite = "calendar.microsoft.write"

	// KindExternal is the agent-scope kind of an event or calendar on a
	// connected platform; its id is "<connection id>:<calendar>:<event>"
	// (externalTarget).
	KindExternal = "calendar.external"

	platformGoogle    = "google"
	platformMicrosoft = "microsoft"
	productCalendar   = "calendar" // shared SOURCE_PRODUCT_LABELS: google.calendar, microsoft.calendar
)

var (
	calReadCaps  = []string{CapGoogleRead, CapMicrosoftRead}
	calWriteCaps = map[string]string{CapGoogleRead: CapGoogleWrite, CapMicrosoftRead: CapMicrosoftWrite}
)

// Capabilities implements app.CapabilityProvider with the narrowest scopes
// that work: Google calendar.readonly lists calendars, events and free/busy
// and calendar.events changes events (RSVP and Meet links included);
// Microsoft Calendars.Read reads events and getSchedule, Calendars.ReadWrite
// changes them (Teams links through isOnlineMeeting included).
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapGoogleRead, Module: "calendar", Provider: platformGoogle, Label: "Google Calendar in Calendar",
			Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly"}},
		{Key: CapGoogleWrite, Module: "calendar", Provider: platformGoogle, Label: "Change Google Calendar events from Calendar", Write: true,
			Scopes: []string{"https://www.googleapis.com/auth/calendar.readonly", "https://www.googleapis.com/auth/calendar.events"}},
		{Key: CapMicrosoftRead, Module: "calendar", Provider: platformMicrosoft, Label: "Outlook Calendar in Calendar",
			Scopes: []string{"Calendars.Read"}},
		{Key: CapMicrosoftWrite, Module: "calendar", Provider: platformMicrosoft, Label: "Change Outlook Calendar events from Calendar", Write: true,
			Scopes: []string{"Calendars.ReadWrite"}},
	}
}

// productLabel is what people call a platform's calendar.
func productLabel(platform string) string {
	if platform == platformMicrosoft {
		return "Outlook Calendar"
	}
	return "Google Calendar"
}

// ---- connections ------------------------------------------------------------

// calConn is one of the caller's connections as calendar sees it.
type calConn struct {
	app.Connection
	readCap  string
	writeCap string // granted write capability ("" = read only)
}

func (c *calConn) label() string { return productLabel(c.Provider) + " · " + c.Account }

func (c *calConn) source(url string) *app.SourceRef {
	return app.ConnectedSource(c.Connection, productCalendar, url)
}

func (s *Service) connections() app.Connections { return app.FindConnections(s.d.Modules) }

// connectedFor lists userID's connections that grant a calendar read
// capability in org (uuid.Nil: no workspace), Google first, then by account.
func (s *Service) connectedFor(ctx context.Context, userID, org uuid.UUID) ([]*calConn, error) {
	cx := s.connections()
	if cx == nil {
		return nil, nil
	}
	byID := map[string]*calConn{}
	var out []*calConn
	for _, rc := range calReadCaps {
		list, err := cx.For(ctx, userID, org, rc)
		if err != nil {
			return nil, err
		}
		for _, cn := range list {
			if byID[cn.ID] != nil {
				continue
			}
			c := &calConn{Connection: cn, readCap: rc}
			byID[cn.ID] = c
			out = append(out, c)
		}
		wl, err := cx.For(ctx, userID, org, calWriteCaps[rc])
		if err != nil {
			return nil, err
		}
		for _, cn := range wl {
			if c := byID[cn.ID]; c != nil && cn.Status == "active" {
				c.writeCap = calWriteCaps[rc]
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return strings.ToLower(out[i].Account) < strings.ToLower(out[j].Account)
	})
	return out, nil
}

// calSession is an authenticated adapter for one connection and request.
type calSession struct {
	conn  *calConn
	ad    calAdapter
	user  uuid.UUID
	write bool
}

var (
	errNoConnections  = httpx.NewError(http.StatusServiceUnavailable, app.ConnectionUnavailable, "Connected accounts aren't available in this app.")
	errConnectionGone = httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connected calendar isn't available. Connect it again in Settings → Connections.")
)

// session opens connID for userID with the read capability, or the write one
// (write). It refuses blocked, read-only and lapsed connections with a fix.
func (s *Service) session(ctx context.Context, userID, org uuid.UUID, connID string, write bool) (*calSession, error) {
	cx := s.connections()
	if cx == nil {
		return nil, errNoConnections
	}
	all, err := s.connectedFor(ctx, userID, org)
	if err != nil {
		return nil, err
	}
	var c *calConn
	for _, x := range all {
		if x.ID == connID {
			c = x
		}
	}
	if c == nil {
		return nil, errConnectionGone
	}
	return s.openConn(ctx, userID, org, c, write)
}

// openConn is session for a connection already resolved by connectedFor.
func (s *Service) openConn(ctx context.Context, userID, org uuid.UUID, c *calConn, write bool) (*calSession, error) {
	cx := s.connections()
	if cx == nil {
		return nil, errNoConnections
	}
	switch c.Status {
	case "blocked":
		return nil, httpx.NewError(http.StatusForbidden, app.ConnectionBlocked, "Your "+orgs.Noun()+" doesn't allow "+productLabel(c.Provider)+" here.")
	case "needs_reauth":
		return nil, s.needsAuth(c, c.readCap)
	}
	capability := c.readCap
	if write {
		if c.writeCap == "" {
			e := httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing, "Allow changes to "+productLabel(c.Provider)+" first: this connection can only read.")
			e.Details = map[string]any{"fix_url": s.grantURL(ctx, userID, org, calWriteCaps[c.readCap]), "capability": calWriteCaps[c.readCap], "connection_id": c.ID}
			return nil, e
		}
		capability = c.writeCap
	}
	if org != uuid.Nil {
		ctx = app.WithConnectionOrg(ctx, org)
	}
	client, cn, err := cx.Client(ctx, userID, c.ID, capability)
	if err != nil {
		return nil, s.connError(c, capability, err)
	}
	cc := *c
	cc.Endpoints = cn.Endpoints
	c = &cc
	var ad calAdapter
	switch c.Provider {
	case platformGoogle:
		ad = newGoogleCalendar(c, client)
	case platformMicrosoft:
		ad = newGraphCalendar(c, client)
	default:
		return nil, httpx.BadRequest("Calendar can't open " + c.Provider + " accounts.")
	}
	return &calSession{conn: c, ad: ad, user: userID, write: write}, nil
}

func (s *Service) needsAuth(c *calConn, capability string) error {
	e := httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+c.label()+" to continue.")
	e.Details = map[string]any{"fix_url": connectURL(capability, "/calendar"), "capability": capability, "connection_id": c.ID}
	return e
}

// connError passes a Connections or platform failure on with its code (never
// a 401: that signs people out) and the link that fixes it.
func (s *Service) connError(c *calConn, capability string, err error) error {
	var he *httpx.Error
	if !errors.As(err, &he) {
		return err
	}
	out := *he
	if out.Status == http.StatusUnauthorized {
		out.Status, out.Code = http.StatusConflict, app.ConnectionNeedsAuth
	}
	switch out.Code {
	case app.ConnectionNeedsAuth, app.ConnectionScopeMissing:
		d := map[string]any{"fix_url": connectURL(capability, "/calendar"), "capability": capability, "connection_id": c.ID}
		for k, v := range he.Details {
			d[k] = v
		}
		out.Details = d
	}
	return &out
}

// connectURL is the connections screen for a capability (TS:
// connectionFixPath), coming back to returnTo.
func connectURL(capability, returnTo string) string {
	q := url.Values{}
	if capability != "" {
		q.Set("capability", capability)
	}
	if strings.HasPrefix(returnTo, "/") && !strings.HasPrefix(returnTo, "//") {
		q.Set("return_to", returnTo)
	}
	if len(q) == 0 {
		return "/settings/connections"
	}
	return "/settings/connections?" + q.Encode()
}

// grantURL is where a person grants capability: the connectors feature's
// offer when it has one, else the connections screen.
func (s *Service) grantURL(ctx context.Context, userID, org uuid.UUID, capability string) string {
	if cx := s.connections(); cx != nil {
		if offers, err := cx.Offers(ctx, userID, org, s.Name()); err == nil {
			for _, o := range offers {
				if o.Capability == capability && o.ConnectURL != "" {
					return o.ConnectURL
				}
			}
		}
	}
	return connectURL(capability, "/calendar")
}

// orgParam reads ?org_id= (optional; the workspace whose connection policy
// applies) and checks the caller belongs to it.
func (s *Service) orgParam(ctx context.Context, r *http.Request, userID uuid.UUID) (uuid.UUID, error) {
	raw := r.URL.Query().Get("org_id")
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	var member bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, id, userID).Scan(&member); err != nil {
		return uuid.Nil, err
	}
	if !member {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return id, nil
}

// ---- agent scope ----------------------------------------------------------------

// externalTarget is the agent-scope id of a connected calendar or event,
// app.ExternalTarget(connection, "<calendar>[:<event>]") — so the agent gate
// also applies the connection's own level — within the agents feature's id rules
// ([A-Za-z0-9_.:/-], up to 200). Other characters become "_" (calendar ids
// are addresses: sam@acme.com → sam_acme.com); a part that would overflow is
// replaced by a hash of itself, so long Graph ids stay distinct.
func externalTarget(connID, calID, eventID string) string {
	clean := func(v string) string {
		var b strings.Builder
		for _, r := range v {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-', r == '/':
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
		return b.String()
	}
	cal, ev := clean(calID), clean(eventID)
	join := func() string {
		if eventID == "" {
			return app.ExternalTarget(connID, cal)
		}
		return app.ExternalTarget(connID, cal+":"+ev)
	}
	if len(join()) > 200 && len(cal) > 24 {
		cal = "h" + hashToken(calID)[:20]
	}
	if len(join()) > 200 {
		ev = "h" + hashToken(eventID)[:32]
	}
	out := join()
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// agentAllows checks the connection's own agent scope for one calendar or
// event (true for people, and without an agent policy). The refusal names
// the level the person set, so the agent knows what to ask for.
func (s *Service) agentAllows(ctx context.Context, caller *auth.Identity, connID, calID, eventID string, need app.AgentLevel) error {
	if _, ok := auth.AgentFrom(ctx); !ok {
		return nil
	}
	policy := app.FindAgentPolicy(s.d.Modules)
	if policy == nil {
		return nil
	}
	lvl, err := policy.ResourceLevel(ctx, caller, app.ConnectionScope(connID), KindExternal, externalTarget(connID, calID, eventID))
	if err != nil {
		return err
	}
	if lvl >= need {
		return nil
	}
	switch lvl {
	case app.AgentRead:
		return httpx.NewError(http.StatusForbidden, "agent_scope_read_only", "The person lets AI agents read this connected calendar but not change it. They can allow suggestions or edits in Settings → AI agents.")
	case app.AgentSuggest:
		return httpx.NewError(http.StatusForbidden, "agent_scope_suggest_only", "The person lets AI agents only suggest changes in this connected calendar, and Calendar runs agent changes directly. Ask them to make the change, or to allow edits for this account in Settings → AI agents.")
	}
	return httpx.NewError(http.StatusForbidden, "agent_scope_off", "The person hasn't given AI agents access to this connected calendar. They can change it in Settings → AI agents.")
}

// agentConnReadable filters connections an agent call may read at all (the
// connection's feature level); people and policy-less apps see all.
func (s *Service) agentConnReadable(ctx context.Context, caller *auth.Identity, conns []*calConn) ([]*calConn, error) {
	if _, ok := auth.AgentFrom(ctx); !ok {
		return conns, nil
	}
	policy := app.FindAgentPolicy(s.d.Modules)
	if policy == nil {
		return conns, nil
	}
	out := conns[:0:0]
	for _, c := range conns {
		_, top, err := policy.FeatureLevel(ctx, caller, app.ConnectionScope(c.ID))
		if err != nil {
			return nil, err
		}
		if top >= app.AgentRead {
			out = append(out, c)
		}
	}
	return out, nil
}

// agentEventsReadable drops events of one connection an agent may not read.
func (s *Service) agentEventsReadable(ctx context.Context, caller *auth.Identity, connID string, items []ConnectedEvent) ([]ConnectedEvent, error) {
	if _, ok := auth.AgentFrom(ctx); !ok || len(items) == 0 {
		return items, nil
	}
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = externalTarget(connID, it.CalendarID, it.ExternalID)
	}
	ok, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(connID), KindExternal, ids)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for i, it := range items {
		if ok[ids[i]] {
			out = append(out, it)
		}
	}
	return out, nil
}

// ---- shapes ------------------------------------------------------------------

// ConnectedCalendar is one calendar of a connected account, with the
// person's choices for it.
type ConnectedCalendar struct {
	ID         string `json:"id"` // the platform's calendar id
	Name       string `json:"name"`
	Color      string `json:"color"`  // a calendar colour token
	Hidden     bool   `json:"hidden"` // left out of the views
	Busy       bool   `json:"busy"`   // its busy time counts for booking links and find-time
	Primary    bool   `json:"primary"`
	CanWrite   bool   `json:"can_write"` // the account may add events and the connection allows changes
	AccessRole string `json:"access_role"`
	Timezone   string `json:"timezone"`
	// Conferencing lists the meeting links the platform makes on this
	// calendar's events (Google Meet, Teams), when it can write.
	Conferencing []ConferenceOption `json:"conferencing"`
}

// CalendarSource is one place Calendar shows events from (shared
// FeatureSource plus write status and calendars).
type CalendarSource struct {
	ID         string              `json:"id"`
	Type       string              `json:"type"` // app | connected
	Provider   string              `json:"provider,omitempty"`
	Product    string              `json:"product,omitempty"`
	Account    string              `json:"account,omitempty"`
	Label      string              `json:"label"`
	Status     string              `json:"status,omitempty"` // active | needs_reauth | blocked
	Capability string              `json:"capability,omitempty"`
	FixURL     string              `json:"fix_url,omitempty"`
	CanWrite   bool                `json:"can_write"`
	WriteURL   string              `json:"write_url,omitempty"` // grants changes when CanWrite is false
	Calendars  []ConnectedCalendar `json:"calendars"`
	Error      *SourceError        `json:"error,omitempty"`
}

// SourceError says why one source (or one of its calendars) didn't load.
type SourceError struct {
	SourceID   string `json:"source_id"`
	CalendarID string `json:"calendar_id,omitempty"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	FixURL     string `json:"fix_url,omitempty"`
}

// CalendarSources is GET /v1/calendar/sources (shared FeatureSources plus
// the meeting providers the app's own events can use).
type CalendarSources struct {
	Sources      []CalendarSource      `json:"sources"`
	Offers       []app.ConnectionOffer `json:"offers"`
	Conferencing []ConferenceOption    `json:"conferencing"`
}

// sourceError turns a failure into a SourceError (code and fix link kept).
func sourceError(sourceID, calID string, err error) *SourceError {
	out := &SourceError{SourceID: sourceID, CalendarID: calID, Code: "connection_unavailable", Message: "Couldn't reach the calendar. Try again."}
	var he *httpx.Error
	if errors.As(err, &he) {
		out.Code, out.Message = he.Code, he.Message
		if f, ok := he.Details["fix_url"].(string); ok {
			out.FixURL = f
		}
	}
	return out
}

// defaultColors are handed to connected calendars in order (the app's own
// calendar uses primary).
var defaultColors = []string{"info", "accent", "success", "warning", "danger", "neutral"}

// sourcesFor builds the caller's sources: the app's own calendar, then each
// connection with its calendars (listed live, cached briefly).
func (s *Service) sourcesFor(ctx context.Context, userID, org uuid.UUID) (CalendarSources, error) {
	out := CalendarSources{
		Sources:      []CalendarSource{{ID: "app", Type: "app", Label: "Calendar", CanWrite: true, Calendars: []ConnectedCalendar{}}},
		Offers:       []app.ConnectionOffer{},
		Conferencing: seamOptions(ctx, userID, org),
	}
	if out.Conferencing == nil {
		out.Conferencing = []ConferenceOption{}
	}
	cx := s.connections()
	if cx == nil {
		return out, nil
	}
	if offers, err := cx.Offers(ctx, userID, org, s.Name()); err == nil && offers != nil {
		out.Offers = offers
	}
	conns, err := s.connectedFor(ctx, userID, org)
	if err != nil {
		return out, err
	}
	prefs, err := s.calendarPrefs(ctx, userID)
	if err != nil {
		return out, err
	}
	colorIdx := 0
	for _, c := range conns {
		src := CalendarSource{ID: c.ID, Type: "connected", Provider: c.Provider, Product: productCalendar, Account: c.Account,
			Label: productLabel(c.Provider), Status: c.Status, Capability: c.readCap, CanWrite: c.writeCap != "", Calendars: []ConnectedCalendar{}}
		if src.Status == "" {
			src.Status = "active"
		}
		if !src.CanWrite {
			src.WriteURL = s.grantURL(ctx, userID, org, calWriteCaps[c.readCap])
		}
		switch c.Status {
		case "needs_reauth":
			src.FixURL = connectURL(c.readCap, "/calendar")
		case "blocked":
		default:
			cals, err := s.calendarsOf(ctx, userID, org, c)
			if err != nil {
				src.Error = sourceError(c.ID, "", err)
				src.FixURL = src.Error.FixURL
				break
			}
			for _, cal := range cals {
				src.Calendars = append(src.Calendars, applyPrefs(cal, prefs[prefKey{c.ID, cal.ID}], &colorIdx))
			}
		}
		out.Sources = append(out.Sources, src)
	}
	return out, nil
}

// applyPrefs fills a calendar's colour, visibility and busy choice from the
// person's saved preference, else the defaults.
func applyPrefs(cal extCalendar, p *calPref, colorIdx *int) ConnectedCalendar {
	out := ConnectedCalendar{ID: cal.ID, Name: cal.Name, Primary: cal.Primary, CanWrite: cal.CanWrite, AccessRole: cal.AccessRole,
		Timezone: cal.Timezone, Hidden: cal.Hidden, Busy: cal.Primary, Conferencing: []ConferenceOption{}}
	out.Color = defaultColors[*colorIdx%len(defaultColors)]
	*colorIdx++
	if p != nil {
		if p.Color != nil && validColor(*p.Color) {
			out.Color = *p.Color
		}
		if p.Hidden != nil {
			out.Hidden = *p.Hidden
		}
		if p.Busy != nil {
			out.Busy = *p.Busy
		}
	}
	if cal.CanWrite && cal.Meeting != "" {
		out.Conferencing = append(out.Conferencing, nativeOption(cal))
	}
	return out
}

// nativeOption is the meeting link a platform makes on its own events.
func nativeOption(cal extCalendar) ConferenceOption {
	o := ConferenceOption{ID: cal.Meeting, Platform: cal.platform, ConnectionID: cal.connID, Account: cal.account, Native: true, Status: "active"}
	switch cal.Meeting {
	case ProviderGoogleMeet:
		o.Label = "Google Meet"
	case ProviderTeams:
		o.Label = "Microsoft Teams"
	}
	return o
}

func (s *Service) sourcesHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	org, err := s.orgParam(r.Context(), r, id.UserID)
	if err != nil {
		return err
	}
	out, err := s.sourcesFor(r.Context(), id.UserID, org)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- per-calendar preferences ---------------------------------------------------

type prefKey struct{ conn, cal string }

type calPref struct {
	Color  *string
	Hidden *bool
	Busy   *bool
}

func (s *Service) calendarPrefs(ctx context.Context, userID uuid.UUID) (map[prefKey]*calPref, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT connection_id, calendar_id, color, hidden, busy FROM calendar_connected_calendars WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[prefKey]*calPref{}
	for rows.Next() {
		var k prefKey
		p := &calPref{}
		if err := rows.Scan(&k.conn, &k.cal, &p.Color, &p.Hidden, &p.Busy); err != nil {
			return nil, err
		}
		out[k] = p
	}
	return out, rows.Err()
}

// updateCalendarPref saves colour, visibility or availability for one of the
// caller's connected calendars: PATCH /calendar/connected/{conn}/calendars/{cal}.
func (s *Service) updateCalendarPref(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	connID, calID, _, err := connPath(r)
	if err != nil {
		return err
	}
	var in struct {
		Color  *string `json:"color"`
		Hidden *bool   `json:"hidden"`
		Busy   *bool   `json:"busy"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Color != nil && !validColor(*in.Color) {
		return httpx.BadRequest("Pick one of the calendar colors.")
	}
	conns, err := s.connectedFor(r.Context(), id.UserID, uuid.Nil)
	if err != nil {
		return err
	}
	found := false
	for _, c := range conns {
		found = found || c.ID == connID
	}
	if !found {
		return errConnectionGone
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO calendar_connected_calendars (user_id, connection_id, calendar_id, color, hidden, busy)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, connection_id, calendar_id) DO UPDATE SET
		  color = COALESCE($4, calendar_connected_calendars.color),
		  hidden = COALESCE($5, calendar_connected_calendars.hidden),
		  busy = COALESCE($6, calendar_connected_calendars.busy), updated_at = now()`,
		id.UserID, connID, calID, in.Color, in.Hidden, in.Busy); err != nil {
		return err
	}
	cache.forgetBusy(id.UserID)
	s.changed(r.Context(), "connected", id.UserID)
	httpx.NoContent(w)
	return nil
}

// connPath reads {conn}, {cal} and {event} (URL-escaped platform ids).
func connPath(r *http.Request) (conn, cal, event string, err error) {
	get := func(name string) (string, error) {
		raw := chiParam(r, name)
		if raw == "" {
			return "", nil
		}
		v, err := url.PathUnescape(raw)
		if err != nil || len(v) > 1024 || strings.ContainsAny(v, "\x00\r\n") {
			return "", httpx.BadRequest("Invalid " + name + " id.")
		}
		return v, nil
	}
	if conn, err = get("conn"); err != nil {
		return
	}
	if cal, err = get("cal"); err != nil {
		return
	}
	event, err = get("event")
	if conn == "" || cal == "" {
		err = httpx.BadRequest("A connection and a calendar are needed.")
	}
	return
}

// ---- time helpers -----------------------------------------------------------------

// connectedHorizon bounds what incremental sync keeps per calendar; windows
// outside it are read live.
func connectedHorizon(now time.Time) (time.Time, time.Time) {
	return now.AddDate(0, 0, -120), now.AddDate(0, 0, 400)
}
