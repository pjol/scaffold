package copytrade

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/exchange"
	"scaffoldapp/backend/internal/exchange/engine"
)

// copyState is one open copy in memory. Every change to rec happens under mu
// and is written through (persist).
type copyState struct {
	mu          sync.Mutex
	rec         copyRec
	leaderAcct  string      // the leader's main account
	orderTimes  []time.Time // copied orders in the last minute
	failures    int         // consecutive rejected orders
	retries     map[string][]time.Time
	persisted   decimal.Decimal // peak last written
	persistedEq decimal.Decimal // equity last written
}

// runtime is the pipeline while this instance runs the exchange engine.
type runtime struct {
	s  *Service
	ex *exchange.Service

	mu       sync.RWMutex
	open     map[uuid.UUID]*copyState
	byLeader map[uuid.UUID]map[uuid.UUID]*copyState
	leaders  atomic.Pointer[map[string]uuid.UUID] // leader main account → leader: the tap's filter (lock-free)
	startMu  sync.Mutex                           // serialises starts (limits are counted, then inserted)

	ch     chan exchange.FillEvent
	drops  atomic.Int64
	shards []chan job

	pxMu   sync.Mutex
	lastPx map[string]decimal.Decimal // leader account|symbol → the leader's last fill price

	framesMu sync.Mutex
	frames   map[uuid.UUID]bool
}

// ordersPerMinute caps copied orders per copy.
const ordersPerMinute = 20

// run is the pipeline for one engine session on this instance.
func (s *Service) run(ctx context.Context, ex *exchange.Service) {
	rt := &runtime{s: s, ex: ex, open: map[uuid.UUID]*copyState{}, byLeader: map[uuid.UUID]map[uuid.UUID]*copyState{},
		ch: make(chan exchange.FillEvent, 10_000), lastPx: map[string]decimal.Decimal{}, frames: map[uuid.UUID]bool{}}
	empty := map[string]uuid.UUID{}
	rt.leaders.Store(&empty)
	for i := 0; i < s.workers; i++ {
		rt.shards = append(rt.shards, make(chan job, 1024))
	}
	for {
		err := rt.load(ctx)
		if err == nil {
			break
		}
		slog.ErrorContext(ctx, "copytrade: loading open copies failed; retrying", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
	s.rt.Store(rt)
	defer s.rt.CompareAndSwap(rt, nil)
	go rt.dispatch(ctx)
	for _, ch := range rt.shards {
		go rt.worker(ctx, ch)
	}
	go rt.publishFrames(ctx)
	go rt.loops(ctx)
	slog.InfoContext(ctx, "copytrade: pipeline running", "open_copies", len(rt.open), "paused", s.paused)
	<-ctx.Done()
}

func (rt *runtime) load(ctx context.Context) error {
	recs, err := listCopies(ctx, rt.s.d.Pool, `WHERE status <> 'stopped' ORDER BY created_at`)
	if err != nil {
		return err
	}
	for _, r := range recs {
		acct, _, err := rt.ex.MainAccount(r.LeaderID)
		if err != nil {
			return err
		}
		rt.index(&copyState{rec: r, leaderAcct: acct, retries: map[string][]time.Time{}, persisted: r.PeakEquity, persistedEq: r.Equity})
	}
	return nil
}

// ---- in-memory index ----

func (rt *runtime) index(cs *copyState) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	id, leader := cs.rec.ID, cs.rec.LeaderID
	rt.open[id] = cs
	if rt.byLeader[leader] == nil {
		rt.byLeader[leader] = map[uuid.UUID]*copyState{}
	}
	rt.byLeader[leader][id] = cs
	rt.rebuildTapLocked()
}

func (rt *runtime) unindex(id, leader uuid.UUID) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.open, id)
	if m := rt.byLeader[leader]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(rt.byLeader, leader)
		}
	}
	rt.rebuildTapLocked()
}

// rebuildTapLocked recomputes the leaders whose fills the tap forwards
// (every leader with an open copy; workers skip copies that aren't active).
func (rt *runtime) rebuildTapLocked() {
	next := make(map[string]uuid.UUID, len(rt.byLeader))
	for leader, copies := range rt.byLeader {
		for _, cs := range copies {
			if cs.leaderAcct != "" {
				next[cs.leaderAcct] = leader
			}
			break
		}
	}
	rt.leaders.Store(&next)
}

func (rt *runtime) state(id uuid.UUID) (*copyState, bool) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	cs, ok := rt.open[id]
	return cs, ok
}

func (rt *runtime) copiesOf(leader uuid.UUID) []*copyState {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]*copyState, 0, len(rt.byLeader[leader]))
	for _, cs := range rt.byLeader[leader] {
		out = append(out, cs)
	}
	return out
}

func (rt *runtime) openStates() []*copyState {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]*copyState, 0, len(rt.open))
	for _, cs := range rt.open {
		out = append(out, cs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rec.ID.String() < out[j].rec.ID.String() })
	return out
}

func (rt *runtime) leadersWithCopies() []uuid.UUID {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]uuid.UUID, 0, len(rt.byLeader))
	for l := range rt.byLeader {
		out = append(out, l)
	}
	return out
}

// ---- the fill tap, dispatcher and workers ----

// tap forwards a leader's fill to the dispatcher: one lock-free map read and
// a non-blocking send (reconcile repairs anything dropped).
func (rt *runtime) tap(f exchange.FillEvent) {
	if f.Kind != engine.KindUser {
		return
	}
	if _, ok := (*rt.leaders.Load())[f.AccountID]; !ok {
		return
	}
	select {
	case rt.ch <- f:
	default:
		rt.drops.Add(1)
	}
}

type jobKind int

const (
	jobFill jobKind = iota // mirror one market after leader fills
	jobSync                // bring every engaged market to target (start with open positions, resume)
)

type job struct {
	kind      jobKind
	cs        *copyState
	symbol    string
	leaderSz  decimal.Decimal
	leaderLev int
	leaderAV  decimal.Decimal
	refPx     decimal.Decimal
	mkt       marketParams
	fillID    string
	fillAt    time.Time
}

type marketParams struct {
	ok          bool
	mark        decimal.Decimal
	szDec       int
	minNotional decimal.Decimal
	maxLev      int
	status      string
}

func (rt *runtime) market(symbol string) marketParams {
	mv, ok, err := rt.ex.Market(symbol)
	if err != nil || !ok {
		return marketParams{}
	}
	return marketParams{ok: true, mark: dec(mv.Stats.MarkPx), szDec: mv.SzDecimals, minNotional: dec(mv.MinNotional),
		maxLev: mv.MaxLeverage, status: mv.Status}
}

func shardOf(id uuid.UUID, n int) int {
	h := fnv.New32a()
	_, _ = h.Write(id[:])
	return int(h.Sum32() % uint32(n))
}

type batch struct {
	leaderAcct, symbol string
	leader             uuid.UUID
	firstAt            time.Time
	lastPx             decimal.Decimal
	lastFill           string
}

// dispatch coalesces each (leader, market) for s.coalesce, then fans out.
func (rt *runtime) dispatch(ctx context.Context) {
	pending := map[string]*batch{}
	flush := make(chan string, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-rt.ch:
			leader, ok := (*rt.leaders.Load())[ev.AccountID]
			if !ok {
				continue
			}
			k := ev.AccountID + "|" + ev.Symbol
			b := pending[k]
			if b == nil {
				b = &batch{leaderAcct: ev.AccountID, symbol: ev.Symbol, leader: leader, firstAt: ev.At}
				pending[k] = b
				time.AfterFunc(rt.s.coalesce, func() {
					select {
					case flush <- k:
					case <-ctx.Done():
					}
				})
			}
			b.lastPx, b.lastFill = ev.Px, ev.FillID
		case k := <-flush:
			b := pending[k]
			delete(pending, k)
			if b != nil {
				rt.fanout(ctx, b)
			}
		}
	}
}

// fanout reads the leader once and hands each of its copies to its shard, in
// a shuffled order so no follower is always first.
func (rt *runtime) fanout(ctx context.Context, b *batch) {
	st, err := rt.ex.AccountState(b.leaderAcct)
	if err != nil {
		return
	}
	lp := st.Positions[b.symbol]
	mkt := rt.market(b.symbol)
	rt.pxMu.Lock()
	rt.lastPx[b.leaderAcct+"|"+b.symbol] = b.lastPx
	rt.pxMu.Unlock()
	copies := rt.copiesOf(b.leader)
	rand.Shuffle(len(copies), func(i, j int) { copies[i], copies[j] = copies[j], copies[i] })
	for _, cs := range copies {
		j := job{kind: jobFill, cs: cs, symbol: b.symbol, leaderSz: lp.Sz, leaderLev: lp.Leverage, leaderAV: st.AccountValue,
			refPx: b.lastPx, mkt: mkt, fillID: b.lastFill, fillAt: b.firstAt}
		select {
		case rt.shards[shardOf(cs.rec.ID, len(rt.shards))] <- j:
		case <-ctx.Done():
			return
		}
	}
}

func (rt *runtime) leaderPx(leaderAcct, symbol string) decimal.Decimal {
	rt.pxMu.Lock()
	defer rt.pxMu.Unlock()
	return rt.lastPx[leaderAcct+"|"+symbol]
}

func (rt *runtime) worker(ctx context.Context, ch chan job) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-ch:
			rt.runJob(ctx, j)
		}
	}
}

func (rt *runtime) runJob(ctx context.Context, j job) {
	cs := j.cs
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.rec.Status != StatusActive || rt.s.paused {
		return
	}
	var evs []eventRec
	switch j.kind {
	case jobFill:
		evs = rt.mirror(ctx, cs, mirrorIn{symbol: j.symbol, leaderSz: j.leaderSz, leaderLev: j.leaderLev, leaderAV: j.leaderAV,
			refPx: j.refPx, mkt: j.mkt, fillID: j.fillID, fillAt: j.fillAt, kind: EventOrder})
	case jobSync:
		evs = rt.syncAll(ctx, cs, EventOrder, false)
	}
	rt.record(ctx, cs, evs)
}

func (rt *runtime) enqueueSync(cs *copyState) {
	select {
	case rt.shards[shardOf(cs.rec.ID, len(rt.shards))] <- job{kind: jobSync, cs: cs}:
	default: // reconcile catches up within a minute
	}
}

// ---- copy frames: the follower's Copy, at most once a second ----

func (rt *runtime) markFrame(id uuid.UUID) {
	rt.framesMu.Lock()
	rt.frames[id] = true
	rt.framesMu.Unlock()
}

func (rt *runtime) publishFrames(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rt.framesMu.Lock()
		due := rt.frames
		rt.frames = map[uuid.UUID]bool{}
		rt.framesMu.Unlock()
		for id := range due {
			cs, ok := rt.state(id)
			if !ok {
				continue
			}
			cs.mu.Lock()
			rec := cs.rec.clone()
			cs.mu.Unlock()
			v, err := rt.view(ctx, rec, nil)
			if err != nil {
				continue
			}
			rt.s.publish(ctx, rec.FollowerID, EventCopyFrame, v)
		}
	}
}
