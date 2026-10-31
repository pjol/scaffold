// Package places is the map feature: places with coordinates, bounding-box
// search with server-side grid clustering, nearest-first lists, geocoding
// proxied to Photon or Mapbox, photos, per-user favourites and a staff review
// queue. Clients read their map settings from GET /v1/places/config.
package places

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

const (
	// photoPurpose is the storage purpose for place photos (public-read).
	photoPurpose = "place_photo"
	// maxPhotosPerPlace caps photos on one listing.
	maxPhotosPerPlace = 10

	defaultCategories = "restaurant:Restaurant,cafe:Café,bar:Bar,shop:Shop,grocery:Grocery," +
		"service:Service,park:Park,culture:Arts and culture,other:Other"
	defaultCenter   = "37.7749,-122.4194" // San Francisco
	defaultStyleURL = "https://tiles.openfreemap.org/styles/liberty"
	defaultPhoton   = "https://photon.komoot.io"
)

type Service struct {
	d   *app.Deps
	cfg settings
	geo Geocoder
}

type settings struct {
	categories      []Category
	categorySet     map[string]bool
	requireApproval bool
	mapStyleURL     string
	center          LatLng
	zoom            int
	serviceArea     *BBox // nil = anywhere
}

// Category is one entry of PLACES_CATEGORIES.
type Category struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LatLng is a WGS84 point.
type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// contentFilter is the optional pre-post text check (moderation.Check when
// the app stitches it in with SetContentFilter).
var contentFilter func(ctx context.Context, text string) error

// SetContentFilter installs a check run on the name and description of every
// submitted or edited place. It returns a *httpx.Error (422) to refuse the
// text. Wire moderation.Check here when the app installs moderation.
func SetContentFilter(fn func(ctx context.Context, text string) error) { contentFilter = fn }

func New(d *app.Deps) *Service {
	storage.RegisterPurpose(storage.Purpose{Name: photoPurpose, Public: true, Types: storage.ImageTypes})

	// Bad values are recorded with config.Problem (the boot fails at config.Check
	// with every problem at once); the defaults keep construction going meanwhile.
	cats, err := parseCategories(config.String("PLACES_CATEGORIES", defaultCategories))
	if err != nil {
		config.Problem("PLACES_CATEGORIES: %v", err)
		cats, _ = parseCategories(defaultCategories)
	}
	center, err := parseLatLng(config.String("PLACES_DEFAULT_CENTER", defaultCenter))
	if err != nil {
		config.Problem("PLACES_DEFAULT_CENTER must be lat,lng (%v)", err)
		center, _ = parseLatLng(defaultCenter)
	}
	zoom := config.Int("PLACES_DEFAULT_ZOOM", 12)
	if zoom < 0 || zoom > 22 {
		config.Problem("PLACES_DEFAULT_ZOOM must be 0 to 22 (got %d)", zoom)
		zoom = 12
	}
	styleURL := config.String("PLACES_MAP_STYLE_URL", defaultStyleURL)
	if !httpURL(styleURL) {
		config.Problem("PLACES_MAP_STYLE_URL must be an http(s) URL (got %q)", styleURL)
		styleURL = defaultStyleURL
	}
	var area *BBox
	if raw := config.String("PLACES_SERVICE_AREA", ""); raw != "" {
		if b, err := parseBBox(raw); err != nil {
			config.Problem("PLACES_SERVICE_AREA must be minLng,minLat,maxLng,maxLat (%v)", err)
		} else {
			area = &b
		}
	}
	photonURL := config.String("PLACES_PHOTON_URL", defaultPhoton)
	if !httpURL(photonURL) {
		config.Problem("PLACES_PHOTON_URL must be an http(s) URL (got %q)", photonURL)
		photonURL = defaultPhoton
	}
	var countries []string
	for _, c := range config.List("PLACES_GEOCODE_COUNTRIES", "") {
		if len(c) != 2 {
			config.Problem("PLACES_GEOCODE_COUNTRIES takes ISO 3166 alpha-2 codes like us,ca (got %q)", c)
			continue
		}
		countries = append(countries, strings.ToLower(c))
	}

	set := make(map[string]bool, len(cats))
	for _, c := range cats {
		set[c.ID] = true
	}
	s := &Service{d: d, cfg: settings{
		categories:      cats,
		categorySet:     set,
		requireApproval: config.Bool("PLACES_REQUIRE_APPROVAL", true),
		mapStyleURL:     styleURL,
		center:          center,
		zoom:            zoom,
		serviceArea:     area,
	}}
	s.geo = newGeocoder(geocoderOptions{
		mapboxToken: config.String("MAPBOX_TOKEN", ""),
		photonURL:   strings.TrimRight(photonURL, "/"),
		countries:   countries,
		ratePerMin:  max(1, config.Int("PLACES_GEOCODE_RATE_PER_MIN", 60)),
		userAgent:   "Scaffold App geocoder (" + d.Cfg.SupportEmail + ")",
		bias:        center,
	})
	slog.Info("places configured", "geocoder", s.geo.Name(), "require_approval", s.cfg.requireApproval,
		"categories", len(cats), "service_area", area != nil)
	return s
}

func (s *Service) Name() string { return "places" }

func (s *Service) MountPublic(r chi.Router) {
	r.Get("/places/config", httpx.Handler(s.clientConfig))
	r.Get("/places", httpx.Handler(s.search))
	r.Get("/places/{id}", httpx.Handler(s.getPlace))
}

func (s *Service) MountUser(r chi.Router) {
	geo := r.With(auth.RateLimit(30, time.Minute))
	geo.Get("/places/geocode", httpx.Handler(s.geocode))
	geo.Get("/places/reverse", httpx.Handler(s.reverse))
	r.Get("/places/mine", httpx.Handler(s.listMine))
	r.Get("/places/{id}/viewer", httpx.Handler(s.viewPlace))
	r.Get("/places/favorites", httpx.Handler(s.listFavorites))
	r.Get("/places/favorites/ids", httpx.Handler(s.favoriteIDs))

	w := r.With(auth.RequireActive)
	w.With(auth.RateLimit(10, time.Hour)).Post("/places", httpx.Handler(s.createPlace))
	w.With(auth.RateLimit(60, time.Hour)).Patch("/places/{id}", httpx.Handler(s.updatePlace))
	w.Delete("/places/{id}", httpx.Handler(s.deletePlace))
	w.With(auth.RateLimit(60, time.Hour)).Post("/places/{id}/photos", httpx.Handler(s.addPhoto))
	w.Delete("/places/{id}/photos/{photo_id}", httpx.Handler(s.removePhoto))
	fav := w.With(auth.RateLimit(120, time.Minute))
	fav.Put("/places/{id}/favorite", httpx.Handler(s.addFavorite))
	fav.Delete("/places/{id}/favorite", httpx.Handler(s.removeFavorite))
}

func (s *Service) MountAdmin(r chi.Router) {
	r.Get("/places", httpx.Handler(s.adminList))
	r.Post("/places", httpx.Handler(s.adminCreate))
	r.Get("/places/{id}", httpx.Handler(s.adminGet))
	r.Patch("/places/{id}", httpx.Handler(s.adminUpdate))
	r.Post("/places/{id}/approve", httpx.Handler(s.adminApprove))
	r.Post("/places/{id}/reject", httpx.Handler(s.adminReject))
	r.Post("/places/{id}/photos", httpx.Handler(s.adminAddPhoto))
	r.Delete("/places/{id}/photos/{photo_id}", httpx.Handler(s.adminRemovePhoto))
	r.With(auth.RequireAdminRole("admin")).Delete("/places/{id}", httpx.Handler(s.adminDelete))
}

// ---- DTOs (packages/shared/src/places/types.ts) -----------------------------

// Place is a full listing.
type Place struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Category    string    `json:"category"`
	Description string    `json:"description"`
	Address     string    `json:"address"`
	Lat         float64   `json:"lat"`
	Lng         float64   `json:"lng"`
	Phone       string    `json:"phone"`
	Website     string    `json:"website"`
	Hours       *Hours    `json:"hours"`
	Photos      []Photo   `json:"photos"`
	Status      string    `json:"status"`      // pending | approved | rejected
	ReviewNote  string    `json:"review_note"` // rejection reason (owner and staff views)
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Photo is one public photo of a place.
type Photo struct {
	ID  uuid.UUID `json:"id"`
	URL string    `json:"url"`
}

// PlaceSummary is a map pin or list row.
type PlaceSummary struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Address   string    `json:"address"`
	Lat       float64   `json:"lat"`
	Lng       float64   `json:"lng"`
	PhotoURL  *string   `json:"photo_url"`
	DistanceM *float64  `json:"distance_m"` // set by ?near= searches
}

// ClientConfig is GET /v1/places/config.
type ClientConfig struct {
	Categories      []Category  `json:"categories"`
	MapStyleURL     string      `json:"map_style_url"`
	DefaultCenter   LatLng      `json:"default_center"`
	DefaultZoom     int         `json:"default_zoom"`
	RequireApproval bool        `json:"require_approval"`
	ServiceArea     *[4]float64 `json:"service_area"` // minLng, minLat, maxLng, maxLat
	MaxPhotos       int         `json:"max_photos"`
	Geocoder        string      `json:"geocoder"` // photon | mapbox (attribution)
}

func (s *Service) clientConfig(w http.ResponseWriter, r *http.Request) error {
	out := ClientConfig{
		Categories:      s.cfg.categories,
		MapStyleURL:     s.cfg.mapStyleURL,
		DefaultCenter:   s.cfg.center,
		DefaultZoom:     s.cfg.zoom,
		RequireApproval: s.cfg.requireApproval,
		MaxPhotos:       maxPhotosPerPlace,
		Geocoder:        s.geo.Name(),
	}
	if a := s.cfg.serviceArea; a != nil {
		out.ServiceArea = &[4]float64{a.MinLng, a.MinLat, a.MaxLng, a.MaxLat}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	httpx.OK(w, out)
	return nil
}

// ---- config parsing ----------------------------------------------------------

// parseCategories reads "id:Label,id2:Label 2" (a bare id gets a label made
// from it). Ids are lowercase letters, digits, - and _.
func parseCategories(raw string) ([]Category, error) {
	var out []Category
	seen := map[string]bool{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, label, _ := strings.Cut(part, ":")
		id, label = strings.TrimSpace(id), strings.TrimSpace(label)
		if !validCategoryID(id) {
			return nil, fmt.Errorf("category id %q must be 1 to 40 lowercase letters, digits, - or _", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("category %q is listed twice", id)
		}
		seen[id] = true
		if label == "" {
			label = strings.ToUpper(id[:1]) + strings.ReplaceAll(id[1:], "_", " ")
		}
		out = append(out, Category{ID: id, Label: label})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("list at least one category")
	}
	return out, nil
}

func validCategoryID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func httpURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// parseLatLng reads "lat,lng".
func parseLatLng(raw string) (LatLng, error) {
	a, b, ok := strings.Cut(raw, ",")
	if !ok {
		return LatLng{}, fmt.Errorf("expected lat,lng")
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	lng, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err1 != nil || err2 != nil || !validLat(lat) || !validLng(lng) {
		return LatLng{}, fmt.Errorf("latitude must be -90 to 90 and longitude -180 to 180")
	}
	return LatLng{Lat: lat, Lng: lng}, nil
}
