package engine

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func (e *Engine) newAccount(c *cmd, userID, kind, label string, credit decimal.Decimal, note string) *Account {
	now := e.nowMs()
	a := &Account{
		ID: uuid.Must(uuid.NewV7()).String(), UserID: userID, Kind: kind, Label: label, Status: AccountActive,
		CreatedMs: now, Positions: map[string]*Position{}, Orders: map[string]*Order{},
	}
	e.indexAccount(a)
	c.created = append(c.created, a)
	c.b.NewAccounts = append(c.b.NewAccounts, a)
	if credit.IsPositive() {
		a.Balance = credit
		a.Deposits = credit
		if a.keepsHistory() {
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerFaucet, Amount: credit, Note: note, Ts: now})
		}
	}
	c.touchAccount(a)
	return a
}

func (e *Engine) ensureSystem(c *cmd, kind, label string, balance decimal.Decimal) *Account {
	if a := e.system[kind+"|"+label]; a != nil {
		return a
	}
	return e.newAccount(c, "", kind, label, balance, "")
}

// EnsureSystem returns the id of a non-user account (the simulation's market
// maker and traders), creating it with credit if needed.
func (e *Engine) EnsureSystem(kind, label string, credit decimal.Decimal) (string, error) {
	if kind == KindUser {
		return "", reject("invalid", "EnsureSystem is for non-user accounts.")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if a := e.system[kind+"|"+label]; a != nil {
		return a.ID, nil
	}
	c := e.begin()
	a := e.newAccount(c, "", kind, label, credit, "")
	if err := e.commit(c); err != nil {
		return "", err
	}
	return a.ID, nil
}

// AccountForUser returns the account id of a user, if they have one.
func (e *Engine) AccountForUser(userID string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a := e.byUser[userID]
	if a == nil {
		return "", false
	}
	return a.ID, true
}

// EnsureUser returns the account id for a user, creating the account (with
// the welcome credit, when one is configured) on first use.
func (e *Engine) EnsureUser(userID string) (string, bool, error) {
	if id, ok := e.AccountForUser(userID); ok {
		return id, false, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if a := e.byUser[userID]; a != nil {
		return a.ID, false, nil
	}
	c := e.begin()
	a := e.newAccount(c, userID, KindUser, "", e.p.WelcomeCredit, "welcome credit")
	if err := e.commit(c); err != nil {
		return "", false, err
	}
	return a.ID, true, nil
}

// Account returns an account by id (callers must not mutate it).
func (e *Engine) Account(id string) *Account {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.accounts[id]
}

// Faucet tops the balance back up to the faucet amount, once per cooldown.
// force skips the cooldown and works without a faucet (the simulation).
func (e *Engine) Faucet(accountID string, force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil {
		return ErrNotFound
	}
	if a.Status != AccountActive {
		return ErrClosed
	}
	amount := e.p.FaucetAmount
	if force && !amount.IsPositive() {
		amount = decimal.NewFromInt(1000)
	}
	if !amount.IsPositive() {
		return &Error{Status: http.StatusNotFound, Code: "faucet_disabled", Message: "There is no faucet on this exchange."}
	}
	now := e.nowMs()
	if !force && a.LastFaucetMs > 0 && now < a.LastFaucetMs+e.p.FaucetCooldown.Milliseconds() {
		return &Error{Status: http.StatusTooManyRequests, Code: "faucet_cooldown",
			Message: fmt.Sprintf("The faucet can be claimed once every %s.", e.p.FaucetCooldown)}
	}
	credit := amount.Sub(a.Balance)
	if !credit.IsPositive() {
		return &Error{Status: http.StatusUnprocessableEntity, Code: "faucet_not_needed",
			Message: "Your balance is already at the faucet amount."}
	}
	c := e.begin()
	a.Balance = a.Balance.Add(credit)
	a.Deposits = a.Deposits.Add(credit)
	a.LastFaucetMs = now
	c.touchAccount(a)
	if a.keepsHistory() {
		c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerFaucet, Amount: credit, Note: "faucet top-up", Ts: now})
	}
	return e.commit(c)
}

// ApplyTransfers settles collateral transfers in one command: deposits and
// adjustments always apply (creating the user's account if needed);
// withdrawals apply only when they leave the account's free collateral at
// or above zero.
func (e *Engine) ApplyTransfers(ts []Transfer) ([]TransferResult, error) {
	if len(ts) == 0 {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.begin()
	now := e.nowMs()
	out := make([]TransferResult, 0, len(ts))
	for _, t := range ts {
		res := TransferResult{ID: t.ID, Status: "applied", Ts: now}
		a := e.byUser[t.UserID]
		if a == nil && t.Kind != "withdrawal" {
			a = e.newAccount(c, t.UserID, KindUser, "", decimal.Zero, "")
		}
		switch {
		case a == nil:
			res.Status, res.Reason = "rejected", "No trading account."
		case a.Status != AccountActive:
			res.Status, res.Reason = "rejected", "The trading account is closed."
		case t.Kind == "withdrawal":
			amt := t.Amount.Abs()
			r := e.risk(a, nil, "", 0)
			free := r.av.Sub(r.im).Sub(r.reserved)
			if free.LessThan(amt) || a.Balance.LessThan(amt) {
				res.Status, res.Reason = "rejected", fmt.Sprintf("Only %s is free to withdraw.", e.money(decimal.Max(decimal.Zero, decimal.Min(free, a.Balance))))
				break
			}
			a.Balance = a.Balance.Sub(amt)
			a.Deposits = a.Deposits.Sub(amt)
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerWithdrawal, Amount: amt.Neg(), Note: t.Note, Ts: now})
		default: // deposit, adjustment
			kind := LedgerDeposit
			if t.Kind == "adjustment" {
				kind = LedgerAdjustment
			}
			a.Balance = a.Balance.Add(t.Amount)
			a.Deposits = a.Deposits.Add(t.Amount)
			c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: kind, Amount: t.Amount, Note: t.Note, Ts: now})
		}
		if a != nil {
			res.AccountID = a.ID
			c.touchAccount(a)
			ev := AccountEvent{Type: "transfer", Amount: e.money(t.Amount), Status: res.Status, Reason: res.Reason}
			id := a.ID
			c.emit(func(p Publisher) { p.AccountEvent(id, ev) })
		}
		out = append(out, res)
	}
	c.b.Transfers = append(c.b.Transfers, out...)
	if err := e.commit(c); err != nil {
		return nil, err
	}
	return out, nil
}

// CloseAccount closes out an account whose owner deleted their user account:
// open orders are cancelled, positions go to the backstop at mark (no fee),
// any remaining balance goes to the backstop, and the account is closed.
func (e *Engine) CloseAccount(accountID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	a := e.accounts[accountID]
	if a == nil || a.Kind != KindUser {
		return ErrNotFound
	}
	if a.Status == AccountClosed {
		return nil
	}
	c := e.begin()
	now := e.nowMs()
	e.cancelAll(c, a, "")
	for _, p := range e.positionsByNotional(a) {
		m := e.markets[p.Symbol]
		if m == nil || !m.mark.IsPositive() {
			continue
		}
		qty := p.Sz.Abs()
		o := e.liqOrder(a, m, p, qty)
		o.Liquidation = false
		bp := e.position(e.backstop, m)
		if bp.Leverage < m.spec.MaxLeverage {
			bp.Leverage = m.spec.MaxLeverage
		}
		e.execute(c, m, o, a, newID(), nil, e.backstop, qty, m.mark, false, decimal.Zero, decimal.Zero)
		o.Status = StatusFilled
		c.touchOrder(o)
		e.updateMark(m)
	}
	if !a.Balance.IsZero() {
		e.backstop.Balance = e.backstop.Balance.Add(a.Balance)
		c.b.Ledger = append(c.b.Ledger, LedgerRec{ID: newID(), AccountID: a.ID, Kind: LedgerAdjustment, Amount: a.Balance.Neg(),
			Note: "account closed", Ts: now})
		a.Balance = decimal.Zero
		c.touchAccount(e.backstop)
	}
	delete(e.byUser, a.UserID)
	a.UserID = ""
	a.Status = AccountClosed
	c.touchAccount(a)
	c.b.Closed = append(c.b.Closed, a.ID)
	return e.commit(c)
}
