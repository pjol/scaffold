package crm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// ---- connected CRMs ---------------------------------------------------------
//
// A workspace runs the app's own CRM, a connected CRM (HubSpot or
// Salesforce, through each person's own connection), or both side by side
// (crm_settings.source_mode). Connected records are read and written live
// with the person's token through a Source, so the platform's own
// permissions (owners, teams, territories, sharing rules) decide what each
// person sees, totals included. Nothing from a platform is copied into crm
// tables; short per-person caches hold schema (properties, pipelines,
// owners) for a minute.

// Platform capabilities (app.CapabilityProvider). Read covers records,
// pipelines, engagements, owners and properties; write adds field edits,
// stage moves, notes, tasks, logged calls, owner assignment and new records.
const (
	CapHubSpotRead     = "crm.hubspot.read"
	CapHubSpotWrite    = "crm.hubspot.write"
	CapSalesforceRead  = "crm.salesforce.read"
	CapSalesforceWrite = "crm.salesforce.write"
)

// Source modes (crm_settings.source_mode).
const (
	SourceModeApp       = "app"       // the app's own CRM only
	SourceModeConnected = "connected" // connected CRMs only; the app's records are hidden
	SourceModeBoth      = "both"      // side by side, with source filters
)

// SourceApp is the source id of the app's own records; SourceAll merges
// every source a list may show.
const (
	SourceApp = "app"
	SourceAll = "all"
)

// appLabel names the app's own records in source filters.
const appLabel = "Scaffold App"

type providerInfo struct {
	ID, Label   string
	Read, Write string
}

var crmProviders = []providerInfo{
	{ID: "hubspot", Label: "HubSpot", Read: CapHubSpotRead, Write: CapHubSpotWrite},
	{ID: "salesforce", Label: "Salesforce", Read: CapSalesforceRead, Write: CapSalesforceWrite},
}

func providerByID(id string) (providerInfo, bool) {
	for _, p := range crmProviders {
		if p.ID == id {
			return p, true
		}
	}
	return providerInfo{}, false
}

// HubSpot scopes per capability (Salesforce grants `api` for both; the
// platform's own profile and sharing decide what the account may change).
var (
	hubspotReadScopes = []string{
		"crm.objects.companies.read", "crm.objects.contacts.read", "crm.objects.deals.read", "crm.objects.owners.read",
		"crm.schemas.companies.read", "crm.schemas.contacts.read", "crm.schemas.deals.read", "sales-email-read",
	}
	hubspotWriteScopes = []string{"crm.objects.companies.write", "crm.objects.contacts.write", "crm.objects.deals.write"}
)

// Capabilities declares what the CRM can do with each platform.
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapHubSpotRead, Module: "crm", Provider: "hubspot", Label: "HubSpot records in the CRM", Scopes: hubspotReadScopes},
		{Key: CapHubSpotWrite, Module: "crm", Provider: "hubspot", Label: "Edit HubSpot records from the CRM", Scopes: append(slices.Clone(hubspotReadScopes), hubspotWriteScopes...), Write: true},
		{Key: CapSalesforceRead, Module: "crm", Provider: "salesforce", Label: "Salesforce records in the CRM", Scopes: []string{"api", "refresh_token"}},
		{Key: CapSalesforceWrite, Module: "crm", Provider: "salesforce", Label: "Edit Salesforce records from the CRM", Scopes: []string{"api", "refresh_token"}, Write: true},
	}
}

// Source is the CrmSource seam: one person's connected CRM account, read and
// written live. HubSpot and Salesforce adapters implement it and map the
// platform's objects onto the CRM's DTO shapes, so the REST routes, search
// and agent tools resolve a Source once and the screens never branch. Every
// value it returns is what that account can see on the platform.
type Source interface {
	Ref() *app.SourceRef
	Connection() app.Connection
	// AccountID is the platform account (HubSpot portal, Salesforce org).
	AccountID(ctx context.Context) string
	CanWrite() bool
	Properties(ctx context.Context, o objectType) ([]Property, error)
	Pipelines(ctx context.Context) ([]XPipeline, error)
	List(ctx context.Context, o objectType, spec listSpec) (*xPage, error)
	Get(ctx context.Context, o objectType, id string) (*XRecordDetail, error)
	Timeline(ctx context.Context, o objectType, id, filter, cursor string, limit int) (*XTimelinePage, error)
	Board(ctx context.Context, pipelineID string, spec listSpec) (*XBoard, error)
	Dashboard(ctx context.Context, pipelineID string, loc *time.Location) (*XDashboard, error)
	Tasks(ctx context.Context, bucket string, loc *time.Location, cursor string, limit int) (*xActivityPage, error)
	TaskSummary(ctx context.Context, loc *time.Location) (TaskSummary, error)
	Search(ctx context.Context, q string, types []objectType, limit int) ([]XSearchHit, error)
	Owners(ctx context.Context) ([]XUserRef, error)
	Update(ctx context.Context, o objectType, id string, fields map[string]json.RawMessage) (xrecordish, error)
	Create(ctx context.Context, o objectType, fields map[string]json.RawMessage) (xrecordish, error)
	MoveStage(ctx context.Context, id, stageID string) (xrecordish, error)
	LogActivity(ctx context.Context, o objectType, id string, req xActivityReq) (*XActivity, error)
	SetTaskDone(ctx context.Context, activityID string, done bool) (*XActivity, error)
	RecordURL(o objectType, id string) string
}

// xActivityReq is an activity logged on a connected record (activityReq with
// the platform's owner id as assignee).
type xActivityReq struct {
	Type            string     `json:"type"`
	Subject         *string    `json:"subject"`
	Body            *string    `json:"body"`
	OccurredAt      *time.Time `json:"occurred_at"`
	DurationMinutes *int       `json:"duration_minutes"`
	Outcome         *string    `json:"outcome"`
	Direction       *string    `json:"direction"`
	DueAt           *time.Time `json:"due_at"`
	AssigneeID      *string    `json:"assignee_id"`
	ReminderAt      *time.Time `json:"reminder_at"`
	Priority        *string    `json:"priority"`
}

// validate applies the same rules as the app's own composer, for the
// activity types platforms take from the app: notes, calls and tasks.
func (req xActivityReq) validate() error {
	switch req.Type {
	case "note":
		if req.Body == nil || strings.TrimSpace(*req.Body) == "" {
			return httpx.BadRequest("Write the note first.")
		}
	case "task":
		if req.Subject == nil || strings.TrimSpace(*req.Subject) == "" {
			return httpx.BadRequest("Give the task a title.")
		}
	case "call":
		if req.Outcome != nil && !slices.Contains(outcomes["call"], *req.Outcome) {
			return httpx.BadRequest("outcome is connected, no_answer, left_voicemail, busy or wrong_number.")
		}
	default:
		return httpx.BadRequest("Connected records take notes, calls and tasks from here; log emails and meetings on the platform.")
	}
	if req.Body != nil && len([]rune(*req.Body)) > maxNoteChars {
		return httpx.BadRequest("That's too long.")
	}
	if req.Direction != nil && *req.Direction != "inbound" && *req.Direction != "outbound" {
		return httpx.BadRequest("direction is inbound or outbound.")
	}
	if req.Priority != nil && !slices.Contains(priorities, *req.Priority) {
		return httpx.BadRequest("priority is none, low, medium or high.")
	}
	if req.DurationMinutes != nil && (*req.DurationMinutes < 0 || *req.DurationMinutes > 1440) {
		return httpx.BadRequest("duration_minutes is 0 to 1440.")
	}
	return nil
}

// ---- workspace mode ---------------------------------------------------------

// sourceSettings is a workspace's connected-CRM policy.
type sourceSettings struct {
	Mode      string
	Providers []string
}

func (st sourceSettings) allowsApp() bool       { return st.Mode != SourceModeConnected }
func (st sourceSettings) allowsConnected() bool { return st.Mode != SourceModeApp }
func (st sourceSettings) allows(provider string) bool {
	return st.allowsConnected() && slices.Contains(st.Providers, provider)
}

func (s *Service) sourceSettings(ctx context.Context, orgID uuid.UUID) (sourceSettings, error) {
	var st sourceSettings
	err := s.d.Pool.QueryRow(ctx, `SELECT source_mode, source_providers FROM crm_settings WHERE org_id = $1`, orgID).Scan(&st.Mode, &st.Providers)
	return st, err
}

// ---- resolving a connection -----------------------------------------------------

func errSourcesOff() *httpx.Error {
	return httpx.NewError(http.StatusNotFound, "crm_source_off", "Connected CRMs are off in this "+orgs.Noun()+". An admin can turn them on in CRM settings.")
}

func errAppSourceOff() *httpx.Error {
	return httpx.NewError(http.StatusNotFound, "crm_source_off", "This "+orgs.Noun()+" works in its connected CRM. Pick a connected source.")
}

func errNoConnection() *httpx.Error {
	return httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connection isn't yours or no longer exists. Check Settings → Connections.")
}

// connectedFor lists userID's CRM connections the workspace allows, with
// each one's provider. Without a connections module there are none.
func (s *Service) connectedFor(ctx context.Context, userID, orgID uuid.UUID, st sourceSettings) ([]app.Connection, error) {
	cs := app.FindConnections(s.d.Modules)
	if cs == nil || !st.allowsConnected() {
		return nil, nil
	}
	var out []app.Connection
	for _, p := range crmProviders {
		if !st.allows(p.ID) {
			continue
		}
		list, err := cs.For(ctx, userID, orgID, p.Read)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			if c.Provider == "" {
				c.Provider = p.ID
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// writable reports which of userID's connections grant the write capability.
func (s *Service) writable(ctx context.Context, userID, orgID uuid.UUID, st sourceSettings) map[string]bool {
	out := map[string]bool{}
	cs := app.FindConnections(s.d.Modules)
	if cs == nil {
		return out
	}
	for _, p := range crmProviders {
		if !st.allows(p.ID) {
			continue
		}
		list, err := cs.For(ctx, userID, orgID, p.Write)
		if err != nil {
			continue
		}
		for _, c := range list {
			if c.Status == "" || c.Status == "active" {
				out[c.ID] = true
			}
		}
	}
	return out
}

// source resolves one of userID's connections in orgID into a Source: the
// workspace must allow connected CRMs and the provider, and the connection
// must be the person's own and grant the read capability.
func (s *Service) source(ctx context.Context, userID, orgID uuid.UUID, connID string) (Source, error) {
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if !st.allowsConnected() {
		return nil, errSourcesOff()
	}
	conns, err := s.connectedFor(ctx, userID, orgID, st)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		if c.ID == connID {
			return s.openSource(ctx, userID, orgID, c, s.writable(ctx, userID, orgID, st)[c.ID])
		}
	}
	return nil, errNoConnection()
}

// sourceForRecord finds the connection an xref names among the orgs where
// userID uses the CRM (record routes carry no org).
func (s *Service) sourceForRecord(ctx context.Context, userID uuid.UUID, connID string) (Source, error) {
	orgIDs, err := s.crmOrgs(ctx, userID)
	if err != nil {
		return nil, err
	}
	var firstErr error
	for _, orgID := range orgIDs {
		if err := s.bootstrap(ctx, orgID); err != nil {
			return nil, err
		}
		src, err := s.source(ctx, userID, orgID, connID)
		if err == nil {
			return src, nil
		}
		if firstErr == nil || app.ConnectionErrorCode(firstErr) == app.ConnectionNotFound {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = errNoCRM()
	}
	return nil, firstErr
}

// openSource builds the adapter for c with the person's authenticated client.
func (s *Service) openSource(ctx context.Context, userID, orgID uuid.UUID, c app.Connection, write bool) (Source, error) {
	cs := app.FindConnections(s.d.Modules)
	if cs == nil {
		return nil, errNoConnection()
	}
	p, ok := providerByID(c.Provider)
	if !ok {
		return nil, errNoConnection()
	}
	ctx = app.WithConnectionOrg(ctx, orgID)
	hc, conn, err := cs.Client(ctx, userID, c.ID, p.Read)
	if err != nil {
		return nil, err
	}
	if conn.ID == "" {
		conn = c
	}
	if conn.Provider == "" {
		conn.Provider = p.ID
	}
	base := &xconn{s: s, cs: cs, userID: userID, orgID: orgID, conn: conn, provider: p, read: hc, write: write,
		ref: app.ConnectedSource(conn, "", "")}
	switch p.ID {
	case "hubspot":
		return newHubSpot(base), nil
	case "salesforce":
		return newSalesforce(base)
	}
	return nil, errNoConnection()
}

// xconn is what both adapters share: the person, the connection and its clients.
type xconn struct {
	s        *Service
	cs       app.Connections
	userID   uuid.UUID
	orgID    uuid.UUID
	conn     app.Connection
	provider providerInfo
	read     *http.Client
	write    bool
	ref      *app.SourceRef
}

func (x *xconn) Ref() *app.SourceRef        { return x.ref }
func (x *xconn) Connection() app.Connection { return x.conn }
func (x *xconn) CanWrite() bool             { return x.write }

// level is the access level connected rows carry.
func (x *xconn) level() access.Level {
	if x.write {
		return access.Edit
	}
	return access.View
}

// writer returns the client for a change: the write capability must be
// granted (the connections module answers connection_scope_missing when not).
func (x *xconn) writer(ctx context.Context) (*http.Client, error) {
	if !x.write {
		return nil, httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing,
			"Turn on editing for "+x.provider.Label+" in Settings → Connections to change records from here.")
	}
	ctx = app.WithConnectionOrg(ctx, x.orgID)
	hc, _, err := x.cs.Client(ctx, x.userID, x.conn.ID, x.provider.Write)
	return hc, err
}

// endpoint is a named API base of the connection, else def.
func (x *xconn) endpoint(name, def string) string {
	if v := strings.TrimRight(x.conn.Endpoints[name], "/"); v != "" {
		return v
	}
	return def
}

// ---- per-person cache ------------------------------------------------------------

// xcache holds schema and lookups per (person, connection) for a short
// while: never records, never shared between people.
type xcache struct {
	mu sync.Mutex
	m  map[string]xcacheEntry
}

type xcacheEntry struct {
	exp time.Time
	v   any
}

const (
	xcacheTTL = time.Minute
	xcacheMax = 4000
)

var sourceCache = &xcache{m: map[string]xcacheEntry{}}

func (c *xcache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.v, true
}

func (c *xcache) put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= xcacheMax {
		now := time.Now()
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= xcacheMax {
			c.m = map[string]xcacheEntry{}
		}
	}
	c.m[key] = xcacheEntry{exp: time.Now().Add(xcacheTTL), v: v}
}

// dropUser forgets everything cached for a person (account deletion).
func (c *xcache) dropUser(userID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := userID.String() + "|"
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}

// cached returns the person's cached value for key, or loads and caches it.
func cached[T any](x *xconn, key string, load func() (T, error)) (T, error) {
	k := x.userID.String() + "|" + x.conn.ID + "|" + key
	if v, ok := sourceCache.get(k); ok {
		if t, ok := v.(T); ok {
			return t, nil
		}
	}
	v, err := load()
	if err != nil {
		return v, err
	}
	sourceCache.put(k, v)
	return v, nil
}

// forget drops one cached key for the person (after a write changes it).
func (x *xconn) forget(key string) {
	k := x.userID.String() + "|" + x.conn.ID + "|" + key
	sourceCache.mu.Lock()
	delete(sourceCache.m, k)
	sourceCache.mu.Unlock()
}

// ---- GET /v1/crm/sources ------------------------------------------------------------

// FeatureSource is one place CRM records can come from (shared FeatureSource).
type FeatureSource struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // app | connected
	Provider   string `json:"provider,omitempty"`
	Product    string `json:"product,omitempty"`
	Account    string `json:"account,omitempty"`
	Label      string `json:"label"`
	Status     string `json:"status,omitempty"`
	Capability string `json:"capability,omitempty"`
	FixURL     string `json:"fix_url,omitempty"`
	// Writable: the connection may change records (the write capability is on).
	Writable bool `json:"writable"`
	// URL opens the account on the platform.
	URL string `json:"url,omitempty"`
}

// Sources is GET /v1/crm/sources: shared FeatureSources plus the workspace's mode.
type Sources struct {
	Sources   []FeatureSource       `json:"sources"`
	Offers    []app.ConnectionOffer `json:"offers"`
	Mode      string                `json:"mode"`
	Providers []string              `json:"providers"`
}

// getSources: GET /v1/crm/sources?org_id= (the only org when omitted).
func (s *Service) getSources(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	userID := me(r)
	var orgID *uuid.UUID
	if v := r.URL.Query().Get("org_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("org_id must be an id.")
		}
		orgID = &id
	}
	oid, err := s.toolOrg(ctx, userID, orgID)
	if err != nil {
		return err
	}
	out, err := s.sources(ctx, userID, oid)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) sources(ctx context.Context, userID, orgID uuid.UUID) (*Sources, error) {
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := &Sources{Sources: []FeatureSource{}, Offers: []app.ConnectionOffer{}, Mode: st.Mode, Providers: st.Providers}
	if st.allowsApp() {
		out.Sources = append(out.Sources, FeatureSource{ID: SourceApp, Type: "app", Label: appLabel, Writable: true})
	}
	if !st.allowsConnected() {
		return out, nil
	}
	conns, err := s.connectedFor(ctx, userID, orgID, st)
	if err != nil {
		return nil, err
	}
	writable := s.writable(ctx, userID, orgID, st)
	for _, c := range conns {
		p, _ := providerByID(c.Provider)
		// The platform's name; clients add the account where two would read the same.
		fs := FeatureSource{ID: c.ID, Type: "connected", Provider: c.Provider, Account: c.Account,
			Label: p.Label, Status: c.Status, Capability: p.Read, Writable: writable[c.ID]}
		if fs.Status == "" {
			fs.Status = "active"
		}
		if fs.Status != "active" {
			fs.FixURL = "/settings/connections?capability=" + url.QueryEscape(p.Read) + "&return_to=" + url.QueryEscape("/crm")
		}
		fs.URL = accountURL(c)
		out.Sources = append(out.Sources, fs)
	}
	if cs := app.FindConnections(s.d.Modules); cs != nil {
		offers, err := cs.Offers(ctx, userID, orgID, s.Name())
		if err == nil {
			for _, o := range offers {
				if st.allows(o.Provider) && strings.HasSuffix(o.Capability, ".read") {
					out.Offers = append(out.Offers, o)
				}
			}
		}
	}
	return out, nil
}

// accountURL opens the connected account on its platform.
func accountURL(c app.Connection) string {
	switch c.Provider {
	case "hubspot":
		if c.AccountID != "" {
			return strings.TrimRight(hubspotAppBase(c), "/") + "/contacts/" + url.PathEscape(c.AccountID)
		}
	case "salesforce":
		if v := c.Endpoints["instance"]; v != "" {
			return strings.TrimRight(v, "/") + "/lightning/page/home"
		}
	}
	return ""
}

// ---- request helpers ------------------------------------------------------------

// sourceParam is the ?source= of an org route: SourceApp (the default when
// the workspace shows the app's records), SourceAll, or a connection id.
func (s *Service) sourceParam(ctx context.Context, r *http.Request, orgID uuid.UUID) (string, sourceSettings, error) {
	st, err := s.sourceSettings(ctx, orgID)
	if err != nil {
		return "", st, err
	}
	v := strings.TrimSpace(r.URL.Query().Get("source"))
	switch {
	case v == "" && st.allowsApp():
		v = SourceApp
	case v == "":
		v = SourceAll
	case v == SourceApp && !st.allowsApp():
		return "", st, errAppSourceOff()
	}
	return v, st, nil
}

// passConnErr keeps connection failures (codes clients act on) as they are.
func passConnErr(err error) error {
	var he *httpx.Error
	if errors.As(err, &he) {
		return he
	}
	return err
}
