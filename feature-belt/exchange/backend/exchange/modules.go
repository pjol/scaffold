package exchange

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/exchange/engine"
)

// Engine support for other modules (copytrade, automated strategies): find
// the service, run work where the engine runs, hear about fills, trade on an
// account and move collateral between a person's accounts.
//
// Live calls (orders, transfers, sub-accounts, account state) work only on
// the instance that runs the engine and return ErrStandby elsewhere; run the
// module's engine-bound work from OnEngine. Engine errors are *engine.Error
// with stable codes; database reads (OrderByClientRef, ActivityOf) work on
// any instance.

// ErrStandby: this instance isn't running the engine (503 exchange_standby).
var ErrStandby = errStandby

type (
	FillEvent    = engine.FillEvent
	PlaceOpts    = engine.PlaceOpts
	AccountState = engine.AccountState
)

// Transfer kinds (Service.Transfer).
const (
	TransferInternal    = engine.LedgerTransfer    // a person's main account ↔ one of their sub-accounts
	TransferProfitShare = engine.LedgerProfitShare // a sub-account → another person's main account
)

// Find returns the installed exchange among the modules (read d.Modules at
// Start or request time, never in New).
func Find(mods []app.Module) *Service {
	for _, m := range mods {
		if s, ok := m.(*Service); ok {
			return s
		}
	}
	return nil
}

// Running reports whether this instance runs the engine now.
func (s *Service) Running() bool { return s.engine() != nil }

// Quote is the quote currency and its display decimals.
func (s *Service) Quote() (currency string, decimals int32) {
	return s.quoteCurrency, s.params.QuoteDecimals
}

// OnEngine runs f in its own goroutine each time this instance starts running
// the engine (and at once if it already does); ctx ends when it stops. Put a
// module's engine-bound loops here: they then run on exactly one instance.
func (s *Service) OnEngine(f func(ctx context.Context)) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.engineHooks = append(s.engineHooks, f)
	if s.engineCtx != nil {
		go f(s.engineCtx)
	}
}

func (s *Service) engineStarted(ctx context.Context) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.engineCtx = ctx
	for _, f := range s.engineHooks {
		go f(ctx)
	}
}

func (s *Service) engineStopped() {
	s.hooksMu.Lock()
	s.engineCtx = nil
	s.hooksMu.Unlock()
}

// OnFill registers f for every fill of a person's account (main or sub),
// called on the engine's instance after the fill is committed, in order, from
// one goroutine (never inside the engine lock). Keep f quick: hand work to
// your own goroutine. Fills arriving faster than hooks consume them are
// dropped once 10,000 are queued (FillsDropped counts them), so treat the
// hook as a prompt and reconcile from state.
func (s *Service) OnFill(f func(FillEvent)) {
	s.hooksMu.Lock()
	s.fillHooks = append(s.fillHooks, f)
	s.hooksMu.Unlock()
}

// FillsDropped is how many fills the hook queue dropped since boot.
func (s *Service) FillsDropped() int64 { return s.fillDrops.Load() }

func (s *Service) deliverFills(ctx context.Context, fills <-chan engine.FillEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-fills:
			s.hooksMu.Lock()
			hooks := slices.Clone(s.fillHooks)
			s.hooksMu.Unlock()
			for _, h := range hooks {
				func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("exchange: fill hook panicked", "panic", r)
						}
					}()
					h(f)
				}()
			}
		}
	}
}

// MainAccount is the person's main trading account id (ok=false: none yet).
func (s *Service) MainAccount(userID uuid.UUID) (string, bool, error) {
	eng, err := s.running()
	if err != nil {
		return "", false, err
	}
	id, ok := eng.AccountForUser(userID.String())
	return id, ok, nil
}

// AccountState is an account (main or sub) at current marks.
func (s *Service) AccountState(accountID string) (AccountState, error) {
	eng, err := s.running()
	if err != nil {
		return AccountState{}, err
	}
	st, ok := eng.State(accountID)
	if !ok {
		return AccountState{}, engine.ErrNotFound
	}
	return st, nil
}

// UserAccountState is the person's main account at current marks.
func (s *Service) UserAccountState(userID uuid.UUID) (AccountState, error) {
	eng, err := s.running()
	if err != nil {
		return AccountState{}, err
	}
	id, ok := eng.AccountForUser(userID.String())
	if !ok {
		return AccountState{}, engine.ErrNoAccount
	}
	st, _ := eng.State(id)
	return st, nil
}

// Summary is an account's summary as GET /exchange/account shows it.
func (s *Service) Summary(accountID string) (engine.AccountSummary, error) {
	eng, err := s.running()
	if err != nil {
		return engine.AccountSummary{}, err
	}
	return eng.Summary(accountID)
}

// Summaries returns many accounts' summaries at once (unknown ids left out).
func (s *Service) Summaries(accountIDs []string) (map[string]engine.AccountSummary, error) {
	eng, err := s.running()
	if err != nil {
		return nil, err
	}
	return eng.Summaries(accountIDs), nil
}

// Market is one market with live stats.
func (s *Service) Market(symbol string) (engine.MarketView, bool, error) {
	eng, err := s.running()
	if err != nil {
		return engine.MarketView{}, false, err
	}
	m, ok := eng.Market(symbol)
	return m, ok, nil
}

// OpenSubaccount returns the person's open sub-account for purpose (a short
// label such as "copy:<id>"), creating it with a zero balance. The person
// needs a main account. Sub-accounts never appear on leaderboards or in
// Scores and ActivityTotals; deleting the person closes them too.
func (s *Service) OpenSubaccount(owner uuid.UUID, purpose string) (string, error) {
	eng, err := s.running()
	if err != nil {
		return "", err
	}
	return eng.OpenSubaccount(owner.String(), purpose)
}

// Subaccounts lists the person's open sub-accounts at current marks.
func (s *Service) Subaccounts(owner uuid.UUID) ([]AccountState, error) {
	eng, err := s.running()
	if err != nil {
		return nil, err
	}
	return eng.Subaccounts(owner.String()), nil
}

// Transfer moves collateral in one atomic command: TransferInternal between a
// person's main account and their own sub-account (either way), or
// TransferProfitShare from a sub-account to another person's main account.
// It refuses more than the source's free collateral.
func (s *Service) Transfer(fromAccountID, toAccountID string, amount decimal.Decimal, kind, note string) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	return eng.Transfer(fromAccountID, toAccountID, amount, kind, note)
}

// CloseSubaccount cancels a sub-account's orders, market-closes its positions
// (within slippageBps; 0 = default) and, once flat, sweeps its balance back
// to the owner's main account and closes it. engine.ErrPositionsOpen means
// some positions found no liquidity: call again later.
func (s *Service) CloseSubaccount(accountID string, slippageBps int) error {
	eng, err := s.running()
	if err != nil {
		return err
	}
	return eng.CloseSubaccount(accountID, slippageBps)
}

// PlaceOrderAs places an order on an account (a person's main account or a
// sub-account) for a module: opts carry its Source, a ClientRef and makers to
// avoid. The caller has checked that the person allowed it.
func (s *Service) PlaceOrderAs(_ context.Context, accountID string, req engine.PlaceOrder, opts PlaceOpts) (engine.OrderView, []engine.Fill, error) {
	eng, err := s.running()
	if err != nil {
		return engine.OrderView{}, nil, err
	}
	return eng.PlaceOrderWith(accountID, req, opts)
}

// ClosePositionAs market-closes sz ("" = all) of an account's position.
func (s *Service) ClosePositionAs(_ context.Context, accountID, symbol, sz string, opts PlaceOpts) (engine.OrderView, []engine.Fill, error) {
	eng, err := s.running()
	if err != nil {
		return engine.OrderView{}, nil, err
	}
	return eng.ClosePositionWith(accountID, symbol, sz, opts)
}

// CancelAllAs cancels an account's open orders (in one market when symbol != "").
func (s *Service) CancelAllAs(_ context.Context, accountID, symbol string) (int, error) {
	eng, err := s.running()
	if err != nil {
		return 0, err
	}
	return eng.CancelAll(accountID, symbol)
}

// PlacedOrder is an order found by its client reference.
type PlacedOrder struct {
	ID, Symbol, Side, Status, Source string
	Sz, FilledSz                     decimal.Decimal
	CreatedAt                        time.Time
}

// OrderByClientRef finds the order an account placed with a ClientRef (the
// newest, if several). Orders are written in the same transaction as their
// fills, so after a crash a found order happened and a missing one didn't.
func OrderByClientRef(ctx context.Context, q db.Querier, accountID, clientRef string) (PlacedOrder, bool, error) {
	var o PlacedOrder
	var sz, filled string
	err := q.QueryRow(ctx, `SELECT o.id::text, m.symbol, o.side, o.status, o.source, o.sz::text, o.filled_sz::text, o.created_at
		FROM exchange_orders o JOIN exchange_markets m ON m.id = o.market_id
		WHERE o.account_id = $1 AND o.client_ref = $2 ORDER BY o.created_at DESC LIMIT 1`, accountID, clientRef).
		Scan(&o.ID, &o.Symbol, &o.Side, &o.Status, &o.Source, &sz, &filled, &o.CreatedAt)
	if db.IsNoRows(err) {
		return o, false, nil
	}
	if err != nil {
		return o, false, err
	}
	o.Sz, o.FilledSz = dec(sz), dec(filled)
	return o, true, nil
}

// ActivityOf is one person's cumulative trading on their main account
// (ActivityTotals for one person; sub-accounts are not theirs to count).
func ActivityOf(ctx context.Context, q db.Querier, userID uuid.UUID) (Activity, error) {
	a := Activity{UserID: userID}
	var vol string
	var first *time.Time
	err := q.QueryRow(ctx, `
		SELECT count(f.id), COALESCE(sum(f.px * f.sz), 0)::text, min(f.created_at),
		  (SELECT count(*) FROM exchange_liquidations l WHERE l.account_id = a.id)
		FROM exchange_accounts a LEFT JOIN exchange_fills f ON f.account_id = a.id
		WHERE a.user_id = $1 AND a.kind = 'user'
		GROUP BY a.id`, userID).Scan(&a.Fills, &vol, &first, &a.Liquidations)
	if db.IsNoRows(err) {
		return a, nil
	}
	if err != nil {
		return a, err
	}
	a.Volume = dec(vol)
	if first != nil {
		a.FirstFill = *first
	}
	return a, nil
}
