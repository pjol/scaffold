# Copy trading

## What it does

- Traders opt in to be copied (Settings → Copy trading): a profit share (0–`COPYTRADE_MAX_PROFIT_SHARE_PCT`%), a minimum allocation, a copier cap and whether they see copiers' names. Only people who meet the leader rules can (default: an active account `COPYTRADE_LEADER_MIN_AGE_DAYS` old with `COPYTRADE_LEADER_MIN_FILLS` fills; replaceable).
- A follower picks a trader in the directory (returns, drawdown, copiers, assets copying), sets an allocation and risk limits, accepts a versioned risk disclosure and starts a copy. The allocation moves into a new exchange sub-account of the follower's.
- The copy mirrors the trader's **target position** (their size × copy value ÷ their value), not their fills, so partial fills, missed events and restarts converge. Each trader fill is coalesced per market for 250 ms, then every copy gets one market order that never trades against the trader; a reconcile pass fixes drift every minute.
- Per-copy limits: max leverage, largest order, largest position per market, stop loss, drawdown limit, copy open positions at start, close or keep on the trader's exit, slippage. Orders that can't go through are skipped and logged by class (too small, risk limit, rejected, slippage).
- Pause, resume, add or withdraw funds, close a position by hand, stop (closing or keeping positions); a live activity log. Profit share is paid daily above a high-water mark, and on every withdrawal and stop.
- Admin → Copy trading: totals, the kill switch's state, every copy; admins can stop one.

## Data

| Table | Purpose |
|---|---|
| `copytrade_leaders` | a leader's offer (`enabled`, profit share %, minimum allocation, max copiers, `show_copiers`) and `eligible`, refreshed every 5 minutes |
| `copytrade_copies` | follower, leader, the exchange sub-account (`account_id`, purpose `copy:<id>`), status and reasons, settings (jsonb), net and gross deposits, peak and last equity, profit share fraction (fixed at start), high-water mark, paid, `ignored` markets, disclosure version and time, `identity_visible` |
| `copytrade_events` | per copy: start, order, skip (with class), transfer, profit share, reconcile, pause, resume, settings, stop; kept `COPYTRADE_EVENT_RETENTION` |
| `copytrade_snapshots` | one row per trader per day: main-account value and net deposits (leader statistics; 120 days) |

Money stays on the exchange: copytrade moves it only with `exchange.Transfer` (main ↔ own sub-account, sub-account → leader for profit share), each one atomic engine command. The pipeline runs on the instance that runs the exchange engine (`exchange.OnEngine`); its loops need no lease there. Database jobs (`copytrade.leaders` 5 min, `copytrade.snapshots` hourly, `copytrade.prune` daily) run once across instances through `jobs.Every`.

## API

All under `/v1`. Decimal strings in the exchange's quote currency. Engine-backed routes answer 503 `exchange_standby` away from the engine's instance.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/copytrade/config` | none | disclosure (version + Markdown), kill switch, limits, default settings |
| GET | `/copytrade/leaders?sort`, `/copytrade/leaders/{user_id}` | user | directory (`return_90d` · `return_30d` · `copiers` · `aum`; ≤ 200, cached 30 s), one card |
| GET / PUT | `/copytrade/leader` | user; PUT active, 30/h | own offer, eligibility with requirements, copiers, assets, share earned |
| GET | `/copytrade/copiers?cursor` | user | own copiers: allocation bucket, name only if `identity_visible` |
| GET / POST | `/copytrade/copies?status&cursor` / `/copytrade/copies` | user; POST active, 10/h | own copies (keyset) / start `{leader_id, allocation, settings?, disclosure_version}` |
| GET / PATCH | `/copytrade/copies/{id}` | user; PATCH active, 60/h | a copy with its sub-account summary / `{settings?, status?: active \| paused}` |
| POST | `/copytrade/copies/{id}/funds` | active, 10/h | `{amount}`: positive adds, negative withdraws (settles the share first) |
| POST | `/copytrade/copies/{id}/stop` | active, 30/h | `{close_positions}` (default true) |
| POST | `/copytrade/copies/{id}/positions/{symbol}/close` | active, 120/min | `{sz?}`; the market then waits for the trader to go flat or flip |
| GET | `/copytrade/copies/{id}/events?cursor` | user | activity, newest first (any instance) |
| GET | `/admin/copytrade/overview`, `/admin/copytrade/copies?status&cursor` | support | totals, kill switch, drops; every copy |
| POST | `/admin/copytrade/copies/{id}/stop` | admin, audited | stop as staff (positions closed) |

Errors: `copytrade_paused`, `self_copy`, `leader_not_accepting`, `leader_full`, `copy_limit`, `disclosure_required`, `already_copying`, `insufficient_collateral`, `copy_stopped`, `leader_ineligible`, plus the exchange's order codes in skip reasons.

Realtime (the follower's `user:<id>` topic, no topics of its own): `copytrade.copy` (the Copy, ≤ 1/s each) and `copytrade.event`. Clients poll while the socket is down. Notifications through `d.Notify`: `copytrade.started`, `copytrade.stopped`, `copytrade.orders_failing`, `copytrade.copier_joined`, `copytrade.copier_left`, `copytrade.profit_share`. Not registered with notifications (copytrade doesn't require it): they deliver with push on, email off.

Agent tools: `copytrade_list_copies`, `copytrade_get_copy`, `copytrade_list_leaders` (read); `copytrade_stop_copy` (write, `Target` = `copytrade.copy`, `Preview`). Agents never start copies, change allocations or move funds.

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/copy` (nav) | open copies (value, PnL, status), stopped ones, Find traders |
| Web | `/copy/[id]` | value, PnL, moved in, peak, profit share; pause/resume, add/withdraw, stop (close or keep); positions with Close; settings with an edit dialog; skips by class; activity with LoadMore |
| Web | `/leaders` (home tile) | sort control, trader rows; `?leader=<user id>` opens the start sheet (allocation against free collateral, leverage, stop loss, open positions, on exit, the disclosure with an accept checkbox, a notice when the trader sees names) |
| Web | `/settings/copy-trading` (Settings, "you") | requirements, the offer form, totals, copiers list |
| Web | `/admin/copytrade` | totals, kill switch, copies table with Stop (admins) |
| Mobile | `/copy`, `/copy/[id]`, `/leaders` (+ start sheet), `/settings/copy-trading`; home entry and Settings row | the same |

Every view has skeleton, empty and error states; banners say when copying is paused.

## Config

| Variable | Default | |
|---|---|---|
| `COPYTRADE_PAUSED` | `false` | kill switch: no new copies, copied orders, reconcile or resumes; stops, stop losses, withdrawals and closes still work |
| `COPYTRADE_MIN_ALLOCATION`, `COPYTRADE_MAX_PROFIT_SHARE_PCT` | `50`, `20` | quote currency; percent (0–50) |
| `COPYTRADE_MAX_COPIERS_PER_LEADER`, `COPYTRADE_MAX_COPIES_PER_FOLLOWER` | `500`, `10` | |
| `COPYTRADE_SETTLE_INTERVAL` | `24h` | profit share settlement (1h–2160h) |
| `COPYTRADE_LEADER_MIN_AGE_DAYS`, `COPYTRADE_LEADER_MIN_FILLS` | `30`, `1` (dev `0`, `0`) | the default leader rules |
| `COPYTRADE_EVENT_RETENTION`, `COPYTRADE_WORKERS` | `2160h`, `8` | |

## Compliance

- **Regulated activity.** Copying a trader's positions with real money is portfolio management or investment advice in most countries (and a leader taking a profit share is paid for it); Apple and Google require the licences for real-money trading apps. With the exchange's test funds (faucet, no withdrawals to the outside) it stays a simulation: say so on screen and in the disclosure.
- The disclosure is versioned (`copy-v1`); the version and acceptance time are stored on each copy. Rewrite it for your product.
- People appear by their core display name and avatar: leaders only once they opt in to be listed; copiers' names reach a leader only for copies started while the leader showed copiers, which the start sheet says. Allocations are shown to leaders as ranges.
- Deletion: the person's leader offer, snapshots and copies (with events) are deleted; the exchange closes their copy sub-accounts with their other trading accounts. Copies of them as a leader stop (positions per each copier's choice). Export: offer, copies with events, snapshots.
- No UGC.

## Stitching

1. Decide whether copy trading may run at all (Compliance) and rewrite `DisclosureText` in `backend/internal/copytrade/disclosure.go`; change `DisclosureVersion` whenever its substance changes.
2. Decide who may lead. To require a rated reputation that isn't under review (reputation feature), add `backend/internal/api/stitch_copytrade_reputation.go` (imports `context`, `fmt`, `uuid`, `app`, `copytrade`, `reputation`):
   ```go
   func init() { RegisterModule(func(d *app.Deps) app.Module {
   	copytrade.SetEligibility(func(ctx context.Context, userID uuid.UUID) (bool, []copytrade.Requirement, error) {
   		ok, reqs, err := copytrade.DefaultEligibility(ctx, d.Pool, userID)
   		if err != nil { return false, nil, err }
   		sum, err := reputation.Get(ctx, d.Pool, userID)
   		if err != nil { return false, nil, err }
   		rated := sum.Status == "rated"
   		progress := sum.Status
   		if sum.Score != nil { progress = fmt.Sprintf("%d (%s)", *sum.Score, sum.Tier) }
   		reqs = append(reqs, copytrade.Requirement{Key: "reputation", Label: "A rated reputation, not under review", Met: rated, Progress: progress})
   		return ok && rated, reqs, nil
   	})
   	return copytradeReputation{}
   }) }
   type copytradeReputation struct{}
   func (copytradeReputation) Name() string { return "copytrade_reputation" }
   ```
   (add `fmt` to the imports). Ineligible leaders' copies pause and resume by themselves when they qualify again.
3. Set the limits (Config). Link people to `/leaders?leader=<user id>` from profiles or the leaderboard.
4. With moderation installed, call `(*copytrade.Service).StopBetween(ctx, blocker, blocked)` where a block is created (find the service in `d.Modules`).
5. Run one API instance or accept the exchange's standby rule (the pipeline follows the engine).

## Extending

- Seams: `SetEligibility`, `DefaultEligibility`, `Requirement`; `StopBetween`; `DefaultSettings`, `Size` (pure sizing), `SettleFee` (pure profit share), `DisclosureVersion` / `DisclosureText`.
- Built on the exchange's module API (`exchange/modules.go`): `OnEngine`, `OnFill`, `OpenSubaccount`, `Transfer`, `CloseSubaccount`, `PlaceOrderAs` / `ClosePositionAs` with `PlaceOpts{Source, ClientRef, AvoidMakers}`, `AccountState`. Don't write exchange tables; move money only through `Transfer`.
- Only main accounts lead (fills of sub-accounts never reach the pipeline), so copy loops can't form. Keep it that way.
- Tests (`copytrade_test.go`): sizing clips and skips, the chase guard, the high-water mark, settings validation, flow-neutral returns.

## Provenance

Distilled from MOX's `backend/internal/copytrade` (types, copies, mirror, sizing, leader, jobs, profitshare, service, http, pgstore), its web copy screens (`web/components/copy`, `/copy`, `/leaders`, `/settings/leader`) and the engine's copy accounts (`engine/placeopts.go`, `accounts.go`, migration 0016). Kept: target-position sizing and its clips, coalescing per leader and market, sharded workers with shuffled order, AvoidMakers, engaged markets, reconcile thresholds and retry caps, monitor stops, the high-water-mark profit share (a share lowers the drawdown peak), skip classes, the disclosure flow. Generalised: scaffold users and display names instead of handles, the exchange's sub-accounts and quote currency instead of copy users and USD, leader rules through a seam instead of MOX reputation, statistics from daily snapshots, the pipeline wherever the engine runs. Fixed or added: copiers' names only with consent captured at start (MOX revealed them whenever the leader turned the setting on), the kill switch pauses instead of stopping every copy, a stopped copy's account is swept and closed once flat (also after keeping positions), orphaned copy accounts after a crash are swept, staff stops are audited, cross-instance stops through the copy row. Dropped: simulated leaders, front-running flags (reputation's concern), follow on start, categories, review pauses (now the generic ineligible pause).

Verified on a dev-up backend with the simulated market (curl): mirroring with coalescing, skips on a thin book and reconcile converging, add and withdraw (over-withdrawal refused), pause and resume, start refusals, a leader turning copying off (positions closed, account swept and closed), a stop that keeps positions then a manual close (swept), consented copier names, a staff stop with its audit row, export. Not verified: live profit share settlement, stop loss and drawdown stops (pure logic is tested), account deletion, `StopBetween`, the orphan sweep, multi-instance hand-over, web and mobile builds (`next build`, `expo export`) and Playwright flows; there are no pipeline unit tests (MOX's rig tests need its in-memory engine adapter).

## Changes

- 1.0.0: first version.
