package catalog

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

const maxResolveNames = 500

var errLoading = httpx.NewError(http.StatusServiceUnavailable, "catalog_loading", "The catalog is loading. Try again in a moment.")

func (s *Service) MountPublic(r chi.Router) {
	if s.cfg.publicReads {
		s.mountReads(r)
	}
}

func (s *Service) MountUser(r chi.Router) {
	if !s.cfg.publicReads {
		s.mountReads(r)
	}
}

func (s *Service) mountReads(r chi.Router) {
	r.Get("/catalog", httpx.Handler(s.handleSummary))
	r.Get("/catalog/search", httpx.Handler(s.handleSearch))
	r.Get("/catalog/items", httpx.Handler(s.handleItems))
	r.Get("/catalog/items/{key}", httpx.Handler(s.handleItem))
	r.Get("/catalog/index", httpx.Handler(s.handleIndex))
	r.With(auth.RateLimit(s.cfg.resolvePerMin, time.Minute)).Post("/catalog/resolve", httpx.Handler(s.handleResolve))
}

// loadedIndex is the current index, or 503 until this instance has loaded one.
func (s *Service) loadedIndex() (*index, error) {
	x := s.idx.Load()
	if !x.loaded {
		return nil, errLoading
	}
	return x, nil
}

// Summary is GET /v1/catalog.
type Summary struct {
	Version     int64      `json:"version"`
	Active      int        `json:"active"`
	Retired     int        `json:"retired"`
	Categories  []Category `json:"categories"`
	ClientIndex bool       `json:"client_index"` // GET /v1/catalog/index is served (the catalog is small enough to search on the device)
}

func (s *Service) handleSummary(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	httpx.OK(w, Summary{Version: x.version, Active: x.nAct, Retired: len(x.recs) - x.nAct, Categories: x.categories(),
		ClientIndex: s.clientIndexOn(x)})
	return nil
}

func (s *Service) clientIndexOn(x *index) bool { return len(x.recs) <= s.cfg.clientIndexMax }

func filterParams(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{Category: strings.TrimSpace(q.Get("category"))}
	if v := q.Get("retired"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return f, httpx.BadRequest("retired must be true or false.")
		}
		f.Retired = b
	}
	return f, nil
}

func (s *Service) handleSearch(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	text := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(NameKey(text)) < 2 {
		return httpx.NewError(http.StatusBadRequest, "query_too_short", "Type at least 2 characters.")
	}
	if utf8.RuneCountInString(text) > maxNameLen {
		return httpx.BadRequest(fmt.Sprintf("Search for at most %d characters.", maxNameLen))
	}
	f, err := filterParams(r)
	if err != nil {
		return err
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return httpx.BadRequest("limit must be a positive number.")
		}
		limit = min(n, 50)
	}
	hits := x.search(text, f, limit)
	items := make([]Item, 0, len(hits))
	for _, h := range hits {
		items = append(items, x.item(h.i))
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// browseCursor is the last item of a page: its name key and key.
type browseCursor struct {
	N string `json:"n"`
	K string `json:"k"`
}

func (s *Service) handleItems(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	f, err := filterParams(r)
	if err != nil {
		return err
	}
	pg := httpx.ParsePage(r)
	var c browseCursor
	if err := httpx.DecodeCursor(pg.Cursor, &c); err != nil {
		return err
	}
	page := httpx.NewPage(x.browse(f, c.N, c.K, pg.Limit+1), pg.Limit, func(i int32) any { return browseCursor{N: x.keys[i], K: x.recs[i].key} })
	httpx.OK(w, httpx.Page[Item]{Items: x.items(page.Items), NextCursor: page.NextCursor})
	return nil
}

func (s *Service) handleItem(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	i, ok := x.byKey[chi.URLParam(r, "key")]
	if !ok {
		return httpx.NotFound("No item with that key.")
	}
	httpx.OK(w, x.item(i))
	return nil
}

// ResolveResult is one name's outcome (POST /v1/catalog/resolve).
type ResolveResult struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Match      string `json:"match,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Item       *Item  `json:"item"`
	Candidates []Item `json:"candidates"`
}

func resolveResult(name string, res Result) ResolveResult {
	out := ResolveResult{Name: name, OK: res.OK, Match: res.Match, Reason: res.Reason, Candidates: res.Candidates}
	if res.OK {
		it := res.Item
		out.Item = &it
	}
	if out.Candidates == nil {
		out.Candidates = []Item{}
	}
	return out
}

func (s *Service) resolveAll(x *index, names []string) []ResolveResult {
	out := make([]ResolveResult, 0, len(names))
	for _, n := range names {
		out = append(out, resolveResult(n, x.lookup(Query{Name: n})))
	}
	return out
}

func (s *Service) handleResolve(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	if len(body.Names) == 0 || len(body.Names) > maxResolveNames {
		return httpx.BadRequest(fmt.Sprintf("Send 1 to %d names.", maxResolveNames))
	}
	for _, n := range body.Names {
		if utf8.RuneCountInString(n) > maxNameLen {
			return httpx.BadRequest(fmt.Sprintf("Names are at most %d characters.", maxNameLen))
		}
	}
	httpx.OK(w, map[string]any{"results": s.resolveAll(x, body.Names)})
	return nil
}

// ---- the client index ----

// IndexItem is one item in GET /v1/catalog/index: enough to search and list
// on the device (the item page fetches the rest).
type IndexItem struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"`
	Category string   `json:"category"`
	Subtitle string   `json:"subtitle"`
	ImageURL string   `json:"image_url"`
	RefPrice *string  `json:"ref_price"`
	Currency string   `json:"currency"`
	Retired  bool     `json:"retired"`
}

// bodyCache is the encoded index response for one version.
type bodyCache struct {
	mu      sync.Mutex
	version int64
	built   bool
	json    []byte
	gz      []byte
	etag    string
}

func (s *Service) indexBody(x *index) (data, gz []byte, etag string) {
	b := &s.body
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.built && b.version == x.version {
		return b.json, b.gz, b.etag
	}
	items := make([]IndexItem, 0, len(x.recs))
	for _, r := range x.recs {
		al := r.aliases
		if al == nil {
			al = []string{}
		}
		it := IndexItem{Key: r.key, Name: r.name, Aliases: al, Category: r.category, Subtitle: r.subtitle, ImageURL: r.imageURL,
			Currency: r.currency, Retired: !r.active()}
		if r.price != "" {
			px := r.price
			it.RefPrice = &px
		}
		items = append(items, it)
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(map[string]any{"version": x.version, "items": items})
	h := fnv.New64a()
	_, _ = h.Write(buf.Bytes())
	var z bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&z, gzip.DefaultCompression)
	_, _ = zw.Write(buf.Bytes())
	_ = zw.Close()
	b.version, b.built, b.json, b.gz = x.version, true, buf.Bytes(), z.Bytes()
	b.etag = fmt.Sprintf(`W/"%d-%016x"`, x.version, h.Sum64())
	return b.json, b.gz, b.etag
}

func (s *Service) handleIndex(w http.ResponseWriter, r *http.Request) error {
	x, err := s.loadedIndex()
	if err != nil {
		return err
	}
	if !s.clientIndexOn(x) {
		return httpx.NewError(http.StatusNotFound, "client_index_off", "The catalog is too large to search on this device. Search online instead.")
	}
	data, gz, etag := s.indexBody(x)
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	if s.cfg.publicReads {
		h.Set("Cache-Control", "public, max-age=300")
	} else {
		h.Set("Cache-Control", "private, max-age=300")
	}
	h.Set("ETag", etag)
	h.Add("Vary", "Accept-Encoding")
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		data = gz
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return nil
}

func etagMatch(header, etag string) bool {
	want := strings.TrimPrefix(etag, "W/")
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || (t != "" && strings.TrimPrefix(t, "W/") == want) {
			return true
		}
	}
	return false
}

func acceptsGzip(ae string) bool {
	for _, part := range strings.Split(ae, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q := strings.ReplaceAll(params, " ", "")
			return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
		}
	}
	return false
}
