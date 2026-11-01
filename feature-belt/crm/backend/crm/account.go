package crm

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount: CRM records are org data and stay. The person's personal
// views go; their name comes off everything else (authorship, history,
// assignments, person-valued properties and mentions). The access feature
// clears them as record owner.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	stmts := []string{
		`DELETE FROM crm_views WHERE owner_id = $1 AND NOT shared`,
		`UPDATE crm_views SET owner_id = NULL WHERE owner_id = $1`,
		`UPDATE crm_activities SET assignee_id = NULL, reminder_at = NULL WHERE assignee_id = $1`,
		`UPDATE crm_activities SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_activities SET mentions = array_remove(mentions, $1) WHERE $1 = ANY (mentions)`,
		`UPDATE crm_events SET actor_id = NULL WHERE actor_id = $1`,
		`UPDATE crm_deal_stages SET entered_by = NULL WHERE entered_by = $1`,
		`UPDATE crm_imports SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_companies SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_contacts SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_deals SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_contact_companies SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_deal_contacts SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_pipelines SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_properties SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_tags SET created_by = NULL WHERE created_by = $1`,
		`UPDATE crm_settings SET updated_by = NULL WHERE updated_by = $1`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	// Person-valued custom properties that name them.
	rows, err := tx.Query(ctx, `SELECT org_id, object_type, key FROM crm_properties WHERE type = 'user'`)
	if err != nil {
		return err
	}
	type prop struct {
		org uuid.UUID
		o   objectType
		key string
	}
	var props []prop
	for rows.Next() {
		var p prop
		var ot string
		if err := rows.Scan(&p.org, &ot, &p.key); err != nil {
			rows.Close()
			return err
		}
		p.o = objectType(ot)
		props = append(props, p)
	}
	rows.Close()
	for _, p := range props {
		if _, err := tx.Exec(ctx, `UPDATE `+p.o.table()+` SET props = props - $2 WHERE org_id = $1 AND props->>$2 = $3`,
			p.org, p.key, userID.String()); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns what the person wrote and holds in CRMs: activities
// they logged, tasks assigned to them, records they own and their views.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type activity struct {
		Type       string  `json:"type"`
		Record     string  `json:"record"`
		Subject    string  `json:"subject"`
		Body       string  `json:"body"`
		OccurredAt string  `json:"occurred_at"`
		DueAt      *string `json:"due_at"`
		Done       bool    `json:"done"`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT a.type, a.record_type || ':' || a.record_id, a.subject, a.body, to_char(a.occurred_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'),
		       to_char(a.due_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), a.done_at IS NOT NULL
		FROM crm_activities a WHERE a.created_by = $1 OR a.assignee_id = $1
		ORDER BY a.occurred_at DESC LIMIT 5000`, userID)
	if err != nil {
		return nil, err
	}
	acts := []activity{}
	for rows.Next() {
		var a activity
		if err := rows.Scan(&a.Type, &a.Record, &a.Subject, &a.Body, &a.OccurredAt, &a.DueAt, &a.Done); err != nil {
			rows.Close()
			return nil, err
		}
		acts = append(acts, a)
	}
	rows.Close()
	type owned struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	ownedList := []owned{}
	for _, o := range objectTypes {
		rows, err := s.d.Pool.Query(ctx, `SELECT r.id, `+o.titleExpr()+` FROM `+o.table()+` r
			JOIN access_resources ar ON ar.kind = $2 AND ar.id = r.id WHERE ar.owner_id = $1 ORDER BY r.created_at LIMIT 5000`, userID, o.kind())
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var t string
			if err := rows.Scan(&id, &t); err != nil {
				rows.Close()
				return nil, err
			}
			ownedList = append(ownedList, owned{Kind: o.kind(), ID: id.String(), Title: t})
		}
		rows.Close()
	}
	type view struct {
		Object string `json:"object"`
		Name   string `json:"name"`
		Shared bool   `json:"shared"`
	}
	views := []view{}
	rows, err = s.d.Pool.Query(ctx, `SELECT object_type, name, shared FROM crm_views WHERE owner_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v view
		if err := rows.Scan(&v.Object, &v.Name, &v.Shared); err != nil {
			rows.Close()
			return nil, err
		}
		views = append(views, v)
	}
	rows.Close()
	return map[string]any{"activities": acts, "owned_records": ownedList, "views": views}, nil
}
