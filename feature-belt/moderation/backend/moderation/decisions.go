package moderation

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// DecisionView is one recorded screening decision as staff see it on a case.
type DecisionView struct {
	ID           uuid.UUID          `json:"id"`
	RulesVersion string             `json:"rules_version"`
	Action       string             `json:"action"`
	RulesAction  string             `json:"rules_action"`
	Labels       []string           `json:"labels"`
	Categories   []string           `json:"categories"`
	Hits         []Hit              `json:"hits"`
	Model        *string            `json:"model"`
	ModelStatus  string             `json:"model_status"`
	Answers      map[string]float64 `json:"answers"`
	Distrusted   bool               `json:"distrusted"`
	LatencyMs    *int64             `json:"latency_ms"`
	CostUSD      *string            `json:"cost_usd"`
	Edit         bool               `json:"edit"`
	CreatedAt    time.Time          `json:"created_at"`
}

// loadDecisions reads the latest decisions recorded for a target, newest first.
func loadDecisions(ctx context.Context, q db.Querier, kind, id string, limit int) ([]DecisionView, error) {
	rows, err := q.Query(ctx, `
		SELECT id, rules_version, action, rules_action, labels, categories, hits, model, model_status, answers,
		       distrusted, latency_ms, cost_usd, edit, created_at
		FROM moderation_decisions WHERE target_kind = $1 AND target_id = $2
		ORDER BY created_at DESC, id DESC LIMIT $3`, kind, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DecisionView{}
	for rows.Next() {
		var (
			d             DecisionView
			hits, answers []byte
			cost          *decimal.Decimal
		)
		if err := rows.Scan(&d.ID, &d.RulesVersion, &d.Action, &d.RulesAction, &d.Labels, &d.Categories, &hits, &d.Model,
			&d.ModelStatus, &answers, &d.Distrusted, &d.LatencyMs, &cost, &d.Edit, &d.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(hits, &d.Hits); err != nil {
			return nil, err
		}
		if len(answers) > 0 {
			if err := json.Unmarshal(answers, &d.Answers); err != nil {
				return nil, err
			}
		}
		if cost != nil {
			c := cost.StringFixed(6)
			d.CostUSD = &c
		}
		if d.Labels == nil {
			d.Labels = []string{}
		}
		if d.Categories == nil {
			d.Categories = []string{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- the person's held and removed content: GET /v1/moderation/content ----

// MyContent is something the caller posted that screening held or staff
// removed, as its author sees it.
type MyContent struct {
	CaseID     uuid.UUID  `json:"case_id"`
	TargetKind string     `json:"target_kind"`
	TargetID   string     `json:"target_id"`
	Text       string     `json:"text"` // the author's own text as captured
	Link       string     `json:"link"`
	State      string     `json:"state"` // in_review | removed | approved
	Reasons    []string   `json:"reasons"`
	CreatedAt  time.Time  `json:"created_at"`
	DecidedAt  *time.Time `json:"decided_at"`
}

func (s *Service) myContent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	list, err := myContent(ctx, s.d.Pool, me, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(m MyContent) any { return cursor{m.CreatedAt, m.CaseID} }))
	return nil
}

// myContent lists cases about the user's content that screening opened or
// that hid or removed it. Open report cases that changed nothing stay
// invisible to the author.
func myContent(ctx context.Context, q db.Querier, me uuid.UUID, before *time.Time, beforeID uuid.UUID, limit int) ([]MyContent, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.target_kind, c.target_id, c.snapshot, c.status, c.action, c.source, c.hidden_at IS NOT NULL,
		       c.created_at, c.resolved_at,
		       (SELECT d.hits FROM moderation_decisions d WHERE d.target_kind = c.target_kind AND d.target_id = c.target_id
		         ORDER BY d.created_at DESC LIMIT 1)
		FROM moderation_cases c
		WHERE c.author_id = $1 AND c.target_kind <> 'user'
		  AND (c.source = 'auto' OR c.hidden_at IS NOT NULL OR c.action = 'remove')
		  AND ($2::timestamptz IS NULL OR (c.created_at, c.id) < ($2, $3))
		ORDER BY c.created_at DESC, c.id DESC LIMIT $4`, me, before, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MyContent{}
	for rows.Next() {
		var (
			m              MyContent
			snap, hits     []byte
			status, source string
			action         *string
			hidden         bool
		)
		if err := rows.Scan(&m.CaseID, &m.TargetKind, &m.TargetID, &snap, &status, &action, &source, &hidden,
			&m.CreatedAt, &m.DecidedAt, &hits); err != nil {
			return nil, err
		}
		var sn Snapshot
		_ = json.Unmarshal(snap, &sn)
		m.Text, m.Link = clip(sn.Text, 280, 280), sn.Link
		switch {
		case status == "open":
			m.State = "in_review"
		case status == "dismissed":
			m.State = "approved"
		case hidden || (action != nil && *action == ActionRemove):
			m.State = "removed"
		default:
			m.State = "approved"
		}
		m.Reasons = []string{}
		var hs []Hit
		if len(hits) > 0 && json.Unmarshal(hits, &hs) == nil {
			for _, h := range hs {
				if h.Reason != "" && DecisionRank(h.Action) >= DecisionRank(DecisionHold) {
					m.Reasons = append(m.Reasons, h.Reason)
				}
			}
		}
		if len(m.Reasons) == 0 && source == "report" {
			m.Reasons = append(m.Reasons, "Reported by other people and reviewed by staff")
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---- decision model health and reset ----

func decisionModelHealth() DecisionModelHealth {
	d := currentDecider()
	if d == nil {
		return DecisionModelHealth{Status: ModelOff, CostUSD: "0", DayCostUSD: "0", DailyCapUSD: "0"}
	}
	if h, ok := d.(DeciderHealth); ok {
		return h.Health()
	}
	return DecisionModelHealth{Configured: true, Status: ModelOK, CostUSD: "0", DayCostUSD: "0", DailyCapUSD: "0"}
}

// resetDecisionModel: POST /v1/admin/moderation/decision-model/reset (admin).
// Turns a model that stopped itself (refused key, no credit) back on. Audited.
func (s *Service) resetDecisionModel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	d := currentDecider()
	rs, ok := d.(DeciderResetter)
	if !ok {
		return httpx.Conflict("The decision model isn't configured, so there's nothing to reset.")
	}
	before := decisionModelHealth()
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		return audit.Log(ctx, tx, staff.UserID, "moderation.decision_model.reset", "moderation_decision_model", before.Model,
			map[string]any{"status": before.Status, "note": before.Note})
	}); err != nil {
		return err
	}
	rs.Reset()
	httpx.OK(w, decisionModelHealth())
	return nil
}
