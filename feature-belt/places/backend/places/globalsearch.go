package places

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Search implements app.SearchProvider: approved places whose name or
// address matches, the rule of the public GET /places?q= list. Name matches
// rank above address matches. For an agent call each place must also be
// readable at the caller's agent scope.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimSpace(query)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT p.id, p.name, p.address, p.created_at
		FROM places p
		WHERE p.status = 'approved' AND (p.name || ' ' || p.address) ILIKE $1
		ORDER BY (p.name ILIKE $2) DESC, (p.name ILIKE $1) DESC, p.created_at DESC, p.id DESC
		LIMIT $3`, "%"+escapeLike(q)+"%", escapeLike(q)+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	_, agent := auth.AgentFrom(ctx)
	lq := strings.ToLower(q)
	out := []app.SearchResult{}
	for rows.Next() {
		var (
			id            uuid.UUID
			name, address string
			created       time.Time
		)
		if err := rows.Scan(&id, &name, &address, &created); err != nil {
			return nil, err
		}
		if agent {
			ok, err := app.AgentAllows(ctx, s.d.Modules, caller, agentFeature, agentKind, id.String(), app.AgentRead)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
		}
		score := 0.45 // address match
		switch ln := strings.ToLower(name); {
		case ln == lq:
			score = 0.9
		case strings.HasPrefix(ln, lq):
			score = 0.8
		case strings.Contains(ln, lq):
			score = 0.65
		}
		out = append(out, app.SearchResult{
			Kind: "places.place", ID: id.String(), Title: name, Snippet: address,
			Link: "/places/" + id.String(), Icon: "map-pin", Score: score, At: created,
		})
	}
	return out, rows.Err()
}
