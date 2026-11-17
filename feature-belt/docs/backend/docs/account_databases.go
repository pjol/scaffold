package docs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// deleteAccountDatabases removes a leaving person from database features:
// their automations stop (and keep no creator), their repeating templates
// stop, their date reminders go, and form responses lose who sent them.
func (s *Service) deleteAccountDatabases(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM docs_row_reminders WHERE user_id = $1`,
		`UPDATE docs_automations SET created_by = NULL, enabled = false, paused_reason = 'The person who set it up left.' WHERE created_by = $1`,
		`UPDATE docs_form_submissions SET user_id = NULL WHERE user_id = $1`,
		`UPDATE docs_pages SET repeat = NULL, repeat_next_at = NULL WHERE repeat->>'by' = $1::text`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// exportAccountDatabases: the automations a person set up and the forms
// they answered (row ids).
func (s *Service) exportAccountDatabases(ctx context.Context, userID uuid.UUID) (any, error) {
	type automation struct {
		ID         uuid.UUID `json:"id"`
		DatabaseID uuid.UUID `json:"database_id"`
		Name       string    `json:"name"`
		CreatedAt  time.Time `json:"created_at"`
	}
	type submission struct {
		ViewID    uuid.UUID  `json:"form_id"`
		RowID     *uuid.UUID `json:"row_id"`
		CreatedAt time.Time  `json:"created_at"`
	}
	out := struct {
		Automations     []automation `json:"automations"`
		FormSubmissions []submission `json:"form_submissions"`
	}{Automations: []automation{}, FormSubmissions: []submission{}}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, database_id, name, created_at FROM docs_automations WHERE created_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.Automations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[automation]); err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT view_id, row_id, created_at FROM docs_form_submissions WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	if out.FormSubmissions, err = pgx.CollectRows(rows, pgx.RowToStructByPos[submission]); err != nil {
		return nil, err
	}
	if out.Automations == nil {
		out.Automations = []automation{}
	}
	if out.FormSubmissions == nil {
		out.FormSubmissions = []submission{}
	}
	return out, nil
}
