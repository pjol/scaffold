package crm

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Lead scoring: rules of contact filters and points; a contact's score is
// the sum of the points of every rule it matches. Scores are recomputed when
// a contact changes, when the rules change, and every few hours (rules on
// dates move with time). Rules see every contact (a score is a property of
// the record, not of the viewer); "me" in a rule matches nobody.

// ScoreRule is a crm_score_rules row.
type ScoreRule struct {
	ID       uuid.UUID `json:"id"`
	Label    string    `json:"label"`
	Filters  []Filter  `json:"filters"`
	Points   int       `json:"points"`
	Position int       `json:"position"`
}

func (s *Service) loadScoreRules(ctx context.Context, q db.Querier, orgID uuid.UUID) ([]ScoreRule, error) {
	rows, err := q.Query(ctx, `SELECT id, label, filters, points, position FROM crm_score_rules WHERE org_id = $1 ORDER BY position, created_at, id`, orgID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ScoreRule, error) {
		var r ScoreRule
		var raw []byte
		err := row.Scan(&r.ID, &r.Label, &raw, &r.Points, &r.Position)
		_ = json.Unmarshal(raw, &r.Filters)
		if r.Filters == nil {
			r.Filters = []Filter{}
		}
		return r, err
	})
	if list == nil {
		list = []ScoreRule{}
	}
	return list, err
}

// scoreMatchSQL is a SELECT of the ids of org's contacts matching one rule.
func scoreMatchSQL(b *sqlb, props []Property, orgID uuid.UUID, rule ScoreRule) (string, error) {
	where, err := buildWhere(b, objContact, props, orgID, listSpec{Filters: rule.Filters}, uuid.Nil)
	if err != nil {
		return "", err
	}
	return `SELECT r.id FROM crm_contacts r ` + recordJoins(objContact) + ` ` + where, nil
}

// rescoreOrg recomputes every contact's score in an org.
func (s *Service) rescoreOrg(ctx context.Context, orgID uuid.UUID) error {
	rules, err := s.loadScoreRules(ctx, s.d.Pool, orgID)
	if err != nil {
		return err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objContact)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE crm_score_tmp (id uuid PRIMARY KEY, score int NOT NULL) ON COMMIT DROP`); err != nil {
			return err
		}
		for _, rule := range rules {
			b := &sqlb{} // no caller: rules see every contact
			sel, err := scoreMatchSQL(b, props, orgID, rule)
			if err != nil {
				slog.WarnContext(ctx, "crm: score rule no longer applies", "rule_id", rule.ID, "err", err)
				continue
			}
			pts := b.arg(rule.Points)
			if _, err := tx.Exec(ctx, `INSERT INTO crm_score_tmp (id, score) SELECT id, `+pts+`::int FROM (`+sel+`) m
				ON CONFLICT (id) DO UPDATE SET score = crm_score_tmp.score + EXCLUDED.score`, b.args...); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE crm_contacts c SET score = COALESCE(t.score, 0)
			FROM crm_contacts c2 LEFT JOIN crm_score_tmp t ON t.id = c2.id
			WHERE c.id = c2.id AND c.org_id = $1 AND c.score IS DISTINCT FROM COALESCE(t.score, 0)`, orgID)
		return err
	})
}

// rescoreContact recomputes one contact's score.
func (s *Service) rescoreContact(ctx context.Context, orgID, id uuid.UUID) error {
	rules, err := s.loadScoreRules(ctx, s.d.Pool, orgID)
	if err != nil || len(rules) == 0 {
		return err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objContact)
	if err != nil {
		return err
	}
	score := 0
	for _, rule := range rules {
		b := &sqlb{} // no caller: rules see every contact
		sel, err := scoreMatchSQL(b, props, orgID, rule)
		if err != nil {
			continue
		}
		var hit bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (`+sel+` AND r.id = `+b.arg(id)+`)`, b.args...).Scan(&hit); err != nil {
			return err
		}
		if hit {
			score += rule.Points
		}
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE crm_contacts SET score = $2 WHERE id = $1 AND score <> $2`, id, score)
	if err == nil && tag.RowsAffected() > 0 {
		s.recordChanged(ctx, objContact, id, false)
	}
	return err
}

// rescoreAll is the periodic job: every org with rules.
func (s *Service) rescoreAll(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT org_id FROM crm_score_rules`)
	if err != nil {
		return err
	}
	orgIDs, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range orgIDs {
		if err := s.rescoreOrg(ctx, id); err != nil {
			slog.WarnContext(ctx, "crm: rescoring failed", "org_id", id, "err", err)
		}
	}
	return nil
}

// listScoreRules: GET /v1/crm/orgs/{orgID}/score-rules (crm.access).
func (s *Service) listScoreRules(w http.ResponseWriter, r *http.Request) error {
	list, err := s.loadScoreRules(r.Context(), s.d.Pool, orgParam(r))
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type scoreRuleReq struct {
	Label    *string   `json:"label"`
	Filters  *[]Filter `json:"filters"`
	Points   *int      `json:"points"`
	Position *int      `json:"position"`
}

func (s *Service) validateRule(ctx context.Context, orgID uuid.UUID, rule *ScoreRule) error {
	rule.Label = strings.TrimSpace(rule.Label)
	if rule.Label == "" || len([]rune(rule.Label)) > 120 {
		return httpx.BadRequest("Give the rule a name of up to 120 characters.")
	}
	if rule.Points == 0 || rule.Points < -1000 || rule.Points > 1000 {
		return httpx.BadRequest("Points are -1,000 to 1,000, not 0.")
	}
	if len(rule.Filters) == 0 || len(rule.Filters) > 10 {
		return httpx.BadRequest("A rule has 1 to 10 conditions.")
	}
	for _, f := range rule.Filters {
		if f.Key == "score" {
			return httpx.BadRequest("A rule can't depend on the score itself.")
		}
		if strings.Contains(string(f.Value), `"me"`) {
			return httpx.BadRequest("Rules apply to everyone: choose people by name, not “me”.")
		}
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objContact)
	if err != nil {
		return err
	}
	b := &sqlb{} // no caller: rules see every contact
	_, err = scoreMatchSQL(b, props, orgID, *rule)
	return err
}

// saveScoreRule: POST /v1/crm/orgs/{orgID}/score-rules and PATCH …/{ruleID}
// (crm.manage_settings, audited); scores are recomputed right after.
func (s *Service) saveScoreRule(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req scoreRuleReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var id uuid.UUID
	isNew := chiParam(r, "ruleID") == ""
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		rule := ScoreRule{Filters: []Filter{}}
		if !isNew {
			var err error
			if id, err = idParam(r, "ruleID"); err != nil {
				return err
			}
			var raw []byte
			err = tx.QueryRow(ctx, `SELECT label, filters, points, position FROM crm_score_rules WHERE id = $1 AND org_id = $2 FOR UPDATE`, id, orgID).
				Scan(&rule.Label, &raw, &rule.Points, &rule.Position)
			if db.IsNoRows(err) {
				return httpx.NotFound("Rule not found.")
			}
			if err != nil {
				return err
			}
			_ = json.Unmarshal(raw, &rule.Filters)
		}
		if req.Label != nil {
			rule.Label = *req.Label
		}
		if req.Filters != nil {
			rule.Filters = *req.Filters
		}
		if req.Points != nil {
			rule.Points = *req.Points
		}
		if req.Position != nil {
			rule.Position = *req.Position
		}
		if err := s.validateRule(ctx, orgID, &rule); err != nil {
			return err
		}
		filters, _ := json.Marshal(rule.Filters)
		if isNew {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_score_rules WHERE org_id = $1`, orgID).Scan(&n); err != nil {
				return err
			}
			if n >= 100 {
				return httpx.BadRequest(orgNounA() + " can have at most 100 scoring rules.")
			}
			if err := tx.QueryRow(ctx, `INSERT INTO crm_score_rules (org_id, label, filters, points, position, created_by) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				orgID, rule.Label, filters, rule.Points, n, me(r)).Scan(&id); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE crm_score_rules SET label = $2, filters = $3, points = $4, position = $5, updated_at = now() WHERE id = $1`,
			id, rule.Label, filters, rule.Points, rule.Position); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.score_rule.save", "crm.score_rule", id.String(), map[string]any{"org_id": orgID, "label": rule.Label, "points": rule.Points})
	})
	if err != nil {
		return err
	}
	s.rescoreSoon(ctx, orgID)
	list, err := s.loadScoreRules(ctx, s.d.Pool, orgID)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, map[string]any{"items": list})
	return nil
}

// deleteScoreRule: DELETE /v1/crm/orgs/{orgID}/score-rules/{ruleID} (crm.manage_settings, audited).
func (s *Service) deleteScoreRule(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "ruleID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var label string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_score_rules WHERE id = $1 AND org_id = $2 RETURNING label`, id, orgID).Scan(&label); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Rule not found.")
			}
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.score_rule.delete", "crm.score_rule", id.String(), map[string]any{"org_id": orgID, "label": label})
	})
	if err != nil {
		return err
	}
	s.rescoreSoon(ctx, orgID)
	httpx.NoContent(w)
	return nil
}

func (s *Service) rescoreSoon(ctx context.Context, orgID uuid.UUID) {
	s.background(ctx, "crm.rescore", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		if err := s.rescoreOrg(ctx, orgID); err != nil {
			return err
		}
		s.settingsChanged(ctx, orgID, "scores")
		return nil
	})
}
