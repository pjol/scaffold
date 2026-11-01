package places

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"scaffoldapp/backend/internal/httpx"
)

// GeoResult is one geocoding match.
type GeoResult struct {
	Label       string  `json:"label"`  // one-line display address
	Name        string  `json:"name"`   // venue or area name when the match is not a street address
	Street      string  `json:"street"` // "600 Guerrero St"
	City        string  `json:"city"`
	Region      string  `json:"region"` // state or province
	Postcode    string  `json:"postcode"`
	CountryCode string  `json:"country_code"` // ISO 3166-1 alpha-2, upper case
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
}

// Geocoder turns text into places and coordinates into an address. The
// implementations call fixed provider endpoints from config; user input only
// ever becomes query parameters, never a URL.
type Geocoder interface {
	Name() string
	Search(ctx context.Context, query string, near *LatLng) ([]GeoResult, error)
	// Reverse returns nil, nil when nothing is found.
	Reverse(ctx context.Context, lat, lng float64) (*GeoResult, error)
}

var (
	errUpstream = errors.New("geocoding provider unavailable")
	errBusy     = errors.New("geocoding rate limit reached")
)

const (
	geocodeLimit    = 6
	geocodeTimeout  = 5 * time.Second
	geocodeCacheTTL = 30 * time.Minute
	geocodeCacheMax = 10_000
)

type geocoderOptions struct {
	mapboxToken string
	photonURL   string
	countries   []string // lower-case alpha-2; empty = worldwide
	ratePerMin  int      // upstream calls per minute across all users (cache hits are free)
	userAgent   string
	bias        LatLng // default proximity when the caller sends none
}

func newGeocoder(o geocoderOptions) Geocoder {
	up := upstream{client: &http.Client{Timeout: geocodeTimeout}, userAgent: o.userAgent}
	var g Geocoder
	if o.mapboxToken != "" {
		g = &mapbox{upstream: up, token: o.mapboxToken, baseURL: mapboxBaseURL, countries: o.countries}
	} else {
		g = &photon{upstream: up, baseURL: o.photonURL, countries: o.countries}
	}
	return &cachedGeocoder{next: g, bias: o.bias, entries: map[string]cacheEntry{},
		limit: newBucket(o.ratePerMin, time.Minute)}
}

type upstream struct {
	client    *http.Client
	userAgent string
}

func (u upstream) getJSON(ctx context.Context, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", u.userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := u.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", errUpstream, redact(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("%w: read: %v", errUpstream, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: HTTP %d", errUpstream, resp.StatusCode)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("%w: decode: %v", errUpstream, err)
	}
	return nil
}

// redact drops the request URL (it may carry the Mapbox token) from
// transport errors before they are logged.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func formatCoord(f float64) string { return strconv.FormatFloat(f, 'f', 6, 64) }

// label joins the non-empty, non-repeated parts of a result.
func label(r GeoResult) string {
	parts := []string{}
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[strings.ToLower(s)] {
			seen[strings.ToLower(s)] = true
			parts = append(parts, s)
		}
	}
	add(r.Name)
	add(r.Street)
	add(r.City)
	add(strings.TrimSpace(r.Region + " " + r.Postcode))
	return strings.Join(parts, ", ")
}

func allowedCountry(countries []string, code string) bool {
	if len(countries) == 0 {
		return true
	}
	for _, c := range countries {
		if strings.EqualFold(c, code) {
			return true
		}
	}
	return false
}

// ---- cache + global rate limit -------------------------------------------------

type cacheEntry struct {
	val     any
	expires time.Time
}

// cachedGeocoder memoises results (including "not found") for 30 minutes and
// caps upstream calls with a token bucket. Errors are never cached.
type cachedGeocoder struct {
	next  Geocoder
	bias  LatLng
	limit *bucket

	mu      sync.Mutex
	entries map[string]cacheEntry
}

func (c *cachedGeocoder) Name() string { return c.next.Name() }

func (c *cachedGeocoder) lookup(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		return nil, false
	}
	return e.val, true
}

func (c *cachedGeocoder) store(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.entries) >= geocodeCacheMax {
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
		if len(c.entries) >= geocodeCacheMax {
			clear(c.entries)
		}
	}
	c.entries[key] = cacheEntry{val: val, expires: now.Add(geocodeCacheTTL)}
}

func round(f float64, dp int) float64 {
	p := math.Pow(10, float64(dp))
	return math.Round(f*p) / p
}

func (c *cachedGeocoder) Search(ctx context.Context, query string, near *LatLng) ([]GeoResult, error) {
	if near == nil {
		near = &c.bias
	}
	key := fmt.Sprintf("s|%s|%.2f,%.2f", strings.ToLower(query), round(near.Lat, 2), round(near.Lng, 2))
	if v, ok := c.lookup(key); ok {
		return v.([]GeoResult), nil
	}
	if !c.limit.take() {
		return nil, errBusy
	}
	out, err := c.next.Search(ctx, query, near)
	if err != nil {
		return nil, err
	}
	c.store(key, out)
	return out, nil
}

func (c *cachedGeocoder) Reverse(ctx context.Context, lat, lng float64) (*GeoResult, error) {
	key := fmt.Sprintf("r|%.4f,%.4f", round(lat, 4), round(lng, 4))
	if v, ok := c.lookup(key); ok {
		return v.(*GeoResult), nil
	}
	if !c.limit.take() {
		return nil, errBusy
	}
	out, err := c.next.Reverse(ctx, lat, lng)
	if err != nil {
		return nil, err
	}
	c.store(key, out)
	return out, nil
}

// bucket is a token bucket: n tokens per window, refilled continuously.
type bucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens per second
	last   time.Time
}

func newBucket(n int, window time.Duration) *bucket {
	return &bucket{tokens: float64(n), max: float64(n), rate: float64(n) / window.Seconds(), last: time.Now()}
}

func (b *bucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens = math.Min(b.max, b.tokens+now.Sub(b.last).Seconds()*b.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ---- handlers --------------------------------------------------------------------

func geoError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, errBusy):
		return httpx.NewError(http.StatusTooManyRequests, "geocoder_busy", "Address search is busy. Try again in a minute.")
	case errors.Is(err, errUpstream):
		slog.WarnContext(ctx, "places: geocoder", "err", err)
		return httpx.NewError(http.StatusServiceUnavailable, "geocoder_unavailable", "Address search isn't available right now. Try again shortly.")
	}
	return err
}

// geocode is GET /v1/places/geocode?q=&near=lat,lng.
func (s *Service) geocode(w http.ResponseWriter, r *http.Request) error {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if n := utf8.RuneCountInString(q); n < 2 || n > 200 {
		return httpx.BadRequest("Type 2 to 200 characters to search.")
	}
	var near *LatLng
	if raw := r.URL.Query().Get("near"); raw != "" {
		p, err := parseLatLng(raw)
		if err != nil {
			return httpx.BadRequest("near must be lat,lng.")
		}
		near = &p
	}
	items, err := s.geo.Search(r.Context(), q, near)
	if err != nil {
		return geoError(r.Context(), err)
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// reverse is GET /v1/places/reverse?lat=&lng=.
func (s *Service) reverse(w http.ResponseWriter, r *http.Request) error {
	lat, okLat, err1 := httpx.QueryFloat(r, "lat")
	lng, okLng, err2 := httpx.QueryFloat(r, "lng")
	if err1 != nil || err2 != nil || !okLat || !okLng || !validLat(lat) || !validLng(lng) {
		return httpx.BadRequest("lat must be -90 to 90 and lng -180 to 180.")
	}
	res, err := s.geo.Reverse(r.Context(), lat, lng)
	if err != nil {
		return geoError(r.Context(), err)
	}
	httpx.OK(w, map[string]any{"result": res})
	return nil
}
