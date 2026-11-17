// Package baskets lets people create composite markets from a list of the
// exchange's markets:
//
//	pasted list or picked markets → parse (tolerant, warnings in words)
//	  → resolve each line to a listed market (symbol, name, alias, fuzzy; or the app's Resolver)
//	  → price each line from the oracle → canonical basket (merged, sorted, hashed)
//	  → preview (value, concentration, blockers, the market spec it would get)
//	  → create or reuse: one exchange market per distinct basket, priced by one
//	    oracle basket feed, created in the same transaction as the basket row
//
// An hourly job archives idle baskets and winds down ones past their end
// date; creating the same basket again revives an archived one. Distilled
// from MOX's deck indexes (internal/decks, internal/deckindex).
package baskets

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/oracle"
)

// Statuses of a basket (its market follows: active, reduce_only, halted).
const (
	StatusActive     = "active"
	StatusReduceOnly = "reduce_only"
	StatusArchived   = "archived"
)

// Limits that aren't configuration.
const (
	maxTextBytes  = 20 << 10 // a pasted list
	maxInputLines = 250      // pasted lines + picks
	maxQuantity   = 10_000   // per line, and per market after merging
	entryWeight   = 25       // an entry counts as this much volume in popularity
	category      = "Baskets"
	feedDecimals  = 4
)

type settings struct {
	prefix        string
	minValue      decimal.Decimal
	minLines      int
	maxLines      int
	maxLeverage   int
	minOrder      decimal.Decimal
	oiMin, oiMax  decimal.Decimal
	perDay        int
	perMonth      int
	capacity      int
	idle          time.Duration
	endGrace      time.Duration
	reportsToFlag int
}

// Service is the baskets module.
type Service struct {
	d       *app.Deps
	cfg     settings
	log     *slog.Logger
	filter  *textFilter
	markets marketCache
}

var prefixRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9]{0,10}$`)

func decimalEnv(key, def string) decimal.Decimal {
	v := config.String(key, def)
	d, err := decimal.NewFromString(strings.TrimSpace(v))
	if err != nil || !d.IsPositive() {
		config.Problem("%s must be a positive decimal (got %q)", key, v)
		return decimal.RequireFromString(def)
	}
	return d
}

func New(d *app.Deps) *Service {
	s := &Service{
		d:   d,
		log: slog.Default().With("module", "baskets"),
		cfg: settings{
			prefix:        strings.ToUpper(strings.TrimSpace(config.String("BASKETS_SYMBOL_PREFIX", "BKT"))),
			minValue:      decimalEnv("BASKETS_MIN_VALUE", "20"),
			minLines:      config.Int("BASKETS_MIN_LINES", 2),
			maxLines:      config.Int("BASKETS_MAX_LINES", 100),
			maxLeverage:   config.Int("BASKETS_MAX_LEVERAGE", 3),
			minOrder:      decimalEnv("BASKETS_MIN_ORDER", "10"),
			oiMin:         decimalEnv("BASKETS_OI_CAP_MIN", "5000"),
			oiMax:         decimalEnv("BASKETS_OI_CAP_MAX", "50000"),
			perDay:        config.Int("BASKETS_PER_DAY", 5),
			perMonth:      config.Int("BASKETS_PER_MONTH", 20),
			capacity:      config.Int("BASKETS_CAPACITY", 5000),
			idle:          time.Duration(config.Int("BASKETS_IDLE_DAYS", 14)) * 24 * time.Hour,
			endGrace:      time.Duration(config.Int("BASKETS_END_GRACE_DAYS", 30)) * 24 * time.Hour,
			reportsToFlag: config.Int("BASKETS_REPORTS_TO_FLAG", 3),
		},
		filter: newTextFilter(config.List("BASKETS_BLOCKED_TERMS", "")),
	}
	c := &s.cfg
	switch {
	case !prefixRe.MatchString(c.prefix):
		config.Problem("BASKETS_SYMBOL_PREFIX must be 1–11 of A–Z and 0–9 (got %q)", c.prefix)
	case c.minLines < 1 || c.minLines > 100:
		config.Problem("BASKETS_MIN_LINES must be 1–100 (got %d)", c.minLines)
	case c.maxLines < c.minLines || c.maxLines > 100:
		config.Problem("BASKETS_MAX_LINES must be BASKETS_MIN_LINES–100 (got %d)", c.maxLines)
	case c.maxLeverage < 1 || c.maxLeverage > 100:
		config.Problem("BASKETS_MAX_LEVERAGE must be 1–100 (got %d)", c.maxLeverage)
	case c.oiMax.LessThan(c.oiMin):
		config.Problem("BASKETS_OI_CAP_MAX must be at least BASKETS_OI_CAP_MIN")
	case c.perDay < 1 || c.perMonth < c.perDay:
		config.Problem("BASKETS_PER_DAY must be at least 1 and BASKETS_PER_MONTH at least BASKETS_PER_DAY")
	case c.capacity < 1:
		config.Problem("BASKETS_CAPACITY must be at least 1 (got %d)", c.capacity)
	case c.idle < 24*time.Hour:
		config.Problem("BASKETS_IDLE_DAYS must be at least 1")
	case c.endGrace < 0:
		config.Problem("BASKETS_END_GRACE_DAYS must be 0 or more")
	case c.reportsToFlag < 1:
		config.Problem("BASKETS_REPORTS_TO_FLAG must be at least 1")
	}
	return s
}

func (s *Service) Name() string { return "baskets" }

// Start runs the lifecycle job hourly, once across instances.
func (s *Service) Start(ctx context.Context) {
	go jobs.Every(ctx, s.d.Pool, "baskets.lifecycle", time.Hour, s.lifecycle)
}

// oracle finds the oracle module (a required feature, so always installed).
func (s *Service) oracle() *oracle.Service {
	for _, m := range s.d.Modules {
		if o, ok := m.(*oracle.Service); ok {
			return o
		}
	}
	return nil
}

// ---- the app's resolver ----

var (
	resolverMu sync.RWMutex
	custom     Resolver
)

// SetResolver replaces how pasted names find markets (a reference catalog
// with many more names than the exchange lists). Call it from an init() or a
// module's Start; nil restores the default, which resolves against the
// exchange's markets. Exact market symbols and picked markets always resolve
// directly.
func SetResolver(r Resolver) {
	resolverMu.Lock()
	defer resolverMu.Unlock()
	custom = r
}

func appResolver() Resolver {
	resolverMu.RLock()
	defer resolverMu.RUnlock()
	return custom
}
