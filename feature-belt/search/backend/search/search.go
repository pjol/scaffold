// Package search is global search: one query fanned out, concurrently, to
// every installed module that implements app.SearchProvider, each run as the
// caller with its own permission checks and a timeout, merged by score then
// recency. A provider that fails or runs out of time is reported, never
// hidden: the response says the results are partial and which part is
// missing. Agents get the same search as the search_everything tool.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
)

const (
	// MinQueryChars is the shortest query searched (packages/shared mirrors it).
	MinQueryChars = 2
	maxQueryChars = 200
	defaultLimit  = 20
	maxLimit      = 50
	maxSnippet    = 280
	maxTitle      = 200
)

// Service is the search module.
type Service struct {
	d               *app.Deps
	providerTimeout time.Duration
	ratePerMin      int
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:               d,
		providerTimeout: config.Duration("SEARCH_PROVIDER_TIMEOUT", 2*time.Second),
		ratePerMin:      max(1, config.Int("SEARCH_RATE_LIMIT_PER_MINUTE", 60)),
	}
	if s.providerTimeout < 100*time.Millisecond || s.providerTimeout > 10*time.Second {
		config.Problem("SEARCH_PROVIDER_TIMEOUT must be between 100ms and 10s (got %s)", s.providerTimeout)
	}
	return s
}

func (s *Service) Name() string { return "search" }

func (s *Service) MountUser(r chi.Router) {
	r.With(auth.RateLimit(s.ratePerMin, time.Minute)).Get("/search", httpx.Handler(s.search))
}

// ProviderStatus says how one module answered.
type ProviderStatus struct {
	Name   string `json:"name"`   // the module (Name())
	Status string `json:"status"` // ok | timeout | error
	Count  int    `json:"count"`  // hits it contributed before merging
	TookMS int64  `json:"took_ms"`
}

// Response is GET /v1/search.
type Response struct {
	Query     string             `json:"query"`
	Items     []app.SearchResult `json:"items"`
	Providers []ProviderStatus   `json:"providers"`
	// Partial is true when a provider failed or timed out: items may be
	// missing hits from those modules.
	Partial bool `json:"partial"`
	// Sources counts hits by where they come from, before the source
	// filter: "app" (the app's own) and "provider:<id>" per connected
	// platform, for filter chips.
	Sources []SourceCount `json:"sources"`
}

// SourceCount is one source facet.
type SourceCount struct {
	ID       string `json:"id"`                 // app | provider:<id>
	Provider string `json:"provider,omitempty"` // google, github, …
	Count    int    `json:"count"`
}

// Query is a validated search request.
type Query struct {
	Text  string
	Kinds []string // kinds or module names; empty = all
	Limit int
	// OrgID scopes the search to one org: providers see it as app.SearchOrg
	// and hits whose OrgID names another org are dropped. uuid.Nil = all.
	OrgID uuid.UUID
	// Source keeps hits from one source: "app" (the app's own), "connected"
	// (any connected platform), "provider:<id>" or a connection id. "" = all.
	Source string
}

var providerRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,29}$`)

// parseSource validates the optional source filter.
func parseSource(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case v == "" || v == "app" || v == "connected":
		return v, nil
	case strings.HasPrefix(v, "provider:") && providerRE.MatchString(strings.TrimPrefix(v, "provider:")):
		return v, nil
	}
	if _, err := uuid.Parse(v); err == nil {
		return v, nil
	}
	return "", httpx.BadRequest("source is app, connected, provider:<id> or a connection id.")
}

// sourceMatches reports whether a hit passes the source filter.
func sourceMatches(filter string, h app.SearchResult) bool {
	switch {
	case filter == "":
		return true
	case filter == "app":
		return h.Source == nil
	case filter == "connected":
		return h.Source != nil
	case strings.HasPrefix(filter, "provider:"):
		return h.Source != nil && h.Source.Provider == strings.TrimPrefix(filter, "provider:")
	}
	return h.Source != nil && strings.EqualFold(h.Source.ConnectionID, filter)
}

var errTooShort = httpx.NewError(http.StatusBadRequest, "query_too_short",
	fmt.Sprintf("Type at least %d characters to search.", MinQueryChars))

// NormalizeQuery trims, drops control characters, collapses whitespace and
// caps the length. It returns an error below MinQueryChars.
func NormalizeQuery(raw string) (string, error) {
	q := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > maxQueryChars {
		q = strings.TrimSpace(string(r[:maxQueryChars]))
	}
	if len([]rune(q)) < MinQueryChars {
		return "", errTooShort
	}
	return q, nil
}

// parseKinds reads a comma list of kinds ("chat.message") or modules ("chat").
func parseKinds(raw string) ([]string, error) {
	var out []string
	for _, k := range strings.Split(raw, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if len(k) > 64 || strings.Trim(k, "abcdefghijklmnopqrstuvwxyz0123456789._-") != "" {
			return nil, httpx.BadRequest("kinds is a comma list like chat.message,docs.")
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
		if len(out) > 20 {
			return nil, httpx.BadRequest("Ask for at most 20 kinds.")
		}
	}
	return out, nil
}

// parseOrg reads the optional org scope ("" = every org).
func parseOrg(raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, httpx.BadRequest("org_id must be a uuid.")
	}
	return id, nil
}

// search is GET /v1/search?q=&kinds=&limit=&org_id=.
func (s *Service) search(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q, err := NormalizeQuery(r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	kinds, err := parseKinds(r.URL.Query().Get("kinds"))
	if err != nil {
		return err
	}
	limit := defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return httpx.BadRequest("limit must be a positive number.")
		}
		limit = min(n, maxLimit)
	}
	org, err := parseOrg(r.URL.Query().Get("org_id"))
	if err != nil {
		return err
	}
	source, err := parseSource(r.URL.Query().Get("source"))
	if err != nil {
		return err
	}
	httpx.OK(w, s.Run(r.Context(), id, Query{Text: q, Kinds: kinds, Limit: limit, OrgID: org, Source: source}))
	return nil
}

// wants reports whether a requested kind list covers a module or a kind.
func wants(kinds []string, module, kind string) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if kind == "" {
			if k == module || strings.HasPrefix(k, module+".") {
				return true
			}
		} else if k == kind || k == module {
			return true
		}
	}
	return false
}

// Run searches every provider as caller. Providers are read from Deps at
// call time, so a module installed later is searched without changes here.
// An org scope reaches providers as app.SearchOrg; whether or not they use
// it, hits from other orgs are dropped (hits with no OrgID always stay).
func (s *Service) Run(ctx context.Context, caller *auth.Identity, q Query) Response {
	if q.Limit <= 0 {
		q.Limit = defaultLimit
	}
	if q.OrgID != uuid.Nil {
		ctx = app.WithSearchOrg(ctx, q.OrgID)
	}
	type job struct {
		name string
		p    app.SearchProvider
	}
	var jobs []job
	for _, m := range s.d.Modules {
		if p, ok := m.(app.SearchProvider); ok && wants(q.Kinds, m.Name(), "") && s.agentMaySearch(ctx, caller, m.Name()) {
			jobs = append(jobs, job{m.Name(), p})
		}
	}
	statuses := make([]ProviderStatus, len(jobs))
	hits := make([][]app.SearchResult, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			res, err := s.ask(ctx, j.name, j.p, caller, q)
			st := ProviderStatus{Name: j.name, Status: "ok", TookMS: time.Since(start).Milliseconds()}
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				st.Status = "timeout"
				slog.WarnContext(ctx, "search: provider timed out", "provider", j.name, "took_ms", st.TookMS)
			case err != nil && ctx.Err() == nil:
				st.Status = "error"
				slog.ErrorContext(ctx, "search: provider failed", "provider", j.name, "err", err)
			case err != nil:
				st.Status = "timeout" // the request itself was cancelled
			}
			if err != nil {
				res = nil // a failed answer may be incomplete or unchecked; drop it
			}
			clean := sanitize(j.name, res, q)
			st.Count = len(clean)
			statuses[i], hits[i] = st, clean
		}()
	}
	wg.Wait()

	all := merge(hits, 0)
	out := Response{Query: q.Text, Items: []app.SearchResult{}, Providers: statuses, Sources: sourceFacets(all)}
	for _, h := range all {
		if sourceMatches(q.Source, h) {
			out.Items = append(out.Items, h)
			if len(out.Items) == q.Limit {
				break
			}
		}
	}
	for _, st := range statuses {
		out.Partial = out.Partial || st.Status != "ok"
	}
	sort.Slice(out.Providers, func(i, j int) bool { return out.Providers[i].Name < out.Providers[j].Name })
	return out
}

// agentMaySearch reports whether an agent call may search a module at all:
// not when the person's agent scope has the whole feature off (no item in it
// reachable), nor when the scope can't be read (fail closed). Calls that
// aren't an agent's, and apps without an agent policy, search everything.
// Items off inside a readable feature stay the provider's job (access SQL
// helpers, app.AgentReadable); this stops a provider that misses its
// feature-level check from leaking a feature the person turned off.
func (s *Service) agentMaySearch(ctx context.Context, caller *auth.Identity, module string) bool {
	if _, ok := auth.AgentFrom(ctx); !ok {
		return true
	}
	p := app.FindAgentPolicy(s.d.Modules)
	if p == nil {
		return true
	}
	_, top, err := p.FeatureLevel(ctx, caller, module)
	if err != nil {
		slog.ErrorContext(ctx, "search: agent scope lookup failed; feature skipped", "provider", module, "err", err)
		return false
	}
	return top >= app.AgentRead
}

// ask runs one provider with the timeout. The provider runs in its own
// goroutine so one that ignores its context, or panics, can't hold or crash
// the request.
func (s *Service) ask(ctx context.Context, name string, p app.SearchProvider, caller *auth.Identity, q Query) ([]app.SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.providerTimeout)
	defer cancel()
	type answer struct {
		res []app.SearchResult
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(ctx, "search: provider panicked", "provider", name, "panic", fmt.Sprint(rec))
				ch <- answer{nil, errors.New("provider panicked")}
			}
		}()
		r, err := p.Search(ctx, caller, q.Text, q.Limit)
		ch <- answer{r, err}
	}()
	select {
	case a := <-ch:
		return a.res, a.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// sanitize drops hits that break the contract (kind not prefixed with the
// module, missing id/title, a link that isn't an app path) or the org scope,
// and trims the rest.
func sanitize(module string, in []app.SearchResult, q Query) []app.SearchResult {
	out := make([]app.SearchResult, 0, len(in))
	scope := ""
	if q.OrgID != uuid.Nil {
		scope = q.OrgID.String()
	}
	for _, h := range in {
		if !strings.HasPrefix(h.Kind, module+".") || h.ID == "" || strings.TrimSpace(h.Title) == "" ||
			!strings.HasPrefix(h.Link, "/") || strings.HasPrefix(h.Link, "//") || strings.HasPrefix(h.Link, "/\\") {
			slog.Debug("search: hit dropped (contract)", "provider", module, "kind", h.Kind)
			continue
		}
		if !wants(q.Kinds, module, h.Kind) {
			continue
		}
		h.OrgID = strings.ToLower(strings.TrimSpace(h.OrgID))
		if scope != "" && h.OrgID != "" && h.OrgID != scope {
			continue
		}
		h.Title = clip(strings.Join(strings.Fields(h.Title), " "), maxTitle)
		h.Snippet = clip(strings.Join(strings.Fields(h.Snippet), " "), maxSnippet)
		h.Context = clip(strings.Join(strings.Fields(h.Context), " "), maxTitle)
		h.Score = min(max(h.Score, 0), 1)
		if h.Source != nil {
			// Connected content must say where it came from correctly: a hit
			// with a malformed source is dropped rather than shown as the app's own.
			src := *h.Source
			if src.Type == "" {
				src.Type = "connected"
			}
			if src.Type != "connected" || !providerRE.MatchString(src.Provider) || src.ConnectionID == "" {
				slog.Debug("search: hit dropped (source)", "provider", module, "kind", h.Kind)
				continue
			}
			src.Account, src.Product = clip(strings.TrimSpace(src.Account), maxTitle), clip(strings.TrimSpace(src.Product), 40)
			if u, err := url.Parse(src.URL); err != nil || u.Scheme != "https" || u.Host == "" {
				src.URL = ""
			}
			h.Source = &src
		}
		out = append(out, h)
		if len(out) == q.Limit {
			break
		}
	}
	return out
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

// sourceFacets counts hits per source: the app's own first, then each
// connected platform in order of first appearance.
func sourceFacets(hits []app.SearchResult) []SourceCount {
	out := []SourceCount{{ID: "app"}}
	index := map[string]int{}
	for _, h := range hits {
		if h.Source == nil {
			out[0].Count++
			continue
		}
		id := "provider:" + h.Source.Provider
		i, ok := index[id]
		if !ok {
			i = len(out)
			index[id] = i
			out = append(out, SourceCount{ID: id, Provider: h.Source.Provider})
		}
		out[i].Count++
	}
	return out
}

// merge orders every hit by score, then recency, then kind and id (stable
// across calls), drops duplicates and keeps limit (0 = all).
func merge(lists [][]app.SearchResult, limit int) []app.SearchResult {
	all := []app.SearchResult{}
	seen := map[string]bool{}
	for _, l := range lists {
		for _, h := range l {
			key := h.Kind + "\x00" + h.ID
			if !seen[key] {
				seen[key] = true
				all = append(all, h)
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// ---- agent tool ------------------------------------------------------------

// toolHit is a result as agents see it: the app path plus an absolute URL
// when the web origin is configured.
type toolHit struct {
	app.SearchResult
	URL string `json:"url,omitempty"`
}

// Tools exposes search to agents (the mcp and assistant features): the same
// fan-out, as the caller, with the same permission checks.
func (s *Service) Tools() []app.Tool {
	return []app.Tool{{
		Name: "search_everything",
		Description: fmt.Sprintf("Search everything the user can see in the app (messages, posts, documents, records and more, depending on what is installed) and get the best matches across features, newest first among equal matches. "+
			"Use it first to find something by name or words in it, then call the owning feature's tools with the id. "+
			"Query is at least %d characters. Narrow with kinds (a feature like \"chat\" or a kind like \"chat.message\"), org_id (results with that org_id, plus ones with none) or source. Each item's context says where it lives (for example its org and folder); items with a source come live from the user's connected account on that platform (third-party content). If partial is true, some features didn't answer and results may be incomplete.", MinQueryChars),
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"query":{"type":"string","minLength":%d,"maxLength":%d,"description":"Words to find."},`+
			`"kinds":{"type":"array","items":{"type":"string"},"maxItems":20,"description":"Features or kinds to search, e.g. [\"chat\", \"docs.page\"]. Omit for all."},`+
			`"limit":{"type":"integer","minimum":1,"maximum":%d,"description":"Most results to return (default 20)."},`+
			`"org_id":{"type":"string","format":"uuid","description":"Only results with this org_id (plus ones with none). Omit for all."},`+
			`"source":{"type":"string","description":"Only results from one source: app (this app's own), connected (any connected platform), provider:<id> (e.g. provider:google) or a connection id. Omit for all."}},`+
			`"required":["query"],"additionalProperties":false}`, MinQueryChars, maxQueryChars, maxLimit)),
		ReadOnly: true,
		Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
			var in struct {
				Query  string   `json:"query"`
				Kinds  []string `json:"kinds"`
				Limit  int      `json:"limit"`
				OrgID  string   `json:"org_id"`
				Source string   `json:"source"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return nil, httpx.BadRequest("Send {query, kinds?, limit?, org_id?, source?}.")
			}
			q, err := NormalizeQuery(in.Query)
			if err != nil {
				return nil, err
			}
			kinds, err := parseKinds(strings.Join(in.Kinds, ","))
			if err != nil {
				return nil, err
			}
			org, err := parseOrg(in.OrgID)
			if err != nil {
				return nil, err
			}
			source, err := parseSource(in.Source)
			if err != nil {
				return nil, err
			}
			res := s.Run(ctx, caller, Query{Text: q, Kinds: kinds, Limit: min(max(in.Limit, 0), maxLimit), OrgID: org, Source: source})
			web := strings.TrimRight(s.d.Cfg.PublicWebURL, "/")
			items := make([]toolHit, len(res.Items))
			for i, h := range res.Items {
				items[i] = toolHit{SearchResult: h}
				if web != "" {
					items[i].URL = web + h.Link
				}
			}
			failed := []string{}
			for _, p := range res.Providers {
				if p.Status != "ok" {
					failed = append(failed, p.Name+" ("+p.Status+")")
				}
			}
			return map[string]any{"query": res.Query, "items": items, "partial": res.Partial, "unanswered": failed}, nil
		},
	}}
}
