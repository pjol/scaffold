package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ManualSource is the push source for staff prints: a feed with a "manual"
// source row takes prices typed in by admins (and history they import).
const ManualSource = "manual"

var (
	pushMu      sync.Mutex
	pushSources = map[string]bool{ManualSource: true}
)

// RegisterPushSource names a source the app feeds itself with Service.Push
// (a webhook from a partner, a domain event), so feeds may use it in their
// source rows. Call it from an init().
func RegisterPushSource(name string) {
	pushMu.Lock()
	defer pushMu.Unlock()
	pushSources[name] = true
}

func isPushSource(name string) bool {
	pushMu.Lock()
	defer pushMu.Unlock()
	return pushSources[name]
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/oracle/feeds", httpx.Handler(s.adminFeeds))
	r.Get("/oracle/feeds/{key}", httpx.Handler(s.adminFeed))
	r.Get("/oracle/sources", httpx.Handler(s.adminSources))
	r.Get("/oracle/observations", httpx.Handler(func(w http.ResponseWriter, r *http.Request) error { return s.serveObservations(w, r, true) }))
	admin := r.With(auth.RequireAdminRole("admin"))
	admin.Post("/oracle/feeds", httpx.Handler(s.adminCreateFeed))
	admin.Patch("/oracle/feeds/{key}", httpx.Handler(s.adminUpdateFeed))
	admin.Put("/oracle/feeds/{key}/sources", httpx.Handler(s.adminPutSources))
	admin.Put("/oracle/feeds/{key}/parts", httpx.Handler(s.adminPutParts))
	admin.With(auth.RateLimit(s.cfg.printsPerMin, time.Minute)).Post("/oracle/feeds/{key}/prints", httpx.Handler(s.adminPostPrint))
	admin.Put("/oracle/feeds/{key}/history", httpx.Handler(s.adminImportHistory))
}

// SourceConfig is one source row as staff edit it.
type SourceConfig struct {
	ID      uuid.UUID       `json:"id"`
	Source  string          `json:"source"`
	Venue   string          `json:"venue"`
	Ref     string          `json:"ref"`
	Params  json.RawMessage `json:"params"`
	Weight  *float64        `json:"weight"`
	Premium *float64        `json:"premium"`
}

// PartConfig is one basket part as staff edit it.
type PartConfig struct {
	Key      string `json:"key"`
	Quantity string `json:"quantity"`
}

// AdminFeed is GET /v1/admin/oracle/feeds/{key}: the detail plus its configuration.
type AdminFeed struct {
	FeedDetail
	SourceConfig []SourceConfig `json:"source_config"`
	PartConfig   []PartConfig   `json:"part_config"`
}

func (s *Service) adminFeeds(w http.ResponseWriter, r *http.Request) error {
	kind, err := kindParam(r)
	if err != nil {
		return err
	}
	pg := httpx.ParsePage(r)
	var after string
	if err := httpx.DecodeCursor(pg.Cursor, &after); err != nil {
		return err
	}
	rows, err := s.listFeeds(r.Context(), feedQuery{q: r.URL.Query().Get("q"), kind: kind, all: true, after: after, limit: pg.Limit + 1})
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(rows, pg.Limit, func(f FeedInfo) any { return f.Key }))
	return nil
}

func (s *Service) adminFeedDetail(ctx context.Context, key string) (AdminFeed, error) {
	fi, err := s.feedByKey(ctx, key)
	if err != nil {
		return AdminFeed{}, err
	}
	d, err := s.feedDetail(ctx, fi, 50)
	if err != nil {
		return AdminFeed{}, err
	}
	out := AdminFeed{FeedDetail: d, SourceConfig: []SourceConfig{}, PartConfig: []PartConfig{}}
	refs, err := loadRefs(ctx, s.d.Pool, &fi.ID)
	if err != nil {
		return out, err
	}
	for _, r := range refs {
		sc := SourceConfig{ID: r.ID, Source: r.Source, Venue: r.Venue, Ref: r.Ref, Params: r.Params, Premium: r.Premium}
		if r.Weight > 0 {
			w := r.Weight
			sc.Weight = &w
		}
		if len(sc.Params) == 0 {
			sc.Params = json.RawMessage(`{}`)
		}
		out.SourceConfig = append(out.SourceConfig, sc)
	}
	for _, p := range d.Parts {
		out.PartConfig = append(out.PartConfig, PartConfig{Key: p.Key, Quantity: p.Quantity})
	}
	return out, nil
}

func (s *Service) adminFeed(w http.ResponseWriter, r *http.Request) error {
	d, err := s.adminFeedDetail(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// SourceHealth is one registered source for the admin screen.
type SourceHealth struct {
	Name        string       `json:"name"`
	Kind        string       `json:"kind"` // poll | push
	Enabled     bool         `json:"enabled"`
	OK          bool         `json:"ok"`
	Note        string       `json:"note,omitempty"`
	Feeds       int          `json:"feeds"`
	Rows        int          `json:"rows"`
	Failing     int          `json:"failing"`
	LastOKAt    *time.Time   `json:"last_ok_at"`
	LastErrorAt *time.Time   `json:"last_error_at"`
	LastError   string       `json:"last_error,omitempty"`
	MinGapMs    int64        `json:"min_gap_ms"`
	CadenceS    int64        `json:"cadence_s"`
	Quota       int          `json:"quota"`
	PerS        int64        `json:"per_s"`
	Lanes       []LaneHealth `json:"lanes"`
}

// LaneHealth is one rate-limit lane (an API host) of a polled source.
type LaneHealth struct {
	Lane     string     `json:"lane"`
	OK       bool       `json:"ok"`
	Note     string     `json:"note,omitempty"`
	LastOKAt *time.Time `json:"last_ok_at"`
}

// SourcesStatus is GET /v1/admin/oracle/sources.
type SourcesStatus struct {
	Items    []SourceHealth `json:"items"`
	Decision DecisionStatus `json:"decision"`
	Leader   LeaderStatus   `json:"leader"`
	Polling  bool           `json:"polling"`
}

// LeaderStatus says which instance runs the pipeline.
type LeaderStatus struct {
	Instance string     `json:"instance,omitempty"`
	Until    *time.Time `json:"until"`
	This     bool       `json:"this"`
}

func (s *Service) adminSources(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	byName := map[string]*SourceHealth{}
	get := func(n string) *SourceHealth {
		if sh := byName[n]; sh != nil {
			return sh
		}
		sh := &SourceHealth{Name: n, Kind: "push", Enabled: true, Lanes: []LaneHealth{}}
		byName[n] = sh
		return sh
	}
	for n := range s.sources {
		get(n)
	}
	pushMu.Lock()
	for n := range pushSources {
		get(n)
	}
	pushMu.Unlock()
	rows, err := s.d.Pool.Query(ctx, `SELECT r.source, count(DISTINCT r.feed_id)::int, count(*)::int, count(*) FILTER (WHERE NOT h.ok)::int,
   max(h.last_ok_at), max(h.last_error_at),
   (array_agg(h.last_error ORDER BY h.last_error_at DESC NULLS LAST) FILTER (WHERE NOT h.ok))[1]
 FROM oracle_feed_sources r LEFT JOIN oracle_ref_health h ON h.ref_id = r.id GROUP BY r.source`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		var feeds, refs, failing int
		var lastOK, lastErrAt *time.Time
		var lastErr *string
		if err := rows.Scan(&n, &feeds, &refs, &failing, &lastOK, &lastErrAt, &lastErr); err != nil {
			rows.Close()
			return err
		}
		sh := get(n)
		sh.Feeds, sh.Rows, sh.Failing, sh.LastOKAt, sh.LastErrorAt = feeds, refs, failing, lastOK, lastErrAt
		if lastErr != nil {
			sh.LastError = *lastErr
		}
	}
	rows.Close()
	rows, err = s.d.Pool.Query(ctx, `SELECT source, lane, ok, coalesce(note, ''), last_ok_at FROM oracle_source_health ORDER BY source, lane`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var n string
		var l LaneHealth
		if err := rows.Scan(&n, &l.Lane, &l.OK, &l.Note, &l.LastOKAt); err != nil {
			rows.Close()
			return err
		}
		if _, polled := s.sources[n]; polled {
			sh := get(n)
			sh.Lanes = append(sh.Lanes, l)
		}
	}
	rows.Close()

	out := SourcesStatus{Items: []SourceHealth{}, Polling: s.cfg.sourcesEnabled}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sh := byName[n]
		if src, ok := s.sources[n]; ok {
			b := src.Budget()
			sh.Kind = "poll"
			sh.Enabled = s.cfg.sourcesEnabled && !s.cfg.disabled[n]
			sh.MinGapMs, sh.CadenceS, sh.Quota, sh.PerS = b.gap().Milliseconds(), int64(b.Cadence/time.Second), b.Quota, int64(b.Per/time.Second)
		}
		limited := 0
		for _, l := range sh.Lanes {
			if !l.OK {
				limited++
			}
		}
		var okp *bool
		if sh.Kind == "poll" && (sh.LastOKAt != nil || sh.LastErrorAt != nil) {
			ok := sh.Failing == 0 && limited == 0
			okp = &ok
		}
		var note *string
		switch {
		case limited > 0:
			v := fmt.Sprintf("%d of %d hosts rate limited", limited, len(sh.Lanes))
			note = &v
		case sh.Failing > 0:
			v := fmt.Sprintf("%d of %d rows failing", sh.Failing, sh.Rows)
			note = &v
		}
		sh.OK, sh.Note = s.sourceNote(n, okp, note)
		out.Items = append(out.Items, *sh)
	}
	switch d := s.decision.(type) {
	case nil:
		out.Decision = DecisionStatus{Note: "Not configured (ORACLE_DECISION_URL)"}
	case *httpDecision:
		out.Decision = d.status()
		out.Decision.Queued = len(s.decideCh)
	default:
		out.Decision = DecisionStatus{Enabled: true, Name: d.Name(), Queued: len(s.decideCh)}
	}
	var until time.Time
	err = s.d.Pool.QueryRow(ctx, `SELECT holder, until FROM oracle_leader WHERE id = 1`).Scan(&out.Leader.Instance, &until)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if err == nil {
		out.Leader.Until = &until
		out.Leader.This = out.Leader.Instance == s.instance
		if until.Before(time.Now()) {
			out.Leader.Instance = ""
		}
	}
	httpx.OK(w, out)
	return nil
}

// ---- feed writes ----

// SourceInput is one source row in a create or replace request.
type SourceInput struct {
	Source  string          `json:"source"`
	Venue   string          `json:"venue"`
	Ref     string          `json:"ref"`
	Params  json.RawMessage `json:"params"`
	Weight  *float64        `json:"weight"`
	Premium *float64        `json:"premium"`
}

// CreateFeedRequest is POST /v1/admin/oracle/feeds.
type CreateFeedRequest struct {
	Key       string        `json:"key"`
	Name      string        `json:"name"`
	Category  string        `json:"category"`
	Quote     string        `json:"quote"`
	Decimals  *int32        `json:"decimals"`
	Kind      string        `json:"kind"`
	SeedPrice *string       `json:"seed_price"`
	Profile   string        `json:"profile"`
	Params    *ParamsDTO    `json:"params"`
	Listed    *bool         `json:"listed"`
	Sources   []SourceInput `json:"sources"`
	Parts     []PartConfig  `json:"parts"`
}

// UpdateFeedRequest is PATCH /v1/admin/oracle/feeds/{key}; omitted fields stay.
type UpdateFeedRequest struct {
	Name      *string    `json:"name"`
	Category  *string    `json:"category"`
	Quote     *string    `json:"quote"`
	Decimals  *int32     `json:"decimals"`
	SeedPrice *string    `json:"seed_price"` // "" clears it
	Params    *ParamsDTO `json:"params"`
	Listed    *bool      `json:"listed"`
	Enabled   *bool      `json:"enabled"`
}

var venueRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

func parsePositive(field, v string) (float64, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, httpx.BadRequest(field + " must be a positive decimal.")
	}
	return f, nil
}

func cleanName(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len([]rune(v)) > 120 {
		return "", httpx.BadRequest("name must be 1 to 120 characters.")
	}
	return v, nil
}

// validateSources checks a feed's source rows and fills defaults.
func (s *Service) validateSources(in []SourceInput) ([]SourceInput, error) {
	seen := map[string]bool{}
	out := make([]SourceInput, 0, len(in))
	if len(in) > 20 {
		return nil, httpx.BadRequest("A feed takes at most 20 sources.")
	}
	for i, si := range in {
		si.Source = strings.TrimSpace(si.Source)
		si.Ref = strings.TrimSpace(si.Ref)
		si.Venue = strings.ToLower(strings.TrimSpace(si.Venue))
		where := fmt.Sprintf("Source %d", i+1)
		src, polled := s.sources[si.Source]
		if !polled && !isPushSource(si.Source) {
			return nil, httpx.BadRequest(fmt.Sprintf("%s: unknown source %q.", where, si.Source))
		}
		if si.Venue == "" {
			if h := sourceHost(si.Ref); h != "" && si.Source == httpSourceName {
				si.Venue = strings.ToLower(h)
			} else {
				si.Venue = si.Source
			}
		}
		if !venueRe.MatchString(si.Venue) {
			return nil, httpx.BadRequest(where + ": venue must be lower-case letters, digits, dot, dash or underscore (up to 40).")
		}
		if seen[si.Venue] {
			return nil, httpx.BadRequest(where + ": each venue appears once per feed.")
		}
		seen[si.Venue] = true
		if len(si.Ref) > 2048 {
			return nil, httpx.BadRequest(where + ": ref is too long.")
		}
		if len(si.Params) == 0 || string(si.Params) == "null" {
			si.Params = json.RawMessage(`{}`)
		}
		var obj map[string]any
		if json.Unmarshal(si.Params, &obj) != nil {
			return nil, httpx.BadRequest(where + ": params must be a JSON object.")
		}
		if si.Weight != nil && (*si.Weight < 0.05 || *si.Weight > 20) {
			return nil, httpx.BadRequest(where + ": weight must be between 0.05 and 20.")
		}
		if si.Premium != nil && (*si.Premium <= -0.5 || *si.Premium > 1) {
			return nil, httpx.BadRequest(where + ": premium must be above -0.5 and at most 1.")
		}
		if v, ok := src.(interface{ ValidateRef(Ref) error }); polled && ok {
			if err := v.ValidateRef(Ref{Venue: si.Venue, Ref: si.Ref, Params: si.Params}); err != nil {
				return nil, httpx.BadRequest(where + ": " + err.Error() + ".")
			}
		}
		out = append(out, si)
	}
	return out, nil
}

func insertSources(ctx context.Context, tx pgx.Tx, feedID uuid.UUID, in []SourceInput) error {
	if _, err := tx.Exec(ctx, `DELETE FROM oracle_feed_sources WHERE feed_id = $1`, feedID); err != nil {
		return err
	}
	for _, si := range in {
		if _, err := tx.Exec(ctx, `INSERT INTO oracle_feed_sources (feed_id, source, venue, ref, params, weight, premium) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			feedID, si.Source, si.Venue, si.Ref, string(si.Params), si.Weight, si.Premium); err != nil {
			return err
		}
	}
	return nil
}

type partIn struct {
	id  uuid.UUID
	qty float64
}

// resolveParts checks basket parts: existing singles, once each, positive quantities.
func resolveParts(ctx context.Context, q db.Querier, basketID uuid.UUID, in []PartConfig) ([]partIn, error) {
	if len(in) == 0 || len(in) > 100 {
		return nil, httpx.BadRequest("A basket takes 1 to 100 parts.")
	}
	seen := map[uuid.UUID]bool{}
	out := make([]partIn, 0, len(in))
	for i, p := range in {
		key := strings.ToUpper(strings.TrimSpace(p.Key))
		qty, err := parsePositive(fmt.Sprintf("Part %d quantity", i+1), p.Quantity)
		if err != nil {
			return nil, err
		}
		var id uuid.UUID
		var kind string
		err = q.QueryRow(ctx, `SELECT id, kind FROM oracle_feeds WHERE key = $1`, key).Scan(&id, &kind)
		if db.IsNoRows(err) {
			return nil, httpx.BadRequest(fmt.Sprintf("Part %d: no feed %s.", i+1, key))
		}
		if err != nil {
			return nil, err
		}
		if kind != FeedSingle || id == basketID {
			return nil, httpx.BadRequest(fmt.Sprintf("Part %d: %s is a basket; parts are single feeds.", i+1, key))
		}
		if seen[id] {
			return nil, httpx.BadRequest(fmt.Sprintf("Part %d: %s appears twice.", i+1, key))
		}
		seen[id] = true
		out = append(out, partIn{id, qty})
	}
	return out, nil
}

func insertParts(ctx context.Context, tx pgx.Tx, basketID uuid.UUID, parts []partIn) error {
	if _, err := tx.Exec(ctx, `DELETE FROM oracle_basket_parts WHERE basket_id = $1`, basketID); err != nil {
		return err
	}
	for i, p := range parts {
		if _, err := tx.Exec(ctx, `INSERT INTO oracle_basket_parts (basket_id, feed_id, quantity, position) VALUES ($1,$2,$3,$4)`, basketID, p.id, p.qty, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) adminCreateFeed(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var req CreateFeedRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	key := strings.ToUpper(strings.TrimSpace(req.Key))
	if !feedKeyOK(key) {
		return httpx.BadRequest("key must be 1 to 48 upper-case letters, digits, dots, dashes or underscores, starting with a letter or digit.")
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	kind := req.Kind
	if kind == "" {
		kind = FeedSingle
	}
	if kind != FeedSingle && kind != FeedBasket {
		return httpx.BadRequest("kind must be single or basket.")
	}
	quote := strings.ToUpper(strings.TrimSpace(req.Quote))
	if quote == "" {
		quote = "USD"
	}
	if len(quote) > 12 {
		return httpx.BadRequest("quote must be up to 12 characters.")
	}
	decimals := int32(2)
	if req.Decimals != nil {
		decimals = *req.Decimals
	}
	if decimals < 0 || decimals > 10 {
		return httpx.BadRequest("decimals must be between 0 and 10.")
	}
	category := strings.TrimSpace(req.Category)
	if len([]rune(category)) > 60 {
		return httpx.BadRequest("category must be up to 60 characters.")
	}
	var seed *float64
	if req.SeedPrice != nil && strings.TrimSpace(*req.SeedPrice) != "" {
		v, err := parsePositive("seed_price", *req.SeedPrice)
		if err != nil {
			return err
		}
		seed = &v
	}
	profile := req.Profile
	if profile == "" {
		profile = "intraday"
	}
	p, ok := Profiles[profile]
	if !ok {
		return httpx.BadRequest("profile must be daily, intraday or realtime.")
	}
	if req.Params != nil {
		p = req.Params.params()
	}
	if err := p.Validate(); err != nil {
		return httpx.BadRequest(err.Error() + ".")
	}
	listed := true
	if req.Listed != nil {
		listed = *req.Listed
	}
	var sources []SourceInput
	if kind == FeedSingle {
		if len(req.Parts) > 0 {
			return httpx.BadRequest("Only baskets have parts.")
		}
		if sources, err = s.validateSources(req.Sources); err != nil {
			return err
		}
	} else if len(req.Sources) > 0 {
		return httpx.BadRequest("Baskets have parts, not sources.")
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO oracle_feeds (key, name, category, quote, decimals, kind, seed_price, half_life_s, stale_after_s,
 max_move_bps_per_min, lookback_s, ref_window_s, burst_window_s, listed, created_by)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
			key, name, category, quote, decimals, kind, seed, int64(p.HalfLife/time.Second), int64(p.StaleAfter/time.Second),
			p.MaxMoveBpsPerMin, int64(p.Lookback/time.Second), int64(p.RefWindow/time.Second), int64(p.BurstWindow/time.Second), listed, me.UserID).Scan(&id)
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A feed with key " + key + " exists.")
		}
		if err != nil {
			return err
		}
		if kind == FeedSingle {
			if err := insertSources(ctx, tx, id, sources); err != nil {
				return err
			}
		} else {
			parts, err := resolveParts(ctx, tx, id, req.Parts)
			if err != nil {
				return err
			}
			if err := insertParts(ctx, tx, id, parts); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, me.UserID, "oracle.feed.create", "oracle_feed", id.String(), map[string]any{"key": key, "kind": kind})
	})
	if err != nil {
		return err
	}
	d, err := s.adminFeedDetail(ctx, key)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, d)
	return nil
}

func (s *Service) feedForWrite(ctx context.Context, q db.Querier, key string) (uuid.UUID, string, error) {
	var id uuid.UUID
	var kind string
	err := q.QueryRow(ctx, `SELECT id, kind FROM oracle_feeds WHERE key = $1 FOR UPDATE`, strings.ToUpper(strings.TrimSpace(key))).Scan(&id, &kind)
	if db.IsNoRows(err) {
		return id, kind, httpx.NotFound("No feed " + key + ".")
	}
	return id, kind, err
}

func (s *Service) adminUpdateFeed(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	key := chi.URLParam(r, "key")
	var req UpdateFeedRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	changes := map[string]any{}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		id, _, err := s.feedForWrite(ctx, tx, key)
		if err != nil {
			return err
		}
		set := func(col string, v any) error {
			changes[col] = v
			_, err := tx.Exec(ctx, `UPDATE oracle_feeds SET `+col+` = $2 WHERE id = $1`, id, v)
			return err
		}
		if req.Name != nil {
			n, err := cleanName(*req.Name)
			if err != nil {
				return err
			}
			if err := set("name", n); err != nil {
				return err
			}
		}
		if req.Category != nil {
			c := strings.TrimSpace(*req.Category)
			if len([]rune(c)) > 60 {
				return httpx.BadRequest("category must be up to 60 characters.")
			}
			if err := set("category", c); err != nil {
				return err
			}
		}
		if req.Quote != nil {
			q := strings.ToUpper(strings.TrimSpace(*req.Quote))
			if q == "" || len(q) > 12 {
				return httpx.BadRequest("quote must be 1 to 12 characters.")
			}
			if err := set("quote", q); err != nil {
				return err
			}
		}
		if req.Decimals != nil {
			if *req.Decimals < 0 || *req.Decimals > 10 {
				return httpx.BadRequest("decimals must be between 0 and 10.")
			}
			if err := set("decimals", *req.Decimals); err != nil {
				return err
			}
		}
		if req.SeedPrice != nil {
			var seed *float64
			if strings.TrimSpace(*req.SeedPrice) != "" {
				v, err := parsePositive("seed_price", *req.SeedPrice)
				if err != nil {
					return err
				}
				seed = &v
			}
			if err := set("seed_price", seed); err != nil {
				return err
			}
		}
		if req.Params != nil {
			p := req.Params.params()
			if err := p.Validate(); err != nil {
				return httpx.BadRequest(err.Error() + ".")
			}
			changes["params"] = req.Params
			if _, err := tx.Exec(ctx, `UPDATE oracle_feeds SET half_life_s=$2, stale_after_s=$3, max_move_bps_per_min=$4, lookback_s=$5,
 ref_window_s=$6, burst_window_s=$7 WHERE id = $1`, id, req.Params.HalfLifeS, req.Params.StaleAfterS, req.Params.MaxMoveBpsPerMin,
				req.Params.LookbackS, req.Params.RefWindowS, req.Params.BurstWindowS); err != nil {
				return err
			}
		}
		if req.Listed != nil {
			if err := set("listed", *req.Listed); err != nil {
				return err
			}
		}
		if req.Enabled != nil {
			if err := set("enabled", *req.Enabled); err != nil {
				return err
			}
		}
		if len(changes) == 0 {
			return httpx.BadRequest("Nothing to change.")
		}
		if _, err := tx.Exec(ctx, `UPDATE oracle_feeds SET updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "oracle.feed.update", "oracle_feed", id.String(), changes)
	})
	if err != nil {
		return err
	}
	d, err := s.adminFeedDetail(ctx, key)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) adminPutSources(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	key := chi.URLParam(r, "key")
	var req struct {
		Sources []SourceInput `json:"sources"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	sources, err := s.validateSources(req.Sources)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		id, kind, err := s.feedForWrite(ctx, tx, key)
		if err != nil {
			return err
		}
		if kind != FeedSingle {
			return httpx.BadRequest("Baskets have parts, not sources.")
		}
		if err := insertSources(ctx, tx, id, sources); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE oracle_feeds SET updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		venues := make([]string, 0, len(sources))
		for _, si := range sources {
			venues = append(venues, si.Source+":"+si.Venue)
		}
		return audit.Log(ctx, tx, me.UserID, "oracle.feed.sources", "oracle_feed", id.String(), map[string]any{"sources": venues})
	})
	if err != nil {
		return err
	}
	d, err := s.adminFeedDetail(ctx, key)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) adminPutParts(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	key := chi.URLParam(r, "key")
	var req struct {
		Parts []PartConfig `json:"parts"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		id, kind, err := s.feedForWrite(ctx, tx, key)
		if err != nil {
			return err
		}
		if kind != FeedBasket {
			return httpx.BadRequest("Only baskets have parts.")
		}
		parts, err := resolveParts(ctx, tx, id, req.Parts)
		if err != nil {
			return err
		}
		if err := insertParts(ctx, tx, id, parts); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE oracle_feeds SET updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "oracle.feed.parts", "oracle_feed", id.String(), map[string]any{"parts": req.Parts})
	})
	if err != nil {
		return err
	}
	d, err := s.adminFeedDetail(ctx, key)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// PostPrintRequest is POST /v1/admin/oracle/feeds/{key}/prints.
type PostPrintRequest struct {
	Price      string     `json:"price"`
	Kind       string     `json:"kind"`
	Qty        *float64   `json:"qty"`
	ObservedAt *time.Time `json:"observed_at"`
	Venue      string     `json:"venue"`
	Note       string     `json:"note"`
}

func (s *Service) adminPostPrint(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	key := strings.ToUpper(chi.URLParam(r, "key"))
	var req PostPrintRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	price, err := parsePositive("price", req.Price)
	if err != nil {
		return err
	}
	kind := req.Kind
	if kind == "" {
		kind = KindSale
	}
	if !validKind(kind) {
		return httpx.BadRequest("kind must be sale, market or listing.")
	}
	at := time.Now()
	if req.ObservedAt != nil {
		at = *req.ObservedAt
	}
	if req.Qty != nil && (*req.Qty <= 0 || *req.Qty > 1e12) {
		return httpx.BadRequest("qty must be positive.")
	}
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 120 {
		return httpx.BadRequest("note must be up to 120 characters.")
	}
	var feedID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var venue string
		var lookbackS int64
		err := tx.QueryRow(ctx, `SELECT f.id, r.venue, f.lookback_s FROM oracle_feeds f JOIN oracle_feed_sources r ON r.feed_id = f.id
 WHERE f.key = $1 AND r.source = $2 AND ($3 = '' OR r.venue = $3) ORDER BY r.venue LIMIT 1`, key, ManualSource, strings.ToLower(req.Venue)).Scan(&feedID, &venue, &lookbackS)
		if db.IsNoRows(err) {
			return httpx.NewError(http.StatusConflict, "no_manual_source", "This feed takes no staff prints. Add a manual source to it first.")
		}
		if err != nil {
			return err
		}
		if err := checkPrintTime(at, time.Duration(lookbackS)*time.Second); err != nil {
			return httpx.BadRequest(err.Error() + ".")
		}
		id := uuid.New()
		if err := pushTx(ctx, tx, feedID, ManualSource, Print{Venue: venue, Kind: kind, Price: price, Qty: deref(req.Qty), At: at, ID: id.String(), Note: note}, &me.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me.UserID, "oracle.print", "oracle_feed", feedID.String(),
			map[string]any{"price": req.Price, "kind": kind, "venue": venue, "observed_at": at.UTC()})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"queued": true})
	return nil
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// checkPrintTime rejects a pushed print the pipeline would drop: from the
// future, or older than the feed's lookback (less a margin for the wait in
// the inbox).
func checkPrintTime(at time.Time, lookback time.Duration) error {
	now := time.Now()
	if at.After(now.Add(time.Minute)) {
		return fmt.Errorf("observed_at can't be in the future")
	}
	if now.Sub(at) > lookback-min(time.Minute, lookback/10) {
		return fmt.Errorf("observed_at must be within the feed's lookback (the last %s)", spanWords(lookback))
	}
	return nil
}

// spanWords: "2 days", "36 hours", "45 minutes".
func spanWords(d time.Duration) string {
	unit := func(n int, one string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %ss", n, one)
	}
	switch {
	case d >= 48*time.Hour && d%(24*time.Hour) == 0:
		return unit(int(d/(24*time.Hour)), "day")
	case d >= time.Hour:
		return unit(int(d/time.Hour), "hour")
	}
	return unit(max(int(d/time.Minute), 1), "minute")
}

// pushTx queues one print in the inbox. Its dedupe key is the observation's
// (source:venue:id), unique per feed, so a retry with the same p.ID is a no-op
// and two feeds can reuse a venue's trade ids.
func pushTx(ctx context.Context, q db.Querier, feedID uuid.UUID, source string, p Print, actor *uuid.UUID) error {
	var qty any
	if p.Qty > 0 {
		qty = p.Qty
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	key := rawPrint{Print: p, Source: source}.dedupe()
	_, err := q.Exec(ctx, `INSERT INTO oracle_inbox (feed_id, source, venue, kind, price, qty, observed_at, note, actor_id, dedupe_key)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (feed_id, dedupe_key) DO NOTHING`,
		feedID, source, p.Venue, p.Kind, p.Price, qty, p.At, nullString(p.Note), actor, key)
	return err
}

// Push queues a print from app code for a feed by key, on any instance; the
// leader screens it like a polled one (also when the feed was created a
// moment ago: the print waits until the leader has loaded it). source must be
// ManualSource or one registered with RegisterPushSource, and the feed must
// have a source row for (source, p.Venue). p.At must be within the feed's
// lookback and not in the future. Pass a stable p.ID (unique per feed, source
// and venue) to make retries idempotent; without one every call is a new print.
func (s *Service) Push(ctx context.Context, q db.Querier, feedKey, source string, p Print) error {
	if !isPushSource(source) {
		return fmt.Errorf("oracle: %q is not a push source", source)
	}
	if !validKind(p.Kind) || p.Price <= 0 || math.IsNaN(p.Price) || math.IsInf(p.Price, 0) {
		return fmt.Errorf("oracle: invalid print")
	}
	if p.At.IsZero() {
		p.At = time.Now()
	}
	var feedID uuid.UUID
	var lookbackS int64
	err := q.QueryRow(ctx, `SELECT f.id, f.lookback_s FROM oracle_feeds f JOIN oracle_feed_sources r ON r.feed_id = f.id
 WHERE f.key = $1 AND r.source = $2 AND r.venue = $3`, feedKey, source, p.Venue).Scan(&feedID, &lookbackS)
	if db.IsNoRows(err) {
		return fmt.Errorf("oracle: feed %s has no %s source for venue %q", feedKey, source, p.Venue)
	}
	if err != nil {
		return err
	}
	if err := checkPrintTime(p.At, time.Duration(lookbackS)*time.Second); err != nil {
		return fmt.Errorf("oracle: feed %s: %w", feedKey, err)
	}
	return pushTx(ctx, q, feedID, source, p, nil)
}

// ImportHistoryRequest is PUT /v1/admin/oracle/feeds/{key}/history.
type ImportHistoryRequest struct {
	Points []struct {
		Day   string  `json:"day"`
		Close string  `json:"close"`
		Low   *string `json:"low"`
		High  *string `json:"high"`
	} `json:"points"`
}

func (s *Service) adminImportHistory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	fi, err := s.feedByKey(ctx, chi.URLParam(r, "key"))
	if err != nil {
		return err
	}
	if fi.Kind != FeedSingle {
		return httpx.BadRequest("Baskets take their history from their parts.")
	}
	var req ImportHistoryRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Points) == 0 || len(req.Points) > historyDays+7 {
		return httpx.BadRequest(fmt.Sprintf("Send 1 to %d points.", historyDays+7))
	}
	pts := make([]DailyPrice, 0, len(req.Points))
	for i, p := range req.Points {
		day, err := time.Parse("2006-01-02", p.Day)
		if err != nil {
			return httpx.BadRequest(fmt.Sprintf("Point %d: day must be YYYY-MM-DD.", i+1))
		}
		c, err := parsePositive(fmt.Sprintf("Point %d close", i+1), p.Close)
		if err != nil {
			return err
		}
		dp := DailyPrice{Day: day, Close: c}
		if p.Low != nil {
			if dp.Low, err = parsePositive(fmt.Sprintf("Point %d low", i+1), *p.Low); err != nil {
				return err
			}
		}
		if p.High != nil {
			if dp.High, err = parsePositive(fmt.Sprintf("Point %d high", i+1), *p.High); err != nil {
				return err
			}
		}
		pts = append(pts, dp)
	}
	live := 0.0
	if fi.Status == StatusLive || fi.Status == StatusStale {
		live = fi.priceF
	}
	n, err := s.storeHistory(ctx, fi.ID, ManualSource, pts, live)
	if err != nil {
		return httpx.BadRequest(err.Error() + ".")
	}
	if err := audit.Log(ctx, s.d.Pool, me.UserID, "oracle.history.import", "oracle_feed", fi.ID.String(), map[string]any{"points": n}); err != nil {
		return err
	}
	s.emitHistory(ctx, fi.ID)
	httpx.OK(w, map[string]any{"stored": n})
	return nil
}
