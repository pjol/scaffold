package places

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ---- account deletion / export ---------------------------------------------------

// DeleteAccount removes the user's favourites and every photo row they
// uploaded (core storage cleanup deletes the objects under their prefix, so
// keeping the rows would leave broken images). Their pending and rejected
// submissions are deleted; approved places are public listings and stay, with
// the owner cleared.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM place_favorites WHERE user_id = $1`,
		`DELETE FROM place_photos WHERE uploaded_by = $1`,
		`DELETE FROM places WHERE owner_id = $1 AND status IN ('pending', 'rejected')`,
		`UPDATE places SET owner_id = NULL, updated_at = now() WHERE owner_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportFavorite struct {
	PlaceID     uuid.UUID `json:"place_id"`
	Name        string    `json:"name"`
	FavoritedAt time.Time `json:"favorited_at"`
}

type exportPhoto struct {
	PlaceID   uuid.UUID `json:"place_id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// ExportAccount returns the user's submitted places, favourites and photos.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+placeCols+` FROM places p WHERE p.owner_id = $1
		ORDER BY p.created_at, p.id`, userID)
	if err != nil {
		return nil, err
	}
	submitted, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Place, error) { return scanPlace(row) })
	if err != nil {
		return nil, err
	}
	ptrs := make([]*Place, len(submitted))
	for i := range submitted {
		ptrs[i] = &submitted[i]
	}
	if err := s.attachPhotos(ctx, s.d.Pool, ptrs); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT f.place_id, p.name, f.created_at FROM place_favorites f
		JOIN places p ON p.id = f.place_id WHERE f.user_id = $1 ORDER BY f.created_at`, userID)
	if err != nil {
		return nil, err
	}
	favorites, err := pgx.CollectRows(rows, pgx.RowToStructByPos[exportFavorite])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT place_id, key, created_at FROM place_photos
		WHERE uploaded_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	photos, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (exportPhoto, error) {
		var p exportPhoto
		var key string
		err := row.Scan(&p.PlaceID, &key, &p.CreatedAt)
		p.URL = s.d.Storage.PublicURL(key)
		return p, err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"submitted": submitted, "favorites": favorites, "photos": photos}, nil
}

// ---- agent tools -----------------------------------------------------------------

func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "places_search",
			Description: "Search approved places on the map by text, category, bounding box or distance from a point. Returns up to 50 places; with near, nearest first with distance in metres.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"q": {"type": "string", "maxLength": 100, "description": "Text matched against name and address."},
					"category": {"type": "string", "description": "Category id from GET /v1/places/config."},
					"bbox": {"type": "array", "items": {"type": "number"}, "minItems": 4, "maxItems": 4,
					         "description": "minLng, minLat, maxLng, maxLat."},
					"near": {"type": "object", "properties": {"lat": {"type": "number"}, "lng": {"type": "number"}},
					         "required": ["lat", "lng"]},
					"radius_m": {"type": "number", "exclusiveMinimum": 0, "maximum": 1000000},
					"limit": {"type": "integer", "minimum": 1, "maximum": 50}
				},
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolSearch,
		},
		{
			Name:        "places_get",
			Description: "Get one place by id: address, coordinates, phone, website, opening hours and photo URLs.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {"id": {"type": "string", "format": "uuid"}},
				"required": ["id"],
				"additionalProperties": false
			}`),
			ReadOnly: true,
			Handler:  s.toolGet,
			Target:   placeTarget,
		},
	}
}

func (s *Service) toolSearch(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		Q        string    `json:"q"`
		Category string    `json:"category"`
		BBox     []float64 `json:"bbox"`
		Near     *LatLng   `json:"near"`
		RadiusM  float64   `json:"radius_m"`
		Limit    int       `json:"limit"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}
	q := query{text: in.Q, limit: 20, radiusM: defaultNearRadiusM}
	if in.Limit > 0 {
		q.limit = min(in.Limit, 50)
	}
	if in.Category != "" {
		if !s.cfg.categorySet[in.Category] {
			return nil, fmt.Errorf("unknown category %q", in.Category)
		}
		q.categories = []string{in.Category}
	}
	if len([]rune(q.text)) > maxQueryRunes {
		return nil, errors.New("q must be 100 characters or fewer")
	}
	if len(in.BBox) > 0 {
		if len(in.BBox) != 4 {
			return nil, errors.New("bbox needs 4 numbers")
		}
		b, err := parseBBox(fmt.Sprintf("%v,%v,%v,%v", in.BBox[0], in.BBox[1], in.BBox[2], in.BBox[3]))
		if err != nil {
			return nil, err
		}
		q.bbox = &b
	}
	if in.Near != nil {
		if !validLat(in.Near.Lat) || !validLng(in.Near.Lng) {
			return nil, errors.New("near must be a valid lat/lng")
		}
		q.near = in.Near
		if in.RadiusM > 0 {
			q.radiusM = min(in.RadiusM, maxNearRadiusM)
		}
		page, err := s.nearest(ctx, q)
		if err != nil {
			return nil, err
		}
		return s.agentPlaces(ctx, caller, page.Items)
	}
	page, err := s.list(ctx, q)
	if err != nil {
		return nil, err
	}
	return s.agentPlaces(ctx, caller, page.Items)
}

// Agent scope: an AI agent acting for the user reads only places their
// agent scope lets agents read (places.place items set from the agents
// feature's ⋮ menu, else the feature's level).
const (
	agentFeature = "places"
	agentKind    = "places.place"
)

// placeTarget names the place places_get reads, so a per-place agent scope
// applies before it runs.
func placeTarget(input json.RawMessage) (kind, id string) {
	var in struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(input, &in) != nil {
		return agentKind, ""
	}
	if _, err := uuid.Parse(in.ID); err != nil {
		return agentKind, ""
	}
	return agentKind, in.ID
}

// agentPlaces drops places the caller's agent may not read.
func (s *Service) agentPlaces(ctx context.Context, caller *auth.Identity, items []PlaceSummary) ([]PlaceSummary, error) {
	ids := make([]string, len(items))
	for i, p := range items {
		ids[i] = p.ID.String()
	}
	ok, err := app.AgentReadable(ctx, s.d.Modules, caller, agentFeature, agentKind, ids)
	if err != nil {
		return nil, err
	}
	out := make([]PlaceSummary, 0, len(items))
	for i, p := range items {
		if ok[ids[i]] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Service) toolGet(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}
	id, err := uuid.Parse(in.ID)
	if err != nil {
		return nil, errors.New("id must be a uuid")
	}
	var owner uuid.UUID
	if caller != nil {
		owner = caller.UserID
	}
	if ok, err := app.AgentAllows(ctx, s.d.Modules, caller, agentFeature, agentKind, id.String(), app.AgentRead); err != nil || !ok {
		if err == nil {
			err = errors.New("place not found")
		}
		return nil, err
	}
	p, err := s.loadPlace(ctx, s.d.Pool, `p.id = $1 AND (p.status = 'approved' OR p.owner_id = $2)`, id, owner)
	var he *httpx.Error
	if errors.As(err, &he) {
		return nil, errors.New("place not found")
	}
	return p, err
}

// ---- development data ------------------------------------------------------------

type seedPlace struct {
	slug, name, category, address, phone, website string
	lat, lng                                      float64
	hours                                         *Hours
}

func daily(open, close string, days ...string) *Hours {
	h := &Hours{Timezone: "America/Los_Angeles", Weekly: Week{
		Mon: []Interval{}, Tue: []Interval{}, Wed: []Interval{}, Thu: []Interval{},
		Fri: []Interval{}, Sat: []Interval{}, Sun: []Interval{}}}
	iv := []Interval{{Open: open, Close: close}}
	set := map[string]*[]Interval{"mon": &h.Weekly.Mon, "tue": &h.Weekly.Tue, "wed": &h.Weekly.Wed,
		"thu": &h.Weekly.Thu, "fri": &h.Weekly.Fri, "sat": &h.Weekly.Sat, "sun": &h.Weekly.Sun}
	if len(days) == 0 {
		days = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	}
	for _, d := range days {
		*set[d] = iv
	}
	return h
}

// San Francisco landmarks and long-standing venues (development only).
var seedPlaces = []seedPlace{
	{slug: "ferry-building", name: "Ferry Building Marketplace", category: "shop", address: "1 Ferry Building, San Francisco, CA 94111",
		lat: 37.7955, lng: -122.3937, website: "https://www.ferrybuildingmarketplace.com", hours: daily("10:00", "19:00")},
	{slug: "tartine", name: "Tartine Bakery", category: "cafe", address: "600 Guerrero St, San Francisco, CA 94110",
		lat: 37.7614, lng: -122.4241, hours: daily("08:00", "17:00")},
	{slug: "caffe-trieste", name: "Caffe Trieste", category: "cafe", address: "601 Vallejo St, San Francisco, CA 94133",
		lat: 37.7987, lng: -122.4075, hours: daily("07:00", "22:00")},
	{slug: "city-lights", name: "City Lights Booksellers", category: "shop", address: "261 Columbus Ave, San Francisco, CA 94133",
		lat: 37.7976, lng: -122.4065, website: "https://citylights.com", hours: daily("10:00", "22:00")},
	{slug: "vesuvio", name: "Vesuvio Cafe", category: "bar", address: "255 Columbus Ave, San Francisco, CA 94133",
		lat: 37.7975, lng: -122.4064, hours: daily("11:00", "02:00")},
	{slug: "toronado", name: "Toronado", category: "bar", address: "547 Haight St, San Francisco, CA 94117",
		lat: 37.7720, lng: -122.4312, hours: daily("11:30", "02:00")},
	{slug: "swan-oyster", name: "Swan Oyster Depot", category: "restaurant", address: "1517 Polk St, San Francisco, CA 94109",
		lat: 37.7905, lng: -122.4206, hours: daily("10:30", "17:30", "mon", "tue", "wed", "thu", "fri", "sat")},
	{slug: "zuni", name: "Zuni Café", category: "restaurant", address: "1658 Market St, San Francisco, CA 94102",
		lat: 37.7736, lng: -122.4216, hours: daily("11:30", "22:00", "tue", "wed", "thu", "fri", "sat", "sun")},
	{slug: "bi-rite", name: "Bi-Rite Market", category: "grocery", address: "3639 18th St, San Francisco, CA 94110",
		lat: 37.7617, lng: -122.4256, hours: daily("08:00", "21:00")},
	{slug: "main-library", name: "San Francisco Main Library", category: "service", address: "100 Larkin St, San Francisco, CA 94102",
		lat: 37.7790, lng: -122.4158, website: "https://sfpl.org"},
	{slug: "dolores-park", name: "Mission Dolores Park", category: "park", address: "Dolores St & 19th St, San Francisco, CA 94114",
		lat: 37.7596, lng: -122.4269, hours: daily("06:00", "22:00")},
	{slug: "alamo-square", name: "Alamo Square", category: "park", address: "Steiner St & Hayes St, San Francisco, CA 94117",
		lat: 37.7764, lng: -122.4346},
	{slug: "lands-end", name: "Lands End Lookout", category: "park", address: "680 Point Lobos Ave, San Francisco, CA 94121",
		lat: 37.7797, lng: -122.5115},
	{slug: "sfmoma", name: "SFMOMA", category: "culture", address: "151 3rd St, San Francisco, CA 94103",
		lat: 37.7857, lng: -122.4011, website: "https://www.sfmoma.org", hours: daily("10:00", "17:00", "mon", "tue", "fri", "sat", "sun")},
	{slug: "de-young", name: "de Young Museum", category: "culture", address: "50 Hagiwara Tea Garden Dr, San Francisco, CA 94118",
		lat: 37.7715, lng: -122.4687, website: "https://www.famsf.org", hours: daily("09:30", "17:15", "tue", "wed", "thu", "fri", "sat", "sun")},
	{slug: "exploratorium", name: "Exploratorium", category: "culture", address: "Pier 15, San Francisco, CA 94111",
		lat: 37.8017, lng: -122.3973, website: "https://www.exploratorium.edu"},
}

// SeedDev adds the San Francisco places: most are staff listings, one is
// Jordan's approved submission, one Sam's pending submission; Sam has two
// favourites. Ids are derived from slugs, so re-running is a no-op.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 3 {
		return nil
	}
	admin, sam, jordan := users[0], users[1], users[2]
	fallback := s.cfg.categories[len(s.cfg.categories)-1].ID
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		ids := map[string]uuid.UUID{}
		for _, p := range seedPlaces {
			id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("scaffold-dev-place:"+p.slug))
			ids[p.slug] = id
			cat := p.category
			if !s.cfg.categorySet[cat] {
				cat = fallback
			}
			var owner, reviewer *uuid.UUID
			status := "approved"
			switch p.slug {
			case "toronado":
				owner, reviewer = &jordan, &admin
			case "exploratorium":
				owner, status = &sam, "pending"
			default:
				reviewer = &admin
			}
			hours, err := hoursJSON(p.hours)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO places (id, owner_id, name, category, address, lat, lng, phone, website, hours, status,
				                    reviewed_by, reviewed_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, CASE WHEN $12::uuid IS NULL THEN NULL ELSE now() END)
				ON CONFLICT (id) DO NOTHING`,
				id, owner, p.name, cat, p.address, p.lat, p.lng, p.phone, p.website, hours, status, reviewer); err != nil {
				return err
			}
		}
		for _, slug := range []string{"tartine", "dolores-park"} {
			if _, err := tx.Exec(ctx, `INSERT INTO place_favorites (user_id, place_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				sam, ids[slug]); err != nil {
				return err
			}
		}
		return nil
	})
}
