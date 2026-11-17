package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// What started a sync (catalog_sync_runs.trigger).
const (
	TriggerSchedule = "schedule" // the nightly job
	TriggerStaff    = "staff"    // Admin → Catalog → Sync now (audited)
	TriggerApp      = "app"      // app code: Service.SyncNow
	TriggerSeed     = "seed"     // cmd/seed's development items
)

// Run statuses.
const (
	RunRunning = "running"
	RunOK      = "ok"
	RunFailed  = "failed"
	RunRefused = "refused" // the safety valve stopped it; nothing changed
)

// SyncRun is one sync (Admin → Catalog). A refused run's counts are what it
// would have done.
type SyncRun struct {
	ID         uuid.UUID  `json:"id"`
	Trigger    string     `json:"trigger"`
	Source     string     `json:"source"`
	Forced     bool       `json:"forced"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Fetched    int        `json:"fetched"`   // items the source sent
	Inserted   int        `json:"inserted"`  // new keys
	Updated    int        `json:"updated"`   // content changed
	Retired    int        `json:"retired"`   // no longer listed
	Restored   int        `json:"restored"`  // listed again after the source retired them
	Unchanged  int        `json:"unchanged"` //
	Skipped    int        `json:"skipped"`   // invalid or repeated keys
	Error      string     `json:"error"`
	Notes      []string   `json:"notes"` // the first few skip reasons
}

const (
	// minGuarded: catalogs with fewer active items than this aren't guarded by
	// CATALOG_MAX_RETIRE_PERCENT (an empty pull is refused at any size).
	minGuarded = 100
	maxNotes   = 5
	staleAfter = 2 * time.Minute // a running sync with no heartbeat this long died with its instance
)

var (
	errSyncRunning = httpx.NewError(409, "sync_running", "A sync is already running. Wait for it to finish.")
	errNoSource    = httpx.NewError(409, "no_source", "No catalog source is set. Set CATALOG_SOURCE_URL, or register a source and set CATALOG_SOURCE.")
	errTooMany     = errors.New("too many items")
)

// plan is what one sync changes.
type plan struct {
	insert    []rec
	update    []rec    // rows to rewrite: content changed, or restored
	retire    []string // active keys the source no longer lists
	restore   []string // keys the source had retired and lists again
	updated   int      // of update, those whose content changed
	unchanged int
	active    int // active items before the sync
}

// diff compares the stored catalog with tonight's pull (validated, one rec
// per key). Pure:
//
//   - a new key is inserted;
//   - a known key's content follows the source, except that aliases only
//     grow and a renamed item keeps its old name as an alias, so saved names
//     keep resolving;
//   - an item the source retired and lists again is restored; one staff
//     retired stays retired (its content still follows the source);
//   - an active item the source no longer lists is retired. Nothing is
//     deleted.
func diff(cur *index, pulled []rec, now time.Time) plan {
	p := plan{active: cur.nAct}
	seen := make([]bool, len(cur.recs))
	for _, n := range pulled {
		i, ok := cur.byKey[n.key]
		if !ok {
			n.firstSeen, n.updatedAt = now, now
			p.insert = append(p.insert, n)
			continue
		}
		seen[i] = true
		old := cur.recs[i]
		m := n
		prior := old.aliases
		if NameKey(old.name) != NameKey(n.name) {
			prior = append(slices.Clone(old.aliases), old.name)
		}
		m.aliases = mergeAliases(prior, n.name, n.aliases)
		m.firstSeen, m.updatedAt = old.firstSeen, old.updatedAt
		m.retiredBy, m.retiredAt = old.retiredBy, old.retiredAt
		restored := old.retiredBy == RetiredBySource
		if restored {
			m.retiredBy, m.retiredAt = "", time.Time{}
			p.restore = append(p.restore, n.key)
		}
		changed := !sameContent(old, m)
		switch {
		case changed:
			p.updated++
			m.updatedAt = now
			p.update = append(p.update, m)
		case restored:
			m.updatedAt = now
			p.update = append(p.update, m)
		default:
			p.unchanged++
		}
	}
	for i, r := range cur.recs {
		if !seen[i] && r.active() {
			p.retire = append(p.retire, r.key)
		}
	}
	return p
}

func sameContent(a, b rec) bool {
	return a.name == b.name && a.category == b.category && a.subtitle == b.subtitle && a.imageURL == b.imageURL &&
		a.attrs == b.attrs && a.price == b.price && a.currency == b.currency && slices.Equal(a.aliases, b.aliases)
}

// refusal is why the safety valve stops a plan, or "" when it may go ahead:
// a pull with no usable items never retires anything, and a catalog of
// minGuarded or more active items never loses more than maxPct of them in
// one sync. valid is the pull's count of usable items.
func (p plan) refusal(valid, maxPct int) string {
	if len(p.retire) == 0 {
		return ""
	}
	if valid == 0 {
		return fmt.Sprintf("The source sent no usable items, which would retire all %d active items. Nothing changed.", len(p.retire))
	}
	if p.active < minGuarded {
		return ""
	}
	if pct := float64(len(p.retire)) * 100 / float64(p.active); pct > float64(maxPct) {
		return fmt.Sprintf("This sync would retire %d of %d active items (%s%%), more than CATALOG_MAX_RETIRE_PERCENT (%d%%). Nothing changed.",
			len(p.retire), p.active, strings.TrimSuffix(strconv.FormatFloat(pct, 'f', 1, 64), ".0"), maxPct)
	}
	return ""
}

// pulled is one source's items, validated, the first of each key kept.
type pulled struct {
	recs    []rec
	fetched int
	skipped int
	notes   []string
}

func (s *Service) pull(ctx context.Context, src Source) (pulled, error) {
	var (
		mu   sync.Mutex
		out  pulled
		keys = map[string]struct{}{}
	)
	skip := func(msg string) {
		out.skipped++
		if len(out.notes) < maxNotes {
			out.notes = append(out.notes, msg)
		}
	}
	ctx = withSkipReporter(ctx, func(err error) {
		mu.Lock()
		defer mu.Unlock()
		out.fetched++
		skip(err.Error())
	})
	err := src.Fetch(ctx, func(it Item) error {
		mu.Lock()
		defer mu.Unlock()
		out.fetched++
		if out.fetched > s.cfg.maxItems {
			return errTooMany
		}
		r, err := normalize(it, s.cfg.allowHTTP)
		if err != nil {
			skip(err.Error())
			return ctx.Err()
		}
		if _, dup := keys[r.key]; dup {
			skip(r.key + ": the key appears more than once; the first is kept")
			return ctx.Err()
		}
		keys[r.key] = struct{}{}
		out.recs = append(out.recs, r)
		return ctx.Err()
	})
	if errors.Is(err, errTooMany) {
		return out, fmt.Errorf("the source sent more than %d items (CATALOG_MAX_ITEMS)", s.cfg.maxItems)
	}
	return out, err
}

// ---- runs ----

// SyncNow starts a sync from the configured source in the background (one at
// a time across instances) and returns its run: call it when the app learns
// the source changed. errors: 409 sync_running, 409 no_source.
func (s *Service) SyncNow(ctx context.Context) (SyncRun, error) {
	if s.src == nil {
		return SyncRun{}, errNoSource
	}
	run, err := s.startRun(ctx, s.d.Pool, TriggerApp, s.src.Name(), false)
	if err != nil {
		return run, err
	}
	s.launch(ctx, run, s.src, false)
	return run, nil
}

// startRun claims the one running slot (catalog_sync_runs_one_running) with a
// new run row, first failing a running row whose instance stopped.
func (s *Service) startRun(ctx context.Context, q db.Querier, trigger, source string, force bool) (SyncRun, error) {
	if _, err := q.Exec(ctx, `UPDATE catalog_sync_runs SET status = 'failed', finished_at = now(),
  error = 'The instance running this sync stopped.'
 WHERE status = 'running' AND heartbeat_at < now() - make_interval(secs => $1)`, staleAfter.Seconds()); err != nil {
		return SyncRun{}, err
	}
	run, err := scanRun(q.QueryRow(ctx, `INSERT INTO catalog_sync_runs (trigger, source, forced) VALUES ($1, $2, $3) RETURNING `+runCols,
		trigger, source, force))
	if db.IsUniqueViolation(err) {
		return run, errSyncRunning
	}
	return run, err
}

// launch runs a claimed sync in the background (bg.Go: shutdown waits for it,
// and it stops with the server).
func (s *Service) launch(ctx context.Context, run SyncRun, src Source, force bool) {
	bg.Go(ctx, "catalog.sync", func(ctx context.Context) error {
		_, err := s.execute(ctx, run, src, force)
		return err
	})
}

// execute runs a claimed sync to the end and records how it went.
func (s *Service) execute(ctx context.Context, run SyncRun, src Source, force bool) (SyncRun, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.sourceTimeout+10*time.Minute)
	defer cancel()
	stop := context.AfterFunc(s.lifetime(), cancel)
	defer stop()
	hb, hbStop := context.WithCancel(ctx)
	defer hbStop()
	go s.heartbeat(hb, run.ID)

	started := time.Now()
	run, err := s.sync(ctx, run, src, force)
	if err != nil || run.Status != RunOK {
		if err != nil {
			run.Status, run.Error = RunFailed, syncErrorText(ctx, err)
		}
		fctx, fcancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer fcancel()
		if ferr := s.finishRun(fctx, s.d.Pool, run); ferr != nil {
			s.log.Error("catalog: can't record the sync's end", "run", run.ID, "err", ferr)
		}
	}
	s.log.Info("catalog: sync", "run", run.ID, "trigger", run.Trigger, "status", run.Status, "fetched", run.Fetched,
		"inserted", run.Inserted, "updated", run.Updated, "retired", run.Retired, "restored", run.Restored,
		"skipped", run.Skipped, "took", time.Since(started).Round(time.Millisecond), "err", run.Error)
	return run, err
}

func syncErrorText(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "The sync took too long (CATALOG_SOURCE_TIMEOUT) and was stopped. Nothing changed."
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		return "The server stopped during the sync. Nothing changed."
	}
	msg := err.Error()
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return msg + ". Nothing changed."
}

func (s *Service) heartbeat(ctx context.Context, id uuid.UUID) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.d.Pool.Exec(ctx, `UPDATE catalog_sync_runs SET heartbeat_at = now() WHERE id = $1 AND status = 'running'`, id); err != nil && ctx.Err() == nil {
				s.log.Warn("catalog: sync heartbeat failed", "run", id, "err", err)
			}
		}
	}
}

// sync pulls, diffs, checks the valve and applies. The returned run carries
// the counts; an OK run is recorded inside the apply transaction.
func (s *Service) sync(ctx context.Context, run SyncRun, src Source, force bool) (SyncRun, error) {
	if err := s.refresh(ctx); err != nil {
		return run, fmt.Errorf("loading the stored catalog: %w", err)
	}
	cur := s.idx.Load()
	got, err := s.pull(ctx, src)
	run.Fetched, run.Skipped, run.Notes = got.fetched, got.skipped, got.notes
	if err != nil {
		return run, err
	}
	p := diff(cur, got.recs, time.Now().UTC())
	run.Inserted, run.Updated, run.Retired, run.Restored, run.Unchanged = len(p.insert), p.updated, len(p.retire), len(p.restore), p.unchanged
	if why := p.refusal(len(got.recs), s.cfg.maxRetirePct); why != "" && !force {
		run.Status, run.Error = RunRefused, why
		return run, nil
	}
	run.Status = RunOK
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		retired, restored, err := applyPlan(ctx, tx, p)
		if err != nil {
			return err
		}
		run.Retired, run.Restored = retired, restored // a staff change during the sync can make these smaller
		if len(p.insert)+p.updated+retired+restored > 0 {
			if err := bumpVersion(ctx, tx); err != nil {
				return err
			}
		}
		return s.finishRun(ctx, tx, run)
	})
	if err != nil {
		run.Status = RunFailed
		return run, err
	}
	if err := s.refresh(ctx); err != nil {
		s.log.Warn("catalog: reload after sync failed", "err", err)
	}
	return run, nil
}

// nightly starts the scheduled sync once per UTC day, at or after
// CATALOG_SYNC_HOUR (a server that was down at that hour catches up the same
// day). jobs.Every runs it on one instance at a time; the sync itself runs in
// the background.
func (s *Service) nightly(ctx context.Context) error {
	if s.src == nil {
		return nil
	}
	now := time.Now().UTC()
	due := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.syncHour, 0, 0, 0, time.UTC)
	if now.Before(due) {
		return nil
	}
	var ran bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_sync_runs WHERE trigger = 'schedule' AND started_at >= $1)`, due).Scan(&ran); err != nil || ran {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM catalog_sync_runs WHERE started_at < now() - interval '400 days' AND status <> 'running'`); err != nil {
		return err
	}
	run, err := s.startRun(ctx, s.d.Pool, TriggerSchedule, s.src.Name(), false)
	if errors.Is(err, errSyncRunning) {
		return nil // a staff sync is running; the next tick tries again
	}
	if err != nil {
		return err
	}
	s.launch(ctx, run, s.src, false)
	return nil
}
