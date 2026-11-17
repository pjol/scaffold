// Package oracle turns raw prices from many sources into one published price
// per feed:
//
//	sources (polled through a rate-limited scheduler with per-source budgets
//	         and backoff, or pushed: staff prints, app code) → prints
//	  → venue premium normalisation (fixed or learned)
//	  → screen (stat-v1 logistic; optional decision model that can only raise risk)
//	  → aggregate (recency-weighted median, per-print cap)
//	  → slew-limited publish every ORACLE_PUBLISH_INTERVAL (baskets = Σ quantity × part price)
//
// One instance leads (a lease row in oracle_leader) and runs the pipeline;
// every instance serves reads from the database. Distilled from MOX
// (internal/oracle) without its collectible sources.
package oracle

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
)

// Observation is one screened print.
type Observation struct {
	ID         uuid.UUID `json:"id"`
	FeedID     uuid.UUID `json:"feed_id"`
	FeedKey    string    `json:"feed_key"`
	Source     string    `json:"source"`
	Venue      string    `json:"venue"`
	Kind       string    `json:"kind"`
	Price      string    `json:"price"`     // normalised by the venue premium
	RawPrice   string    `json:"raw_price"` // as the venue reported it
	Premium    float64   `json:"premium"`   // the premium removed (0.12 = 12% above the other venues)
	Qty        *float64  `json:"qty"`
	ObservedAt time.Time `json:"observed_at"`
	ReceivedAt time.Time `json:"received_at"`
	Verdict    string    `json:"verdict"`
	Score      float64   `json:"score"` // manipulation score, 0..1
	Reasons    []string  `json:"reasons"`
	Model      string    `json:"model"`
	Weight     float64   `json:"weight"` // effective weight in the target when received
}

// PricePoint is one published price.
type PricePoint struct {
	FeedID   uuid.UUID
	Key      string
	Price    float64 // 0 = none yet
	Target   float64
	Status   string
	At       time.Time
	Decimals int32
}

type settings struct {
	publishEvery   time.Duration
	sourcesEnabled bool
	disabled       map[string]bool
	learnPremiums  bool
	publicReads    bool
	priceRetention time.Duration
	obsRetention   time.Duration
	printsPerMin   int
}

// Service is the oracle module.
type Service struct {
	d        *app.Deps
	cfg      settings
	log      *slog.Logger
	sources  map[string]Source
	decision DecisionModel
	instance string

	mu         sync.RWMutex
	leading    bool
	leadStart  time.Time
	feeds      map[uuid.UUID]*feed
	byKey      map[string]*feed
	order      []*feed
	prices     map[uuid.UUID]PricePoint
	lastPub    map[uuid.UUID]PricePoint
	lastSaved  map[uuid.UUID]PricePoint
	lastTick   time.Time
	lastSave   time.Time
	limiters   map[string]*limiter // by laneKey(source, lane)
	refLane    map[uuid.UUID]string
	restoredOK map[string]time.Time
	catalogSig string
	onPublish  []func(PricePoint)

	fmu      sync.Mutex
	fetchLog map[string]fetchRec

	ingestCh chan []rawPrint
	reload   chan struct{} // catalog reload now (an inbox print waits on an unknown feed)
	decideCh chan decideJob
	writes   chan dbWrite
	events   chan event
	dropped  atomic.Int64

	runMu     sync.Mutex
	runCancel context.CancelFunc
}

// rawPrint is a print on its way into the screen.
type rawPrint struct {
	Print
	Source string
	Live   bool // arrived after this leadership's first poll of its task: eligible for the decision model
	Actor  *uuid.UUID
	key    string // dedupe key already computed (an inbox row's)
	pushed bool   // from the inbox: a drop is logged, since the caller got a 202
}

// dedupe is the print's key in its feed (oracle_observations is unique on
// (feed_id, dedupe_key)): source:venue:id, or one derived from the print.
func (r rawPrint) dedupe() string {
	if r.key != "" {
		return r.key
	}
	if r.ID != "" {
		return r.Source + ":" + r.Venue + ":" + r.ID
	}
	return fmt.Sprintf("%s:%s:%s:%d:%s", r.Source, r.Venue, r.Kind, r.At.UnixMilli(), strconv.FormatFloat(r.Price, 'g', 12, 64))
}

type decideJob struct {
	r       *rec
	f       *feed
	state   map[string]string
	stat    float64
	reasons []string
	vw      float64
	actor   *uuid.UUID
}

func New(d *app.Deps) *Service {
	host, _ := os.Hostname()
	s := &Service{
		d:   d,
		log: slog.Default().With("module", "oracle"),
		cfg: settings{
			publishEvery:   config.Duration("ORACLE_PUBLISH_INTERVAL", 3*time.Second),
			sourcesEnabled: config.Bool("ORACLE_SOURCES_ENABLED", true),
			disabled:       map[string]bool{},
			learnPremiums:  config.Bool("ORACLE_LEARN_PREMIUMS", true),
			publicReads:    config.Bool("ORACLE_PUBLIC_READS", true),
			priceRetention: config.Duration("ORACLE_PRICE_RETENTION", 30*24*time.Hour),
			obsRetention:   config.Duration("ORACLE_OBSERVATION_RETENTION", 180*24*time.Hour),
			printsPerMin:   config.Int("ORACLE_PRINTS_PER_MINUTE", 30),
		},
		instance: fmt.Sprintf("%s:%d:%s", host, os.Getpid(), uuid.NewString()[:8]),
		feeds:    map[uuid.UUID]*feed{}, byKey: map[string]*feed{},
		prices: map[uuid.UUID]PricePoint{}, lastPub: map[uuid.UUID]PricePoint{}, lastSaved: map[uuid.UUID]PricePoint{},
		limiters: map[string]*limiter{}, refLane: map[uuid.UUID]string{}, restoredOK: map[string]time.Time{}, fetchLog: map[string]fetchRec{},
		ingestCh: make(chan []rawPrint, 256), reload: make(chan struct{}, 1), decideCh: make(chan decideJob, 20000),
		writes: make(chan dbWrite, 20000), events: make(chan event, 4096),
	}
	for _, n := range config.List("ORACLE_DISABLE_SOURCES", "") {
		s.cfg.disabled[n] = true
	}
	if s.cfg.publishEvery < 500*time.Millisecond || s.cfg.publishEvery > time.Minute {
		config.Problem("ORACLE_PUBLISH_INTERVAL must be between 500ms and 1m (got %s)", s.cfg.publishEvery)
	}
	if s.cfg.obsRetention < 30*24*time.Hour {
		config.Problem("ORACLE_OBSERVATION_RETENTION must be at least 720h (got %s)", s.cfg.obsRetention)
	}
	if s.cfg.priceRetention < 24*time.Hour {
		config.Problem("ORACLE_PRICE_RETENTION must be at least 24h (got %s)", s.cfg.priceRetention)
	}
	s.cfg.printsPerMin = max(1, s.cfg.printsPerMin)
	s.sources = registeredSources(d)

	decisionMu.Lock()
	custom := decisionCustom
	decisionMu.Unlock()
	if custom != nil {
		s.decision = custom
	} else if hd := newHTTPDecision(
		config.String("ORACLE_DECISION_URL", ""),
		config.String("ORACLE_DECISION_API_KEY", ""),
		config.String("ORACLE_DECISION_MODEL", ""),
		config.Int("ORACLE_DECISION_MAX_RPS", 20),
		config.Duration("ORACLE_DECISION_TIMEOUT", 2*time.Second),
	); hd != nil {
		hd.warn = s.log.Warn
		s.decision = hd
	}
	names := make([]string, 0, len(s.sources))
	for n := range s.sources {
		names = append(names, n)
	}
	slices.Sort(names)
	s.log.Info("oracle: configured", "sources", strings.Join(names, ","), "polling", s.cfg.sourcesEnabled,
		"decision_model", s.decision != nil, "public_reads", s.cfg.publicReads)
	return s
}

func (s *Service) Name() string { return "oracle" }

// OnPublish registers a function called with every published price on the
// leading instance, from the publisher goroutine (it must not block). Call it
// from another module's Start. Consumers on other instances subscribe to the
// realtime events or call Current.
func (s *Service) OnPublish(fn func(PricePoint)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPublish = append(s.onPublish, fn)
}

// Start runs the leadership loop: the instance holding the lease runs the
// pipeline; the others retry every 5 s.
func (s *Service) Start(ctx context.Context) {
	s.startPrune(ctx)
	go s.run(ctx)
}

const (
	leaseFor   = 20 * time.Second
	leaseRenew = 5 * time.Second
	grace      = 10 * time.Minute // prints timestamped this far before the leadership started still count as live
)

func (s *Service) claim(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var holder string
	err := s.d.Pool.QueryRow(ctx, `INSERT INTO oracle_leader (id, holder, until) VALUES (1, $1, now() + make_interval(secs => $2))
 ON CONFLICT (id) DO UPDATE SET holder = EXCLUDED.holder, until = EXCLUDED.until
 WHERE oracle_leader.holder = EXCLUDED.holder OR oracle_leader.until < now()
 RETURNING holder`, s.instance, leaseFor.Seconds()).Scan(&holder)
	if db.IsNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return holder == s.instance, nil
}

func (s *Service) run(ctx context.Context) {
	for {
		ok, err := s.claim(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("oracle: leadership check failed", "err", err)
		}
		if ok {
			s.lead(ctx)
		}
		select {
		case <-ctx.Done():
			rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			_, _ = s.d.Pool.Exec(rctx, `DELETE FROM oracle_leader WHERE holder = $1`, s.instance)
			cancel()
			return
		case <-time.After(leaseRenew):
		}
	}
}

// lead runs the pipeline until the lease is lost or ctx ends.
func (s *Service) lead(ctx context.Context) {
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.feeds, s.byKey, s.order = map[uuid.UUID]*feed{}, map[string]*feed{}, nil
	s.prices, s.lastPub, s.lastSaved = map[uuid.UUID]PricePoint{}, map[uuid.UUID]PricePoint{}, map[uuid.UUID]PricePoint{}
	s.limiters, s.refLane, s.restoredOK = map[string]*limiter{}, map[uuid.UUID]string{}, map[string]time.Time{}
	s.lastTick, s.lastSave = time.Time{}, time.Time{}
	s.leadStart = time.Now()
	s.mu.Unlock()
	s.fmu.Lock()
	s.fetchLog = map[string]fetchRec{}
	s.fmu.Unlock()

	if err := s.loadCatalog(lctx); err != nil {
		s.log.Error("oracle: can't load feeds; not leading", "err", err)
		return
	}
	s.restore(lctx)
	s.mu.Lock()
	s.leading = true
	s.publishLocked(time.Now(), false) // restored prices are visible before the first tick
	s.mu.Unlock()
	s.log.Info("oracle: leading", "instance", s.instance, "feeds", len(s.order))

	var wg sync.WaitGroup
	spawn := func(fn func(context.Context)) {
		wg.Add(1)
		go func() { defer wg.Done(); fn(lctx) }()
	}
	spawn(s.writer)
	spawn(s.emitter)
	spawn(s.ingestWorker)
	spawn(s.publisher)
	spawn(s.inboxPoller)
	spawn(s.catalogWatcher)
	if s.decision != nil {
		for i := 0; i < 8; i++ {
			spawn(s.decideWorker)
		}
	}
	s.restartRunners(lctx)

	t := time.NewTicker(leaseRenew)
	defer t.Stop()
	failing := time.Time{}
loop:
	for {
		select {
		case <-lctx.Done():
			break loop
		case <-t.C:
			ok, err := s.claim(lctx)
			switch {
			case err != nil && lctx.Err() == nil:
				if failing.IsZero() {
					failing = time.Now()
				}
				if time.Since(failing) > leaseFor-leaseRenew {
					s.log.Warn("oracle: can't renew leadership; stepping down", "err", err)
					break loop
				}
			case !ok:
				s.log.Warn("oracle: leadership taken over; stepping down")
				break loop
			default:
				failing = time.Time{}
			}
		}
	}
	s.mu.Lock()
	s.leading = false
	s.mu.Unlock()
	s.stopRunners()
	cancel()
	wg.Wait()
}

// restartRunners (re)starts every source poller and the history backfill
// with the current feed set.
func (s *Service) restartRunners(ctx context.Context) {
	s.stopRunners()
	if !s.cfg.sourcesEnabled {
		s.log.Info("oracle: polling off (ORACLE_SOURCES_ENABLED=false); pushed prints still screen and publish")
		return
	}
	rctx, cancel := context.WithCancel(ctx)
	s.runMu.Lock()
	s.runCancel = cancel
	s.runMu.Unlock()
	s.mu.RLock()
	by := s.refsBySource()
	s.mu.RUnlock()
	for name, refs := range by {
		src, ok := s.sources[name]
		if !ok || s.cfg.disabled[name] {
			continue
		}
		lanes := map[string][]Task{}
		for _, t := range src.Tasks(refs) {
			lanes[t.Lane] = append(lanes[t.Lane], t)
			s.mu.Lock()
			for _, r := range t.Refs {
				s.refLane[r.ID] = t.Lane
			}
			s.mu.Unlock()
		}
		for lane, tasks := range lanes {
			lim := s.limiterFor(name, lane, src.Budget())
			go s.runSource(rctx, src, lane, lim, tasks)
		}
	}
	go s.runHistory(rctx)
}

func (s *Service) stopRunners() {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.runCancel != nil {
		s.runCancel()
		s.runCancel = nil
	}
}

// catalogWatcher reloads feeds when staff change them (on any instance):
// every 5 s, or at once when the inbox holds a print for a feed the leader
// hasn't loaded yet.
func (s *Service) catalogWatcher(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.reload:
		}
		sig, err := catalogSignature(ctx, s.d.Pool)
		if err != nil {
			continue
		}
		s.mu.RLock()
		same := sig == s.catalogSig
		s.mu.RUnlock()
		if same {
			continue
		}
		if err := s.loadCatalog(ctx); err != nil {
			s.log.Warn("oracle: reload feeds failed", "err", err)
			continue
		}
		s.restartRunners(ctx)
		s.emit(feedsTopic, EventFeeds, map[string]any{"changed": true})
	}
}

// inboxPoller drains pushed prints (staff prints, app pushes, dev seeds):
// rows are claimed once with SKIP LOCKED and screened like polled prints.
// A row for a feed this leader hasn't loaded yet (created a moment ago, on
// any instance) stays unclaimed and triggers a catalog reload.
func (s *Service) inboxPoller(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		for {
			n, err := s.drainInbox(ctx)
			if err != nil && ctx.Err() == nil {
				s.log.Warn("oracle: inbox", "err", err)
			}
			if n < 500 {
				break
			}
		}
		if err := s.checkInboxFeeds(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("oracle: inbox", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// loadedFeeds lists the feeds the leader has loaded.
func (s *Service) loadedFeeds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.order))
	for _, f := range s.order {
		ids = append(ids, f.id.String())
	}
	return ids
}

func (s *Service) drainInbox(ctx context.Context) (int, error) {
	ids := s.loadedFeeds()
	if len(ids) == 0 {
		return 0, nil
	}
	rows, err := s.d.Pool.Query(ctx, `UPDATE oracle_inbox SET taken_at = now() WHERE id IN (
   SELECT id FROM oracle_inbox WHERE taken_at IS NULL AND feed_id = ANY($1::uuid[]) ORDER BY id LIMIT 500 FOR UPDATE SKIP LOCKED)
 RETURNING feed_id, source, venue, kind, price::float8, coalesce(qty, 0)::float8, observed_at, coalesce(note, ''), actor_id, dedupe_key`, ids)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var batch []rawPrint
	s.mu.RLock()
	since := s.leadStart.Add(-grace)
	s.mu.RUnlock()
	for rows.Next() {
		rp := rawPrint{pushed: true}
		if err := rows.Scan(&rp.FeedID, &rp.Source, &rp.Venue, &rp.Kind, &rp.Price, &rp.Qty, &rp.At, &rp.Note, &rp.Actor, &rp.key); err != nil {
			return 0, err
		}
		rp.Live = rp.At.After(since)
		batch = append(batch, rp)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(batch) > 0 {
		s.ingest(ctx, batch)
	}
	return len(batch), nil
}

// checkInboxFeeds asks for a catalog reload when unclaimed prints wait on a
// feed the leader hasn't loaded.
func (s *Service) checkInboxFeeds(ctx context.Context) error {
	var waiting bool
	err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM oracle_inbox WHERE taken_at IS NULL AND NOT (feed_id = ANY($1::uuid[])))`,
		s.loadedFeeds()).Scan(&waiting)
	if err != nil {
		return err
	}
	if waiting {
		select {
		case s.reload <- struct{}{}:
		default:
		}
	}
	return nil
}

// ---- ingest & screening ----

func (s *Service) ingest(ctx context.Context, prints []rawPrint) {
	select {
	case s.ingestCh <- prints:
	case <-ctx.Done():
	}
}

func (s *Service) ingestWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-s.ingestCh:
			slices.SortStableFunc(batch, func(a, b rawPrint) int { return a.At.Compare(b.At) })
			for _, rp := range batch {
				s.screenOne(ctx, rp)
			}
		}
	}
}

// screenOne runs one print through premium normalisation and stat-v1 and
// records it. With a decision model, a live print stays pending (not in the
// target) until the model answers or fails open.
func (s *Service) screenOne(ctx context.Context, rp rawPrint) {
	now := time.Now()
	s.mu.Lock()
	f := s.feeds[rp.FeedID]
	dedupe := rp.dedupe()
	if f == nil || f.kind != FeedSingle || !validKind(rp.Kind) || rp.Price <= 0 || math.IsNaN(rp.Price) || math.IsInf(rp.Price, 0) {
		s.mu.Unlock()
		if rp.pushed {
			s.log.Warn("oracle: pushed print dropped: unknown feed, a basket, or an invalid print", "feed_id", rp.FeedID, "source", rp.Source, "venue", rp.Venue)
		}
		return
	}
	if _, dup := f.seen[dedupe]; dup {
		s.mu.Unlock()
		return
	}
	if age := now.Sub(rp.At); age > f.p.Lookback {
		s.mu.Unlock()
		if rp.pushed { // Push and the admin route check this, so only a long wait in the inbox gets here
			s.log.Warn("oracle: pushed print dropped: older than the feed's lookback", "feed", f.key, "source", rp.Source,
				"venue", rp.Venue, "observed_at", rp.At, "lookback", f.p.Lookback)
		}
		return
	}
	prem, learned := f.premiumFor(rp.Venue, s.cfg.learnPremiums)
	px := rp.Price / (1 + prem)
	ft := measure(f, rp.Venue, rp.Kind, px, rp.Qty, rp.At, now, f.price)
	sc, reasons := score(ft, rp.Kind, f.p.StaleAfter)
	if len(reasons) == 0 {
		reasons = []string{"consistent with recent prints"}
	}
	if prem != 0 {
		how := "fixed"
		if learned {
			how = "learned"
		}
		reasons = append([]string{fmt.Sprintf("venue premium %+.1f%% removed (%s)", 100*prem, how)}, reasons...)
	}
	if rp.Note != "" {
		reasons = append([]string{truncate(rp.Note, 120)}, reasons...)
	}
	verdict, factor := verdictFor(sc)
	vw := 1.0
	if ref := f.refFor(rp.Venue); ref != nil && ref.Weight > 0 {
		vw = ref.Weight
	}
	o := Observation{
		ID: uuid.New(), FeedID: f.id, FeedKey: f.key, Source: rp.Source, Venue: rp.Venue, Kind: rp.Kind,
		Price: fmtPrice(px, f.decimals+2), RawPrice: fmtRaw(rp.Price), Premium: math.Round(prem*1e4) / 1e4,
		ObservedAt: rp.At.UTC(), ReceivedAt: now.UTC(), Verdict: verdict, Score: round3(sc), Reasons: reasons, Model: statModel,
	}
	if rp.Qty > 0 {
		q := rp.Qty
		o.Qty = &q
	}
	useModel := false
	if hd, ok := s.decision.(*httpDecision); ok {
		useModel = rp.Live && hd.usable(now)
	} else if s.decision != nil {
		useModel = rp.Live
	}
	r := &rec{obs: o, dedupe: dedupe, px: px, raw: rp.Price, qty: rp.Qty, observed: rp.At,
		baseW: kindWeight(rp.Kind, rp.Qty) * vw * factor, pending: useModel}
	f.learnPremium(rp.Venue, rp.Price, rp.At)
	f.add(r)
	var state map[string]string
	if useModel {
		state = decisionState(f, rp.Venue, rp.Kind, ft)
	}
	s.mu.Unlock()

	job := decideJob{r: r, f: f, state: state, stat: sc, reasons: reasons, vw: vw, actor: rp.Actor}
	if !useModel {
		s.finalize(job, nil)
		return
	}
	select {
	case s.decideCh <- job:
	default:
		s.finalize(job, nil) // queue full: fail open
	}
}

func (s *Service) decideWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.decideCh:
			dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			a, err := s.decision.Decide(dctx, job.state)
			cancel()
			if err != nil {
				s.finalize(job, nil)
			} else {
				s.finalize(job, &a)
			}
		}
	}
}

// finalize applies the decision (stat-v1, or stat-v1 + model), makes the
// record count, persists and emits it.
func (s *Service) finalize(job decideJob, a *Decision) {
	r, f := job.r, job.f
	now := time.Now()
	sc := job.stat
	verdict, factor := verdictFor(sc)
	reasons := job.reasons
	model := statModel
	if a != nil {
		var why string
		sc, verdict, factor, why = combineDecision(job.stat, *a)
		reasons = append(append([]string(nil), reasons...), why)
		model = statModel + "+" + a.Model
	}
	s.mu.Lock()
	age := max(now.Sub(r.observed), 0)
	r.baseW = kindWeight(r.obs.Kind, r.qty) * job.vw * factor
	r.obs.Verdict = verdict
	r.obs.Score = round3(sc)
	r.obs.Reasons = reasons
	r.obs.Model = model
	r.obs.Weight = math.Round(r.baseW*math.Exp(-math.Ln2*age.Hours()/f.p.HalfLife.Hours())*1e4) / 1e4
	f.resolve(r)
	o := r.obs
	baskets := make([]uuid.UUID, 0, len(f.baskets))
	for _, b := range f.baskets {
		baskets = append(baskets, b.id)
	}
	s.mu.Unlock()

	s.persistObs(r, job.actor)
	s.emit(feedTopic(f.id), EventObservation, o)
	s.emit(observationsTopic, EventObservation, o)
	for _, b := range baskets {
		s.emit(feedTopic(b), EventObservation, o)
	}
}

// ---- publishing ----

func (s *Service) publisher(ctx context.Context) {
	t := time.NewTicker(s.cfg.publishEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.mu.Lock()
			pts := s.publishLocked(now, true)
			hooks := s.onPublish
			s.mu.Unlock()
			for _, fn := range hooks {
				for _, p := range pts {
					fn(p)
				}
			}
		}
	}
}

func statusRank(st string) int {
	switch st {
	case StatusOff:
		return 3
	case StatusStale:
		return 2
	case StatusSeed:
		return 1
	}
	return 0
}

// publishLocked recomputes targets, slews prices, persists what changed and
// emits it. step=false only refreshes the snapshot (after a restore).
func (s *Service) publishLocked(now time.Time, step bool) []PricePoint {
	dt := time.Duration(0)
	if step && !s.lastTick.IsZero() {
		dt = min(now.Sub(s.lastTick), time.Minute)
	}
	if step {
		s.lastTick = now
	}
	for _, f := range s.order {
		if f.kind == FeedSingle && f.enabled {
			f.recompute(now)
			f.slew(dt)
		}
	}
	save := now.Sub(s.lastSave) >= 30*time.Second
	var changed []priceDTO
	pts := make([]PricePoint, 0, len(s.order))
	for _, f := range s.order {
		pp := PricePoint{FeedID: f.id, Key: f.key, At: now, Decimals: f.decimals, Status: f.status(now)}
		if f.kind == FeedSingle {
			if f.price > 0 {
				pp.Price, pp.Target = roundTo(f.price, f.decimals), roundTo(f.target, f.decimals)
			}
		} else if f.enabled {
			pp.Status = StatusLive
			px, tgt, complete := 0.0, 0.0, len(f.parts) > 0
			for _, pt := range f.parts {
				pf := s.prices[pt.f.id]
				if pf.Price <= 0 {
					complete = false
				}
				px += pt.qty * pt.f.price
				tgt += pt.qty * pt.f.target
				if statusRank(pf.Status) > statusRank(pp.Status) {
					pp.Status = pf.Status
				}
			}
			if pp.Status == StatusOff {
				pp.Status = StatusStale // a disabled part freezes the basket
			}
			if complete {
				pp.Price, pp.Target = roundTo(px, f.decimals), roundTo(tgt, f.decimals)
			} else if pp.Status == StatusLive {
				pp.Status = StatusSeed
			}
		}
		s.prices[f.id] = pp
		pts = append(pts, pp)
		last, seen := s.lastPub[f.id]
		if !seen || last.Price != pp.Price || last.Target != pp.Target || last.Status != pp.Status {
			s.lastPub[f.id] = pp
			s.persistState(pp, f)
			changed = append(changed, toPriceDTO(pp))
			s.emit(feedTopic(f.id), EventPrice, toPriceDTO(pp))
		}
		// Price history: every 30 s on change, at least every 5 min.
		if pp.Price > 0 && pp.Status != StatusOff {
			ls, ok := s.lastSaved[f.id]
			if !ok || (now.Sub(ls.At) >= 30*time.Second && ls.Price != pp.Price) || now.Sub(ls.At) >= 5*time.Minute {
				s.lastSaved[f.id] = pp
				s.persistPrice(pp)
				if pp.Status == StatusLive || pp.Status == StatusStale {
					s.persistDailyClose(pp)
				}
			}
		}
	}
	if len(changed) > 0 {
		s.emit(feedsTopic, EventPrices, map[string]any{"items": changed})
	}
	if save && step {
		s.lastSave = now
		for _, f := range s.order {
			for venue, ps := range f.premiums {
				v, _ := ps.value()
				s.persistPremium(f.id, venue, ps, v)
			}
		}
	}
	return pts
}

// Current returns a feed's latest published price by key: from memory on the
// leading instance, from the database elsewhere.
func (s *Service) Current(ctx context.Context, key string) (PricePoint, bool, error) {
	s.mu.RLock()
	if s.leading {
		if f := s.byKey[key]; f != nil {
			pp, ok := s.prices[f.id]
			s.mu.RUnlock()
			return pp, ok, nil
		}
	}
	s.mu.RUnlock()
	var pp PricePoint
	var price, target *float64
	err := s.d.Pool.QueryRow(ctx, `SELECT f.id, f.key, f.decimals, st.price::float8, st.target::float8, coalesce(st.status, 'seed'), coalesce(st.updated_at, f.created_at)
 FROM oracle_feeds f LEFT JOIN oracle_feed_state st ON st.feed_id = f.id WHERE f.key = $1`, key).
		Scan(&pp.FeedID, &pp.Key, &pp.Decimals, &price, &target, &pp.Status, &pp.At)
	if db.IsNoRows(err) {
		return PricePoint{}, false, nil
	}
	if err != nil {
		return PricePoint{}, false, err
	}
	if price != nil {
		pp.Price = *price
	}
	if target != nil {
		pp.Target = *target
	}
	return pp, true, nil
}

// ---- formatting ----

func roundTo(x float64, decimals int32) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(x*p) / p
}

func round3(x float64) float64 { return math.Round(x*1000) / 1000 }

// fmtPrice formats a price as a decimal string with exactly decimals places.
func fmtPrice(x float64, decimals int32) string {
	return strconv.FormatFloat(roundTo(x, decimals), 'f', int(decimals), 64)
}

// fmtRaw formats a reported price as the shortest decimal that round-trips.
func fmtRaw(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }
