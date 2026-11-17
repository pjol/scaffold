# Reputation

## What it does

- Each person gets a transparent 0–100 score (rep-v1) from the last 90 days of a series the app supplies. The score has five parts: performance (Sharpe-like, shrunk when there are few days, 30/90-day blend), risk (max drawdown of the time-weighted index, minus 10 per incident), consistency (positive weeks among active weeks of the last 13), community (optional) and tenure. A part without data gives its weight to the rest.
- Returns are time-weighted: a day's return is (close − open − flows) / max(open, floor), so money put in never offsets a loss.
- A score shows only once every requirement is met (days since first activity, active days, activity, volume). `/reputation` shows progress on each requirement, every part's weight, points and raw inputs, the 90-day stats and the curve. Tiers: building < 40 ≤ steady < 60 ≤ strong < 75 ≤ elite.
- Anti-gaming: wash pairs and vote rings (when the app supplies their data) and any module's `reputation.Flag` put a person under review: score hidden, off the leaderboard. Staff clear or confirm the flag in Admin → Reputation (audited); a confirmed flag keeps the score hidden for 90 days.
- Other people see only a person's score, tier and status (the ring on profiles and lists); the breakdown is for the person and staff. With the leaderboard feature installed, a "Reputation" board ranks rated people.

## Data

| Table | Purpose |
|---|---|
| `reputation_snapshots` | each person's cumulative counters from the `Source` (value, net flows, activity, volume, incidents, exposure) every `REPUTATION_SNAPSHOT_INTERVAL`; pruned after 14 days |
| `reputation_days` | one row per person per UTC day: open, close, flows, active, activity, volume, incidents; `source` = `snapshots` (rolled up) or `pushed` (`RecordDay`, never overwritten); pruned after 400 days. The day's return is computed when scoring, so a floor change applies to every day |
| `reputation_scores` | per person: status, score, tier, components, requirements, stats (JSON), first activity, `computed_at` |
| `reputation_flags` | integrity flags: kind, counterparty, metrics, status (open / cleared / confirmed), note, decision; at most one open flag per person, kind and counterparty |

Jobs (each runs on one instance at a time): `reputation.snapshot` runs every interval. It samples every `Source`, rolls the sampled people up into day rows and rescores everyone whose day rows changed. A day's counters are its close snapshot minus its open snapshot, so no event history is needed, and a day without snapshots gets no row. `reputation.daily` rescores everyone (ages and the window move), runs the detectors and prunes old rows.

## API

All under `/v1`, signed in.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/reputation/me` | user | the caller's full `Reputation` (computed on the first call) |
| GET | `/reputation/people/{id}` | user | `{user_id, score, status, tier}` |
| GET | `/reputation/people?ids=a,b` | user | up to 100 summaries, for lists |
| GET | `/admin/reputation/flags?status&cursor&limit` | support | flags: open ones oldest first, decided ones newest first (keyset) |
| POST | `/admin/reputation/flags/{id}/decision` | support, audited | `{decision: "clear" \| "confirm", note}` → the flag; 409 `flag_decided` |
| GET | `/admin/reputation/people/{id}` | support | anyone's full `Reputation` |

`Reputation` = the summary plus `version`, `computed_at`, `components[{key, label, weight, value, points, scored, detail, metrics}]`, `requirements[{key, label, met, progress}]` and `stats{return_30d_pct, return_90d_pct, gain_30d, max_drawdown_90d_pct, volume_30d, activity_30d, active_days_90d, incidents_90d, equity[{day, index}]}`. No realtime: clients refetch every 5 minutes. Agent tool: `reputation_get` (read-only; the caller's own reputation, or another person's summary).

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/reputation` (home tile, account menu) | ring and status, "How the score is made" (each part's points, value × weight, raw inputs, total), requirements, 90-day stats and curve |
| Web | `/admin/reputation` (admin nav) | flags by status (`?status`); a row opens a sheet with what was detected, the person's full reputation, and Clear / Confirm with a note |
| Mobile | `/reputation` (home entry) | the same as web's `/reputation` |
| Both | `ReputationRing`, `ReputationBadge` (`features/reputation/index.ts` on web and mobile) | the ring for profiles and lists; dashed with "–" when unrated or under review |

Every view has skeleton, empty and error states.

## Config

| Variable | Default | |
|---|---|---|
| `REPUTATION_SNAPSHOT_INTERVAL` | `15m` (dev `1m`) | sampling and rescoring cadence, 1m–24h |
| `REPUTATION_MIN_VOLUME` | `500` | volume needed over 90 days, in the app's units |
| `REPUTATION_RETURN_FLOOR` | `100` | returns divide by the larger of the open and this, in the app's units |
| `REPUTATION_LEADERBOARD` | `true` | offer the Reputation board to the leaderboard feature |

## Compliance

- Deletion removes the person's snapshots, days and score. Integrity flags stay as fraud evidence, pseudonymised by the anonymised users row (retention rules in compliance.md).
- Export: the full reputation, every day row, and the flags about the person (kind, status, dates; not the other person or the detector's figures).
- No UGC. A score is a fact about a person, so only the person and staff see the breakdown. If the score gates money (copy trading, payouts), say so in the product and the privacy policy.

## Stitching

1. Supply the series. With the exchange feature, add `backend/internal/api/stitch_reputation_exchange.go` (this file was compiled against exchange 1.1.0):
   ```go
   package api

   import (
   	"context"

   	"scaffoldapp/backend/internal/app"
   	"scaffoldapp/backend/internal/exchange"
   	"scaffoldapp/backend/internal/reputation"
   )

   func init() { RegisterModule(func(d *app.Deps) app.Module { return &reputationExchange{d: d} }) }

   type reputationExchange struct{ d *app.Deps }

   func (*reputationExchange) Name() string { return "reputation_exchange" }

   func (x *reputationExchange) ReputationSamples(ctx context.Context) ([]reputation.Sample, error) {
   	scores, err := exchange.Scores(ctx, x.d.Pool)
   	if err != nil {
   		return nil, err
   	}
   	acts, err := exchange.ActivityTotals(ctx, x.d.Pool)
   	if err != nil {
   		return nil, err
   	}
   	out := make([]reputation.Sample, 0, len(scores))
   	for _, sc := range scores {
   		a, ok := acts[sc.UserID]
   		if !ok || !sc.Traded {
   			continue
   		}
   		out = append(out, reputation.Sample{UserID: sc.UserID, Value: sc.AccountValue, NetFlows: sc.Deposits,
   			Activity: int64(a.Fills), Volume: a.Volume, Incidents: int64(a.Liquidations), FirstActivity: a.FirstFill})
   	}
   	return out, nil
   }
   ```
   In the same file's `init()`, rename the words: `reputation.DefaultModel.Labels["incidents"] = "liquidations"`, `Labels["activity"] = "Fills in 90 days"`. Without the exchange, implement `reputation.Source` on any module, or push days with `reputation.RecordDay(ctx, q, userID, reputation.Day{…})`. `RecordDay` is also how to backfill history from before install; otherwise counting starts at the first snapshot.
2. Set `REPUTATION_MIN_VOLUME` and `REPUTATION_RETURN_FLOOR` in the app's units.
3. Community: with the forum and moderation (1.1.0) features installed, plug them in from an `init()` (imports add `time`, `github.com/google/uuid`, `db`, `forum`, `moderation`):
   ```go
   reputation.SetCommunitySignals(func(ctx context.Context, q db.Querier, id uuid.UUID, since time.Time) (reputation.Community, error) {
   	karma, err := forum.Karma(ctx, q, id, since)
   	if err != nil {
   		return reputation.Community{}, err
   	}
   	strikes, err := moderation.Strikes(ctx, q, id, since)
   	return reputation.Community{Karma: karma, Strikes: strikes, Posted: karma != 0 || strikes != 0}, err
   })
   ```
   If the forum has its own has-posted read, use it for `Posted`. Without signals, the community part is unscored.
4. Anti-gaming: `SetPairSource` supplies pairs of people who traded with each other since a time (trade count, volume, each side's gain); volume shares come from the day rows. `SetVoteSource` supplies upvotes since a time. Other modules raise their own flags in their own transaction: `reputation.Flag(ctx, tx, reputation.IntegrityFlag{UserID: id, Kind: "copy_front_run", Metrics: m})`.
5. Place `ReputationBadge` on profiles and list rows (prime a list with `useReputationSummaries(ids)`). Move the home tile and account-menu entry in the registries if reputation belongs elsewhere.

## Extending

- Seams: `Source`, `Sample`, `RecordDay`, `SetCommunitySignals`, `SetPairSource`, `SetVoteSource`, `Flag`, `IntegrityFlag`. Other modules read a person's standing with `Get(ctx, q, userID) (Summary, error)`, where `Summary` is `{Score *int; Status string; Tier string}`. It reads one row, inside the caller's transaction, for badges and eligibility checks (copy-trading leaders, payouts).
- `reputation.DefaultModel` (score.go) can be changed from an `init()`, before `New` copies it:
  - `Weights`, in points: performance 40, risk 20, consistency 15, community 10, tenure 5. A part's effective weight is its points / the scored parts' points.
  - `Tiers`: `{elite 75, strong 60, steady 40, building 0}`, highest first. Add new keys to `REPUTATION_TIER_LABEL` in shared.
  - Requirements: `MinAgeDays` 14, `MinActiveDays` 14, `MinActivity` 20, `MinVolume` (env). Also `ReturnFloor` (env) and `Labels`.
- The formula constants (90/30-day windows, shrinkage 20, std floor 0.002, a 60% drawdown scores 0, the penalties, 13 weeks) are rep-v1 itself. Change them together with `Version` and `score_test.go` (golden scores, shrinkage, redistribution, gates, flow exclusion, rollup counters, detectors).
- Don't write `reputation_scores` yourself: statuses come from `Rescore` (and `Flag`).

## Provenance

Distilled from MOX `backend/internal/reputation` (score.go, rollup.go, integrity.go, integrity_db.go, jobs.go, service.go, http.go; branch worktree-wf_70d3fcd7-22d-2) and `web/components/reputation` (ReputationRing, ReputationExplain).

- Kept: the rep-v1 formula with its golden numbers, time-weighted returns that exclude flows, redistribution of unscored weight, requirements with progress, the wash-pair and vote-ring detectors, and under-review on any open flag (and for 90 days after a confirmation).
- Generalised: the series comes from any `Source` as cumulative counters (MOX read its engine, ledger and fills tables) or from `RecordDay`. Removed: account kinds, the simulated and copy statuses, the decks component, win rate, leverage, handles, copy-trading leader eligibility and the front-running rule (copy trading raises its own flags with `Flag`). Uses scaffold users, support/admin guards, audit, keyset paging, and `jobs.Every` instead of advisory-locked tickers.
- Fixed: explicit `RecordDay` backfill replaces the ledger bootstrap; weights are points, so removing a part needs no renormalising; a counter that goes down counts as 0.
- Verified: the Go unit tests; and on a backend-only boot with the exchange stitch, the routes, the seeded states, a flag decision with its audit row, and exchange fills reaching snapshots and day rows.
- Not verified: `next build`, `expo export`, Playwright and device checks; the community, pair and vote seams against a real forum, moderation feature or pair source (unit tests only).

## Changes

- 1.0.0: first version.
