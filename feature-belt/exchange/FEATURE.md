# Exchange

## What it does

- Perpetual futures on any asset the app prices: markets come from the app's catalog (database rows, seeded from `EXCHANGE_CATALOG_FILE`, `UpsertCatalog` or Admin → Exchange), each with max leverage, tick size, size step, minimum order and an open interest cap.
- Price-time priority order books; market orders (IOC within a slippage limit from the mark) and limit orders, reduce-only and post-only; self-trades expire the resting order; maker and taker fees.
- One cross-margin account per person in the quote currency (`EXCHANGE_QUOTE_CURRENCY`): a ledger of every balance change, an optional test-funds faucet, and deposits/withdrawals the app makes with `exchange.Deposit` / `exchange.Withdraw`.
- Mark price (median of index + EMA basis, book and index, clamped to ±10% of the index and rate-limited to 0.3%/s), funding every `EXCHANGE_FUNDING_INTERVAL` from impact prices, liquidations into the book (20% at a time above `EXCHANGE_LIQ_PARTIAL_NOTIONAL`) and a backstop account that takes over accounts below 2/3 of maintenance margin and absorbs bad debt.
- Live market data: 1m–1d candles with history paging (1d continues into the price feed's daily history), sparklines, L2 book, trade tape and 24h stats over REST and realtime.
- Optional simulated market maker and 12 traders (`EXCHANGE_SIM_ENABLED`) for demos; their trades are flagged `sim` and stats show the simulated share.

## Data

| Table | Purpose |
|---|---|
| `exchange_markets` | the catalog; `status` (active / reduce_only / halted) and `manual_price` belong to admins |
| `exchange_accounts` | one per user (`user_id`), plus system (backstop, fees) and simulated accounts; `balance`, `deposits` (PnL = account value − deposits) |
| `exchange_positions` | signed size, entry, leverage, funding since open |
| `exchange_orders` | every order; open ones rebuild the books on boot |
| `exchange_trades` | public tape (pruned after `EXCHANGE_TRADE_RETENTION`), sim flag per side |
| `exchange_fills`, `exchange_ledger`, `exchange_funding_payments`, `exchange_liquidations` | an account's private history (users only) |
| `exchange_funding_rates` | each market's hourly rate, premium and index at settlement |
| `exchange_candles` | 1m candles (`t` unix ms); longer intervals aggregate them |
| `exchange_candle_backfill` | daily bars from the price feed's history (`PriceHistory`) |
| `exchange_transfers` | collateral in/out with an idempotency key; the engine settles `pending` rows within a second |

The engine is in memory and runs on one instance at a time (Postgres advisory lock). Every command is written in one transaction before its lock is released, and rolled back in memory if the write fails. Other instances answer engine-backed routes with 503 `exchange_standby` and take over within 10 s if the leader goes; history routes read the database on any instance.

## API

All under `/v1`. Decimal strings for money, prices and sizes. History lists are `{items, next_cursor}`, keyset on `(created_at, id)`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/exchange/config` | none | quote currency and decimals, faucet, sim flag, intervals |
| GET | `/exchange/markets?q&category`, `/exchange/markets/{symbol}` | none | markets with live stats |
| GET | `/exchange/markets/{symbol}/book?depth` | none | L2 book (≤100 levels a side) |
| GET | `/exchange/markets/{symbol}/trades?limit` | none | latest trades, newest first |
| GET | `/exchange/markets/{symbol}/candles?interval&limit&before` | none | `{items (oldest first), next_before}`; pass `next_before` as `before` for older |
| GET | `/exchange/sparklines?interval&points` | none | every market's closes (30 s cache) |
| GET | `/exchange/account` | user | summary, positions with liquidation price, open orders; creates the account (welcome credit = faucet amount) |
| GET | `/exchange/orders?status`, `/exchange/fills`, `/exchange/funding`, `/exchange/ledger` | user | history |
| POST | `/exchange/orders` | active, 120/min | `{symbol, side, type, sz \| sz_quote, px?, leverage?, reduce_only?, post_only?, slippage_bps?}` → `{order, fills}` |
| DELETE | `/exchange/orders/{id}`, `/exchange/orders?symbol` | active, 120/min | cancel one / all |
| POST | `/exchange/positions/{symbol}/close` | active, 120/min | market-close `{sz?}` |
| POST | `/exchange/faucet` | active, 10/h | top the balance up to the faucet amount, once per cooldown |
| GET | `/admin/exchange/overview`, `/admin/exchange/markets` | support | totals (volume, OI, fees, backstop), catalog with live stats |
| POST / PUT | `/admin/exchange/markets`, `/admin/exchange/markets/{symbol}` | admin, audited | list / edit a market |
| PUT | `/admin/exchange/markets/{symbol}/status`, `…/price` | admin, audited | status; set or clear (`""`) the manual index price |

Order errors carry codes clients branch on: `insufficient_margin`, `price_band` (limit beyond ±15% of the index), `post_only_would_cross`, `reduce_only`, `min_notional`, `no_liquidity`, `oi_cap`, `price_unavailable`, `index_stale`, `market_halted`, `market_reduce_only`, `faucet_cooldown`, `exchange_standby`.

Realtime (signed in): topic `exchange:<market id>` carries `exchange.book` (≤4/s), `exchange.trades` and `exchange.ticker` (stats + the open candle of every interval, 1/s); topic `exchange:00000000-0000-0000-0000-000000000000` carries `exchange.tickers` (all stats, every 2 s); the user's own topic gets `exchange.account` (order, fill, funding, liquidation, transfer) and `exchange.summary`. Signed-out and reconnecting clients poll.

Agent tools: `exchange_list_markets`, `exchange_get_market`, `exchange_get_candles`, `exchange_get_account`, `exchange_list_fills` (read); `exchange_place_order`, `exchange_cancel_order`, `exchange_close_position` (writes with `Target` = the market or order and a `Preview`, so agent scopes gate them and Suggest shows the order first). No tool moves collateral. Global search finds markets (`exchange.market`).

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/markets` (nav, home tile) | search, category chips, sort (`?q&category&sort`); rows with price, 24h change, 24h sparkline, volume, open interest, funding |
| Web | `/markets/[symbol]` | stats (mark, index status, change, volume, OI, funding countdown), candle chart (lightweight-charts; older history loads at the left edge), ticket, book (a price click → limit) and trades, account summary, positions / open orders / fills (`?tab`) |
| Web | `/portfolio` (nav, home tile) | summary, faucet, positions with market close, open orders with cancel, history: fills, funding, ledger (`?history`) |
| Web | `/admin/exchange` | totals, catalog table with status select, edit and manual price dialogs |
| Mobile | Markets tab, `/markets/[symbol]`, `/portfolio` (home entry) | the same; the chart pages with Earlier / Later, the ticket is a sheet behind Buy and Sell |

Web session mount: toasts for liquidations and settled transfers. Every view has skeleton, empty and error states.

## Config

| Variable | Default | |
|---|---|---|
| `EXCHANGE_QUOTE_CURRENCY`, `EXCHANGE_QUOTE_DECIMALS` | `USD`, `2` | display only |
| `EXCHANGE_TAKER_FEE`, `EXCHANGE_MAKER_FEE` | `0.00045`, `0.00015` | fractions of notional; fees go to the fee account, which also pays maker rebates (a negative maker fee, never more than the taker fee) |
| `EXCHANGE_DEFAULT_LEVERAGE` | `3` | until the trader picks |
| `EXCHANGE_FAUCET_AMOUNT`, `EXCHANGE_FAUCET_COOLDOWN` | `0` (dev `1000`), `1h` | `0` = no faucet and no welcome credit |
| `EXCHANGE_SIM_ENABLED` | `false` (dev `true`) | simulated market maker and traders |
| `EXCHANGE_CATALOG_FILE` | — | JSON array of `CatalogMarket`, upserted when the engine starts |
| `EXCHANGE_PRICE_POLL_INTERVAL`, `EXCHANGE_PRICE_STALE_AFTER` | `2s`, `5m` | feed polling; a live price older than this is stale |
| `EXCHANGE_FUNDING_INTERVAL`, `EXCHANGE_LIQ_PARTIAL_NOTIONAL`, `EXCHANGE_TRADE_RETENTION` | `1h`, `10000`, `168h` | |

Catalog seed prices are used only with `APP_ENV=development`; elsewhere a market trades once the price feed (or an admin's manual price) reports.

## Compliance

- Deleting an account marks its trading account `closing` and unlinks it inside the deletion transaction; the engine then cancels its orders, hands positions and balance to the backstop and closes it. Orders, fills and ledger stay as anonymous financial records. Pending transfers are rejected.
- Export: account, positions, orders, fills, funding, ledger and transfers.
- Public trades carry no identities (only a `sim` flag). No UGC.
- With real-money collateral the app offers leveraged derivatives: licensed activity in most countries, and Apple (5.1.1, 3.1.5) and Google Play require the licences for real-money trading apps. Test funds (the faucet, no withdrawals) keep it a simulation; say so in the product.

## Stitching

1. Define the catalog: `EXCHANGE_CATALOG_FILE`, `exchange.UpsertCatalog(ctx, pool, markets)` from app code, or Admin → Exchange. Replace the development catalog in `SeedDev` (`backend/internal/exchange/account.go`).
2. Install a price feed. With the oracle feature, add `backend/internal/api/stitch_exchange_oracle.go` (imports `context`, `github.com/shopspring/decimal`, `app`, `exchange`, `oracle`):
   ```go
   func init() { RegisterModule(func(d *app.Deps) app.Module { return &exchangeOracleFeed{d: d} }) }
   type exchangeOracleFeed struct{ d *app.Deps }
   func (*exchangeOracleFeed) Name() string { return "exchange_oracle_feed" }
   func (f *exchangeOracleFeed) Start(context.Context) {
   	var o *oracle.Service
   	for _, m := range f.d.Modules { if s, ok := m.(*oracle.Service); ok { o = s } }
   	if o == nil { return }
   	price := func(pp oracle.PricePoint) (exchange.Price, bool) {
   		if pp.Status != "live" || pp.Price <= 0 { return exchange.Price{}, false }
   		return exchange.Price{Value: decimal.NewFromFloat(pp.Price).Round(pp.Decimals), At: pp.At}, true
   	}
   	exchange.SetPriceFeed(exchange.PriceFeedFunc(func(ctx context.Context, keys []string) (map[string]exchange.Price, error) {
   		out := map[string]exchange.Price{}
   		for _, k := range keys {
   			pp, ok, err := o.Current(ctx, k)
   			if err != nil { return out, err }
   			if p, live := price(pp); ok && live { out[k] = p }
   		}
   		return out, nil
   	}))
   	o.OnPublish(func(pp oracle.PricePoint) { if p, live := price(pp); live { exchange.PushPrice(pp.Key, p) } })
   }
   ```
   A market's `price_feed` is the oracle feed key (the dev metals use `GOLD-OZ` and `SILVER-OZ`). Without the oracle, implement `exchange.PriceFeed` (and optionally `PriceHistory`) over your source and call `exchange.SetPriceFeed` from an `init()`.
3. Decide where collateral comes from: test funds (`EXCHANGE_FAUCET_AMOUNT`) or real money through `exchange.Deposit` / `exchange.Withdraw` in the same transaction as the payment, checking `exchange.TransferStatus` before paying anything out. Never both.
4. Run one API instance, or accept that only the leader serves trading (the others answer 503 `exchange_standby`). With more than one instance set `REDIS_URL` so realtime reaches every socket.
5. Decide whether demos run the simulation outside development (`EXCHANGE_SIM_ENABLED`), and say on screen that sim trades are simulated.
6. With the leaderboard feature installed, a "Profit and loss" board (everyone who has traded) appears on its own.
7. Rename "Markets" / "Portfolio" in `web/src/features/exchange/registry.ts` and `mobile/src/features/exchange/registry.tsx` if the product calls them something else.

## Extending

- Seams: `PriceFeed`, `PriceFeedFunc`, `PriceHistory`, `SetPriceFeed`, `PushPrice`, `Deposit`, `Withdraw`, `TransferStatus`, `UpsertCatalog`, `Scores`; risk and fee knobs in `engine.Params`.
- The engine (`internal/exchange/engine`) is pure and tested (`engine_test.go`: priority, margin, reduce-only, bands, statuses, funding, mark, liquidation, backstop, transfers, close-out, rollback; `sim/sim_test.go` runs 2,000 simulated actions). Change behaviour together with its tests, and mutate engine state only inside a command (`begin` → `commit`).
- Don't write `exchange_accounts`, positions or orders directly: the engine owns them in memory. Go through transfers, the catalog or the API.

## Provenance

Distilled from MOX (`backend/internal/engine`, `internal/sim`, `internal/store`, the REST and realtime layers) without its collectibles catalog, handles, oracle internals and index baskets. Generalised: markets live in the database and reload live (status, manual price, parameters); index prices come from any feed (pull and push) with stale handling and development-only seed prices; collateral moves through a transfer inbox any instance can write; one engine elected by advisory lock instead of one process; accounts belong to scaffold users and are closed out on deletion; public trades carry no identities. Kept: decimal money throughout, atomic commands with in-memory rollback, the mark, funding and liquidation formulas and their tests. Added: candle history paging (`before`), keyset history routes, admin catalog tools, agent tools, search.

## Changes

- 1.0.0: first version.
