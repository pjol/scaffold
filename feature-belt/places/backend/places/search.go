package places

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

const (
	defaultSearchLimit = 100
	maxSearchLimit     = 500 // pins per page; zoomed-out maps use ?cluster=true
	maxClusterCells    = 1000
	defaultNearRadiusM = 50_000
	maxNearRadiusM     = 1_000_000
	maxQueryRunes      = 100
)

// query is a parsed GET /v1/places request.
type query struct {
	categories []string
	text       string
	bbox       *BBox
	near       *LatLng
	radiusM    float64
	cluster    bool
	zoom       int
	limit      int
	cursor     string
}

func (s *Service) parseQuery(r *http.Request) (query, error) {
	v := r.URL.Query()
	q := query{limit: defaultSearchLimit, cursor: v.Get("cursor"), radiusM: defaultNearRadiusM}
	if raw := strings.TrimSpace(v.Get("category")); raw != "" {
		for c := range strings.SplitSeq(raw, ",") {
			c = strings.TrimSpace(c)
			if !s.cfg.categorySet[c] {
				return q, httpx.BadRequest(fmt.Sprintf("Unknown category %q.", c))
			}
			q.categories = append(q.categories, c)
		}
	}
	q.text = strings.Join(strings.Fields(v.Get("q")), " ")
	if len([]rune(q.text)) > maxQueryRunes {
		return q, httpx.BadRequest("Search text must be 100 characters or fewer.")
	}
	if raw := v.Get("bbox"); raw != "" {
		b, err := parseBBox(raw)
		if err != nil {
			return q, httpx.BadRequest("bbox must be minLng,minLat,maxLng,maxLat: " + err.Error() + ".")
		}
		q.bbox = &b
	}
	if raw := v.Get("near"); raw != "" {
		p, err := parseLatLng(raw)
		if err != nil {
			return q, httpx.BadRequest("near must be lat,lng.")
		}
		q.near = &p
		if raw := v.Get("radius_m"); raw != "" {
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil || f <= 0 || f > maxNearRadiusM {
				return q, httpx.BadRequest("radius_m must be between 1 and 1000000.")
			}
			q.radiusM = f
		}
	}
	if v.Get("cluster") == "true" {
		q.cluster = true
		z, err := strconv.Atoi(v.Get("zoom"))
		if err != nil || z < 0 || z > 22 {
			return q, httpx.BadRequest("Clustering needs zoom between 0 and 22.")
		}
		q.zoom = z
		if q.bbox == nil {
			return q, httpx.BadRequest("Clustering needs a bbox.")
		}
		if q.near != nil {
			return q, httpx.BadRequest("Use either near or cluster, not both.")
		}
	}
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return q, httpx.BadRequest("limit must be a positive number.")
		}
		q.limit = min(n, maxSearchLimit)
	}
	return q, nil
}

// search is GET /v1/places: approved places in a bbox (keyset by newest), or
// nearest-first with ?near=, or grid clusters with ?cluster=true&zoom=.
func (s *Service) search(w http.ResponseWriter, r *http.Request) error {
	q, err := s.parseQuery(r)
	if err != nil {
		return err
	}
	switch {
	case q.cluster:
		out, err := s.clusters(r.Context(), q)
		if err != nil {
			return err
		}
		httpx.OK(w, out)
	case q.near != nil:
		page, err := s.nearest(r.Context(), q)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
	default:
		page, err := s.list(r.Context(), q)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
	}
	return nil
}

// sqlWhere assembles a WHERE clause from fixed fragments; every value goes in
// as a numbered argument, never into the SQL text.
type sqlWhere struct {
	parts []string
	args  []any
}

func (w *sqlWhere) arg(v any) string {
	w.args = append(w.args, v)
	return "$" + strconv.Itoa(len(w.args))
}

func (w *sqlWhere) and(cond string) { w.parts = append(w.parts, cond) }

func (w *sqlWhere) sql() string { return strings.Join(w.parts, " AND ") }

func (w *sqlWhere) bbox(b BBox) {
	w.and("p.lat BETWEEN " + w.arg(b.MinLat) + " AND " + w.arg(b.MaxLat))
	if b.MinLng <= b.MaxLng {
		w.and("p.lng BETWEEN " + w.arg(b.MinLng) + " AND " + w.arg(b.MaxLng))
	} else {
		w.and("(p.lng >= " + w.arg(b.MinLng) + " OR p.lng <= " + w.arg(b.MaxLng) + ")")
	}
}

// filters adds the approved-only, category, text and bbox conditions.
func (w *sqlWhere) filters(q query) {
	w.and("p.status = 'approved'")
	if len(q.categories) > 0 {
		w.and("p.category = ANY(" + w.arg(q.categories) + ")")
	}
	if q.text != "" {
		w.and("(p.name || ' ' || p.address) ILIKE " + w.arg("%"+escapeLike(q.text)+"%"))
	}
	if q.bbox != nil {
		w.bbox(*q.bbox)
	}
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// summaryCols selects a PlaceSummary (distance added by the caller).
const summaryCols = `p.id, p.name, p.category, p.address, p.lat, p.lng,
	(SELECT ph.key FROM place_photos ph WHERE ph.place_id = p.id ORDER BY ph.position, ph.created_at LIMIT 1)`

func (s *Service) scanSummaries(rows pgx.Rows) ([]PlaceSummary, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (PlaceSummary, error) {
		var p PlaceSummary
		var key *string
		if err := row.Scan(&p.ID, &p.Name, &p.Category, &p.Address, &p.Lat, &p.Lng, &key, &p.DistanceM); err != nil {
			return p, err
		}
		if key != nil {
			u := s.d.Storage.PublicURL(*key)
			p.PhotoURL = &u
		}
		return p, nil
	})
}

type listCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func (s *Service) list(ctx context.Context, q query) (httpx.Page[PlaceSummary], error) {
	var c listCursor
	if err := httpx.DecodeCursor(q.cursor, &c); err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	w := &sqlWhere{}
	w.filters(q)
	if !c.T.IsZero() {
		w.and("(p.created_at, p.id) < (" + w.arg(c.T) + ", " + w.arg(c.ID) + ")")
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+summaryCols+`, NULL::float8, p.created_at FROM places p
		WHERE `+w.sql()+`
		ORDER BY p.created_at DESC, p.id DESC LIMIT `+w.arg(q.limit+1), w.args...)
	if err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	type row struct {
		PlaceSummary
		createdAt time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var out row
		var key *string
		p := &out.PlaceSummary
		if err := r.Scan(&p.ID, &p.Name, &p.Category, &p.Address, &p.Lat, &p.Lng, &key, &p.DistanceM, &out.createdAt); err != nil {
			return out, err
		}
		if key != nil {
			u := s.d.Storage.PublicURL(*key)
			p.PhotoURL = &u
		}
		return out, nil
	})
	if err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	page := httpx.NewPage(list, q.limit, func(r row) any { return listCursor{r.createdAt, r.ID} })
	out := httpx.Page[PlaceSummary]{Items: make([]PlaceSummary, len(page.Items)), NextCursor: page.NextCursor}
	for i, r := range page.Items {
		out.Items[i] = r.PlaceSummary
	}
	return out, nil
}

// distanceSQL is the haversine distance in metres from (lat, lng) params.
func distanceSQL(lat, lng string) string {
	return `(2 * 6371008.8 * asin(least(1, sqrt(
		power(sin(radians(p.lat - ` + lat + `) / 2), 2) +
		cos(radians(` + lat + `)) * cos(radians(p.lat)) * power(sin(radians(p.lng - ` + lng + `) / 2), 2)))))`
}

type nearCursor struct {
	D  float64   `json:"d"`
	ID uuid.UUID `json:"id"`
}

// nearest orders by haversine distance within radiusM (bbox prefilter on
// the (lat, lng) index), keyset on (distance, id).
func (s *Service) nearest(ctx context.Context, q query) (httpx.Page[PlaceSummary], error) {
	var c nearCursor
	if err := httpx.DecodeCursor(q.cursor, &c); err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	w := &sqlWhere{}
	w.filters(q)
	w.bbox(bboxAround(*q.near, q.radiusM))
	dist := distanceSQL(w.arg(q.near.Lat)+"::float8", w.arg(q.near.Lng)+"::float8")
	outer := "n.distance_m <= " + w.arg(q.radiusM)
	if c.ID != uuid.Nil {
		outer += " AND (n.distance_m, n.id) > (" + w.arg(c.D) + "::float8, " + w.arg(c.ID) + ")"
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT n.id, n.name, n.category, n.address, n.lat, n.lng,
		       (SELECT ph.key FROM place_photos ph WHERE ph.place_id = n.id ORDER BY ph.position, ph.created_at LIMIT 1),
		       n.distance_m
		FROM (SELECT p.id, p.name, p.category, p.address, p.lat, p.lng, `+dist+` AS distance_m
		      FROM places p WHERE `+w.sql()+`) n
		WHERE `+outer+`
		ORDER BY n.distance_m, n.id LIMIT `+w.arg(q.limit+1), w.args...)
	if err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	list, err := s.scanSummaries(rows)
	if err != nil {
		return httpx.Page[PlaceSummary]{}, err
	}
	return httpx.NewPage(list, q.limit, func(p PlaceSummary) any { return nearCursor{*p.DistanceM, p.ID} }), nil
}

// Cluster is a grid cell holding two or more places.
type Cluster struct {
	ID    string     `json:"id"` // "<zoom>:<x>:<y>", stable for a zoom level
	Lat   float64    `json:"lat"`
	Lng   float64    `json:"lng"`
	Count int        `json:"count"`
	BBox  [4]float64 `json:"bbox"` // members' extent: minLng, minLat, maxLng, maxLat
}

// ClusterResult is GET /v1/places?cluster=true: cells with one place come
// back as places, denser cells as clusters.
type ClusterResult struct {
	Clusters  []Cluster      `json:"clusters"`
	Places    []PlaceSummary `json:"places"`
	Truncated bool           `json:"truncated"` // more than 1000 cells; zoom in
}

func (s *Service) clusters(ctx context.Context, q query) (ClusterResult, error) {
	out := ClusterResult{Clusters: []Cluster{}, Places: []PlaceSummary{}}
	w := &sqlWhere{}
	w.filters(q)
	cell := w.arg(clusterCell(q.zoom))
	rows, err := s.d.Pool.Query(ctx, `
		WITH pts AS (
			SELECT p.id, p.lat, p.lng,
			       floor(((p.lng + 180) / 360) / `+cell+`)::bigint AS cx,
			       floor(((1 - ln(tan(radians(45 + LEAST(GREATEST(p.lat, -85.05112878), 85.05112878) / 2))) / pi()) / 2)
			             / `+cell+`)::bigint AS cy
			FROM places p WHERE `+w.sql()+`
		)
		SELECT cx, cy, count(*)::int, avg(lat), avg(lng), min(lng), min(lat), max(lng), max(lat), (array_agg(id))[1]
		FROM pts GROUP BY cx, cy ORDER BY count(*) DESC, cx, cy LIMIT `+w.arg(maxClusterCells+1), w.args...)
	if err != nil {
		return out, err
	}
	var singles []uuid.UUID
	n := 0
	for rows.Next() {
		n++
		if n > maxClusterCells {
			out.Truncated = true
			break
		}
		var (
			cx, cy int64
			c      Cluster
			id     uuid.UUID
		)
		if err := rows.Scan(&cx, &cy, &c.Count, &c.Lat, &c.Lng, &c.BBox[0], &c.BBox[1], &c.BBox[2], &c.BBox[3], &id); err != nil {
			rows.Close()
			return out, err
		}
		if c.Count == 1 {
			singles = append(singles, id)
			continue
		}
		c.ID = fmt.Sprintf("%d:%d:%d", q.zoom, cx, cy)
		out.Clusters = append(out.Clusters, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(singles) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT `+summaryCols+`, NULL::float8 FROM places p
			WHERE p.id = ANY($1) ORDER BY p.created_at DESC, p.id DESC`, singles)
		if err != nil {
			return out, err
		}
		if out.Places, err = s.scanSummaries(rows); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ---- place detail --------------------------------------------------------------

const placeCols = `p.id, p.name, p.category, p.description, p.address, p.lat, p.lng, p.phone, p.website,
	p.hours, p.status, p.review_note, p.created_at, p.updated_at`

func scanPlace(row pgx.Row, extra ...any) (Place, error) {
	var p Place
	var hours []byte
	dst := append([]any{&p.ID, &p.Name, &p.Category, &p.Description, &p.Address, &p.Lat, &p.Lng, &p.Phone,
		&p.Website, &hours, &p.Status, &p.ReviewNote, &p.CreatedAt, &p.UpdatedAt}, extra...)
	if err := row.Scan(dst...); err != nil {
		return p, err
	}
	if len(hours) > 0 && string(hours) != "null" {
		var h Hours
		if err := json.Unmarshal(hours, &h); err != nil {
			return p, fmt.Errorf("places: stored hours for %s: %w", p.ID, err)
		}
		p.Hours = &h
	}
	p.Photos = []Photo{}
	return p, nil
}

// attachPhotos fills Photos for every place in one query.
func (s *Service) attachPhotos(ctx context.Context, q db.Querier, list []*Place) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	byID := make(map[uuid.UUID]*Place, len(list))
	for i, p := range list {
		ids[i] = p.ID
		byID[p.ID] = p
	}
	rows, err := q.Query(ctx, `SELECT place_id, id, key FROM place_photos
		WHERE place_id = ANY($1) ORDER BY position, created_at, id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var placeID, id uuid.UUID
		var key string
		if err := rows.Scan(&placeID, &id, &key); err != nil {
			return err
		}
		if p := byID[placeID]; p != nil {
			p.Photos = append(p.Photos, Photo{ID: id, URL: s.d.Storage.PublicURL(key)})
		}
	}
	return rows.Err()
}

// loadPlace returns a place with photos. where is a fixed SQL condition on p
// with $1 = id (and further args).
func (s *Service) loadPlace(ctx context.Context, q db.Querier, where string, args ...any) (*Place, error) {
	p, err := scanPlace(q.QueryRow(ctx, `SELECT `+placeCols+` FROM places p WHERE `+where, args...))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Place not found.")
	}
	if err != nil {
		return nil, err
	}
	if err := s.attachPhotos(ctx, q, []*Place{&p}); err != nil {
		return nil, err
	}
	return &p, nil
}

// getPlace is GET /v1/places/{id}: approved places only.
func (s *Service) getPlace(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := s.loadPlace(r.Context(), s.d.Pool, `p.id = $1 AND p.status = 'approved'`, id)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}
