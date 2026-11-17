package exchange

import (
	"context"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Search implements app.SearchProvider: listed markets whose symbol, name or
// category match (market data is public, so every caller sees every market).
func (s *Service) Search(ctx context.Context, _ *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := s.d.Pool.Query(ctx, `
		SELECT symbol, name, category, updated_at, (upper(symbol) = upper($1)) AS exact
		FROM exchange_markets
		WHERE symbol ILIKE $2 ESCAPE '\' OR name ILIKE $2 ESCAPE '\' OR category ILIKE $2 ESCAPE '\'
		ORDER BY exact DESC, sort, symbol LIMIT $3`, q, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.SearchResult
	for rows.Next() {
		var sym, name, cat string
		var at time.Time
		var exact bool
		if err := rows.Scan(&sym, &name, &cat, &at, &exact); err != nil {
			return nil, err
		}
		score := 0.6
		if exact {
			score = 0.95
		}
		out = append(out, app.SearchResult{Kind: "exchange.market", ID: sym, Title: name + " · " + sym, Snippet: cat,
			Link: "/markets/" + sym, Icon: "candlestick-chart", Score: score, At: at})
	}
	return out, rows.Err()
}
