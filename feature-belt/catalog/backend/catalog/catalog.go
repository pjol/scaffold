// Package catalog is a reference catalog: a large, read-mostly set of items
// (thousands to hundreds of thousands) imported from an outside source,
// resolved by name and searched in memory.
//
//	source (JSON/NDJSON over HTTPS, a file, or one registered with RegisterSource)
//	  → sync, nightly at CATALOG_SYNC_HOUR UTC or on demand by staff, one at a time
//	    across instances: validate → diff against the stored items (insert, update,
//	    retire; keys are never deleted) → refuse when it would retire more than
//	    CATALOG_MAX_RETIRE_PERCENT → apply in one transaction, bump the version
//	  → catalog_items + catalog_state.version
//	  → every instance rebuilds an immutable index when the version changes and
//	    swaps it in atomically, so reads never lock and never see half a sync.
//
// Other modules call Lookup, Search and Item. Distilled from MOX
// (internal/universe) without its card domain. Unrelated to the exchange
// feature's catalog of markets.
package catalog

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/jobs"
)

// Item is one catalog entry. Key is the source's stable id. Items are never
// deleted: an item the source stops listing is retired, and comes back when
// the source lists it again (unless staff retired it).
type Item struct {
	Key         string            `json:"key"`
	Name        string            `json:"name"`
	Aliases     []string          `json:"aliases"` // other names it resolves by; only grow (a renamed item keeps its old name)
	Category    string            `json:"category"`
	Subtitle    string            `json:"subtitle"`
	ImageURL    string            `json:"image_url"`
	Attrs       map[string]string `json:"attrs"`
	RefPrice    *decimal.Decimal  `json:"ref_price"` // a reference price from the source, not a live one; nil = none
	Currency    string            `json:"currency"`  // ISO 4217, set with RefPrice
	Retired     bool              `json:"retired"`
	RetiredAt   *time.Time        `json:"retired_at"`
	RetiredBy   string            `json:"retired_by"` // "" while active, else RetiredBySource or RetiredByStaff
	FirstSeenAt time.Time         `json:"first_seen_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Who retired an item.
const (
	RetiredBySource = "source" // the source stopped listing it; restored when it lists it again
	RetiredByStaff  = "staff"  // retired by hand; stays retired until staff restore it
)

// How a name resolved (Result.Match).
const (
	MatchExact = "exact" // the item's name
	MatchAlias = "alias" // one of its aliases
	MatchFuzzy = "fuzzy" // Damerau-Levenshtein ≤ 2 on a name of ≥ 6 characters, one closest item: show it as a guess
)

// Why a name didn't resolve (Result.Reason).
const (
	ReasonUnknown   = "unknown"   // no item by that name
	ReasonAmbiguous = "ambiguous" // several active items share it
)

// Query is one name to resolve, as a person typed or pasted it. Lookup
// normalises it itself (NameKey), so pass the raw text.
type Query struct {
	Name string
}

// Result is a Lookup outcome: OK with Item and Match, or Reason with up to 5
// Candidates (the tied items, then names that start with the query, then
// names that contain it).
type Result struct {
	Item       Item
	OK         bool
	Match      string // MatchExact | MatchAlias | MatchFuzzy, when OK
	Reason     string // ReasonUnknown | ReasonAmbiguous, when !OK
	Candidates []Item
}

// Filter narrows Search and the item lists. Zero values don't filter.
type Filter struct {
	Category string // one category, exactly as stored
	Retired  bool   // include retired items (left out by default)
}

// Category is a category with its count of active items.
type Category struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type settings struct {
	publicReads    bool
	syncHour       int // UTC hour of the nightly sync; -1 = no nightly sync
	maxRetirePct   int
	maxItems       int
	sourceTimeout  time.Duration
	reloadEvery    time.Duration
	resolvePerMin  int
	clientIndexMax int
	allowHTTP      bool // image URLs and the http source may use plain http (development)
}

// Service is the catalog module.
type Service struct {
	d   *app.Deps
	cfg settings
	log *slog.Logger
	src Source // nil: no source configured, so no syncs

	idx    atomic.Pointer[index]
	loadMu sync.Mutex
	reload chan struct{}
	base   atomic.Pointer[context.Context] // Start's context: syncs stop with the server
	body   bodyCache                       // GET /v1/catalog/index
}

func New(d *app.Deps) *Service {
	dev := d.Cfg != nil && d.Cfg.IsDevelopment()
	s := &Service{
		d:      d,
		log:    slog.Default().With("module", "catalog"),
		reload: make(chan struct{}, 1),
		cfg: settings{
			publicReads:    config.Bool("CATALOG_PUBLIC_READS", true),
			syncHour:       config.Int("CATALOG_SYNC_HOUR", 3),
			maxRetirePct:   config.Int("CATALOG_MAX_RETIRE_PERCENT", 40),
			maxItems:       config.Int("CATALOG_MAX_ITEMS", 500_000),
			sourceTimeout:  config.Duration("CATALOG_SOURCE_TIMEOUT", 10*time.Minute),
			reloadEvery:    config.Duration("CATALOG_RELOAD_INTERVAL", 30*time.Second),
			resolvePerMin:  config.Int("CATALOG_RESOLVE_PER_MINUTE", 30),
			clientIndexMax: config.Int("CATALOG_CLIENT_INDEX_MAX", 20_000),
			allowHTTP:      dev,
		},
	}
	if h := s.cfg.syncHour; h < -1 || h > 23 {
		config.Problem("CATALOG_SYNC_HOUR must be 0 to 23 (UTC), or -1 for no nightly sync (got %d)", h)
	}
	if p := s.cfg.maxRetirePct; p < 1 || p > 100 {
		config.Problem("CATALOG_MAX_RETIRE_PERCENT must be 1 to 100 (got %d)", p)
	}
	if n := s.cfg.maxItems; n < 1 || n > 5_000_000 {
		config.Problem("CATALOG_MAX_ITEMS must be 1 to 5000000 (got %d)", n)
	}
	if t := s.cfg.sourceTimeout; t < 10*time.Second || t > 50*time.Minute {
		config.Problem("CATALOG_SOURCE_TIMEOUT must be 10s to 50m (got %s)", t)
	}
	if t := s.cfg.reloadEvery; t < time.Second || t > 10*time.Minute {
		config.Problem("CATALOG_RELOAD_INTERVAL must be 1s to 10m (got %s)", t)
	}
	if n := s.cfg.resolvePerMin; n < 1 {
		config.Problem("CATALOG_RESOLVE_PER_MINUTE must be at least 1 (got %d)", n)
	}
	if n := s.cfg.clientIndexMax; n < 0 {
		config.Problem("CATALOG_CLIENT_INDEX_MAX must be 0 or more (got %d)", n)
	}
	s.idx.Store(emptyIndex())
	s.src = selectSource(d)
	srcName := "none"
	if s.src != nil {
		srcName = s.src.Name()
	}
	s.log.Info("catalog: configured", "source", srcName, "nightly_hour_utc", s.cfg.syncHour, "public_reads", s.cfg.publicReads)
	return s
}

func (s *Service) Name() string { return "catalog" }

// Start loads the catalog in the background, reloads it whenever another
// instance (or a sync here) changes it, and schedules the nightly sync.
func (s *Service) Start(ctx context.Context) {
	s.base.Store(&ctx)
	go s.watch(ctx)
	if s.cfg.syncHour >= 0 {
		go jobs.Every(ctx, s.d.Pool, "catalog.nightly", 5*time.Minute, s.nightly)
	}
}

// lifetime is Start's context (Background before Start, as in cmd/seed).
func (s *Service) lifetime() context.Context {
	if p := s.base.Load(); p != nil {
		return *p
	}
	return context.Background()
}

// ---- the Go API other modules use ----

// Lookup resolves a name: exact name, then alias, then fuzzy, else unresolved
// with candidates. A name that several items share resolves to the one that
// isn't retired when there is exactly one; otherwise it is ambiguous.
// Retired items still resolve (Item.Retired says so).
func (s *Service) Lookup(q Query) Result { return s.idx.Load().lookup(q) }

// Search ranks items by their best name: the exact name, then names starting
// with q, then a later word starting with q, then names containing q, then
// aliases (starting with q, then containing it). Ties keep name order. q needs
// at least 2 characters; limit defaults to 20 and is capped at 50.
func (s *Service) Search(q string, f Filter, limit int) []Item {
	x := s.idx.Load()
	hits := x.search(q, f, limit)
	out := make([]Item, 0, len(hits))
	for _, h := range hits {
		out = append(out, x.item(h.i))
	}
	return out
}

// Item finds an item by key, retired ones too.
func (s *Service) Item(key string) (Item, bool) {
	x := s.idx.Load()
	i, ok := x.byKey[key]
	if !ok {
		return Item{}, false
	}
	return x.item(i), true
}

// Categories are the categories of active items, largest first.
func (s *Service) Categories() []Category { return s.idx.Load().categories() }

// Version is the loaded catalog's version (catalog_state.version); it changes
// with every sync that changes something and every retire or restore.
func (s *Service) Version() int64 { return s.idx.Load().version }

// Loaded reports whether this instance has loaded the catalog yet (reads
// before that see an empty catalog).
func (s *Service) Loaded() bool { return s.idx.Load().loaded }
