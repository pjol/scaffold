package engine

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// What other modules build on (copy trading, automated strategies): orders
// placed on someone's behalf, sub-accounts, internal transfers, a fill hook
// and live account reads. Every write is one atomic command like the rest of
// the engine.

// Order sources: who placed an order. Modules may also pass their own name
// (lowercase letters, digits and _, at most 24 characters).
const (
	SourceUser        = "user"
	SourceAgent       = "agent" // an AI agent acting for the person (agent tools)
	SourceCopy        = "copy"  // the copytrade feature
	SourceLiquidation = "liquidation"
	SourceSystem      = "system" // close-outs (account deletion, CloseSubaccount)
)

var sourceRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)

// PlaceOpts qualify an order placed on someone's behalf (PlaceOrderWith).
type PlaceOpts struct {
	// Source is who placed it (Source* or a module's name); "" = SourceUser.
	// SourceLiquidation and SourceSystem are the engine's own.
	Source string
	// ClientRef is the caller's reference (a copy event id), persisted on the
	// order so exchange.OrderByClientRef finds it after a crash. ≤ 200 bytes.
	ClientRef string
	// AvoidMakers never trade with these accounts' resting orders: a crossing
	// one is cancelled instead (expire-maker), so a copy never fills against
	// its leader.
	AvoidMakers []string
}

const maxClientRef = 200

func (q *orderReq) withOpts(o PlaceOpts) error {
	switch {
	case o.Source == "":
		q.source = SourceUser
	case o.Source == SourceLiquidation || o.Source == SourceSystem:
		return reject("invalid", fmt.Sprintf("Order source %q is the engine's own.", o.Source))
	case !sourceRe.MatchString(o.Source):
		return reject("invalid", fmt.Sprintf("Order source %q must be lowercase letters, digits or _ (at most 24).", o.Source))
	default:
		q.source = o.Source
	}
	if len(o.ClientRef) > maxClientRef {
		return reject("invalid", "client_ref is too long.")
	}
	q.clientRef = o.ClientRef
	for _, id := range o.AvoidMakers {
		if id == "" {
			continue
		}
		if q.avoid == nil {
			q.avoid = map[string]bool{}
		}
		q.avoid[id] = true
	}
	return nil
}

// ---- fill hook ----

// FillEvent is one fill of a person's account (main or sub), delivered after
// the command that made it is committed.
type FillEvent struct {
	FillID, OrderID, TradeID string
	AccountID                string
	Kind                     string // KindUser | KindSub
	UserID                   string // the person (Account.Person); "" once they deleted their account
	Symbol, Side             string // side of this account
	Px, Sz, Fee, RealizedPnl decimal.Decimal
	Liquidity                string // maker | taker
	Liquidation              bool
	At                       time.Time
}

// SetFillHook installs f, called once per persisted fill of a person's
// account right after its command commits (never for a rolled-back one). It
// runs inside the engine lock: it must not block or call back into the
// engine (hand the event to a channel).
func (e *Engine) SetFillHook(f func(FillEvent)) {
	e.mu.Lock()
	e.onFill = f
	e.mu.Unlock()
}

func (e *Engine) fillEvent(f FillRec) FillEvent {
	ev := FillEvent{FillID: f.ID, OrderID: f.OrderID, TradeID: f.TradeID, AccountID: f.AccountID, Symbol: f.Symbol, Side: f.Side,
		Px: f.Px, Sz: f.Sz, Fee: f.Fee, RealizedPnl: f.RealizedPnl, Liquidity: f.Liquidity, Liquidation: f.Liquidation, At: msTime(f.Ts)}
	if a := e.accounts[f.AccountID]; a != nil {
		ev.Kind, ev.UserID = a.Kind, a.Person()
	}
	return ev
}

// ---- live reads ----

// PositionState is one open position.
type PositionState struct {
	Symbol   string
	Sz       decimal.Decimal // signed: long > 0
	EntryPx  decimal.Decimal
	MarkPx   decimal.Decimal
	Leverage int
}

// AccountState is an account at current marks.
type AccountState struct {
	ID, Kind, UserID, OwnerID, Label, Status string
	Balance, Deposits                        decimal.Decimal
	AccountValue                             decimal.Decimal // balance + unrealized PnL
	FreeCollateral                           decimal.Decimal // account value − initial margin − margin reserved by orders, ≥ 0
	Positions                                map[string]PositionState
	CreatedAt                                time.Time
}

func (e *Engine) state(a *Account) AccountState {
	r := e.risk(a, nil, "", 0)
	s := AccountState{ID: a.ID, Kind: a.Kind, UserID: a.UserID, OwnerID: a.OwnerID, Label: a.Label, Status: a.Status,
		Balance: a.Balance, Deposits: a.Deposits, AccountValue: r.av,
		FreeCollateral: decimal.Max(decimal.Zero, r.av.Sub(r.im).Sub(r.reserved)),
		Positions:      map[string]PositionState{}, CreatedAt: msTime(a.CreatedMs)}
	for sym, p := range a.Positions {
		if p.Sz.IsZero() {
			continue
		}
		mark := decimal.Zero
		if m := e.markets[sym]; m != nil {
			mark = m.mark
		}
		s.Positions[sym] = PositionState{Symbol: sym, Sz: p.Sz, EntryPx: p.EntryPx, MarkPx: mark, Leverage: p.Leverage}
	}
	return s
}

// State returns an account at current marks.
func (e *Engine) State(accountID string) (AccountState, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a := e.accounts[accountID]
	if a == nil {
		return AccountState{}, false
	}
	return e.state(a), true
}

// PositionOf is an account's signed size on symbol and the leverage it last
// used there (0 sz when flat). ok reports whether the account exists.
func (e *Engine) PositionOf(accountID, symbol string) (sz decimal.Decimal, lev int, ok bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a := e.accounts[accountID]
	if a == nil {
		return decimal.Zero, 0, false
	}
	if p := a.Positions[symbol]; p != nil {
		return p.Sz, p.Leverage, true
	}
	return decimal.Zero, 0, true
}

// Summaries returns many accounts' summaries under one read lock (unknown ids are left out).
func (e *Engine) Summaries(ids []string) map[string]AccountSummary {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]AccountSummary, len(ids))
	for _, id := range ids {
		if a := e.accounts[id]; a != nil {
			out[id] = e.summary(a)
		}
	}
	return out
}

// ---- sub-accounts ----

// ErrNoAccount: the person has no (active) main trading account.
var ErrNoAccount = &Error{Status: http.StatusUnprocessableEntity, Code: "no_account",
	Message: "There is no trading account to fund this from yet."}

// ErrPositionsOpen: CloseSubaccount could not close every position (no
// liquidity within the slippage limit, or a halted market). What it could do
// is done; call it again later.
var ErrPositionsOpen = &Error{Status: http.StatusConflict, Code: "positions_open",
	Message: "Some positions couldn't be closed yet (no liquidity within the slippage limit). Try again shortly."}

// OpenSubaccount returns the person's active sub-account for purpose,
// creating it (zero balance; fund it with Transfer) when there is none. The
// person must have an active main account.
func (e *Engine) OpenSubaccount(ownerUserID, purpose string) (string, error) {
	purpose = strings.TrimSpace(purpose)
	if ownerUserID == "" || purpose == "" || len(purpose) > 120 {
		return "", reject("invalid", "A sub-account needs an owner and a purpose (at most 120 characters).")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if main := e.byUser[ownerUserID]; main == nil || main.Status != AccountActive {
		return "", ErrNoAccount
	}
	for _, a := range e.accounts {
		if a.Kind == KindSub && a.OwnerID == ownerUserID && a.Label == purpose && a.Status == AccountActive {
			return a.ID, nil
		}
	}
	c := e.begin()
	a := e.newAccount(c, "", KindSub, purpose, decimal.Zero, "")
	a.OwnerID = ownerUserID
	if err := e.commit(c); err != nil {
		return "", err
	}
	return a.ID, nil
}

// Subaccounts lists the person's open sub-accounts, oldest first.
func (e *Engine) Subaccounts(ownerUserID string) []AccountState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []AccountState
	for _, a := range e.accounts {
		if a.Kind == KindSub && ownerUserID != "" && a.OwnerID == ownerUserID && a.Status != AccountClosed {
			out = append(out, e.state(a))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Transfer moves collateral between two accounts in one command:
//
//   - LedgerTransfer: a person's main account and one of their own
//     sub-accounts, either way;
//   - LedgerProfitShare: a sub-account to another person's main account.
//
// The amount must be positive and within the source's free collateral and
// balance. Both sides get a ledger row, and Deposits move with the balance,
// so neither account's PnL (account value − deposits) changes.
func (e *Engine) Transfer(fromID, toID string, amount decimal.Decimal, kind, note string) error {
	if !amount.IsPositive() {
		return reject("invalid", "amount must be positive.")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	from, to := e.accounts[fromID], e.accounts[toID]
	if from == nil || to == nil {
		return ErrNotFound
	}
	if from == to {
		return reject("invalid", "Can't transfer to the same account.")
	}
	if from.Status != AccountActive || to.Status != AccountActive {
		return ErrClosed
	}
	switch kind {
	case LedgerTransfer:
		own := func(sub, main *Account) bool {
			return sub.Kind == KindSub && main.Kind == KindUser && sub.OwnerID != "" && sub.OwnerID == main.UserID
		}
		if !own(from, to) && !own(to, from) {
			return reject("invalid", "Transfers go between a main account and its own sub-accounts.")
		}
	case LedgerProfitShare:
		if from.Kind != KindSub || to.Kind != KindUser || to.UserID == "" || from.OwnerID == to.UserID {
			return reject("invalid", "A profit share goes from a sub-account to another person's main account.")
		}
	default:
		return reject("invalid", fmt.Sprintf("Unknown transfer kind %q.", kind))
	}
	r := e.risk(from, nil, "", 0)
	free := decimal.Max(decimal.Zero, decimal.Min(r.av.Sub(r.im).Sub(r.reserved), from.Balance))
	if amount.GreaterThan(free) {
		return &Error{Status: http.StatusUnprocessableEntity, Code: "insufficient_collateral",
			Message: fmt.Sprintf("Only %s can be moved.", e.money(free))}
	}
	c := e.begin()
	e.move(c, from, to, amount, kind, note)
	return e.commit(c)
}

// move books a transfer of amount from → to (balance and deposits, a ledger row each side).
func (e *Engine) move(c *cmd, from, to *Account, amount decimal.Decimal, kind, note string) {
	now := e.nowMs()
	from.Balance, from.Deposits = from.Balance.Sub(amount), from.Deposits.Sub(amount)
	to.Balance, to.Deposits = to.Balance.Add(amount), to.Deposits.Add(amount)
	c.touchAccount(from)
	c.touchAccount(to)
	for _, x := range []struct {
		a   *Account
		amt decimal.Decimal
	}{{from, amount.Neg()}, {to, amount}} {
		if x.a.keepsHistory() {
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: x.a.ID, Kind: kind, Amount: x.amt, Note: note, Ts: now})
		}
	}
}

// CloseSubaccount closes one of a person's sub-accounts: open orders are
// cancelled, positions are market-closed into the book (reduce-only, within
// slippageBps of the mark; 0 = the default), and once it is flat the balance
// goes back to the owner's main account and the account is closed. A
// position that can't close (no liquidity, a halted market) stays open: the
// call commits what it did and returns ErrPositionsOpen.
func (e *Engine) CloseSubaccount(accountID string, slippageBps int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil || a.Kind != KindSub {
		return ErrNotFound
	}
	if a.Status == AccountClosed {
		return nil
	}
	c := e.begin()
	e.cancelAll(c, a, "")
	for _, p := range e.positionsByNotional(a) {
		q := orderReq{sym: p.Symbol, side: Sell, typ: TypeMarket, sz: p.Sz.Abs(), lev: p.Leverage, reduceOnly: true,
			slippageBps: slippageBps, source: SourceSystem}
		if p.Sz.IsNegative() {
			q.side = Buy
		}
		_, _, _ = e.place(c, a, q) // a rejection leaves the position for the next call
	}
	if hasPosition(a) {
		if err := e.commit(c); err != nil {
			return err
		}
		return ErrPositionsOpen
	}
	now := e.nowMs()
	switch main := e.byUser[a.OwnerID]; {
	case a.Balance.IsNegative():
		e.absorbBadDebt(c, a, now)
	case !a.Balance.IsPositive():
	case main != nil && a.OwnerID != "" && main.Status == AccountActive:
		e.move(c, a, main, a.Balance, LedgerTransfer, "sub-account closed")
	default: // the owner has no main account any more
		e.backstop.Balance = e.backstop.Balance.Add(a.Balance)
		c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerAdjustment, Amount: a.Balance.Neg(),
			Note: "account closed", Ts: now})
		a.Balance = decimal.Zero
		c.touchAccount(e.backstop)
	}
	a.Status = AccountClosed
	c.touchAccount(a)
	c.b.Closed = append(c.b.Closed, a.ID)
	return e.commit(c)
}
