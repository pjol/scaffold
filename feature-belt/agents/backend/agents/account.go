package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount removes the user's settings, overrides, agents seen and
// suggestions (deferred calls with their inputs). A pause a deleted staff
// member put on someone else stays: their users row is anonymised, not gone.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM agents_suggestions WHERE user_id = $1`,
		`DELETE FROM agents_resource_scopes WHERE user_id = $1`,
		`DELETE FROM agents_feature_scopes WHERE user_id = $1`,
		`DELETE FROM agents_clients WHERE user_id = $1`,
		`DELETE FROM agents_settings WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

type exportScope struct {
	Feature    string    `json:"feature"`
	Kind       string    `json:"kind,omitempty"`
	ResourceID string    `json:"resource_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Level      Level     `json:"level"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type exportSuggestion struct {
	ID        uuid.UUID       `json:"id"`
	Agent     string          `json:"agent"`
	Via       string          `json:"via"`
	Tool      string          `json:"tool"`
	Summary   string          `json:"summary"`
	Before    string          `json:"before,omitempty"`
	After     string          `json:"after,omitempty"`
	Input     json.RawMessage `json:"input"`
	Status    string          `json:"status"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	DecidedAt *time.Time      `json:"decided_at,omitempty"`
}

// ExportAccount returns the user's agent settings, overrides, the agents
// that acted for them and their suggestions.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	st, err := s.state(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	scopes := []exportScope{}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT feature, '', '', label, level, updated_at FROM agents_feature_scopes WHERE user_id = $1
		UNION ALL
		SELECT feature, kind, resource_id, title, level, updated_at FROM agents_resource_scopes WHERE user_id = $1
		ORDER BY 1, 2, 3`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e exportScope
		if err := rows.Scan(&e.Feature, &e.Kind, &e.ResourceID, &e.Title, &e.Level, &e.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		scopes = append(scopes, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	agents, err := s.agentsSeen(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	suggestions := []exportSuggestion{}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT id, client, via, tool, summary, before_text, after_text, input,
		       CASE WHEN status = 'pending' AND expires_at <= now() THEN 'expired' ELSE status END,
		       error_message, created_at, decided_at
		FROM agents_suggestions WHERE user_id = $1 ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e exportSuggestion
		var input []byte
		if err := rows.Scan(&e.ID, &e.Agent, &e.Via, &e.Tool, &e.Summary, &e.Before, &e.After, &input, &e.Status, &e.Error, &e.CreatedAt, &e.DecidedAt); err != nil {
			return nil, err
		}
		e.Input = json.RawMessage(input)
		suggestions = append(suggestions, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"default_level": st.Default,
		"paused":        st.UserPaused,
		"scopes":        scopes,
		"agents":        agents,
		"suggestions":   suggestions,
	}, nil
}
