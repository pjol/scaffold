package oracle

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// FeedInfo is one feed with its published price (GET /v1/oracle/feeds).
type FeedInfo struct {
	ID        uuid.UUID  `json:"id"`
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	Category  string     `json:"category"`
	Quote     string     `json:"quote"`
	Decimals  int32      `json:"decimals"`
	Kind      string     `json:"kind"`
	Listed    bool       `json:"listed"`
	Enabled   bool       `json:"enabled"`
	Price     *string    `json:"price"`
	Target    *string    `json:"target"`
	Status    string     `json:"status"`
	UpdatedAt *time.Time `json:"updated_at"`
	Venues    []string   `json:"venues"`
	PartCount int        `json:"part_count"`

	priceF float64
}

// ParamsDTO is Params on the wire, in seconds.
type ParamsDTO struct {
	HalfLifeS        int64   `json:"half_life_s"`
	StaleAfterS      int64   `json:"stale_after_s"`
	MaxMoveBpsPerMin float64 `json:"max_move_bps_per_min"`
	LookbackS        int64   `json:"lookback_s"`
	RefWindowS       int64   `json:"ref_window_s"`
	BurstWindowS     int64   `json:"burst_window_s"`
}

func toParamsDTO(p Params) ParamsDTO {
	return ParamsDTO{HalfLifeS: int64(p.HalfLife / time.Second), StaleAfterS: int64(p.StaleAfter / time.Second), MaxMoveBpsPerMin: p.MaxMoveBpsPerMin,
		LookbackS: int64(p.Lookback / time.Second), RefWindowS: int64(p.RefWindow / time.Second), BurstWindowS: int64(p.BurstWindow / time.Second)}
}

func (d ParamsDTO) params() Params {
	return Params{HalfLife: time.Duration(d.HalfLifeS) * time.Second, StaleAfter: time.Duration(d.StaleAfterS) * time.Second, MaxMoveBpsPerMin: d.MaxMoveBpsPerMin,
		Lookback: time.Duration(d.LookbackS) * time.Second, RefWindow: time.Duration(d.RefWindowS) * time.Second, BurstWindow: time.Duration(d.BurstWindowS) * time.Second}
}

// FeedSource is one venue of a single, with its health.
type FeedSource struct {
	Venue          string     `json:"venue"`
	Source         string     `json:"source"`
	LastPrice      *string    `json:"last_price"`
	LastAt         *time.Time `json:"last_at"`
	OK             bool       `json:"ok"`
	Note           string     `json:"note,omitempty"`
	Premium        float64    `json:"premium"`
	PremiumLearned bool       `json:"premium_learned"`
	Weight         float64    `json:"weight"`
}

// FeedPart is one part of a basket.
type FeedPart struct {
	FeedID   uuid.UUID `json:"feed_id"`
	Key      string    `json:"key"`
	Name     string    `json:"name"`
	Quantity string    `json:"quantity"`
	Price    *string   `json:"price"`
	Status   string    `json:"status"`
}

// PricePt is one published price in the last day.
type PricePt struct {
	At    time.Time `json:"at"`
	Price string    `json:"price"`
}

// FeedDetail is GET /v1/oracle/feeds/{key}.
type FeedDetail struct {
	Feed           FeedInfo      `json:"feed"`
	SeedPrice      *string       `json:"seed_price"`
	Params         ParamsDTO     `json:"params"`
	LastAcceptedAt *time.Time    `json:"last_accepted_at"`
	Sources        []FeedSource  `json:"sources"`
	Parts          []FeedPart    `json:"parts"`
	Recent         []PricePt     `json:"recent"`
	Observations   []Observation `json:"observations"`
}

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
	r.Get("/oracle/feeds", httpx.Handler(s.handleFeeds))
	r.Get("/oracle/feeds/{key}", httpx.Handler(s.handleFeed))
	r.Get("/oracle/feeds/{key}/history", httpx.Handler(s.handleHistory))
	r.Get("/oracle/observations", httpx.Handler(s.handleObservations))
}

// ---- reads ----

const feedSelect = `SELECT f.id, f.key, f.name, f.category, f.quote, f.decimals, f.kind, f.listed, f.enabled,
  st.price::float8, st.target::float8, st.status, st.updated_at, f.seed_price::float8,
  coalesce((SELECT array_agg(r.venue ORDER BY r.venue) FROM oracle_feed_sources r WHERE r.feed_id = f.id), '{}'),
  (SELECT count(*) FROM oracle_basket_parts p WHERE p.basket_id = f.id)::int
 FROM oracle_feeds f LEFT JOIN oracle_feed_state st ON st.feed_id = f.id`

func scanFeedInfo(row interface{ Scan(...any) error }) (FeedInfo, error) {
	var fi FeedInfo
	var price, target, seed *float64
	var status *string
	err := row.Scan(&fi.ID, &fi.Key, &fi.Name, &fi.Category, &fi.Quote, &fi.Decimals, &fi.Kind, &fi.Listed, &fi.Enabled,
		&price, &target, &status, &fi.UpdatedAt, &seed, &fi.Venues, &fi.PartCount)
	if err != nil {
		return fi, err
	}
	fi.Status = StatusSeed
	if status != nil {
		fi.Status = *status
	}
	if !fi.Enabled {
		fi.Status = StatusOff
	}
	if price == nil && status == nil && seed != nil && fi.Kind == FeedSingle {
		price = seed // never published yet: the seed is the price
	}
	if price != nil && *price > 0 {
		p := fmtPrice(*price, fi.Decimals)
		fi.Price, fi.priceF = &p, *price
	}
	if target != nil && *target > 0 {
		t := fmtPrice(*target, fi.Decimals)
		fi.Target = &t
	}
	if fi.Venues == nil {
		fi.Venues = []string{}
	}
	return fi, nil
}

type feedQuery struct {
	q     string
	kind  string
	all   bool // unlisted and disabled too (staff)
	after string
	limit int
}

func (s *Service) listFeeds(ctx context.Context, fq feedQuery) ([]FeedInfo, error) {
	rows, err := s.d.Pool.Query(ctx, feedSelect+`
 WHERE ($1 OR (f.listed AND f.enabled))
   AND ($2 = '' OR strpos(lower(f.key), lower($2)) > 0 OR strpos(lower(f.name), lower($2)) > 0)
   AND ($3 = '' OR f.kind = $3)
   AND ($4 = '' OR f.key > $4)
 ORDER BY f.key LIMIT $5`, fq.all, strings.TrimSpace(fq.q), fq.kind, fq.after, fq.limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedInfo{}
	for rows.Next() {
		fi, err := scanFeedInfo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, fi)
	}
	return out, rows.Err()
}

func (s *Service) feedByKey(ctx context.Context, key string) (FeedInfo, error) {
	fi, err := scanFeedInfo(s.d.Pool.QueryRow(ctx, feedSelect+` WHERE f.key = $1`, strings.ToUpper(strings.TrimSpace(key))))
	if db.IsNoRows(err) {
		return fi, httpx.NotFound("No feed " + key + ".")
	}
	return fi, err
}

func kindParam(r *http.Request) (string, error) {
	switch k := r.URL.Query().Get("kind"); k {
	case "", FeedSingle, FeedBasket:
		return k, nil
	}
	return "", httpx.BadRequest("kind must be single or basket.")
}

func (s *Service) handleFeeds(w http.ResponseWriter, r *http.Request) error {
	kind, err := kindParam(r)
	if err != nil {
		return err
	}
	pg := httpx.ParsePage(r)
	var after string
	if err := httpx.DecodeCursor(pg.Cursor, &after); err != nil {
		return err
	}
	rows, err := s.listFeeds(r.Context(), feedQuery{q: r.URL.Query().Get("q"), kind: kind, after: after, limit: pg.Limit + 1})
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(rows, pg.Limit, func(f FeedInfo) any { return f.Key }))
	return nil
}

func (s *Service) handleFeed(w http.ResponseWriter, r *http.Request) error {
	fi, err := s.feedByKey(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		return err
	}
	d, err := s.feedDetail(r.Context(), fi, 50)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) feedDetail(ctx context.Context, fi FeedInfo, nObs int) (FeedDetail, error) {
	d := FeedDetail{Feed: fi, Sources: []FeedSource{}, Parts: []FeedPart{}, Recent: []PricePt{}}
	var seed *float64
	var hl, st, lb, rw, bw int64
	err := s.d.Pool.QueryRow(ctx, `SELECT f.seed_price::float8, f.half_life_s, f.stale_after_s, f.max_move_bps_per_min::float8, f.lookback_s,
 f.ref_window_s, f.burst_window_s, st.last_accepted_at FROM oracle_feeds f LEFT JOIN oracle_feed_state st ON st.feed_id = f.id WHERE f.id = $1`, fi.ID).
		Scan(&seed, &hl, &st, &d.Params.MaxMoveBpsPerMin, &lb, &rw, &bw, &d.LastAcceptedAt)
	if err != nil {
		return d, err
	}
	d.Params.HalfLifeS, d.Params.StaleAfterS, d.Params.LookbackS, d.Params.RefWindowS, d.Params.BurstWindowS = hl, st, lb, rw, bw
	if seed != nil {
		p := fmtPrice(*seed, fi.Decimals)
		d.SeedPrice = &p
	}
	feedIDs := []uuid.UUID{fi.ID}
	if fi.Kind == FeedSingle {
		if d.Sources, err = s.feedSources(ctx, fi); err != nil {
			return d, err
		}
	} else {
		rows, err := s.d.Pool.Query(ctx, feedSelect+` JOIN oracle_basket_parts bp ON bp.feed_id = f.id WHERE bp.basket_id = $1 ORDER BY bp.position, f.key`, fi.ID)
		if err != nil {
			return d, err
		}
		var parts []FeedInfo
		for rows.Next() {
			pf, err := scanFeedInfo(rows)
			if err != nil {
				rows.Close()
				return d, err
			}
			parts = append(parts, pf)
		}
		rows.Close()
		qty, err := loadParts(ctx, s.d.Pool, &fi.ID)
		if err != nil {
			return d, err
		}
		q := map[uuid.UUID]float64{}
		for _, p := range qty {
			q[p.FeedID] = p.Quantity
		}
		for _, pf := range parts {
			d.Parts = append(d.Parts, FeedPart{FeedID: pf.ID, Key: pf.Key, Name: pf.Name, Quantity: fmtRaw(q[pf.ID]), Price: pf.Price, Status: pf.Status})
			feedIDs = append(feedIDs, pf.ID)
		}
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT at, price::float8 FROM oracle_prices WHERE feed_id = $1 AND at > now() - interval '24 hours' ORDER BY at`, fi.ID)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var at time.Time
		var px float64
		if err := rows.Scan(&at, &px); err != nil {
			rows.Close()
			return d, err
		}
		d.Recent = append(d.Recent, PricePt{At: at, Price: fmtPrice(px, fi.Decimals)})
	}
	rows.Close()
	d.Observations, err = s.queryObservations(ctx, obsQuery{feeds: feedIDs, limit: nObs})
	return d, err
}

func (s *Service) feedSources(ctx context.Context, fi FeedInfo) ([]FeedSource, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT r.venue, r.source, coalesce(r.weight, 1)::float8, r.premium::float8, vp.premium::float8,
   h.ok, h.last_error, lo.raw_price::float8, lo.observed_at
 FROM oracle_feed_sources r
 LEFT JOIN oracle_ref_health h ON h.ref_id = r.id
 LEFT JOIN oracle_venue_premiums vp ON vp.feed_id = r.feed_id AND vp.venue = r.venue
 LEFT JOIN LATERAL (SELECT raw_price, observed_at FROM oracle_observations o WHERE o.feed_id = r.feed_id AND o.venue = r.venue
   ORDER BY observed_at DESC LIMIT 1) lo ON true
 WHERE r.feed_id = $1 ORDER BY r.venue`, fi.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FeedSource{}
	for rows.Next() {
		var fs FeedSource
		var fixed, learned, last *float64
		var ok *bool
		var lastErr *string
		if err := rows.Scan(&fs.Venue, &fs.Source, &fs.Weight, &fixed, &learned, &ok, &lastErr, &last, &fs.LastAt); err != nil {
			return nil, err
		}
		if last != nil {
			p := fmtRaw(*last)
			fs.LastPrice = &p
		}
		switch {
		case fixed != nil:
			fs.Premium = *fixed
		case learned != nil && s.cfg.learnPremiums:
			fs.Premium, fs.PremiumLearned = *learned, *learned != 0
		}
		fs.OK, fs.Note = s.sourceNote(fs.Source, ok, lastErr)
		out = append(out, fs)
	}
	return out, rows.Err()
}

// sourceNote explains a source's state in a few words.
func (s *Service) sourceNote(source string, ok *bool, lastErr *string) (bool, string) {
	if source == ManualSource {
		return true, "staff prints"
	}
	if _, registered := s.sources[source]; !registered {
		if _, pushed := pushSources[source]; pushed {
			return true, "pushed by the app"
		}
		return false, "source not installed"
	}
	if s.cfg.disabled[source] {
		return false, "turned off by ORACLE_DISABLE_SOURCES"
	}
	if !s.cfg.sourcesEnabled {
		return false, "polling off (ORACLE_SOURCES_ENABLED)"
	}
	if ok == nil {
		return false, "waiting for the first fetch"
	}
	if lastErr != nil && *lastErr != "" {
		return *ok, *lastErr
	}
	return *ok, ""
}

func (s *Service) handleHistory(w http.ResponseWriter, r *http.Request) error {
	fi, err := s.feedByKey(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		return err
	}
	days := 365
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 {
		days = min(v, historyDays)
	}
	bars, err := s.HistoryBars(r.Context(), fi, days, fi.priceF)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": bars})
	return nil
}

type obsQuery struct {
	feeds   []uuid.UUID
	verdict string
	before  *obsCursor
	limit   int
}

type obsCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) queryObservations(ctx context.Context, q obsQuery) ([]Observation, error) {
	var bt *time.Time
	var bid *uuid.UUID
	if q.before != nil {
		bt, bid = &q.before.T, &q.before.ID
	}
	var feeds []uuid.UUID
	if len(q.feeds) > 0 {
		feeds = q.feeds
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT o.id, o.feed_id, f.key, f.decimals, o.source, o.venue, o.kind, o.price::float8, o.raw_price::float8,
   o.premium::float8, o.qty::float8, o.observed_at, o.received_at, o.verdict, o.score::float8, o.reasons, o.model, o.weight::float8
 FROM oracle_observations o JOIN oracle_feeds f ON f.id = o.feed_id
 WHERE ($1::uuid[] IS NULL OR o.feed_id = ANY($1))
   AND ($2 = '' OR o.verdict = $2)
   AND ($3::timestamptz IS NULL OR (o.received_at, o.id) < ($3, $4::uuid))
 ORDER BY o.received_at DESC, o.id DESC LIMIT $5`, feeds, q.verdict, bt, bid, q.limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Observation{}
	for rows.Next() {
		var o Observation
		var dec int32
		var px, raw float64
		var reasons []string
		if err := rows.Scan(&o.ID, &o.FeedID, &o.FeedKey, &dec, &o.Source, &o.Venue, &o.Kind, &px, &raw, &o.Premium, &o.Qty,
			&o.ObservedAt, &o.ReceivedAt, &o.Verdict, &o.Score, &reasons, &o.Model, &o.Weight); err != nil {
			return nil, err
		}
		o.Price, o.RawPrice = fmtPrice(px, dec+2), fmtRaw(raw)
		o.Reasons = reasons
		if o.Reasons == nil {
			o.Reasons = []string{}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func verdictParam(r *http.Request) (string, error) {
	switch v := r.URL.Query().Get("verdict"); v {
	case "", VerdictAccepted, VerdictDownweighted, VerdictRejected:
		return v, nil
	}
	return "", httpx.BadRequest("verdict must be accepted, downweighted or rejected.")
}

// feedIDsFor is the feed and, for a basket, its parts.
func (s *Service) feedIDsFor(ctx context.Context, fi FeedInfo) ([]uuid.UUID, error) {
	ids := []uuid.UUID{fi.ID}
	if fi.Kind == FeedBasket {
		parts, err := loadParts(ctx, s.d.Pool, &fi.ID)
		if err != nil {
			return nil, err
		}
		for _, p := range parts {
			ids = append(ids, p.FeedID)
		}
	}
	return ids, nil
}

func (s *Service) handleObservations(w http.ResponseWriter, r *http.Request) error {
	return s.serveObservations(w, r, false)
}

func (s *Service) serveObservations(w http.ResponseWriter, r *http.Request, staff bool) error {
	verdict, err := verdictParam(r)
	if err != nil {
		return err
	}
	pg := httpx.ParsePage(r)
	var cur *obsCursor
	if pg.Cursor != "" {
		cur = &obsCursor{}
		if err := httpx.DecodeCursor(pg.Cursor, cur); err != nil {
			return err
		}
	}
	q := obsQuery{verdict: verdict, before: cur, limit: pg.Limit + 1}
	if key := r.URL.Query().Get("feed"); key != "" {
		fi, err := s.feedByKey(r.Context(), key)
		if err != nil {
			return err
		}
		if q.feeds, err = s.feedIDsFor(r.Context(), fi); err != nil {
			return err
		}
	} else if !staff {
		visible, err := s.visibleFeedIDs(r.Context())
		if err != nil {
			return err
		}
		q.feeds = visible
	}
	rows, err := s.queryObservations(r.Context(), q)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(rows, pg.Limit, func(o Observation) any { return obsCursor{T: o.ReceivedAt, ID: o.ID} }))
	return nil
}

// visibleFeedIDs are the feeds people see: listed and enabled singles, and
// unlisted singles that are part of a listed basket.
func (s *Service) visibleFeedIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM oracle_feeds WHERE enabled AND listed
 UNION SELECT bp.feed_id FROM oracle_basket_parts bp JOIN oracle_feeds b ON b.id = bp.basket_id WHERE b.enabled AND b.listed`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
