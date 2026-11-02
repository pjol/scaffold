package repos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Opening connected repositories: find the person's connection, get its
// signed client (app.Connections), learn who they are on the platform and
// whether writing is granted, and wrap it in the provider's RepoSource.

const (
	remoteTimeout     = 20 * time.Second // one platform request
	remoteMaxBody     = 32 << 20         // one platform response
	viewerTTL         = 10 * time.Minute
	writeGrantTTL     = 30 * time.Second
	immutableTTL      = 10 * time.Minute // answers keyed by a commit id
	remoteListTTL     = 60 * time.Second
	remoteCacheMax    = 4000
	connectedPerPage  = 30
	connectedScanMax  = 5 // pages of 100 read when filtering a list by text
	searchPerConnTime = 1500 * time.Millisecond
)

// remotePerPage is the page size for paged diffs (a variable so tests can
// make the platforms paginate).
var remotePerPage = 100

// remoteRepo is one connected repository opened for one person. The
// provider adapters (github.go, gitlab.go) embed it.
type remoteRepo struct {
	s        *Service
	id       extID
	userID   uuid.UUID
	conn     app.Connection
	hc       *http.Client
	api      string // API base, no trailing slash
	web      string // web base (links, git)
	graphql  string
	platform string // GitHub | GitLab
	canWrite bool   // the write capability is granted on this connection
	write    bool   // opened with the write capability (a write request)
	viewer   remoteUser
}

type remoteUser struct {
	ID    string // platform id as text
	Login string
	Name  string
}

func (r *remoteRepo) source(url string) *app.SourceRef {
	return app.ConnectedSource(r.conn, "", url)
}

func (r *remoteRepo) link(sub string) string {
	if sub == "" {
		return "/repos/" + r.id.String()
	}
	return "/repos/" + r.id.String() + "/" + sub
}

// ---- opening -------------------------------------------------------------------

func (s *Service) connections() app.Connections { return app.FindConnections(s.d.Modules) }

var errNoConnections = httpx.NewError(http.StatusNotFound, "not_found", "Connected repositories aren't available on this server.")

// openRemote opens a connected repository for userID. write asks for the
// write capability (every change goes through it). Agent calls are held to
// the connection's agent scope as well as repos'.
func (s *Service) openRemote(ctx context.Context, userID uuid.UUID, id extID, write bool) (RepoSource, *remoteRepo, error) {
	conns := s.connections()
	if conns == nil {
		return nil, nil, errNoConnections
	}
	if _, ok := auth.AgentFrom(ctx); ok {
		caller := auth.FromContext(ctx)
		if caller == nil {
			caller = &auth.Identity{UserID: userID}
		}
		need := app.AgentRead
		if write {
			need = app.AgentEdit
		}
		for _, feature := range []string{app.ConnectionScope(id.ConnID), "repos"} {
			ok, err := app.AgentAllows(ctx, s.d.Modules, caller, feature, ExternalKind, id.target(), need)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				return nil, nil, httpx.NewError(http.StatusForbidden, "agent_scope", "The person hasn't given AI agents this access to "+platformNames[id.Provider]+" ("+id.Path+").")
			}
		}
	}
	hc, conn, err := conns.Client(ctx, userID, id.ConnID, capability(id.Provider, write))
	if err != nil {
		return nil, nil, s.connErr(err, id.Provider, write)
	}
	if conn.Provider != id.Provider {
		return nil, nil, errRepoNotFound
	}
	r := &remoteRepo{s: s, id: id, userID: userID, conn: conn, hc: hc, platform: platformNames[id.Provider], write: write, canWrite: write}
	r.api, r.web, r.graphql = endpoints(conn)
	if r.api == "" {
		return nil, nil, httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, r.platform+" isn't set up for this connection.")
	}
	if !write {
		r.canWrite = s.writeGranted(ctx, userID, conn)
	}
	if r.viewer, err = s.remoteViewer(ctx, r); err != nil {
		return nil, nil, err
	}
	switch id.Provider {
	case ProviderGitHub:
		return &githubSource{r}, r, nil
	default:
		return &gitlabSource{r}, r, nil
	}
}

// endpoints reads the connection's API and web bases, deriving one from
// the other for the public hosts and self-managed GitLab.
func endpoints(c app.Connection) (api, web, graphql string) {
	trim := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }
	api, web, graphql = trim(c.Endpoints["api"]), trim(c.Endpoints["web"]), trim(c.Endpoints["graphql"])
	switch c.Provider {
	case ProviderGitHub:
		if api == "" && (web == "" || web == "https://github.com") {
			api = "https://api.github.com"
		} else if api == "" {
			api = web + "/api/v3" // GitHub Enterprise Server
		}
		if web == "" {
			if api == "https://api.github.com" {
				web = "https://github.com"
			} else {
				web = strings.TrimSuffix(api, "/api/v3")
			}
		}
		if graphql == "" {
			if api == "https://api.github.com" {
				graphql = api + "/graphql"
			} else {
				graphql = web + "/api/graphql"
			}
		}
	case ProviderGitLab:
		if api == "" && web == "" {
			web = "https://gitlab.com"
		}
		if api == "" {
			api = web + "/api/v4"
		}
		if web == "" {
			web = strings.TrimSuffix(api, "/api/v4")
		}
		if graphql == "" {
			graphql = web + "/api/graphql"
		}
	}
	return
}

// writeGranted reports whether the connection also grants writing.
func (s *Service) writeGranted(ctx context.Context, userID uuid.UUID, conn app.Connection) bool {
	key := "wg:" + userID.String() + ":" + conn.ID
	if v, ok := s.rcache.get(key); ok {
		return v.(bool)
	}
	ok := false
	if list, err := s.connections().For(ctx, userID, uuid.Nil, capability(conn.Provider, true)); err == nil {
		for _, c := range list {
			if c.ID == conn.ID && c.Status == "active" {
				ok = true
			}
		}
	}
	s.rcache.set(key, ok, writeGrantTTL)
	return ok
}

// grantWriteURL is where the person turns writing on for this platform.
func (s *Service) grantWriteURL(ctx context.Context, userID uuid.UUID, provider, returnTo string) string {
	return "/settings/connections?capability=" + url.QueryEscape(capability(provider, true)) + "&return_to=" + url.QueryEscape(returnTo)
}

// remoteViewer is who the connection is on the platform (cached).
func (s *Service) remoteViewer(ctx context.Context, r *remoteRepo) (remoteUser, error) {
	key := "viewer:" + r.userID.String() + ":" + r.conn.ID
	if v, ok := s.rcache.get(key); ok {
		return v.(remoteUser), nil
	}
	var u struct {
		ID       json.Number `json:"id"`
		Login    string      `json:"login"`    // GitHub
		Username string      `json:"username"` // GitLab
		Name     string      `json:"name"`
	}
	if _, err := r.get(ctx, r.api+"/user", &u); err != nil {
		return remoteUser{}, s.remoteErr(err, r)
	}
	v := remoteUser{ID: u.ID.String(), Login: u.Login, Name: u.Name}
	if v.Login == "" {
		v.Login = u.Username
	}
	s.rcache.set(key, v, viewerTTL)
	return v, nil
}

// ---- requests ------------------------------------------------------------------

// platformError is a non-2xx answer from the platform; remoteErr turns
// what an adapter didn't handle into an API error.
type platformError struct {
	Status     int
	Message    string
	RetryAfter string
	RateLimit  bool
}

func (e *platformError) Error() string { return fmt.Sprintf("platform %d: %s", e.Status, e.Message) }

func asPlatform(err error) *platformError {
	var pe *platformError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}

func platformStatus(err error) int {
	if pe := asPlatform(err); pe != nil {
		return pe.Status
	}
	return 0
}

type reqOpts struct {
	accept string
	header map[string]string
	raw    *[]byte // receives the body instead of decoding JSON
}

func (r *remoteRepo) get(ctx context.Context, u string, out any) (http.Header, error) {
	return r.do(ctx, http.MethodGet, u, nil, out, reqOpts{})
}

func (r *remoteRepo) send(ctx context.Context, method, u string, body, out any) (http.Header, error) {
	return r.do(ctx, method, u, body, out, reqOpts{})
}

// do makes one request with the connection's client and decodes JSON.
func (r *remoteRepo) do(ctx context.Context, method, u string, body, out any, o reqOpts) (http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	accept := o.accept
	if accept == "" {
		accept = "application/json"
		if r.id.Provider == ProviderGitHub {
			accept = "application/vnd.github+json"
		}
	}
	req.Header.Set("Accept", accept)
	if r.id.Provider == ProviderGitHub {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	for k, v := range o.header {
		req.Header.Set(k, v)
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, remoteMaxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > remoteMaxBody {
		return nil, httpx.NewError(http.StatusBadGateway, app.ConnectionTooLarge, r.platform+" sent more than this app reads at once.")
	}
	if resp.StatusCode >= 300 {
		pe := &platformError{Status: resp.StatusCode, Message: platformMessage(data), RetryAfter: resp.Header.Get("Retry-After")}
		pe.RateLimit = resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(strings.ToLower(pe.Message), "rate limit")))
		return resp.Header, pe
	}
	if o.raw != nil {
		*o.raw = data
		return resp.Header, nil
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, r.platform+" sent an answer this app couldn't read.")
		}
	}
	return resp.Header, nil
}

// platformMessage pulls a readable message out of an error body (GitHub
// {message, errors[]}, GitLab {message} / {error} / {message:{field:[…]}}).
func platformMessage(data []byte) string {
	var body struct {
		Message any `json:"message"`
		Error   any `json:"error"`
		Errors  any `json:"errors"`
	}
	if json.Unmarshal(data, &body) != nil {
		return truncate(strings.TrimSpace(string(data)), 300)
	}
	var parts []string
	flat := func(v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				parts = append(parts, t)
			}
		case []any:
			for _, x := range t {
				switch y := x.(type) {
				case string:
					parts = append(parts, y)
				case map[string]any:
					if m, ok := y["message"].(string); ok && m != "" {
						parts = append(parts, m)
					} else if c, ok := y["code"].(string); ok {
						parts = append(parts, fmt.Sprint(y["field"], " ", c))
					}
				}
			}
		case map[string]any:
			for k, x := range t {
				if l, ok := x.([]any); ok {
					for _, y := range l {
						parts = append(parts, fmt.Sprint(k, " ", y))
					}
				}
			}
		}
	}
	flat(body.Message)
	flat(body.Error)
	flat(body.Errors)
	return truncate(strings.Join(parts, ": "), 300)
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// remoteErr turns a platform answer or transport failure into an API
// error. The platform's 401 means the person must reconnect, never that
// their session here ended.
func (s *Service) remoteErr(err error, r *remoteRepo) error {
	if err == nil {
		return nil
	}
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		if code := app.ConnectionErrorCode(err); code != "" {
			return s.connErr(err, r.id.Provider, r.write)
		}
		return apiErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return httpx.NewError(http.StatusGatewayTimeout, app.ConnectionUnavailable, r.platform+" took too long to answer. Try again.")
	}
	pe := asPlatform(err)
	if pe == nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, r.platform+" couldn't be reached. Try again.")
		}
		return err
	}
	switch {
	case pe.Status == http.StatusUnauthorized:
		e := httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "Reconnect "+r.platform+" ("+r.conn.Account+") to keep using it here.")
		e.Details = map[string]any{"fix_url": "/settings/connections?capability=" + url.QueryEscape(capability(r.id.Provider, r.write)), "connection_id": r.conn.ID}
		return e
	case pe.RateLimit:
		msg := r.platform + " is limiting requests from this account. Try again"
		if secs, err := strconv.Atoi(pe.RetryAfter); err == nil && secs > 0 {
			msg += fmt.Sprintf(" in %s", (time.Duration(secs) * time.Second).String())
		} else {
			msg += " in a few minutes"
		}
		e := httpx.NewError(http.StatusTooManyRequests, app.ConnectionRateLimited, msg+".")
		if pe.RetryAfter != "" {
			e.Details = map[string]any{"retry_after": pe.RetryAfter}
		}
		return e
	case pe.Status == http.StatusForbidden:
		return httpx.NewError(http.StatusForbidden, "forbidden", r.platform+" refused: "+orDefault(pe.Message, "this account can't do that here."))
	case pe.Status == http.StatusNotFound:
		return httpx.NotFound("Not found on " + r.platform + ", or this account can't see it.")
	case pe.Status == http.StatusConflict:
		return httpx.NewError(http.StatusConflict, "conflict", r.platform+": "+orDefault(pe.Message, "that conflicts with a newer change."))
	case pe.Status >= 500:
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, r.platform+" had a problem answering. Try again.")
	default:
		return httpx.NewError(http.StatusUnprocessableEntity, "platform_refused", r.platform+": "+orDefault(pe.Message, "the request was refused."))
	}
}

// connErr explains a Connections failure in repos' words, keeping its code.
func (s *Service) connErr(err error, provider string, write bool) error {
	var e *httpx.Error
	if !errors.As(err, &e) {
		return err
	}
	out := *e
	out.Details = map[string]any{}
	for k, v := range e.Details {
		out.Details[k] = v
	}
	name := platformNames[provider]
	switch e.Code {
	case app.ConnectionNeedsAuth:
		out.Message = "Reconnect " + name + " to keep using it here."
	case app.ConnectionScopeMissing:
		if write {
			out.Message = "Turn on changes for " + name + " in Settings → Connections to do this."
		} else {
			out.Message = "Turn on " + name + " repositories in Settings → Connections."
		}
	case app.ConnectionNotFound:
		return errRepoNotFound
	}
	if _, ok := out.Details["fix_url"]; !ok {
		out.Details["fix_url"] = "/settings/connections?capability=" + url.QueryEscape(capability(provider, write))
	}
	return &out
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// nextPage reads the next page number from a GitHub Link header or
// GitLab's x-next-page (0 when there is none).
func nextPage(h http.Header) int {
	if h == nil {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("X-Next-Page"))); err == nil && n > 0 {
		return n
	}
	for _, part := range strings.Split(h.Get("Link"), ",") {
		target, params, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(params, `rel="next"`) {
			continue
		}
		u, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>"))
		if err != nil {
			continue
		}
		if n, err := strconv.Atoi(u.Query().Get("page")); err == nil {
			return n
		}
	}
	return 0
}

// lastPage reads rel="last" (GitHub) or x-total (GitLab), for counts.
func lastPage(h http.Header) int {
	if h == nil {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("X-Total"))); err == nil {
		return n
	}
	for _, part := range strings.Split(h.Get("Link"), ",") {
		target, params, ok := strings.Cut(part, ";")
		if !ok || !strings.Contains(params, `rel="last"`) {
			continue
		}
		if u, err := url.Parse(strings.Trim(strings.TrimSpace(target), "<>")); err == nil {
			if n, err := strconv.Atoi(u.Query().Get("page")); err == nil {
				return n
			}
		}
	}
	return -1
}

// ---- cache ---------------------------------------------------------------------

// ttlCache keeps short-lived answers per person and connection (keys
// always carry both). Content keyed by a commit id doesn't change.
type ttlCache struct {
	mu sync.Mutex
	m  map[string]ttlEntry
}

type ttlEntry struct {
	v   any
	exp time.Time
}

func newTTLCache() *ttlCache { return &ttlCache{m: map[string]ttlEntry{}} }

func (c *ttlCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.exp) {
		delete(c.m, key)
		return nil, false
	}
	return e.v, true
}

func (c *ttlCache) set(key string, v any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= remoteCacheMax {
		now := time.Now()
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
		for k := range c.m {
			if len(c.m) < remoteCacheMax*3/4 {
				break
			}
			delete(c.m, k)
		}
	}
	c.m[key] = ttlEntry{v: v, exp: time.Now().Add(ttl)}
}

// forget drops a person's cached answers for one connection (after writes).
func (c *ttlCache) forget(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}

func (r *remoteRepo) cacheKey(parts ...string) string {
	return "r:" + r.userID.String() + ":" + r.conn.ID + ":" + r.id.Path + ":" + strings.Join(parts, ":")
}

func (r *remoteRepo) forgetMutable() {
	r.s.rcache.forget("r:" + r.userID.String() + ":" + r.conn.ID + ":" + r.id.Path + ":m:")
}

// ---- sources and lists ---------------------------------------------------------

// FeatureSource / FeatureSources mirror packages/shared sources.
type FeatureSource struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Provider   string `json:"provider,omitempty"`
	Product    string `json:"product,omitempty"`
	Account    string `json:"account,omitempty"`
	Label      string `json:"label"`
	Status     string `json:"status,omitempty"`
	Capability string `json:"capability,omitempty"`
	FixURL     string `json:"fix_url,omitempty"`
}

type FeatureSources struct {
	Sources []FeatureSource       `json:"sources"`
	Offers  []app.ConnectionOffer `json:"offers"`
}

// connectedSources lists the person's GitHub and GitLab connections usable
// for repositories in orgID.
func (s *Service) connectedSources(ctx context.Context, userID, orgID uuid.UUID) ([]FeatureSource, error) {
	conns := s.connections()
	if conns == nil {
		return nil, nil
	}
	var out []FeatureSource
	for _, p := range []string{ProviderGitHub, ProviderGitLab} {
		cap := capability(p, false)
		list, err := conns.For(ctx, userID, orgID, cap)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			fs := FeatureSource{ID: c.ID, Type: "connected", Provider: c.Provider, Account: c.Account, Label: platformNames[p], Status: c.Status, Capability: cap}
			if c.Status != "active" {
				fs.FixURL = "/settings/connections?capability=" + url.QueryEscape(cap) + "&return_to=" + url.QueryEscape("/repos")
			}
			out = append(out, fs)
		}
	}
	return out, nil
}

// listSources is GET /v1/repos/sources?org_id=: the app's own
// repositories, then each connected account, and what could be connected.
func (s *Service) listSources(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	orgID := uuid.Nil
	if v := r.URL.Query().Get("org_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("org_id must be a workspace id.")
		}
		if role, err := orgs.RoleOf(r.Context(), s.d.Pool, id, me.UserID); err != nil {
			return err
		} else if role == "" {
			return httpx.NotFound("Workspace not found.")
		}
		orgID = id
	}
	out := FeatureSources{Sources: []FeatureSource{{ID: "app", Type: "app", Label: s.appName()}}, Offers: []app.ConnectionOffer{}}
	conns := s.connections()
	if conns != nil {
		list, err := s.connectedSources(r.Context(), me.UserID, orgID)
		if err != nil {
			return err
		}
		out.Sources = append(out.Sources, list...)
		offers, err := conns.Offers(r.Context(), me.UserID, orgID, "repos")
		if err != nil {
			return err
		}
		for _, o := range offers {
			if !strings.HasSuffix(o.Capability, ".write") { // writing is offered on a connected repository itself
				out.Offers = append(out.Offers, o)
			}
		}
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) appName() string {
	return "Scaffold App"
}

// connectedLister is what a provider offers for listing an account's
// repositories (no single repository open yet).
type connectedLister interface {
	listRepos(ctx context.Context, q, sort, owner string, page int) ([]XRepoSummary, int, error)
	owners(ctx context.Context) ([]string, error)
}

// openAccount opens a connection without a repository, for lists and search.
func (s *Service) openAccount(ctx context.Context, userID uuid.UUID, connID string) (connectedLister, *remoteRepo, error) {
	conns := s.connections()
	if conns == nil {
		return nil, nil, errNoConnections
	}
	var conn app.Connection
	var hc *http.Client
	var err error
	for _, p := range []string{ProviderGitHub, ProviderGitLab} {
		hc, conn, err = conns.Client(ctx, userID, connID, capability(p, false))
		if err == nil && conn.Provider == p {
			break
		}
		if code := app.ConnectionErrorCode(err); err != nil && code != app.ConnectionNotFound && code != app.ConnectionScopeMissing {
			return nil, nil, s.connErr(err, p, false)
		}
		hc = nil
	}
	if hc == nil {
		return nil, nil, httpx.NotFound("That connection isn't one of yours, or it isn't connected for repositories.")
	}
	r := &remoteRepo{s: s, id: extID{Provider: conn.Provider, ConnID: conn.ID}, userID: userID, conn: conn, hc: hc, platform: platformNames[conn.Provider]}
	r.api, r.web, r.graphql = endpoints(conn)
	if r.viewer, err = s.remoteViewer(ctx, r); err != nil {
		return nil, nil, err
	}
	if conn.Provider == ProviderGitHub {
		return &githubSource{r}, r, nil
	}
	return &gitlabSource{r}, r, nil
}

// listConnectedRepos is GET /v1/repos/connections/{connectionID}/repos
// ?q=&sort=recent|name&owner=&cursor=&org_id=: repositories the account
// can open on the platform, most recently active first.
func (s *Service) listConnectedRepos(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	ctx := r.Context()
	if v := r.URL.Query().Get("org_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			ctx = app.WithConnectionOrg(ctx, id)
		}
	}
	lister, rr, err := s.openAccount(ctx, me.UserID, chi.URLParam(r, "connectionID"))
	if err != nil {
		return err
	}
	var c struct {
		P int `json:"p"`
	}
	if err := httpx.DecodeCursor(r.URL.Query().Get("cursor"), &c); err != nil {
		return err
	}
	if c.P < 1 {
		c.P = 1
	}
	sort := r.URL.Query().Get("sort")
	if sort != "name" {
		sort = "recent"
	}
	rows, next, err := lister.listRepos(ctx, strings.TrimSpace(r.URL.Query().Get("q")), sort, strings.TrimSpace(r.URL.Query().Get("owner")), c.P)
	if err != nil {
		return s.remoteErr(err, rr)
	}
	rows, err = s.agentFilterSummaries(ctx, rows)
	if err != nil {
		return err
	}
	page := httpx.Page[XRepoSummary]{Items: rows}
	if page.Items == nil {
		page.Items = []XRepoSummary{}
	}
	if next > 0 {
		page.NextCursor = httpx.EncodeCursor(map[string]int{"p": next})
	}
	owners, _ := lister.owners(ctx)
	httpx.OK(w, map[string]any{"items": page.Items, "next_cursor": page.NextCursor, "owners": owners, "account": rr.conn.Account, "provider": rr.conn.Provider})
	return nil
}

// agentFilterSummaries drops rows an agent may not read (the connection's
// scope and repos'), so lists, tools and search agree.
func (s *Service) agentFilterSummaries(ctx context.Context, rows []XRepoSummary) ([]XRepoSummary, error) {
	if _, ok := auth.AgentFrom(ctx); !ok || len(rows) == 0 {
		return rows, nil
	}
	caller := auth.FromContext(ctx)
	byConn := map[string][]string{}
	for _, row := range rows {
		if x, ok := parseExtID(row.ID); ok {
			byConn[x.ConnID] = append(byConn[x.ConnID], x.target())
		}
	}
	allowed := map[string]bool{}
	for conn, ids := range byConn {
		a, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(conn), ExternalKind, ids)
		if err != nil {
			return nil, err
		}
		b, err := app.AgentReadable(ctx, s.d.Modules, caller, "repos", ExternalKind, ids)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			allowed[id] = a[id] && b[id]
		}
	}
	out := rows[:0]
	for _, row := range rows {
		if x, ok := parseExtID(row.ID); ok && allowed[x.target()] {
			out = append(out, row)
		}
	}
	return out, nil
}

// levelFrom maps a platform role onto the app's levels, capped at view
// when writing isn't granted on the connection.
func levelFrom(manage, push, comment, canWrite bool) string {
	switch {
	case !canWrite:
		return "view"
	case manage:
		return "manage"
	case push:
		return "edit"
	case comment:
		return "comment"
	}
	return "view"
}

func levelAtLeast(level, min string) bool {
	rank := map[string]int{"none": 0, "view": 1, "comment": 2, "edit": 3, "manage": 4}
	return rank[level] >= rank[min]
}

// encodePath escapes each path segment for a URL (slashes stay).
func encodePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
