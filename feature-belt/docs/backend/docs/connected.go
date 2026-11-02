package docs

// Connected docs: Notion pages and databases and Google Docs, browsed,
// rendered in the docs editor and changed live with the person's own
// connection (app.Connections, the connectors feature). Nothing from a
// platform is stored here except what people create themselves: a Docs
// page they copied, or a "connected page" block linking one.
//
// Rules that hold for every connected route and tool:
//   - A connection is used only by the person it belongs to: every call goes
//     through Connections.Client with the caller's id, so they reach exactly
//     what that account reaches on the platform. Per-person caches (editing
//     sessions, tree roots, user names) are keyed by person and connection
//     and never shared.
//   - Reads need the provider's read capability, changes its write
//     capability (granted separately, incrementally).
//   - Agents are capped by the docs scope and the connection's own scope
//     (app.ConnectionScope); writes are separate tools with Source set.
//   - Platform errors are passed on honestly (no access, changed since, rate
//     limited, reconnect), never as a 401 (that signs people out).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// KindExternal is the agent-scope kind of a page on a connected platform;
// its id is "<connection id>:<platform page id>".
const KindExternal = "docs.external"

// Capabilities docs declares (app.CapabilityProvider). Read and write are
// separate so people grant changes only when they want them.
const (
	CapNotionRead   = "docs.notion.read"
	CapNotionWrite  = "docs.notion.write"
	CapGoogleRead   = "docs.google.read"
	CapGoogleWrite  = "docs.google.write"
	providerNotion  = "notion"
	providerGoogle  = "google"
	productNotion   = "notion"
	productGDocs    = "docs"
	connSearchLimit = 3 * time.Second // per connection, for global search
)

// Capabilities implements app.CapabilityProvider. Notion's OAuth has no
// scopes: the person picks the pages the app may open on Notion's consent
// screen (and can share more later from a page's ••• → Connections), and
// what the integration may do is set when the app is registered (read,
// update and insert content, comments). Google needs Docs to read and edit
// document bodies and Drive to list documents and read or add comments.
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapNotionRead, Module: "docs", Provider: providerNotion, Label: "Notion pages you share, in Docs"},
		{Key: CapNotionWrite, Module: "docs", Provider: providerNotion, Label: "Edit Notion pages from Docs", Write: true},
		{Key: CapGoogleRead, Module: "docs", Provider: providerGoogle, Label: "Google Docs in Docs",
			Scopes: []string{"https://www.googleapis.com/auth/documents.readonly", "https://www.googleapis.com/auth/drive.readonly"}},
		{Key: CapGoogleWrite, Module: "docs", Provider: providerGoogle, Label: "Edit Google Docs from Docs", Write: true,
			Scopes: []string{"https://www.googleapis.com/auth/documents", "https://www.googleapis.com/auth/drive"}},
	}
}

var (
	docsReadCaps  = []string{CapNotionRead, CapGoogleRead}
	docsWriteCaps = map[string]string{CapNotionRead: CapNotionWrite, CapGoogleRead: CapGoogleWrite}
)

// platformLabel is how people name a provider's docs product.
func platformLabel(provider string) string {
	if provider == providerGoogle {
		return "Google Docs"
	}
	return "Notion"
}

func productOf(provider string) string {
	if provider == providerGoogle {
		return productGDocs
	}
	return productNotion
}

// ConnectedSource is one connected account in Docs (sources.FeatureSource
// plus whether changes are allowed).
type ConnectedSource struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // app | connected
	Provider   string `json:"provider,omitempty"`
	Product    string `json:"product,omitempty"`
	Account    string `json:"account,omitempty"`
	AccountID  string `json:"account_id,omitempty"` // the platform's id (a Notion workspace)
	Label      string `json:"label"`
	Status     string `json:"status,omitempty"` // active | needs_reauth | blocked
	Capability string `json:"capability,omitempty"`
	FixURL     string `json:"fix_url,omitempty"`
	CanWrite   bool   `json:"can_write"`
	WriteURL   string `json:"write_url,omitempty"` // grants changes, when CanWrite is false
}

// ConnectedSources is GET /v1/docs/sources (sources.FeatureSources).
type ConnectedSources struct {
	Sources []ConnectedSource     `json:"sources"`
	Offers  []app.ConnectionOffer `json:"offers"`
}

// ---- connections -------------------------------------------------------------------

// docsConn is one of the caller's connections as docs sees it.
type docsConn struct {
	app.Connection
	readCap  string // the read capability it grants
	writeCap string // the write capability, when granted and active ("" = read only)
}

func (c *docsConn) label() string { return platformLabel(c.Provider) + " · " + c.Account }

func (c *docsConn) source(url string) *app.SourceRef {
	return app.ConnectedSource(c.Connection, productOf(c.Provider), url)
}

func (s *Service) connections() app.Connections { return app.FindConnections(s.d.Modules) }

// connectedFor lists the caller's connections that grant a docs read
// capability in org (uuid.Nil: no workspace context), in a stable order.
func (s *Service) connectedFor(ctx context.Context, me, org uuid.UUID) ([]*docsConn, error) {
	cx := s.connections()
	if cx == nil {
		return nil, nil
	}
	byID := map[string]*docsConn{}
	var order []string
	for _, rc := range docsReadCaps {
		list, err := cx.For(ctx, me, org, rc)
		if err != nil {
			return nil, err
		}
		for _, cn := range list {
			if byID[cn.ID] != nil {
				continue
			}
			byID[cn.ID] = &docsConn{Connection: cn, readCap: rc}
			order = append(order, cn.ID)
		}
		wl, err := cx.For(ctx, me, org, docsWriteCaps[rc])
		if err != nil {
			return nil, err
		}
		for _, cn := range wl {
			if c := byID[cn.ID]; c != nil && c.readCap == rc && cn.Status == "active" {
				c.writeCap = docsWriteCaps[rc]
			}
		}
	}
	out := make([]*docsConn, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider > out[j].Provider // notion before google
		}
		return strings.ToLower(out[i].Account) < strings.ToLower(out[j].Account)
	})
	return out, nil
}

// connSession is an authenticated platform adapter for one request.
type connSession struct {
	conn *docsConn
	ad   docsAdapter
	me   uuid.UUID
	org  uuid.UUID
	s    *Service
	// write: the client signs with the write capability.
	write bool
}

var (
	errNoConnections  = httpx.NewError(http.StatusServiceUnavailable, app.ConnectionUnavailable, "Connected accounts aren't available in this app.")
	errConnectionGone = httpx.NewError(http.StatusNotFound, app.ConnectionNotFound, "That connected account isn't available. Connect it again in Settings → Connections.")
)

// open starts a session on connID for the caller: the read capability, or
// the write one. It refuses a blocked or read-only connection with a fix link.
func (s *Service) open(ctx context.Context, me, org uuid.UUID, connID string, write bool) (*connSession, error) {
	cx := s.connections()
	if cx == nil {
		return nil, errNoConnections
	}
	all, err := s.connectedFor(ctx, me, org)
	if err != nil {
		return nil, err
	}
	var conn *docsConn
	for _, c := range all {
		if c.ID == connID {
			conn = c
		}
	}
	if conn == nil {
		return nil, errConnectionGone
	}
	switch conn.Status {
	case "blocked":
		return nil, httpx.NewError(http.StatusForbidden, app.ConnectionBlocked, "Your "+orgs.Noun()+" doesn't allow "+platformLabel(conn.Provider)+" here.")
	case "needs_reauth":
		return nil, s.needsAuth(conn, conn.readCap)
	}
	capability := conn.readCap
	if write {
		if conn.writeCap == "" {
			e := httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing,
				"Allow changes to "+platformLabel(conn.Provider)+" first: this connection can only read.")
			e.Details = map[string]any{"fix_url": s.grantURL(ctx, me, org, docsWriteCaps[conn.readCap]), "capability": docsWriteCaps[conn.readCap], "connection_id": conn.ID}
			return nil, e
		}
		capability = conn.writeCap
	}
	if org != uuid.Nil {
		ctx = app.WithConnectionOrg(ctx, org)
	}
	client, cn, err := cx.Client(ctx, me, connID, capability)
	if err != nil {
		return nil, s.connError(conn, capability, err)
	}
	conn.Connection.Endpoints = cn.Endpoints
	cs := &connSession{conn: conn, me: me, org: org, s: s, write: write}
	p := &platform{client: client, name: platformLabel(conn.Provider), wrap: func(err error) error { return s.connError(conn, capability, err) }}
	switch conn.Provider {
	case providerNotion:
		cs.ad = newNotion(conn, p, me)
	case providerGoogle:
		cs.ad = newGoogleDocs(conn, p)
	default:
		return nil, httpx.BadRequest("Docs can't open " + conn.Provider + " accounts.")
	}
	return cs, nil
}

func (s *Service) needsAuth(conn *docsConn, capability string) error {
	e := httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+conn.label()+" to continue.")
	e.Details = map[string]any{"fix_url": connectURL(capability, ""), "capability": capability, "connection_id": conn.ID}
	return e
}

// connError passes a Connections failure on with its code (never a 401:
// that signs people out) and the link that fixes it.
func (s *Service) connError(conn *docsConn, capability string, err error) error {
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
		d := map[string]any{"fix_url": connectURL(capability, ""), "capability": capability, "connection_id": conn.ID}
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
func (s *Service) grantURL(ctx context.Context, me, org uuid.UUID, capability string) string {
	if cx := s.connections(); cx != nil {
		if offers, err := cx.Offers(ctx, me, org, s.Name()); err == nil {
			for _, o := range offers {
				if o.Capability == capability && o.ConnectURL != "" {
					return o.ConnectURL
				}
			}
		}
	}
	return connectURL(capability, "")
}

// sources builds GET /v1/docs/sources: the app's own pages, then each
// connected account, and the capabilities still on offer.
func (s *Service) sources(ctx context.Context, me, org uuid.UUID) (ConnectedSources, error) {
	out := ConnectedSources{Sources: []ConnectedSource{{ID: "app", Type: "app", Label: "Docs", CanWrite: true}}, Offers: []app.ConnectionOffer{}}
	cx := s.connections()
	if cx == nil {
		return out, nil
	}
	conns, err := s.connectedFor(ctx, me, org)
	if err != nil {
		return out, err
	}
	offers, err := cx.Offers(ctx, me, org, s.Name())
	if err != nil {
		return out, err
	}
	for _, c := range conns {
		src := ConnectedSource{ID: c.ID, Type: "connected", Provider: c.Provider, Product: productOf(c.Provider), Account: c.Account, AccountID: c.AccountID,
			Label: platformLabel(c.Provider), Status: c.Status, Capability: c.readCap, CanWrite: c.writeCap != ""}
		if c.Status != "active" {
			src.FixURL = connectURL(c.readCap, "")
		}
		if !src.CanWrite {
			src.WriteURL = connectURL(docsWriteCaps[c.readCap], "")
			for _, o := range offers {
				if o.Capability == docsWriteCaps[c.readCap] && o.ConnectURL != "" {
					src.WriteURL = o.ConnectURL
				}
			}
		}
		out.Sources = append(out.Sources, src)
	}
	// Offer what the person could still connect for reading; changes are
	// offered per account (WriteURL).
	for _, o := range offers {
		if strings.HasPrefix(o.Capability, "docs.") && !strings.HasSuffix(o.Capability, ".write") {
			out.Offers = append(out.Offers, o)
		}
	}
	return out, nil
}

func (cs *connSession) sourceView() ConnectedSource {
	c := cs.conn
	v := ConnectedSource{ID: c.ID, Type: "connected", Provider: c.Provider, Product: productOf(c.Provider), Account: c.Account, AccountID: c.AccountID,
		Label: platformLabel(c.Provider), Status: c.Status, Capability: c.readCap, CanWrite: c.writeCap != ""}
	if !v.CanWrite {
		v.WriteURL = connectURL(docsWriteCaps[c.readCap], "")
	}
	return v
}

// ---- links and ids ---------------------------------------------------------------------

// ConnectedLink is the app path that opens a connected page.
func ConnectedLink(connID, ref string) string {
	return "/docs/c/" + url.PathEscape(connID) + "/" + url.PathEscape(ref)
}

// externalTarget is the agent-scope id of a connected page:
// "<connection id>:<platform id>", within the agents feature's id rules
// ([A-Za-z0-9_.:/-], up to 200).
func externalTarget(connID, platformID string) string {
	id := app.ExternalTarget(connID, platformID)
	if len(id) > 200 {
		id = id[:200]
	}
	return id
}

// validRef accepts platform ids: Notion UUIDs (with or without dashes) and
// Google document ids.
func validRef(ref string) bool {
	if ref == "" || len(ref) > 128 {
		return false
	}
	for _, r := range ref {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// agentAllows checks the connection's own agent scope for one page (always
// true for people and without an agent policy).
func (s *Service) agentAllows(ctx context.Context, caller *auth.Identity, connID, platformID string, need app.AgentLevel) error {
	ok, err := app.AgentAllows(ctx, s.d.Modules, caller, app.ConnectionScope(connID), KindExternal, externalTarget(connID, platformID), need)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	switch need {
	case app.AgentRead:
		return httpx.NewError(http.StatusForbidden, "agent_scope_off", "The person hasn't given AI agents access to this connected account. They can change it in Settings → AI agents.")
	case app.AgentSuggest:
		return httpx.NewError(http.StatusForbidden, "agent_scope_read_only", "The person lets AI agents read this connected account but not change it. They can allow suggestions or edits in Settings → AI agents.")
	}
	return httpx.NewError(http.StatusForbidden, "agent_scope_suggest_only", "The person lets AI agents only suggest changes in this connected account. Ask them to make the change, or to allow edits in Settings → AI agents.")
}

// orgParam reads ?org_id= (optional) and checks the caller is a member.
func (s *Service) orgParam(ctx context.Context, raw string, me uuid.UUID) (uuid.UUID, error) {
	if raw == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, id, me)
	if err != nil {
		return uuid.Nil, err
	}
	if role == "" {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return id, nil
}

// ---- platform HTTP -------------------------------------------------------------------

// platform is the HTTP plumbing both adapters share: JSON requests with the
// person's signing client, and platform errors turned into honest httpx
// errors named after the product.
type platform struct {
	client *http.Client
	name   string            // "Notion", "Google Docs"
	wrap   func(error) error // Connections failures (reconnect, blocked, host refused) with their fix link
	// headers go on every request (Notion-Version).
	headers map[string]string
}

const maxPlatformBody = 16 << 20

// do sends in (nil: no body) and decodes a 2xx answer into out (nil:
// discard). Anything else becomes an httpx error.
func (p *platform) do(ctx context.Context, method, u string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if app.ConnectionErrorCode(err) != "" {
			return p.wrap(err)
		}
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" didn't answer. Try again shortly.")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return p.statusError(resp, b)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPlatformBody)).Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" sent an answer Docs couldn't read. Try again.")
	}
	return nil
}

// platformError reads Notion's ({"object":"error","code","message"}) and
// Google's ({"error":{"code","message","status"}}) error bodies.
func platformError(b []byte) (code, message string) {
	var e struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
		Error   *struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &e) != nil {
		return "", ""
	}
	if e.Error != nil {
		return e.Error.Status, e.Error.Message
	}
	return strings.Trim(string(e.Code), `"`), e.Message
}

// statusError says what went wrong on the platform, in its words.
func (p *platform) statusError(resp *http.Response, body []byte) error {
	code, msg := platformError(body)
	detail := map[string]any{"status": resp.StatusCode}
	if msg != "" {
		detail["platform_message"] = snippet(msg, 300)
	}
	e := func(status int, c, m string) error {
		he := httpx.NewError(status, c, m)
		he.Details = detail
		return he
	}
	lower := strings.ToLower(msg)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return p.wrap(httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+p.name+" to continue."))
	case http.StatusForbidden:
		if strings.Contains(lower, "rate") || strings.Contains(lower, "quota") {
			return p.rateLimited(resp, detail)
		}
		return e(http.StatusForbidden, "connected_forbidden", "This "+p.name+" account doesn't have permission to do that. Ask the owner for access in "+p.name+".")
	case http.StatusNotFound:
		return e(http.StatusNotFound, "not_found", "This isn't in "+p.name+" anymore, or this account can't open it."+notionShareHint(p.name))
	case http.StatusConflict:
		return e(http.StatusConflict, "changed_on_platform", "It changed in "+p.name+" while saving. Reload to see the latest, then try again.")
	case http.StatusPreconditionFailed:
		return e(http.StatusConflict, "changed_on_platform", "It changed in "+p.name+" since you opened it. Reload to see the latest.")
	case http.StatusTooManyRequests:
		return p.rateLimited(resp, detail)
	case http.StatusBadRequest:
		if code == "FAILED_PRECONDITION" || strings.Contains(lower, "revision") {
			return e(http.StatusConflict, "changed_on_platform", "It changed in "+p.name+" since you opened it. Reload to see the latest.")
		}
		return e(http.StatusBadRequest, "invalid_request", p.name+" refused that change.")
	}
	if resp.StatusCode >= 500 {
		return e(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" isn't responding. Try again shortly.")
	}
	return e(http.StatusBadGateway, app.ConnectionUnavailable, p.name+" answered "+strconv.Itoa(resp.StatusCode)+". Try again.")
}

func notionShareHint(name string) string {
	if name != "Notion" {
		return ""
	}
	return " In Notion, share the page with this app from its ••• menu → Connections."
}

func (p *platform) rateLimited(resp *http.Response, detail map[string]any) error {
	he := httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, p.name+" is limiting requests right now. Try again in a minute.")
	if ra := resp.Header.Get("Retry-After"); ra != "" {
		detail["retry_after"] = ra
	}
	he.Details = detail
	return he
}

// endpoint is a connection's API base by name, else def (the platform's
// public host; self-hosted or test deployments set Endpoints).
func endpoint(conn *docsConn, name, def string) string {
	if v := strings.TrimRight(conn.Endpoints[name], "/"); v != "" {
		return v
	}
	return def
}

// isHTTPStatus reports an httpx error with that status.
func isHTTPStatus(err error, status int) bool {
	var he *httpx.Error
	return errors.As(err, &he) && he.Status == status
}
