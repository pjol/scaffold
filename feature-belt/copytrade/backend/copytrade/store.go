package copytrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// Postgres access (2900_copytrade_init.sql). The engine-side runtime keeps
// open copies in memory and writes through.

var (
	errRowGone    = errors.New("copytrade: copy row gone")
	errActivePair = errors.New("copytrade: already copying this leader")
)

// copyRec is a copytrade_copies row.
type copyRec struct {
	ID, FollowerID, LeaderID        uuid.UUID
	AccountID                       string
	Status, StopReason, PauseReason string
	Settings                        Settings
	NetDeposits, GrossDeposits      decimal.Decimal
	PeakEquity, Equity              decimal.Decimal
	ProfitShare                     decimal.Decimal // fraction
	HWMGain, ProfitSharePaid        decimal.Decimal
	LastSettledAt                   time.Time
	Ignored                         map[string]int // symbol → sign of the leader's position at the start
	CloseOnStop, IdentityVisible    bool
	DisclosureVersion               string
	DisclosureAt, CreatedAt         time.Time
	UpdatedAt                       time.Time
	StoppedAt, AccountClosedAt      *time.Time
}

func (c copyRec) clone() copyRec {
	out := c
	out.Ignored = make(map[string]int, len(c.Ignored))
	for k, v := range c.Ignored {
		out.Ignored[k] = v
	}
	if c.Settings.StopLossPct != nil {
		v := *c.Settings.StopLossPct
		out.Settings.StopLossPct = &v
	}
	if c.Settings.MaxDrawdownPct != nil {
		v := *c.Settings.MaxDrawdownPct
		out.Settings.MaxDrawdownPct = &v
	}
	return out
}

// eventRec is a copytrade_events row.
type eventRec struct {
	ID, CopyID   uuid.UUID
	Kind         string
	Symbol, Side string
	Sz, Notional *decimal.Decimal
	OrderID      string
	LeaderFillID string
	SkipClass    string
	Reason       string
	LatencyMs    *int64
	At           time.Time
}

const copyCols = `id, follower_id, leader_id, account_id::text, status, stop_reason, pause_reason, settings, net_deposits::text,
	gross_deposits::text, peak_equity::text, equity::text, profit_share::text, hwm_gain::text, profit_share_paid::text,
	last_settled_at, ignored, close_on_stop, identity_visible, disclosure_version, disclosure_accepted_at, created_at, updated_at,
	stopped_at, account_closed_at`

func scanCopy(row pgx.Row) (copyRec, error) {
	var c copyRec
	var settings, ignored []byte
	var nd, gd, pk, eq, ps, hwm, paid string
	err := row.Scan(&c.ID, &c.FollowerID, &c.LeaderID, &c.AccountID, &c.Status, &c.StopReason, &c.PauseReason, &settings,
		&nd, &gd, &pk, &eq, &ps, &hwm, &paid, &c.LastSettledAt, &ignored, &c.CloseOnStop, &c.IdentityVisible,
		&c.DisclosureVersion, &c.DisclosureAt, &c.CreatedAt, &c.UpdatedAt, &c.StoppedAt, &c.AccountClosedAt)
	if err != nil {
		return c, err
	}
	_ = json.Unmarshal(settings, &c.Settings)
	_ = json.Unmarshal(ignored, &c.Ignored)
	if c.Ignored == nil {
		c.Ignored = map[string]int{}
	}
	c.NetDeposits, c.GrossDeposits, c.PeakEquity, c.Equity = dec(nd), dec(gd), dec(pk), dec(eq)
	c.ProfitShare, c.HWMGain, c.ProfitSharePaid = dec(ps), dec(hwm), dec(paid)
	return c, nil
}

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func insertCopy(ctx context.Context, q db.Querier, c copyRec) error {
	settings, _ := json.Marshal(c.Settings)
	ignored, _ := json.Marshal(c.Ignored)
	_, err := q.Exec(ctx, `INSERT INTO copytrade_copies (id, follower_id, leader_id, account_id, status, settings, net_deposits,
		  gross_deposits, peak_equity, equity, profit_share, last_settled_at, ignored, close_on_stop, identity_visible,
		  disclosure_version, disclosure_accepted_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		c.ID, c.FollowerID, c.LeaderID, c.AccountID, c.Status, settings, c.NetDeposits.String(), c.GrossDeposits.String(),
		c.PeakEquity.String(), c.Equity.String(), c.ProfitShare.String(), c.LastSettledAt, ignored, c.CloseOnStop,
		c.IdentityVisible, c.DisclosureVersion, c.DisclosureAt, c.CreatedAt, c.UpdatedAt)
	if db.IsUniqueViolation(err, "copytrade_copies_active_pair") {
		return errActivePair
	}
	return err
}

func updateCopy(ctx context.Context, q db.Querier, c copyRec) error {
	settings, _ := json.Marshal(c.Settings)
	ignored, _ := json.Marshal(c.Ignored)
	tag, err := q.Exec(ctx, `UPDATE copytrade_copies SET status = $2, stop_reason = $3, pause_reason = $4, settings = $5,
		  net_deposits = $6, gross_deposits = $7, peak_equity = $8, equity = $9, hwm_gain = $10, profit_share_paid = $11,
		  last_settled_at = $12, ignored = $13, close_on_stop = $14, updated_at = $15, stopped_at = $16, account_closed_at = $17
		WHERE id = $1`,
		c.ID, c.Status, c.StopReason, c.PauseReason, settings, c.NetDeposits.String(), c.GrossDeposits.String(),
		c.PeakEquity.String(), c.Equity.String(), c.HWMGain.String(), c.ProfitSharePaid.String(), c.LastSettledAt, ignored,
		c.CloseOnStop, c.UpdatedAt, c.StoppedAt, c.AccountClosedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return errRowGone
	}
	return err
}

func getCopy(ctx context.Context, q db.Querier, id uuid.UUID) (copyRec, bool, error) {
	c, err := scanCopy(q.QueryRow(ctx, `SELECT `+copyCols+` FROM copytrade_copies WHERE id = $1`, id))
	if db.IsNoRows(err) {
		return c, false, nil
	}
	return c, err == nil, err
}

func listCopies(ctx context.Context, q db.Querier, where string, args ...any) ([]copyRec, error) {
	rows, err := q.Query(ctx, `SELECT `+copyCols+` FROM copytrade_copies `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []copyRec
	for rows.Next() {
		c, err := scanCopy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// pageCursor is the keyset position of a (created_at, id) page.
type pageCursor struct {
	T  time.Time `json:"t"`
	ID string    `json:"id"`
}

// keyset appends "AND (col_t, col_id) < (cursor)" when the cursor is set.
func keyset(where string, args []any, c pageCursor, tCol, idCol string) (string, []any) {
	if c.ID == "" {
		return where, args
	}
	args = append(args, c.T, c.ID)
	return where + fmt.Sprintf(` AND (%s, %s) < ($%d, $%d::uuid)`, tCol, idCol, len(args)-1, len(args)), args
}

// batcher is a pool or a transaction.
type batcher interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

func insertEvents(ctx context.Context, q batcher, evs []eventRec) error {
	if len(evs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, e := range evs {
		b.Queue(`INSERT INTO copytrade_events (id, copy_id, kind, symbol, side, sz, notional, order_id, leader_fill_id, skip_class,
			  reason, latency_ms, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			e.ID, e.CopyID, e.Kind, e.Symbol, e.Side, decPtrString(e.Sz), decPtrString(e.Notional), nullUUID(e.OrderID),
			nullUUID(e.LeaderFillID), e.SkipClass, e.Reason, e.LatencyMs, e.At)
	}
	return q.SendBatch(ctx, b).Close()
}

func decPtrString(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func nullUUID(s string) any {
	if _, err := uuid.Parse(s); err != nil {
		return nil
	}
	return s
}

func listEvents(ctx context.Context, q db.Querier, copyID uuid.UUID, c pageCursor, limit int) ([]eventRec, error) {
	where, args := keyset(`WHERE copy_id = $1`, []any{copyID}, c, "created_at", "id")
	args = append(args, limit)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT id, copy_id, kind, symbol, side, sz::text, notional::text, COALESCE(order_id::text, ''),
		  COALESCE(leader_fill_id::text, ''), skip_class, reason, latency_ms, created_at
		FROM copytrade_events %s ORDER BY created_at DESC, id DESC LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRec
	for rows.Next() {
		var e eventRec
		var sz, notional *string
		if err := rows.Scan(&e.ID, &e.CopyID, &e.Kind, &e.Symbol, &e.Side, &sz, &notional, &e.OrderID, &e.LeaderFillID,
			&e.SkipClass, &e.Reason, &e.LatencyMs, &e.At); err != nil {
			return nil, err
		}
		if sz != nil {
			v := dec(*sz)
			e.Sz = &v
		}
		if notional != nil {
			v := dec(*notional)
			e.Notional = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func skipCounts(ctx context.Context, q db.Querier, copyID uuid.UUID, since time.Time) (SkipCounts, error) {
	var s SkipCounts
	rows, err := q.Query(ctx, `SELECT skip_class, count(*) FROM copytrade_events
		WHERE copy_id = $1 AND created_at >= $2 AND skip_class <> '' GROUP BY skip_class`, copyID, since)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var class string
		var n int
		if err := rows.Scan(&class, &n); err != nil {
			return s, err
		}
		s.add(class, n)
	}
	return s, rows.Err()
}

// ---- leaders ----

// leaderRec is a copytrade_leaders row.
type leaderRec struct {
	UserID                   uuid.UUID
	Enabled, ShowCopiers     bool
	Eligible                 bool
	ProfitSharePct, MinAlloc decimal.Decimal
	MaxCopiers               int
	EnabledAt                *time.Time
}

func (l leaderRec) settings() LeaderSettings {
	return LeaderSettings{Enabled: l.Enabled, ProfitSharePct: l.ProfitSharePct.String(), MinAllocation: l.MinAlloc.String(),
		MaxCopiers: l.MaxCopiers, ShowCopiers: l.ShowCopiers}
}

func getLeader(ctx context.Context, q db.Querier, userID uuid.UUID) (leaderRec, bool, error) {
	l := leaderRec{UserID: userID}
	var ps, mn string
	err := q.QueryRow(ctx, `SELECT enabled, show_copiers, eligible, profit_share_pct::text, min_allocation::text, max_copiers, enabled_at
		FROM copytrade_leaders WHERE user_id = $1`, userID).
		Scan(&l.Enabled, &l.ShowCopiers, &l.Eligible, &ps, &mn, &l.MaxCopiers, &l.EnabledAt)
	if db.IsNoRows(err) {
		return l, false, nil
	}
	if err != nil {
		return l, false, err
	}
	l.ProfitSharePct, l.MinAlloc = dec(ps), dec(mn)
	return l, true, nil
}

// person reads a user's display name and avatar.
func person(ctx context.Context, q db.Querier, id uuid.UUID) Person {
	p := Person{ID: id.String()}
	_ = q.QueryRow(ctx, `SELECT display_name, avatar_url FROM users WHERE id = $1`, id).Scan(&p.DisplayName, &p.AvatarURL)
	if p.DisplayName == "" {
		p.DisplayName = "Trader"
	}
	return p
}
