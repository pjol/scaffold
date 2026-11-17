package exchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// Score is one user's standing on the exchange.
type Score struct {
	UserID       uuid.UUID
	Pnl          decimal.Decimal // account value − net deposits
	AccountValue decimal.Decimal
	Deposits     decimal.Decimal
	Traded       bool // has at least one fill
}

// Scores returns every active user account's PnL, valuing open positions at
// each market's latest candle close. It reads the database, so it works on
// any instance (a leaderboard source, for example; see FEATURE.md).
func Scores(ctx context.Context, q db.Querier) ([]Score, error) {
	rows, err := q.Query(ctx, `
		WITH marks AS (
		  SELECT m.id, (SELECT c FROM exchange_candles WHERE market_id = m.id ORDER BY t DESC LIMIT 1) AS px
		  FROM exchange_markets m)
		SELECT a.user_id, a.balance::text, a.deposits::text,
		  COALESCE(sum(p.sz * (COALESCE(k.px, p.entry_px) - p.entry_px)), 0)::text,
		  EXISTS (SELECT 1 FROM exchange_fills f WHERE f.account_id = a.id)
		FROM exchange_accounts a
		LEFT JOIN exchange_positions p ON p.account_id = a.id AND p.sz <> 0
		LEFT JOIN marks k ON k.id = p.market_id
		WHERE a.kind = 'user' AND a.status = 'active' AND a.user_id IS NOT NULL
		GROUP BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Score
	for rows.Next() {
		var sc Score
		var bal, dep, upnl string
		if err := rows.Scan(&sc.UserID, &bal, &dep, &upnl, &sc.Traded); err != nil {
			return nil, err
		}
		sc.Deposits = dec(dep)
		sc.AccountValue = dec(bal).Add(dec(upnl))
		sc.Pnl = sc.AccountValue.Sub(sc.Deposits)
		out = append(out, sc)
	}
	return out, rows.Err()
}

// LeaderboardBoard and LeaderboardScores make the exchange a source for the
// leaderboard feature when it is installed (it finds them among the modules):
// a "pnl" board ranking everyone who has traded by profit and loss.
func (s *Service) LeaderboardBoard() (id, title, unit string) {
	return "pnl", "Profit and loss", s.quoteCurrency
}

func (s *Service) LeaderboardScores(ctx context.Context) (map[uuid.UUID]decimal.Decimal, error) {
	scores, err := Scores(ctx, s.d.Pool)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]decimal.Decimal, len(scores))
	for _, sc := range scores {
		if sc.Traded {
			out[sc.UserID] = sc.Pnl.Round(s.params.QuoteDecimals)
		}
	}
	return out, nil
}
