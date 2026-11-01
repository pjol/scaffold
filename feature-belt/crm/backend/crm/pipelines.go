package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Stage is one column of a pipeline.
type Stage struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Position    int       `json:"position"`
	Probability int       `json:"probability"`
	Outcome     string    `json:"outcome"` // open | won | lost
}

// Pipeline is an org's sequence of deal stages.
type Pipeline struct {
	ID       uuid.UUID `json:"id"`
	OrgID    uuid.UUID `json:"org_id"`
	Name     string    `json:"name"`
	Position int       `json:"position"`
	Stages   []Stage   `json:"stages"`
}

type stageInput struct {
	ID          *uuid.UUID `json:"id"`
	Name        string     `json:"name"`
	Probability int        `json:"probability"`
	Outcome     string     `json:"outcome"`
}

func defaultStages() []stageInput {
	return []stageInput{
		{Name: "Qualified", Probability: 10, Outcome: "open"},
		{Name: "Meeting booked", Probability: 25, Outcome: "open"},
		{Name: "Proposal sent", Probability: 50, Outcome: "open"},
		{Name: "Negotiation", Probability: 75, Outcome: "open"},
		{Name: "Won", Probability: 100, Outcome: "won"},
		{Name: "Lost", Probability: 0, Outcome: "lost"},
	}
}

func validateStages(in []stageInput) error {
	if len(in) == 0 || len(in) > 30 {
		return httpx.BadRequest("A pipeline needs 1 to 30 stages.")
	}
	seen := map[string]bool{}
	open := 0
	for i := range in {
		st := &in[i]
		st.Name = strings.TrimSpace(st.Name)
		if st.Name == "" || len([]rune(st.Name)) > 60 {
			return httpx.BadRequest("Give every stage a name of up to 60 characters.")
		}
		if seen[strings.ToLower(st.Name)] {
			return httpx.BadRequest(fmt.Sprintf("Two stages are called “%s”. Give each a different name.", st.Name))
		}
		seen[strings.ToLower(st.Name)] = true
		switch st.Outcome {
		case "", "open":
			st.Outcome = "open"
			open++
			if st.Probability < 0 || st.Probability > 100 {
				return httpx.BadRequest("Probability must be between 0 and 100.")
			}
		case "won":
			st.Probability = 100
		case "lost":
			st.Probability = 0
		default:
			return httpx.BadRequest("A stage's outcome is open, won or lost.")
		}
	}
	if open == 0 {
		return httpx.BadRequest("A pipeline needs at least one open stage.")
	}
	return nil
}

func createPipelineTx(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID, name string, stages []stageInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO crm_pipelines (org_id, name, position, created_by)
		VALUES ($1, $2, (SELECT COALESCE(max(position) + 1, 0) FROM crm_pipelines WHERE org_id = $1), $3) RETURNING id`,
		orgID, name, nullUUID(userID)).Scan(&id)
	if db.IsUniqueViolation(err, "crm_pipelines_name_key") {
		return id, httpx.Conflict("A pipeline with that name already exists.")
	}
	if err != nil {
		return id, err
	}
	for i, st := range stages {
		if _, err := tx.Exec(ctx, `INSERT INTO crm_stages (pipeline_id, org_id, name, position, probability, outcome) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, orgID, st.Name, i, st.Probability, st.Outcome); err != nil {
			return id, err
		}
	}
	return id, nil
}

func (s *Service) loadPipelines(ctx context.Context, q db.Querier, orgID uuid.UUID) ([]Pipeline, error) {
	rows, err := q.Query(ctx, `
		SELECT p.id, p.org_id, p.name, p.position, s.id, s.name, s.position, s.probability, s.outcome
		FROM crm_pipelines p JOIN crm_stages s ON s.pipeline_id = p.id
		WHERE p.org_id = $1 ORDER BY p.position, p.id, s.position, s.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pipeline{}
	for rows.Next() {
		var p Pipeline
		var st Stage
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.Position, &st.ID, &st.Name, &st.Position, &st.Probability, &st.Outcome); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].ID != p.ID {
			p.Stages = []Stage{}
			out = append(out, p)
		}
		out[len(out)-1].Stages = append(out[len(out)-1].Stages, st)
	}
	return out, rows.Err()
}

// listPipelines: GET /v1/crm/orgs/{orgID}/pipelines
func (s *Service) listPipelines(w http.ResponseWriter, r *http.Request) error {
	list, err := s.loadPipelines(r.Context(), s.d.Pool, orgParam(r))
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type pipelineReq struct {
	Name       *string                 `json:"name"`
	Position   *int                    `json:"position"`
	Stages     []stageInput            `json:"stages"`
	StageMoves map[uuid.UUID]uuid.UUID `json:"stage_moves"` // removed stage → stage its deals move to
}

// createPipeline: POST /v1/crm/orgs/{orgID}/pipelines {name, stages}
func (s *Service) createPipeline(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req pipelineReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.TrimSpace(deref(req.Name))
	if name == "" || len([]rune(name)) > 80 {
		return httpx.BadRequest("Give the pipeline a name of up to 80 characters.")
	}
	if req.Stages == nil {
		req.Stages = defaultStages()
	}
	if err := validateStages(req.Stages); err != nil {
		return err
	}
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_pipelines WHERE org_id = $1`, orgID).Scan(&n); err != nil {
			return err
		}
		if n >= 50 {
			return httpx.BadRequest(orgNounA() + " can have at most 50 pipelines.")
		}
		var err error
		if id, err = createPipelineTx(ctx, tx, orgID, me(r), name, req.Stages); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.pipeline.create", "crm.pipeline", id.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "pipelines")
	return s.respondPipeline(w, r, orgID, id, http.StatusCreated)
}

func (s *Service) respondPipeline(w http.ResponseWriter, r *http.Request, orgID, id uuid.UUID, status int) error {
	list, err := s.loadPipelines(r.Context(), s.d.Pool, orgID)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.ID == id {
			httpx.JSON(w, status, p)
			return nil
		}
	}
	return httpx.NotFound("Pipeline not found.")
}

// updatePipeline: PATCH /v1/crm/orgs/{orgID}/pipelines/{pipelineID}
// {name?, position?, stages?, stage_moves?}. stages replaces the list in
// order (ids keep a stage; no id adds one; a missing id removes it, and
// stage_moves says where its deals go).
func (s *Service) updatePipeline(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	pid, err := idParam(r, "pipelineID")
	if err != nil {
		return err
	}
	var req pipelineReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Stages != nil {
		if err := validateStages(req.Stages); err != nil {
			return err
		}
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var before string
		if err := tx.QueryRow(ctx, `SELECT name FROM crm_pipelines WHERE id = $1 AND org_id = $2 FOR UPDATE`, pid, orgID).Scan(&before); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Pipeline not found.")
			}
			return err
		}
		data := map[string]any{"org_id": orgID, "name": before}
		if req.Name != nil {
			name := strings.TrimSpace(*req.Name)
			if name == "" || len([]rune(name)) > 80 {
				return httpx.BadRequest("Give the pipeline a name of up to 80 characters.")
			}
			if _, err := tx.Exec(ctx, `UPDATE crm_pipelines SET name = $2, updated_at = now() WHERE id = $1`, pid, name); err != nil {
				if db.IsUniqueViolation(err, "crm_pipelines_name_key") {
					return httpx.Conflict("A pipeline with that name already exists.")
				}
				return err
			}
			data["new_name"] = name
		}
		if req.Position != nil {
			if _, err := tx.Exec(ctx, `UPDATE crm_pipelines SET position = $2, updated_at = now() WHERE id = $1`, pid, *req.Position); err != nil {
				return err
			}
		}
		if req.Stages != nil {
			if err := s.replaceStages(ctx, tx, me(r), orgID, pid, req.Stages, req.StageMoves); err != nil {
				return err
			}
			names := []string{}
			for _, st := range req.Stages {
				names = append(names, st.Name)
			}
			data["stages"] = names
		}
		return audit.Log(ctx, tx, me(r), "crm.pipeline.update", "crm.pipeline", pid.String(), data)
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "pipelines")
	s.boardChanged(ctx, pid)
	return s.respondPipeline(w, r, orgID, pid, http.StatusOK)
}

func (s *Service) replaceStages(ctx context.Context, tx pgx.Tx, userID, orgID, pid uuid.UUID, stages []stageInput, moves map[uuid.UUID]uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id FROM crm_stages WHERE pipeline_id = $1`, pid)
	if err != nil {
		return err
	}
	existing := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = true
	}
	rows.Close()
	kept := map[uuid.UUID]bool{}
	// Park positions out of the way, then write the new order.
	if _, err := tx.Exec(ctx, `UPDATE crm_stages SET position = position + 1000 WHERE pipeline_id = $1`, pid); err != nil {
		return err
	}
	for i, st := range stages {
		if st.ID != nil {
			if !existing[*st.ID] {
				return httpx.BadRequest("A stage in the list isn't part of this pipeline.")
			}
			kept[*st.ID] = true
			if _, err := tx.Exec(ctx, `UPDATE crm_stages SET name = $2, position = $3, probability = $4, outcome = $5, updated_at = now() WHERE id = $1`,
				*st.ID, st.Name, i, st.Probability, st.Outcome); err != nil {
				return err
			}
			continue
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO crm_stages (pipeline_id, org_id, name, position, probability, outcome) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			pid, orgID, st.Name, i, st.Probability, st.Outcome).Scan(&id); err != nil {
			return err
		}
		kept[id] = true
	}
	// Closed-state bookkeeping follows each stage's (possibly new) outcome.
	if _, err := tx.Exec(ctx, `UPDATE crm_deals d SET closed_at = CASE WHEN st.outcome = 'open' THEN NULL ELSE COALESCE(d.closed_at, now()) END
		FROM crm_stages st WHERE st.id = d.stage_id AND d.pipeline_id = $1
		AND (st.outcome = 'open') = (d.closed_at IS NOT NULL)`, pid); err != nil {
		return err
	}
	for id := range existing {
		if kept[id] {
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_deals WHERE stage_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			to, ok := moves[id]
			if !ok || !kept[to] {
				var name string
				_ = tx.QueryRow(ctx, `SELECT name FROM crm_stages WHERE id = $1`, id).Scan(&name)
				e := httpx.NewError(http.StatusConflict, "stage_has_deals",
					fmt.Sprintf("“%s” has %s. Choose a stage to move them to.", name, plural(n, "deal", "deals")))
				e.Details = map[string]any{"stage_id": id, "deal_count": n}
				return e
			}
			st, err := loadStage(ctx, tx, orgID, to)
			if err != nil {
				return asHTTP(err)
			}
			if err := bulkMoveStage(ctx, tx, userID, orgID, id, st); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_stages WHERE id = $1`, id); err != nil {
			return err
		}
	}
	return nil
}

// bulkMoveStage moves every deal in one stage to another (a removed stage).
func bulkMoveStage(ctx context.Context, tx pgx.Tx, userID, orgID, from uuid.UUID, to *stageInfo) error {
	rows, err := tx.Query(ctx, `SELECT d.id, st.name FROM crm_deals d JOIN crm_stages st ON st.id = d.stage_id WHERE d.stage_id = $1`, from)
	if err != nil {
		return err
	}
	type mv struct {
		id   uuid.UUID
		name string
	}
	var list []mv
	for rows.Next() {
		var m mv
		if err := rows.Scan(&m.id, &m.name); err != nil {
			rows.Close()
			return err
		}
		list = append(list, m)
	}
	rows.Close()
	for _, m := range list {
		d := &Deal{Record: Record{ID: m.id}, Stage: StageRef{ID: from, Name: m.name}}
		if err := moveDealStageTx(ctx, tx, userID, orgID, d, to); err != nil {
			return err
		}
	}
	return nil
}

// deletePipeline: DELETE /v1/crm/orgs/{orgID}/pipelines/{pipelineID} (no deals left).
func (s *Service) deletePipeline(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	pid, err := idParam(r, "pipelineID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `SELECT name FROM crm_pipelines WHERE id = $1 AND org_id = $2 FOR UPDATE`, pid, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Pipeline not found.")
			}
			return err
		}
		var deals, pipelines int
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM crm_deals WHERE pipeline_id = $1), (SELECT count(*) FROM crm_pipelines WHERE org_id = $2)`,
			pid, orgID).Scan(&deals, &pipelines); err != nil {
			return err
		}
		if pipelines <= 1 {
			return httpx.Conflict("Keep at least one pipeline.")
		}
		if deals > 0 {
			e := httpx.NewError(http.StatusConflict, "pipeline_has_deals", fmt.Sprintf("“%s” has %s. Move them to another pipeline first.", name, plural(deals, "deal", "deals")))
			e.Details = map[string]any{"deal_count": deals}
			return e
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm_pipelines WHERE id = $1`, pid); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.pipeline.delete", "crm.pipeline", pid.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	s.settingsChanged(ctx, orgID, "pipelines")
	httpx.NoContent(w)
	return nil
}

// ---- stage moves ------------------------------------------------------------------

// moveDealStageTx moves a deal to a stage (possibly in another pipeline),
// closing its open history row, opening a new one and writing the timeline.
func moveDealStageTx(ctx context.Context, tx pgx.Tx, userID, orgID uuid.UUID, d *Deal, to *stageInfo) error {
	var enteredAt time.Time
	err := tx.QueryRow(ctx, `UPDATE crm_deal_stages SET exited_at = now() WHERE deal_id = $1 AND exited_at IS NULL RETURNING entered_at`, d.ID).Scan(&enteredAt)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO crm_deal_stages (deal_id, org_id, pipeline_id, stage_id, stage_name, entered_by) VALUES ($1, $2, $3, $4, $5, $6)`,
		d.ID, orgID, to.PipelineID, to.ID, to.Name, nullUUID(userID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm_deals SET stage_id = $2, pipeline_id = $3, stage_entered_at = now(),
		closed_at = CASE WHEN $4 = 'open' THEN NULL ELSE now() END, updated_at = now() WHERE id = $1`,
		d.ID, to.ID, to.PipelineID, to.Outcome); err != nil {
		return err
	}
	data := map[string]any{"from_stage": d.Stage.Name, "to_stage": to.Name, "to_outcome": to.Outcome}
	if !enteredAt.IsZero() {
		data["seconds_in_stage"] = int64(time.Since(enteredAt).Seconds())
	}
	if d.PipelineID != uuid.Nil && d.PipelineID != to.PipelineID {
		var from, toName string
		_ = tx.QueryRow(ctx, `SELECT (SELECT name FROM crm_pipelines WHERE id = $1), (SELECT name FROM crm_pipelines WHERE id = $2)`, d.PipelineID, to.PipelineID).Scan(&from, &toName)
		data["from_pipeline"], data["to_pipeline"] = from, toName
	}
	return addEvent(ctx, tx, orgID, objDeal, d.ID, "stage_changed", userID, data)
}

type stageReq struct {
	StageID uuid.UUID `json:"stage_id"`
}

// moveStage: POST /v1/crm/deals/{id}/stage {stage_id} (edit) — the board's drag.
func (s *Service) moveStage(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	var req stageReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.StageID == uuid.Nil {
		return httpx.BadRequest("Choose a stage.")
	}
	rec, err := s.moveDeal(r.Context(), me(r), id, req.StageID)
	if err != nil {
		return err
	}
	httpx.OK(w, rec)
	return nil
}

// moveDeal is the stage move REST, bulk and agent tools share.
func (s *Service) moveDeal(ctx context.Context, userID, id, stageID uuid.UUID) (recordish, error) {
	return s.update(ctx, userID, objDeal, id, map[string]json.RawMessage{"stage": json.RawMessage(`"` + stageID.String() + `"`)})
}

// StageStay is one stay of a deal in a stage.
type StageStay struct {
	StageID   uuid.UUID  `json:"stage_id"`
	StageName string     `json:"stage_name"`
	EnteredAt time.Time  `json:"entered_at"`
	ExitedAt  *time.Time `json:"exited_at"`
	Seconds   int64      `json:"seconds"`
	By        *UserRef   `json:"by"`
}

// stageHistory: GET /v1/crm/deals/{id}/stages → stays (newest first) and the
// total time in each stage.
func (s *Service) stageHistory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "id")
	if err != nil {
		return err
	}
	if _, _, err := s.requireRecord(ctx, s.d.Pool, me(r), objDeal, id, access.View); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT h.stage_id, h.stage_name, h.entered_at, h.exited_at,
		       extract(epoch FROM COALESCE(h.exited_at, now()) - h.entered_at)::bigint, u.id, u.display_name, u.avatar_url
		FROM crm_deal_stages h LEFT JOIN users u ON u.id = h.entered_by AND u.status <> 'deleted'
		WHERE h.deal_id = $1 ORDER BY h.entered_at DESC, h.id DESC LIMIT 200`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	stays := []StageStay{}
	totals := map[uuid.UUID]int64{}
	names := map[uuid.UUID]string{}
	order := []uuid.UUID{}
	for rows.Next() {
		var st StageStay
		var uid *uuid.UUID
		var uname, avatar *string
		if err := rows.Scan(&st.StageID, &st.StageName, &st.EnteredAt, &st.ExitedAt, &st.Seconds, &uid, &uname, &avatar); err != nil {
			return err
		}
		if uid != nil && uname != nil {
			st.By = &UserRef{ID: *uid, Name: *uname, AvatarURL: avatar}
		}
		if _, ok := totals[st.StageID]; !ok {
			order = append(order, st.StageID)
		}
		totals[st.StageID] += st.Seconds
		names[st.StageID] = st.StageName
		stays = append(stays, st)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	type total struct {
		StageID   uuid.UUID `json:"stage_id"`
		StageName string    `json:"stage_name"`
		Seconds   int64     `json:"seconds"`
	}
	out := []total{}
	for i := len(order) - 1; i >= 0; i-- {
		out = append(out, total{StageID: order[i], StageName: names[order[i]], Seconds: totals[order[i]]})
	}
	httpx.OK(w, map[string]any{"stays": stays, "totals": out})
	return nil
}

// ---- board -------------------------------------------------------------------------

// MoneyTotal is a sum in one currency.
type MoneyTotal struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
	Weighted int64  `json:"weighted"`
}

// BoardColumn is one stage with its visible deals and their totals.
type BoardColumn struct {
	Stage      Stage        `json:"stage"`
	Count      int          `json:"count"`
	Totals     []MoneyTotal `json:"totals"`
	Deals      []recordish  `json:"deals"`
	NextCursor *string      `json:"next_cursor"`
}

// Board is GET /v1/crm/orgs/{orgID}/board.
type Board struct {
	Pipeline Pipeline      `json:"pipeline"`
	Columns  []BoardColumn `json:"columns"`
	Open     []MoneyTotal  `json:"open_totals"`
}

const boardColumnSize = 25

// board: GET /v1/crm/orgs/{orgID}/board?pipeline_id=&filters=&q=&tz= — every
// count, total and card comes from the visible-records query.
func (s *Service) board(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	spec, err := parseListSpec(r.URL.Query().Get)
	if err != nil {
		return err
	}
	var pid *uuid.UUID
	if v := r.URL.Query().Get("pipeline_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("pipeline_id must be a pipeline id.")
		}
		pid = &id
	}
	b, err := s.loadBoard(ctx, me(r), orgID, pid, spec)
	if err != nil {
		return err
	}
	httpx.OK(w, b)
	return nil
}

func (s *Service) loadBoard(ctx context.Context, userID, orgID uuid.UUID, pid *uuid.UUID, spec listSpec) (*Board, error) {
	board, err := s.boardTotals(ctx, userID, orgID, pid, spec)
	if err != nil {
		return nil, err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objDeal)
	if err != nil {
		return nil, err
	}
	for i := range board.Columns {
		col := &board.Columns[i]
		if col.Count == 0 {
			continue
		}
		cs := spec
		stageID := col.Stage.ID
		cs.where = append(append([]func(*sqlb) string{}, spec.where...), func(b *sqlb) string { return "r.stage_id = " + b.arg(stageID) })
		cs.SortKey, cs.SortDir, cs.Cursor, cs.Limit = "close_date", "asc", "", boardColumnSize
		page, err := s.queryRecords(ctx, s.d.Pool, userID, orgID, objDeal, props, cs, false)
		if err != nil {
			return nil, err
		}
		col.Deals, col.NextCursor = page.Items, page.NextCursor
	}
	return board, nil
}

// stageTotals sums visible deals per stage and currency.
func (s *Service) stageTotals(ctx context.Context, userID, orgID uuid.UUID, props []Property, spec listSpec) (map[uuid.UUID][]MoneyTotal, map[uuid.UUID]int, error) {
	b := newSQL(ctx, userID)
	where, err := buildWhere(b, objDeal, props, orgID, spec, userID)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.stage_id, r.currency, count(*), COALESCE(sum(r.amount), 0)::bigint,
		       COALESCE(sum(r.amount * st.probability / 100), 0)::bigint
		`+visibleFrom(objDeal)+` `+where+`
		GROUP BY r.stage_id, r.currency ORDER BY r.stage_id, count(*) DESC`, b.args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	totals := map[uuid.UUID][]MoneyTotal{}
	counts := map[uuid.UUID]int{}
	for rows.Next() {
		var id uuid.UUID
		var t MoneyTotal
		var n int
		if err := rows.Scan(&id, &t.Currency, &n, &t.Amount, &t.Weighted); err != nil {
			return nil, nil, err
		}
		counts[id] += n
		totals[id] = append(totals[id], t)
	}
	return totals, counts, rows.Err()
}
