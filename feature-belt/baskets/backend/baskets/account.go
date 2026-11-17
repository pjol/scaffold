package baskets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/exchange"
)

// DeleteAccount: the person's baskets stay listed (other people trade them)
// with the creator removed; their entries are deleted; their reports stay as
// anonymous moderation records.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE baskets SET created_by = NULL, updated_at = now() WHERE created_by = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE baskets b SET entries = greatest(b.entries - 1, 0) FROM basket_entries e
 WHERE e.basket_id = b.id AND e.user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM basket_entries WHERE user_id = $1`, userID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE basket_reports SET reporter_id = NULL WHERE reporter_id = $1`, userID)
	return err
}

// ExportAccount returns the baskets the person created, their entries and their reports.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type created struct {
		Symbol      string     `json:"symbol"`
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Status      string     `json:"status"`
		EndsOn      *time.Time `json:"ends_on"`
		CreatedAt   time.Time  `json:"created_at"`
	}
	type entry struct {
		Symbol    string    `json:"symbol"`
		Source    string    `json:"source"`
		CreatedAt time.Time `json:"created_at"`
	}
	type report struct {
		Symbol     string     `json:"symbol"`
		Reason     string     `json:"reason"`
		Note       string     `json:"note"`
		CreatedAt  time.Time  `json:"created_at"`
		ResolvedAt *time.Time `json:"resolved_at"`
	}
	out := struct {
		Created []created `json:"created"`
		Entries []entry   `json:"entries"`
		Reports []report  `json:"reports"`
	}{Created: []created{}, Entries: []entry{}, Reports: []report{}}
	rows, err := s.d.Pool.Query(ctx, `SELECT symbol, name, description, status, ends_on, created_at FROM baskets WHERE created_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c created
		if err := rows.Scan(&c.Symbol, &c.Name, &c.Description, &c.Status, &c.EndsOn, &c.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Created = append(out.Created, c)
	}
	rows.Close()
	if rows, err = s.d.Pool.Query(ctx, `SELECT b.symbol, e.source, e.created_at FROM basket_entries e JOIN baskets b ON b.id = e.basket_id
 WHERE e.user_id = $1 ORDER BY e.created_at`, userID); err != nil {
		return nil, err
	}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Symbol, &e.Source, &e.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Entries = append(out.Entries, e)
	}
	rows.Close()
	if rows, err = s.d.Pool.Query(ctx, `SELECT b.symbol, r.reason, r.note, r.created_at, r.resolved_at FROM basket_reports r
 JOIN baskets b ON b.id = r.basket_id WHERE r.reporter_id = $1 ORDER BY r.created_at`, userID); err != nil {
		return nil, err
	}
	for rows.Next() {
		var r report
		if err := rows.Scan(&r.Symbol, &r.Reason, &r.Note, &r.CreatedAt, &r.ResolvedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Reports = append(out.Reports, r)
	}
	rows.Close()
	return out, rows.Err()
}

// SeedDev lists two development baskets over the exchange's development
// metals (GOLD and SILVER, priced by the oracle's GOLD-OZ and SILVER-OZ) and
// gives the second dev identity an entry on the first. It seeds the oracle
// and exchange first (their seeds are idempotent and run in file order,
// after this one), and prices lines from the catalog's development seed
// prices when the oracle hasn't published yet. Safe to run repeatedly.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) == 0 {
		return nil
	}
	for _, name := range []string{"oracle", "exchange"} {
		for _, m := range s.d.Modules {
			if sd, ok := m.(app.DevSeeder); ok && m.Name() == name {
				if err := sd.SeedDev(ctx, users); err != nil {
					return err
				}
			}
		}
	}
	ms, err := exchange.Markets(ctx, s.d.Pool)
	if err != nil {
		return err
	}
	seedPx := map[string]decimal.Decimal{}
	for _, m := range ms {
		seedPx[m.Symbol] = dec(m.SeedPrice)
	}
	if !seedPx["GOLD"].IsPositive() || !seedPx["SILVER"].IsPositive() {
		s.log.Info("baskets: dev seed skipped (no GOLD and SILVER markets)")
		return nil
	}
	s.markets.clear()
	seeds := []struct {
		name, desc string
		picks      []Pick
		users      []uuid.UUID
	}{
		{"Gold & silver", "One ounce of gold with forty of silver.", []Pick{{"GOLD", 1}, {"SILVER", 40}}, users[:min(2, len(users))]},
		{"Silver heavy", "", []Pick{{"GOLD", 1}, {"SILVER", 120}}, users[:1]},
	}
	for _, sd := range seeds {
		ev, err := s.evaluate(ctx, Input{Picks: sd.picks})
		if err != nil {
			return err
		}
		for i := range ev.items {
			if it := &ev.items[i]; !it.priced && it.status != "" { // the feed exists but hasn't published
				it.price, it.priced = seedPx[it.symbol], true
			}
		}
		ev.totals()
		if ev.unresolved > 0 || len(ev.unpriced) > 0 {
			s.log.Info("baskets: dev seed skipped", "basket", sd.name)
			continue
		}
		for _, u := range sd.users {
			if _, _, err := s.commit(ctx, u, "seed", ev, sd.name, sd.desc, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
