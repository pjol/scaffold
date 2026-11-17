package crm

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/safehttp"
)

// Workflows: when a record is created, a property changes, a deal or ticket
// changes stage, or a date property is reached, records that match the
// conditions (list filters) are enrolled and the actions run in order, with
// delays between them. A workflow runs as its owner — the person who last
// turned it on — with exactly their access: a record they can't open isn't
// enrolled, and an action they couldn't do by hand fails in the run log.
//
// Loop protection: writes a workflow makes carry its id; they never enroll a
// workflow already in the chain, and chains stop at depth 3. Without
// re-enrollment a record enters a workflow once; with it, at most once an
// hour (date triggers once per date).

const (
	maxWorkflowDepth   = 3
	maxActions         = 20
	maxDelayMinutes    = 60 * 24 * 90
	workflowRunBatch   = 50
	reenrollAfter      = time.Hour
	webhookTimeout     = 10 * time.Second
	maxWorkflowsPerOrg = 200
	dateEnrollBatch    = 500
)

// WorkflowTrigger starts a workflow.
type WorkflowTrigger struct {
	Type       string     `json:"type"`                  // created | property_changed | stage_changed | date_reached
	Property   string     `json:"property,omitempty"`    // property_changed, date_reached
	StageID    *uuid.UUID `json:"stage_id,omitempty"`    // stage_changed: entered this stage (nil: any)
	OffsetDays int        `json:"offset_days,omitempty"` // date_reached: days after (negative: before) the date
}

// WorkflowAction is one step.
type WorkflowAction struct {
	Type string `json:"type"` // delay | set_property | create_task | rotate_owner | notify | add_tag | create_deal | create_ticket | webhook

	Minutes int `json:"minutes,omitempty"` // delay

	Property string          `json:"property,omitempty"` // set_property
	Value    json.RawMessage `json:"value,omitempty"`

	Title      string     `json:"title,omitempty"` // create_task; create_deal / create_ticket: name (merge fields allowed)
	Body       string     `json:"body,omitempty"`  // create_task, notify message
	DueDays    int        `json:"due_days,omitempty"`
	AssigneeID *uuid.UUID `json:"assignee_id,omitempty"` // create_task: nil = the record's owner
	Priority   string     `json:"priority,omitempty"`

	TeamID *uuid.UUID `json:"team_id,omitempty"` // rotate_owner: round robin among the team's members

	UserIDs     []uuid.UUID `json:"user_ids,omitempty"` // notify
	NotifyOwner bool        `json:"notify_owner,omitempty"`

	TagID *uuid.UUID `json:"tag_id,omitempty"` // add_tag

	PipelineID *uuid.UUID `json:"pipeline_id,omitempty"` // create_deal / create_ticket
	StageID    *uuid.UUID `json:"stage_id,omitempty"`

	URL string `json:"url,omitempty"` // webhook
}

// Workflow is a crm_workflows row.
type Workflow struct {
	ID         uuid.UUID        `json:"id"`
	OrgID      uuid.UUID        `json:"org_id"`
	Name       string           `json:"name"`
	ObjectType objectType       `json:"object_type"`
	Enabled    bool             `json:"enabled"`
	Trigger    WorkflowTrigger  `json:"trigger"`
	Conditions []Filter         `json:"conditions"`
	Actions    []WorkflowAction `json:"actions"`
	Reenroll   bool             `json:"reenroll"`
	Owner      *UserRef         `json:"owner"`
	Secret     *string          `json:"secret,omitempty"` // shown to people who manage workflows
	Stats      *WorkflowStats   `json:"stats"` // people who manage workflows only
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`

	ownerID *uuid.UUID
	secret  string
	state   map[string]int
}

// WorkflowStats counts runs.
type WorkflowStats struct {
	Enrolled int `json:"enrolled"`
	Waiting  int `json:"waiting"`
	Done     int `json:"done"`
	Failed   int `json:"failed"`
}

// WorkflowRunStep is a line of a run's log.
type WorkflowRunStep struct {
	Step    int       `json:"step"`
	Type    string    `json:"type"`
	Status  string    `json:"status"` // done | failed | skipped | waiting
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// WorkflowRun is one enrollment.
type WorkflowRun struct {
	ID         uuid.UUID         `json:"id"`
	Record     RecordRef         `json:"record"`
	Status     string            `json:"status"`
	Step       int               `json:"step"`
	ResumeAt   *time.Time        `json:"resume_at"`
	Log        []WorkflowRunStep `json:"log"`
	CreatedAt  time.Time         `json:"created_at"`
	FinishedAt *time.Time        `json:"finished_at"`
}

const workflowCols = `w.id, w.org_id, w.name, w.object_type, w.enabled, w.trigger, w.conditions, w.actions, w.reenroll, w.owner_id,
	u.display_name, u.avatar_url, w.secret, w.state, w.created_at, w.updated_at`

func scanWorkflow(row pgx.Row) (*Workflow, error) {
	wf := &Workflow{}
	var trig, conds, acts, state []byte
	var name, avatar *string
	if err := row.Scan(&wf.ID, &wf.OrgID, &wf.Name, &wf.ObjectType, &wf.Enabled, &trig, &conds, &acts, &wf.Reenroll, &wf.ownerID,
		&name, &avatar, &wf.secret, &state, &wf.CreatedAt, &wf.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trig, &wf.Trigger)
	_ = json.Unmarshal(conds, &wf.Conditions)
	_ = json.Unmarshal(acts, &wf.Actions)
	_ = json.Unmarshal(state, &wf.state)
	if wf.Conditions == nil {
		wf.Conditions = []Filter{}
	}
	if wf.Actions == nil {
		wf.Actions = []WorkflowAction{}
	}
	if wf.state == nil {
		wf.state = map[string]int{}
	}
	if wf.ownerID != nil && name != nil {
		wf.Owner = &UserRef{ID: *wf.ownerID, Name: *name, AvatarURL: avatar}
	}
	return wf, nil
}

const workflowFrom = `FROM crm_workflows w LEFT JOIN users u ON u.id = w.owner_id AND u.status <> 'deleted'`

// ---- context: the chain of workflows a write comes from -----------------------------

type chainKey struct{}

type workflowChain struct {
	ids   []uuid.UUID
	depth int
	name  string // the workflow writing now (timeline: "Sam (via workflow “…”)")
}

func chainFrom(ctx context.Context) workflowChain {
	c, _ := ctx.Value(chainKey{}).(workflowChain)
	return c
}

func withChain(ctx context.Context, wf *Workflow, depth int) context.Context {
	c := chainFrom(ctx)
	ids := append(append([]uuid.UUID{}, c.ids...), wf.ID)
	return context.WithValue(ctx, chainKey{}, workflowChain{ids: ids, depth: depth, name: wf.Name})
}

// ---- validation ----------------------------------------------------------------------

func newSecret() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return "whsec_" + hex.EncodeToString(b[:])
}

// validateWorkflow checks a workflow against its object's properties and the org.
func (s *Service) validateWorkflow(ctx context.Context, orgID, userID uuid.UUID, wf *Workflow) error {
	wf.Name = strings.TrimSpace(wf.Name)
	if wf.Name == "" || len([]rune(wf.Name)) > 120 {
		return httpx.BadRequest("Give the workflow a name of up to 120 characters.")
	}
	o, ok := parseObject(string(wf.ObjectType))
	if !ok {
		return httpx.BadRequest("object_type is company, contact, deal or ticket.")
	}
	wf.ObjectType = o
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
	if err != nil {
		return err
	}
	t := &wf.Trigger
	switch t.Type {
	case "created":
		t.Property, t.StageID, t.OffsetDays = "", nil, 0
	case "property_changed":
		p, ok := findProp(props, t.Property)
		if !ok || !p.Editable {
			return httpx.BadRequest("Choose a property people can change for the trigger.")
		}
		t.StageID, t.OffsetDays = nil, 0
	case "stage_changed":
		if !o.pipelined() {
			return httpx.BadRequest("Only deals and tickets change stage.")
		}
		if t.StageID != nil {
			st, err := loadStage(ctx, s.d.Pool, orgID, *t.StageID)
			if err != nil || st.Object != o {
				return httpx.BadRequest("Choose a stage from a " + o.label() + " pipeline.")
			}
		}
		t.Property, t.OffsetDays = "", 0
	case "date_reached":
		p, ok := findProp(props, t.Property)
		if !ok || (p.Type != "date" && p.Type != "datetime") {
			return httpx.BadRequest("Choose a date property for the trigger.")
		}
		if t.OffsetDays < -365 || t.OffsetDays > 365 {
			return httpx.BadRequest("The offset is up to 365 days before or after the date.")
		}
		t.StageID = nil
	default:
		return httpx.BadRequest("trigger.type is created, property_changed, stage_changed or date_reached.")
	}
	if len(wf.Conditions) > maxFilters {
		return httpx.BadRequest(fmt.Sprintf("Use at most %d conditions.", maxFilters))
	}
	b := newSQL(ctx, userID)
	if _, err := buildWhere(b, o, props, orgID, listSpec{Filters: wf.Conditions}, userID); err != nil {
		return err
	}
	if len(wf.Actions) == 0 || len(wf.Actions) > maxActions {
		return httpx.BadRequest(fmt.Sprintf("A workflow has 1 to %d actions.", maxActions))
	}
	dev := s.d.Cfg.IsDevelopment()
	for i := range wf.Actions {
		a := &wf.Actions[i]
		bad := func(msg string) error { return httpx.BadRequest(fmt.Sprintf("Action %d: %s", i+1, msg)) }
		switch a.Type {
		case "delay":
			if a.Minutes < 1 || a.Minutes > maxDelayMinutes {
				return bad("a delay is 1 minute to 90 days.")
			}
		case "set_property":
			p, ok := findProp(props, a.Property)
			if !ok || !p.Editable {
				return bad("choose a property people can change.")
			}
			if accessFields[a.Property] {
				return bad("use “Rotate owner” to change owners; teams change by hand.")
			}
			if len(a.Value) == 0 {
				a.Value = json.RawMessage("null")
			}
		case "create_task":
			a.Title = strings.TrimSpace(a.Title)
			if a.Title == "" || len([]rune(a.Title)) > 200 {
				return bad("give the task a title of up to 200 characters.")
			}
			if a.DueDays < 0 || a.DueDays > 365 {
				return bad("a task is due 0 to 365 days later.")
			}
			if a.Priority == "" {
				a.Priority = "none"
			}
			if !slices.Contains(priorities, a.Priority) {
				return bad("priority is none, low, medium or high.")
			}
			if a.AssigneeID != nil {
				if ok, err := isOrgMember(ctx, s.d.Pool, orgID, *a.AssigneeID); err != nil || !ok {
					return bad("assign the task to a member.")
				}
			}
		case "rotate_owner":
			if a.TeamID == nil {
				return bad("choose a team to rotate owners among.")
			}
			var exists bool
			if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_teams WHERE id = $1 AND org_id = $2)`, *a.TeamID, orgID).Scan(&exists); err != nil || !exists {
				return bad("choose a team from this workspace.")
			}
		case "notify":
			if len(a.UserIDs) == 0 && !a.NotifyOwner {
				return bad("choose who to notify.")
			}
			if len(a.UserIDs) > 25 {
				return bad("notify at most 25 people.")
			}
			for _, u := range a.UserIDs {
				if ok, err := isOrgMember(ctx, s.d.Pool, orgID, u); err != nil || !ok {
					return bad("notify members only.")
				}
			}
			a.Body = strings.TrimSpace(a.Body)
			if a.Body == "" || len([]rune(a.Body)) > 300 {
				return bad("write a message of up to 300 characters.")
			}
		case "add_tag":
			if a.TagID == nil {
				return bad("choose a tag.")
			}
			var exists bool
			if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_tags WHERE id = $1 AND org_id = $2)`, *a.TagID, orgID).Scan(&exists); err != nil || !exists {
				return bad("choose a tag from this workspace.")
			}
		case "create_deal", "create_ticket":
			target := objDeal
			if a.Type == "create_ticket" {
				target = objTicket
			}
			a.Title = strings.TrimSpace(a.Title)
			if a.Title == "" || len([]rune(a.Title)) > 200 {
				return bad("give the new " + target.label() + " a name of up to 200 characters.")
			}
			if a.StageID != nil {
				st, err := loadStage(ctx, s.d.Pool, orgID, *a.StageID)
				if err != nil || st.Object != target {
					return bad("choose a stage from a " + target.label() + " pipeline.")
				}
				a.PipelineID = &st.PipelineID
			}
		case "webhook":
			a.URL = strings.TrimSpace(a.URL)
			if _, err := safehttp.CheckURL(a.URL, dev, dev); err != nil {
				return bad("enter a public https URL.")
			}
		default:
			return bad("unknown action \"" + a.Type + "\".")
		}
	}
	return nil
}

// ---- routes --------------------------------------------------------------------------

func (s *Service) loadWorkflow(ctx context.Context, q db.Querier, orgID, id uuid.UUID) (*Workflow, error) {
	wf, err := scanWorkflow(q.QueryRow(ctx, `SELECT `+workflowCols+` `+workflowFrom+` WHERE w.id = $1 AND w.org_id = $2`, id, orgID))
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Workflow not found.")
	}
	return wf, err
}

func (s *Service) withStats(ctx context.Context, list []*Workflow, secret bool) error {
	for _, wf := range list {
		// Runs count records whatever their access, so only workflow managers see them.
		if !secret {
			continue
		}
		wf.Stats = &WorkflowStats{}
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE status IN ('waiting', 'running')),
			count(*) FILTER (WHERE status = 'done'), count(*) FILTER (WHERE status = 'failed') FROM crm_workflow_runs WHERE workflow_id = $1`, wf.ID).
			Scan(&wf.Stats.Enrolled, &wf.Stats.Waiting, &wf.Stats.Done, &wf.Stats.Failed); err != nil {
			return err
		}
		v := wf.secret
		wf.Secret = &v
	}
	return nil
}

// listWorkflows: GET /v1/crm/orgs/{orgID}/workflows (crm.access; secrets for crm.manage_workflows).
func (s *Service) listWorkflows(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	rows, err := s.d.Pool.Query(ctx, `SELECT `+workflowCols+` `+workflowFrom+` WHERE w.org_id = $1 ORDER BY lower(w.name), w.id LIMIT 500`, orgID)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Workflow, error) { return scanWorkflow(row) })
	if err != nil {
		return err
	}
	if list == nil {
		list = []*Workflow{}
	}
	if err := s.withStats(ctx, list, allowedHere(ctx, s.d.Pool, orgID, me(r), PermWorkflows)); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list, "can_manage": allowedHere(ctx, s.d.Pool, orgID, me(r), PermWorkflows)})
	return nil
}

func (s *Service) getWorkflow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := idParam(r, "workflowID")
	if err != nil {
		return err
	}
	wf, err := s.loadWorkflow(ctx, s.d.Pool, orgParam(r), id)
	if err != nil {
		return err
	}
	if err := s.withStats(ctx, []*Workflow{wf}, allowedHere(ctx, s.d.Pool, orgParam(r), me(r), PermWorkflows)); err != nil {
		return err
	}
	httpx.OK(w, wf)
	return nil
}

type workflowReq struct {
	Name       *string           `json:"name"`
	ObjectType *string           `json:"object_type"`
	Enabled    *bool             `json:"enabled"`
	Trigger    *WorkflowTrigger  `json:"trigger"`
	Conditions *[]Filter         `json:"conditions"`
	Actions    *[]WorkflowAction `json:"actions"`
	Reenroll   *bool             `json:"reenroll"`
}

func (req workflowReq) apply(wf *Workflow) {
	if req.Name != nil {
		wf.Name = *req.Name
	}
	if req.ObjectType != nil {
		wf.ObjectType = objectType(*req.ObjectType)
	}
	if req.Enabled != nil {
		wf.Enabled = *req.Enabled
	}
	if req.Trigger != nil {
		wf.Trigger = *req.Trigger
	}
	if req.Conditions != nil {
		wf.Conditions = *req.Conditions
	}
	if req.Actions != nil {
		wf.Actions = *req.Actions
	}
	if req.Reenroll != nil {
		wf.Reenroll = *req.Reenroll
	}
}

// createWorkflow: POST /v1/crm/orgs/{orgID}/workflows (crm.manage_workflows, audited). Starts off.
func (s *Service) createWorkflow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	var req workflowReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	wf := &Workflow{Conditions: []Filter{}}
	req.apply(wf)
	if err := s.validateWorkflow(ctx, orgID, me(r), wf); err != nil {
		return err
	}
	trig, _ := json.Marshal(wf.Trigger)
	conds, _ := json.Marshal(wf.Conditions)
	acts, _ := json.Marshal(wf.Actions)
	var id uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm_workflows WHERE org_id = $1`, orgID).Scan(&n); err != nil {
			return err
		}
		if n >= maxWorkflowsPerOrg {
			return httpx.BadRequest(orgNounA() + " can have at most 200 workflows.")
		}
		var owner *uuid.UUID
		if wf.Enabled {
			owner = ptr(me(r))
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm_workflows (org_id, name, object_type, enabled, trigger, conditions, actions, reenroll, owner_id, secret, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`, orgID, wf.Name, string(wf.ObjectType), wf.Enabled, trig, conds, acts,
			wf.Reenroll, owner, newSecret(), me(r)).Scan(&id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.workflow.create", "crm.workflow", id.String(), map[string]any{"org_id": orgID, "name": wf.Name, "enabled": wf.Enabled})
	})
	if err != nil {
		return err
	}
	out, err := s.loadWorkflow(ctx, s.d.Pool, orgID, id)
	if err != nil {
		return err
	}
	_ = s.withStats(ctx, []*Workflow{out}, true)
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// updateWorkflow: PATCH /v1/crm/orgs/{orgID}/workflows/{workflowID} (crm.manage_workflows,
// audited). Turning it on (or changing it while on) makes the caller its owner.
func (s *Service) updateWorkflow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "workflowID")
	if err != nil {
		return err
	}
	var req workflowReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		wf, err := scanWorkflow(tx.QueryRow(ctx, `SELECT `+workflowCols+` `+workflowFrom+` WHERE w.id = $1 AND w.org_id = $2 FOR UPDATE OF w`, id, orgID))
		if db.IsNoRows(err) {
			return httpx.NotFound("Workflow not found.")
		}
		if err != nil {
			return err
		}
		if req.ObjectType != nil && *req.ObjectType != string(wf.ObjectType) {
			return httpx.BadRequest("A workflow's record type can't change. Make a new workflow.")
		}
		wasOn := wf.Enabled
		req.apply(wf)
		if err := s.validateWorkflow(ctx, orgID, me(r), wf); err != nil {
			return err
		}
		owner := wf.ownerID
		if wf.Enabled {
			owner = ptr(me(r)) // whoever turns it on, or changes it while on, is who it runs as
		}
		trig, _ := json.Marshal(wf.Trigger)
		conds, _ := json.Marshal(wf.Conditions)
		acts, _ := json.Marshal(wf.Actions)
		if _, err := tx.Exec(ctx, `UPDATE crm_workflows SET name = $2, enabled = $3, trigger = $4, conditions = $5, actions = $6, reenroll = $7,
			owner_id = $8, updated_at = now() WHERE id = $1`, id, wf.Name, wf.Enabled, trig, conds, acts, wf.Reenroll, owner); err != nil {
			return err
		}
		if wasOn && !wf.Enabled { // turned off: waiting runs stop
			if _, err := tx.Exec(ctx, `UPDATE crm_workflow_runs SET status = 'stopped', finished_at = now(), updated_at = now(),
				log = log || jsonb_build_array(jsonb_build_object('step', step, 'type', 'stop', 'status', 'skipped', 'message', 'The workflow was turned off.', 'at', now()))
				WHERE workflow_id = $1 AND status = 'waiting'`, id); err != nil {
				return err
			}
		}
		return audit.Log(ctx, tx, me(r), "crm.workflow.update", "crm.workflow", id.String(), map[string]any{"org_id": orgID, "name": wf.Name, "enabled": wf.Enabled})
	})
	if err != nil {
		return err
	}
	out, err := s.loadWorkflow(ctx, s.d.Pool, orgID, id)
	if err != nil {
		return err
	}
	_ = s.withStats(ctx, []*Workflow{out}, true)
	httpx.OK(w, out)
	return nil
}

// deleteWorkflow: DELETE /v1/crm/orgs/{orgID}/workflows/{workflowID} (crm.manage_workflows, audited).
func (s *Service) deleteWorkflow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "workflowID")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM crm_workflows WHERE id = $1 AND org_id = $2 RETURNING name`, id, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Workflow not found.")
			}
			return err
		}
		return audit.Log(ctx, tx, me(r), "crm.workflow.delete", "crm.workflow", id.String(), map[string]any{"org_id": orgID, "name": name})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// listWorkflowRuns: GET /v1/crm/orgs/{orgID}/workflows/{workflowID}/runs?cursor= —
// newest first; records the caller can't open show without a name.
func (s *Service) listWorkflowRuns(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	id, err := idParam(r, "workflowID")
	if err != nil {
		return err
	}
	if _, err := s.loadWorkflow(ctx, s.d.Pool, orgID, id); err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	args := []any{id, page.Limit + 1}
	where := `workflow_id = $1`
	if page.Cursor != "" {
		var c struct {
			At time.Time `json:"at"`
			ID uuid.UUID `json:"id"`
		}
		if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
			return err
		}
		args = append(args, c.At, c.ID)
		where += ` AND (created_at, id) < ($3, $4)`
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, record_type, record_id, status, step, CASE WHEN status = 'waiting' THEN resume_at END, log, created_at, finished_at
		FROM crm_workflow_runs WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $2`, args...)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*WorkflowRun, error) {
		run := &WorkflowRun{}
		var raw []byte
		err := row.Scan(&run.ID, &run.Record.Type, &run.Record.ID, &run.Status, &run.Step, &run.ResumeAt, &raw, &run.CreatedAt, &run.FinishedAt)
		_ = json.Unmarshal(raw, &run.Log)
		if run.Log == nil {
			run.Log = []WorkflowRunStep{}
		}
		return run, err
	})
	if err != nil {
		return err
	}
	byType := map[objectType][]uuid.UUID{}
	for _, run := range list {
		byType[run.Record.Type] = append(byType[run.Record.Type], run.Record.ID)
	}
	for o, ids := range byType {
		refs, err := s.visibleRefs(ctx, s.d.Pool, me(r), o, ids)
		if err != nil {
			return err
		}
		for _, run := range list {
			if run.Record.Type == o {
				if ref, ok := refs[run.Record.ID]; ok {
					run.Record.Name = ref.Name
				} else {
					redactRun(run)
				}
			}
		}
	}
	if list == nil {
		list = []*WorkflowRun{}
	}
	httpx.OK(w, httpx.NewPage(list, page.Limit, func(run *WorkflowRun) any {
		return map[string]any{"at": run.CreatedAt, "id": run.ID}
	}))
	return nil
}

// stepNames names each action for a run log the viewer can't read in full.
var stepNames = map[string]string{"delay": "Wait", "set_property": "Set a property", "create_task": "Create a task", "rotate_owner": "Rotate the owner",
	"notify": "Notify people", "add_tag": "Add a tag", "create_deal": "Create a deal", "create_ticket": "Create a ticket", "webhook": "Send to a webhook"}

// redactRun strips what a run's log says about a record the viewer can't
// open (its name, task titles, values set), keeping each step's kind and
// outcome.
func redactRun(run *WorkflowRun) {
	run.Record.Name = ""
	for i := range run.Log {
		name := stepNames[run.Log[i].Type]
		if name == "" {
			name = "Step"
		}
		run.Log[i].Message = name + " · " + run.Log[i].Status
	}
}

// ---- enrolling ---------------------------------------------------------------------------

// recordEvent is a committed write workflows may react to.
type recordEvent struct {
	orgID   uuid.UUID
	o       objectType
	id      uuid.UUID
	kind    string // created | updated
	changes []change
	stage   *uuid.UUID // the stage entered, when the write moved stage
}

// matches reports whether t fires for e.
func (t WorkflowTrigger) matches(e recordEvent) bool {
	switch t.Type {
	case "created":
		return e.kind == "created"
	case "property_changed":
		if e.kind != "updated" {
			return false
		}
		for _, c := range e.changes {
			if c.Key == t.Property {
				return true
			}
		}
	case "stage_changed":
		if e.stage == nil {
			return false
		}
		return t.StageID == nil || *t.StageID == *e.stage
	}
	return false
}

// enroll starts matching enabled workflows for a committed write. ctx
// carries the chain of workflows the write came from.
func (s *Service) enroll(ctx context.Context, e recordEvent) error {
	chain := chainFrom(ctx)
	if chain.depth >= maxWorkflowDepth {
		slog.WarnContext(ctx, "crm: workflow chain stopped at its depth limit", "record_id", e.id)
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+workflowCols+` `+workflowFrom+` WHERE w.org_id = $1 AND w.object_type = $2 AND w.enabled
		AND w.trigger->>'type' <> 'date_reached'`, e.orgID, string(e.o))
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Workflow, error) { return scanWorkflow(row) })
	if err != nil {
		return err
	}
	enrolled := false
	for _, wf := range list {
		if !wf.Trigger.matches(e) || slices.Contains(chain.ids, wf.ID) {
			continue
		}
		ok, err := s.enrollOne(ctx, wf, e.o, e.id, e.id.String(), chain.depth)
		if err != nil {
			slog.WarnContext(ctx, "crm: workflow enroll failed", "workflow_id", wf.ID, "err", err)
			continue
		}
		enrolled = enrolled || ok
	}
	if enrolled {
		s.background(ctx, "crm.workflow_runs", s.runDueWorkflows)
	}
	return nil
}

// enrollOne enrolls a record when the owner can open it, it meets the
// conditions and the enrollment rules allow it.
func (s *Service) enrollOne(ctx context.Context, wf *Workflow, o objectType, id uuid.UUID, key string, depth int) (bool, error) {
	if wf.ownerID == nil {
		return false, nil
	}
	ok, err := s.meetsConditions(ctx, wf, *wf.ownerID, o, id)
	if err != nil || !ok {
		return false, err
	}
	var last *time.Time
	if err := s.d.Pool.QueryRow(ctx, `SELECT max(created_at) FROM crm_workflow_runs WHERE workflow_id = $1 AND enroll_key = $2`, wf.ID, key).Scan(&last); err != nil {
		return false, err
	}
	if last != nil && (!wf.Reenroll || time.Since(*last) < reenrollAfter) {
		return false, nil
	}
	_, err = s.d.Pool.Exec(ctx, `INSERT INTO crm_workflow_runs (workflow_id, org_id, record_type, record_id, enroll_key, depth) VALUES ($1, $2, $3, $4, $5, $6)`,
		wf.ID, wf.OrgID, string(o), id, key, depth+1)
	return err == nil, err
}

// meetsConditions: the record is visible to userID and matches the filters.
func (s *Service) meetsConditions(ctx context.Context, wf *Workflow, userID uuid.UUID, o objectType, id uuid.UUID) (bool, error) {
	props, err := s.loadProperties(ctx, s.d.Pool, wf.OrgID, o)
	if err != nil {
		return false, err
	}
	ctx = context.WithoutCancel(ctx)
	b := &sqlb{args: []any{userID, false}}
	where, err := buildWhere(b, o, props, wf.OrgID, listSpec{Filters: wf.Conditions}, userID)
	if err != nil {
		return false, err
	}
	var exists bool
	err = s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 `+visibleFrom(o)+` `+where+` AND r.id = `+b.arg(id)+`)`, b.args...).Scan(&exists)
	return exists, err
}

// enrollDates enrolls records whose date property (plus the offset) is today
// (UTC), once per date.
func (s *Service) enrollDates(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+workflowCols+` `+workflowFrom+` WHERE w.enabled AND w.trigger->>'type' = 'date_reached'`)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*Workflow, error) { return scanWorkflow(row) })
	if err != nil {
		return err
	}
	today := time.Now().UTC().Format("2006-01-02")
	enrolled := false
	for _, wf := range list {
		props, err := s.loadProperties(ctx, s.d.Pool, wf.OrgID, wf.ObjectType)
		if err != nil {
			return err
		}
		p, ok := findProp(props, wf.Trigger.Property)
		if !ok || wf.ownerID == nil {
			continue
		}
		e := p.expr
		args := []any{wf.OrgID, today, wf.Trigger.OffsetDays}
		if strings.Contains(e, "{K}") {
			args = append(args, p.Key)
			e = strings.ReplaceAll(e, "{K}", "$4::text")
		}
		d := "(" + e + ")::date"
		if p.Type == "datetime" {
			d = "((" + e + ") AT TIME ZONE 'UTC')::date"
		}
		ids, err := s.d.Pool.Query(ctx, `SELECT r.id FROM `+wf.ObjectType.table()+` r `+recordJoins(wf.ObjectType)+`
			WHERE r.org_id = $1 AND `+d+` + $3::int = $2::date LIMIT `+strconv.Itoa(dateEnrollBatch), args...)
		if err != nil {
			slog.WarnContext(ctx, "crm: date workflow query failed", "workflow_id", wf.ID, "err", err)
			continue
		}
		recs, err := pgx.CollectRows(ids, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range recs {
			ok, err := s.enrollOne(ctx, wf, wf.ObjectType, id, id.String()+"@"+today, 0)
			if err != nil {
				return err
			}
			enrolled = enrolled || ok
		}
	}
	if enrolled {
		return s.runDueWorkflows(ctx)
	}
	return nil
}

// ---- running ----------------------------------------------------------------------------

type claimedRun struct {
	id, workflowID, recordID uuid.UUID
	o                        objectType
	step, depth              int
	log                      []WorkflowRunStep
}

// runDueWorkflows runs every waiting run whose time has come (each claimed
// with SKIP LOCKED, so instances never run one twice).
func (s *Service) runDueWorkflows(ctx context.Context) error {
	for i := 0; i < 20; i++ {
		var runs []claimedRun
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `UPDATE crm_workflow_runs SET status = 'running', updated_at = now() WHERE id IN (
				SELECT id FROM crm_workflow_runs WHERE status = 'waiting' AND resume_at <= now() ORDER BY resume_at LIMIT $1 FOR UPDATE SKIP LOCKED)
				RETURNING id, workflow_id, record_type, record_id, step, depth, log`, workflowRunBatch)
			if err != nil {
				return err
			}
			runs, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (claimedRun, error) {
				var c claimedRun
				var raw []byte
				err := row.Scan(&c.id, &c.workflowID, &c.o, &c.recordID, &c.step, &c.depth, &raw)
				_ = json.Unmarshal(raw, &c.log)
				return c, err
			})
			return err
		})
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			return nil
		}
		for _, run := range runs {
			s.advance(ctx, run)
		}
	}
	return nil
}

// advance runs a claimed run's steps until a delay, the end or a failure.
func (s *Service) advance(ctx context.Context, run claimedRun) {
	ctx = context.WithoutCancel(ctx)
	finish := func(status string, resume *time.Time) {
		logJSON, _ := json.Marshal(run.log)
		var err error
		if status == "waiting" {
			_, err = s.d.Pool.Exec(ctx, `UPDATE crm_workflow_runs SET status = 'waiting', step = $2, resume_at = $3, log = $4, updated_at = now() WHERE id = $1`,
				run.id, run.step, *resume, logJSON)
		} else {
			_, err = s.d.Pool.Exec(ctx, `UPDATE crm_workflow_runs SET status = $2, step = $3, log = $4, finished_at = now(), updated_at = now() WHERE id = $1`,
				run.id, status, run.step, logJSON)
		}
		if err != nil {
			slog.WarnContext(ctx, "crm: saving a workflow run failed", "run_id", run.id, "err", err)
		}
	}
	note := func(typ, status, msg string) {
		run.log = append(run.log, WorkflowRunStep{Step: run.step, Type: typ, Status: status, Message: msg, At: time.Now().UTC()})
		if len(run.log) > 100 {
			run.log = run.log[len(run.log)-100:]
		}
	}
	var wf *Workflow
	var orgID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT org_id FROM crm_workflows WHERE id = $1`, run.workflowID).Scan(&orgID)
	if err == nil {
		wf, err = s.loadWorkflow(ctx, s.d.Pool, orgID, run.workflowID)
	}
	if err != nil {
		note("start", "failed", "The workflow is gone.")
		finish("failed", nil)
		return
	}
	if !wf.Enabled {
		note("start", "skipped", "The workflow was turned off.")
		finish("stopped", nil)
		return
	}
	if wf.ownerID == nil || !s.canRunWorkflows(ctx, wf.OrgID, *wf.ownerID) {
		note("start", "failed", "Its owner can't run workflows here any more. Turn it on again to run it as you.")
		finish("failed", nil)
		return
	}
	owner := *wf.ownerID
	actx := withChain(ctx, wf, run.depth)
	for run.step < len(wf.Actions) {
		a := wf.Actions[run.step]
		if a.Type == "delay" {
			resume := time.Now().Add(time.Duration(a.Minutes) * time.Minute)
			note("delay", "done", "Waiting until "+resume.UTC().Format("2 Jan 2006 15:04")+" UTC.")
			run.step++
			finish("waiting", &resume)
			return
		}
		msg, err := s.runAction(actx, wf, run.step, a, owner, run.o, run.recordID)
		if err != nil {
			text := err.Error()
			if he, ok := err.(*httpx.Error); ok {
				text = he.Message
			}
			note(a.Type, "failed", text)
			finish("failed", nil)
			return
		}
		note(a.Type, "done", msg)
		run.step++
	}
	finish("done", nil)
}

// canRunWorkflows: the owner is still a member with crm.access and crm.manage_workflows.
func (s *Service) canRunWorkflows(ctx context.Context, orgID, userID uuid.UUID) bool {
	ok, err := isOrgMember(ctx, s.d.Pool, orgID, userID)
	return err == nil && ok && allowedHere(ctx, s.d.Pool, orgID, userID, PermAccess) && allowedHere(ctx, s.d.Pool, orgID, userID, PermWorkflows)
}

// runAction does one action as the owner and says what it did.
func (s *Service) runAction(ctx context.Context, wf *Workflow, step int, a WorkflowAction, owner uuid.UUID, o objectType, id uuid.UUID) (string, error) {
	rec, err := s.getOne(ctx, s.d.Pool, owner, o, id)
	if err != nil {
		return "", httpx.NotFound("The record is gone or its owner can't open it.")
	}
	title := rec.base().Title
	switch a.Type {
	case "set_property":
		_, err := s.updateWith(ctx, owner, o, id, map[string]json.RawMessage{a.Property: a.Value}, false)
		if err != nil {
			return "", err
		}
		return "Set " + a.Property + " on “" + title + "”.", nil
	case "add_tag":
		tags := []string{}
		for _, t := range rec.base().Tags {
			tags = append(tags, t.ID.String())
		}
		if slices.Contains(tags, a.TagID.String()) {
			return "“" + title + "” already had the tag.", nil
		}
		raw, _ := json.Marshal(append(tags, a.TagID.String()))
		if _, err := s.updateWith(ctx, owner, o, id, map[string]json.RawMessage{"tags": raw}, false); err != nil {
			return "", err
		}
		return "Tagged “" + title + "”.", nil
	case "rotate_owner":
		members, err := access.TeamMemberIDs(ctx, s.d.Pool, *a.TeamID)
		if err != nil {
			return "", err
		}
		slices.SortFunc(members, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		if len(members) == 0 {
			return "", httpx.BadRequest("The team has no members.")
		}
		var next int
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			key := strconv.Itoa(step)
			if err := tx.QueryRow(ctx, `SELECT COALESCE((state->>$2)::int, 0) FROM crm_workflows WHERE id = $1 FOR UPDATE`, wf.ID, key).Scan(&next); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE crm_workflows SET state = jsonb_set(state, ARRAY[$2], to_jsonb($3::int)) WHERE id = $1`, wf.ID, key, next+1)
			return err
		})
		if err != nil {
			return "", err
		}
		pick := members[next%len(members)]
		raw, _ := json.Marshal(pick.String())
		if _, err := s.updateWith(ctx, owner, o, id, map[string]json.RawMessage{"owner": raw}, false); err != nil {
			return "", err
		}
		var name string
		_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, pick).Scan(&name)
		return "Made " + name + " the owner of “" + title + "”.", nil
	case "create_task":
		assignee := a.AssigneeID
		if assignee == nil {
			if rec.base().Owner == nil {
				assignee = &owner
			} else {
				assignee = &rec.base().Owner.ID
			}
		}
		due := time.Now().AddDate(0, 0, a.DueDays)
		subject, body, prio := s.mergeRecord(a.Title, rec), s.mergeRecord(a.Body, rec), a.Priority
		if _, err := s.logActivity(ctx, owner, o, id, activityReq{Type: "task", Subject: &subject, Body: &body, DueAt: &due,
			AssigneeID: assignee, Priority: &prio}); err != nil {
			return "", err
		}
		return "Created the task “" + subject + "”.", nil
	case "notify":
		users := append([]uuid.UUID{}, a.UserIDs...)
		if a.NotifyOwner && rec.base().Owner != nil {
			users = append(users, rec.base().Owner.ID)
		}
		sent := 0
		for _, u := range slices.Compact(users) {
			// Only people who can open the record hear about it.
			if lvl, err := access.LevelOf(ctx, s.d.Pool, u, o.kind(), id); err != nil || lvl < access.View {
				continue
			}
			s.send(ctx, notify.Notification{UserID: u, Kind: NotifyWorkflow, Title: clip(s.mergeRecord(a.Body, rec), 200),
				Body: wf.Name + " · " + title, Link: o.link(id), Data: map[string]any{"type": o, "id": id, "workflow_id": wf.ID}})
			sent++
		}
		return fmt.Sprintf("Notified %s.", plural(sent, "person", "people")), nil
	case "create_deal", "create_ticket":
		target := objDeal
		if a.Type == "create_ticket" {
			target = objTicket
		}
		name, _ := json.Marshal(s.mergeRecord(a.Title, rec))
		in := map[string]json.RawMessage{"name": name}
		if a.StageID != nil {
			in["stage"] = json.RawMessage(`"` + a.StageID.String() + `"`)
		}
		switch o {
		case objCompany:
			in["company_id"] = json.RawMessage(`"` + id.String() + `"`)
		case objContact:
			in["contact_ids"] = json.RawMessage(`["` + id.String() + `"]`)
			if c := rec.(*Contact).Company; c != nil {
				in["company_id"] = json.RawMessage(`"` + c.ID.String() + `"`)
			}
		case objDeal:
			if target == objTicket {
				in["deal_id"] = json.RawMessage(`"` + id.String() + `"`)
			}
			if c := rec.(*Deal).Company; c != nil {
				in["company_id"] = json.RawMessage(`"` + c.ID.String() + `"`)
			}
		}
		created, err := s.create(ctx, owner, wf.OrgID, target, in, false)
		if err != nil {
			return "", err
		}
		return "Created the " + target.label() + " “" + created.base().Title + "”.", nil
	case "webhook":
		return s.workflowWebhook(ctx, wf, a, rec)
	}
	return "", httpx.BadRequest("Unknown action.")
}

// mergeRecord fills {{record.name}} (and {{record.owner}}) in workflow text.
func (s *Service) mergeRecord(text string, rec recordish) string {
	b := rec.base()
	owner := ""
	if b.Owner != nil {
		owner = b.Owner.Name
	}
	return strings.NewReplacer("{{record.name}}", b.Title, "{{record.owner}}", owner).Replace(text)
}

// workflowWebhook POSTs the record (as its owner sees it) to the action's URL,
// signed with the workflow's secret: X-CRM-Signature: t=<unix>,v1=<hex HMAC-SHA256 of "t.body">.
func (s *Service) workflowWebhook(ctx context.Context, wf *Workflow, a WorkflowAction, rec recordish) (string, error) {
	dev := s.d.Cfg.IsDevelopment()
	if _, err := safehttp.CheckURL(a.URL, dev, dev); err != nil {
		return "", httpx.BadRequest("The webhook URL isn't allowed: " + err.Error())
	}
	body, err := json.Marshal(map[string]any{
		"workflow": map[string]any{"id": wf.ID, "name": wf.Name}, "record": rec, "sent_at": time.Now().UTC(),
	})
	if err != nil {
		return "", err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signWebhook(wf.secret, ts, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Scaffold App CRM workflows")
	req.Header.Set("X-CRM-Signature", "t="+ts+",v1="+sig)
	res, err := safehttp.Client(safehttp.Options{Timeout: webhookTimeout, MaxBody: 64 << 10, MaxRedirects: 0, AllowPrivate: dev, AllowHTTP: dev}).Do(req)
	if err != nil {
		if safehttp.IsBlocked(err) {
			return "", httpx.BadRequest("The webhook URL points somewhere private.")
		}
		return "", httpx.NewError(http.StatusBadGateway, "webhook_failed", "The webhook didn't answer.")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode >= 300 {
		return "", httpx.NewError(http.StatusBadGateway, "webhook_failed", fmt.Sprintf("The webhook answered %d.", res.StatusCode))
	}
	return fmt.Sprintf("Sent to the webhook (%d).", res.StatusCode), nil
}

func signWebhook(secret, ts string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}
