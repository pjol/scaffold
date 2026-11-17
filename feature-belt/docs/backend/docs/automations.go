package docs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/safehttp"
)

// Automations: when a row is added, a property changes (optionally to given
// values) or on a daily/weekly schedule, and the row matches the
// conditions, run actions — set a property, add a row, notify people, send
// a signed webhook. They run as their creator with that person's access at
// run time (a creator who can no longer edit the database pauses them),
// never as an agent; at most 60 runs a minute each; actions an automation
// takes can trigger others three levels deep, no further; every run is
// logged (30 days) and changes to automations are audited.

// AutomationTrigger: row_added, property_edited ({property, values: any of
// these new values, empty = any change}) or schedule ({every: day|week,
// weekdays, time, tz}).
type AutomationTrigger struct {
	Type     string            `json:"type"`
	Property string            `json:"property,omitempty"`
	Values   []json.RawMessage `json:"values,omitempty"`
	Every    string            `json:"every,omitempty"`
	Weekdays []int             `json:"weekdays,omitempty"`
	Time     string            `json:"time,omitempty"`
	TZ       string            `json:"tz,omitempty"`
}

// AutomationAction: set_property ({property, value} on the row; on a
// schedule, on every matching row), add_row ({database_id, values,
// template_id}), notify ({people, person_property, message}) or webhook
// ({url}).
type AutomationAction struct {
	Type           string                     `json:"type"`
	Property       string                     `json:"property,omitempty"`
	Value          json.RawMessage            `json:"value,omitempty"`
	DatabaseID     *uuid.UUID                 `json:"database_id,omitempty"`
	Values         map[string]json.RawMessage `json:"values,omitempty"`
	TemplateID     *uuid.UUID                 `json:"template_id,omitempty"`
	People         []uuid.UUID                `json:"people,omitempty"`
	PersonProperty string                     `json:"person_property,omitempty"`
	Message        string                     `json:"message,omitempty"`
	URL            string                     `json:"url,omitempty"`
}

type Automation struct {
	ID           uuid.UUID          `json:"id"`
	DatabaseID   uuid.UUID          `json:"database_id"`
	Name         string             `json:"name"`
	Enabled      bool               `json:"enabled"`
	Trigger      AutomationTrigger  `json:"trigger"`
	Conditions   []Filter           `json:"conditions"`
	Actions      []AutomationAction `json:"actions"`
	Secret       string             `json:"secret,omitempty"` // signs webhooks; shown to editors
	NextRunAt    *time.Time         `json:"next_run_at"`
	PausedReason *string            `json:"paused_reason"`
	CreatedBy    *uuid.UUID         `json:"created_by"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	LastRunAt    *time.Time         `json:"last_run_at"`
}

type AutomationRun struct {
	ID        uuid.UUID  `json:"id"`
	RowID     *uuid.UUID `json:"row_id"`
	Status    string     `json:"status"`
	Detail    string     `json:"detail"`
	CreatedAt time.Time  `json:"created_at"`
}

const (
	maxAutomations      = 25
	maxAutomationRunMin = 60
	maxAutomationDepth  = 3
)

type automationDepthKey struct{}

func automationDepth(ctx context.Context) int {
	d, _ := ctx.Value(automationDepthKey{}).(int)
	return d
}

const automationCols = `id, database_id, name, enabled, trigger, conditions, actions, secret, next_run_at, paused_reason, created_by, created_at, updated_at, last_run_at`

func scanAutomations(rows pgx.Rows) ([]Automation, error) {
	defer rows.Close()
	out := []Automation{}
	for rows.Next() {
		var a Automation
		var trig, conds, acts []byte
		if err := rows.Scan(&a.ID, &a.DatabaseID, &a.Name, &a.Enabled, &trig, &conds, &acts, &a.Secret, &a.NextRunAt, &a.PausedReason,
			&a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.LastRunAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(trig, &a.Trigger)
		_ = json.Unmarshal(conds, &a.Conditions)
		_ = json.Unmarshal(acts, &a.Actions)
		if a.Conditions == nil {
			a.Conditions = []Filter{}
		}
		if a.Actions == nil {
			a.Actions = []AutomationAction{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- routes ------------------------------------------------------------------------

// automationDB checks edit on the database (automations are an editor's
// tool) and refuses agents: an automation runs later without an agent's cap.
func (s *Service) automationDB(ctx context.Context, me, dbID uuid.UUID) (*Page, error) {
	if access.ViaAgent(ctx) {
		return nil, httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't set up automations.")
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, dbID, access.Edit)
	if err != nil {
		return nil, err
	}
	if dbp.Kind != "database" {
		return nil, errNotDatabase
	}
	return dbp, nil
}

// listAutomations: GET /docs/databases/{id}/automations (edit).
func (s *Service) listAutomations(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.automationDB(ctx, me, id); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+automationCols+` FROM docs_automations WHERE database_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return err
	}
	list, err := scanAutomations(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type automationReq struct {
	Name       *string             `json:"name"`
	Enabled    *bool               `json:"enabled"`
	Trigger    *AutomationTrigger  `json:"trigger"`
	Conditions *[]Filter           `json:"conditions"`
	Actions    *[]AutomationAction `json:"actions"`
}

// cleanAutomation validates an automation against its database: the
// trigger's property, conditions, and each action's target (databases the
// creator can edit, people in the workspace, https webhooks).
func (s *Service) cleanAutomation(ctx context.Context, me uuid.UUID, dbp *Page, a *Automation) error {
	a.Name = strings.TrimSpace(cleanText(a.Name))
	if a.Name == "" {
		a.Name = "Automation"
	}
	if utf8.RuneCountInString(a.Name) > 100 {
		return httpx.BadRequest("Give the automation a name under 100 characters.")
	}
	t := &a.Trigger
	switch t.Type {
	case "row_added":
		t.Property, t.Values, t.Every, t.Weekdays, t.Time, t.TZ = "", nil, "", nil, "", ""
	case "property_edited":
		p, ok := findProp(dbp.Schema, t.Property)
		if !ok || !writable(p) && p.Type != "title" {
			return httpx.BadRequest("Choose a property whose changes start it.")
		}
		if len(t.Values) > 20 {
			t.Values = t.Values[:20]
		}
		t.Every, t.Weekdays, t.Time, t.TZ = "", nil, "", ""
	case "schedule":
		if t.Every != "day" && t.Every != "week" {
			return httpx.BadRequest("Run it every day or every week.")
		}
		rep, err := cleanRepeat(Repeat{Every: t.Every, Interval: 1, Weekdays: t.Weekdays, Time: t.Time, TZ: t.TZ})
		if err != nil {
			return err
		}
		t.Weekdays, t.Property, t.Values = rep.Weekdays, "", nil
		next := nextRepeat(*rep, time.Now())
		a.NextRunAt = &next
	default:
		return httpx.BadRequest("Start it when a row is added, a property changes, or on a schedule.")
	}
	if t.Type != "schedule" {
		a.NextRunAt = nil
	}
	n := 0
	a.Conditions = cleanFilters(dbp.Schema, a.Conditions, 1, &n)
	if len(a.Actions) == 0 || len(a.Actions) > 10 {
		return httpx.BadRequest("An automation does 1 to 10 things.")
	}
	for i := range a.Actions {
		act := &a.Actions[i]
		switch act.Type {
		case "set_property":
			p, ok := findProp(dbp.Schema, act.Property)
			if !ok || (!writable(p) && p.Type != "title") {
				return httpx.BadRequest("Choose a property for each “Edit property” step.")
			}
			if p.Type != "title" && p.Type != "relation" {
				var v any
				_ = json.Unmarshal(act.Value, &v)
				raw, _ := json.Marshal(resolveDynamic(v, me, t.TZ))
				if _, err := s.cleanValue(ctx, dbp.OrgID, p, raw); err != nil {
					return err
				}
			}
		case "add_row":
			if act.DatabaseID == nil {
				return httpx.BadRequest("Choose the database each “Add row” step adds to.")
			}
			target, err := requirePage(ctx, s.d.Pool, me, *act.DatabaseID, access.Edit)
			if err != nil || target.Kind != "database" || target.OrgID != dbp.OrgID {
				return httpx.BadRequest("Choose a database you can edit for each “Add row” step.")
			}
		case "notify":
			act.Message = strings.TrimSpace(cleanText(act.Message))
			if utf8.RuneCountInString(act.Message) > 500 {
				return httpx.BadRequest("Keep the notification under 500 characters.")
			}
			act.People = dedupeIDs(act.People)
			if len(act.People) > 50 {
				return httpx.BadRequest("Notify up to 50 people.")
			}
			if len(act.People) > 0 {
				var n int
				if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, dbp.OrgID, act.People).Scan(&n); err != nil {
					return err
				}
				if n != len(act.People) {
					return httpx.BadRequest("Notify people in this workspace.")
				}
			}
			if act.PersonProperty != "" {
				if p, ok := findProp(dbp.Schema, act.PersonProperty); !ok || (p.Type != "person" && p.Type != "created_by" && p.Type != "last_edited_by") {
					return httpx.BadRequest("Choose a person property to notify.")
				}
			}
			if len(act.People) == 0 && act.PersonProperty == "" {
				return httpx.BadRequest("Choose who each “Notify” step tells.")
			}
		case "webhook":
			u, err := safehttp.CheckURL(strings.TrimSpace(act.URL), s.d.Cfg.IsDevelopment(), s.d.Cfg.IsDevelopment())
			if err != nil {
				return httpx.BadRequest("Webhooks go to a public https address.")
			}
			act.URL = u.String()
		default:
			return httpx.BadRequest("Each step edits a property, adds a row, notifies people or sends a webhook.")
		}
	}
	return nil
}

func newSecret() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// createAutomation: POST /docs/databases/{id}/automations (edit).
func (s *Service) createAutomation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req automationReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	dbp, err := s.automationDB(ctx, me, id)
	if err != nil {
		return err
	}
	if err := editable(dbp, true); err != nil {
		return err
	}
	a := Automation{DatabaseID: id, Enabled: true, Conditions: []Filter{}}
	if req.Name != nil {
		a.Name = *req.Name
	}
	if req.Trigger != nil {
		a.Trigger = *req.Trigger
	}
	if req.Conditions != nil {
		a.Conditions = *req.Conditions
	}
	if req.Actions != nil {
		a.Actions = *req.Actions
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if err := s.cleanAutomation(ctx, me, dbp, &a); err != nil {
		return err
	}
	var out []Automation
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM docs_automations WHERE database_id = $1`, id).Scan(&n); err != nil {
			return err
		}
		if n >= maxAutomations {
			return httpx.Conflict(fmt.Sprintf("A database has up to %d automations.", maxAutomations))
		}
		trig, _ := json.Marshal(a.Trigger)
		conds, _ := json.Marshal(a.Conditions)
		acts, _ := json.Marshal(a.Actions)
		rows, err := tx.Query(ctx, `INSERT INTO docs_automations (database_id, name, enabled, trigger, conditions, actions, secret, next_run_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+automationCols, id, a.Name, a.Enabled, trig, conds, acts, newSecret(), a.NextRunAt, me)
		if err != nil {
			return err
		}
		if out, err = scanAutomations(rows); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "docs.automation.create", "docs.automation", out[0].ID.String(), map[string]any{"database_id": id, "name": a.Name})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, out[0])
	return nil
}

func (s *Service) automationFor(ctx context.Context, me, id uuid.UUID) (*Automation, *Page, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+automationCols+` FROM docs_automations WHERE id = $1`, id)
	if err != nil {
		return nil, nil, err
	}
	list, err := scanAutomations(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(list) == 0 {
		return nil, nil, httpx.NotFound("Automation not found.")
	}
	dbp, err := s.automationDB(ctx, me, list[0].DatabaseID)
	if err != nil {
		if he, ok := err.(*httpx.Error); ok && he.Status == http.StatusNotFound {
			return nil, nil, httpx.NotFound("Automation not found.")
		}
		return nil, nil, err
	}
	return &list[0], dbp, nil
}

// updateAutomation: PATCH /docs/automations/{automationID} (edit on its
// database). Whoever saves it becomes the person it runs as.
func (s *Service) updateAutomation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "automationID")
	if err != nil {
		return err
	}
	var req automationReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, dbp, err := s.automationFor(ctx, me, id)
	if err != nil {
		return err
	}
	if req.Name != nil {
		a.Name = *req.Name
	}
	if req.Trigger != nil {
		a.Trigger = *req.Trigger
	}
	if req.Conditions != nil {
		a.Conditions = *req.Conditions
	}
	if req.Actions != nil {
		a.Actions = *req.Actions
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if err := s.cleanAutomation(ctx, me, dbp, a); err != nil {
		return err
	}
	var out []Automation
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		trig, _ := json.Marshal(a.Trigger)
		conds, _ := json.Marshal(a.Conditions)
		acts, _ := json.Marshal(a.Actions)
		rows, err := tx.Query(ctx, `UPDATE docs_automations SET name = $2, enabled = $3, trigger = $4, conditions = $5, actions = $6, next_run_at = $7,
			created_by = $8, paused_reason = NULL, updated_at = now() WHERE id = $1 RETURNING `+automationCols,
			id, a.Name, a.Enabled, trig, conds, acts, a.NextRunAt, me)
		if err != nil {
			return err
		}
		if out, err = scanAutomations(rows); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "docs.automation.update", "docs.automation", id.String(), map[string]any{"database_id": a.DatabaseID, "enabled": a.Enabled})
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out[0])
	return nil
}

// deleteAutomation: DELETE /docs/automations/{automationID} (edit).
func (s *Service) deleteAutomation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "automationID")
	if err != nil {
		return err
	}
	a, _, err := s.automationFor(ctx, me, id)
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM docs_automations WHERE id = $1`, id); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "docs.automation.delete", "docs.automation", id.String(), map[string]any{"database_id": a.DatabaseID, "name": a.Name})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// automationRuns: GET /docs/automations/{automationID}/runs (edit): the
// last 100 runs.
func (s *Service) automationRuns(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "automationID")
	if err != nil {
		return err
	}
	if _, _, err := s.automationFor(ctx, me, id); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, row_id, status, detail, created_at FROM docs_automation_runs WHERE automation_id = $1 ORDER BY created_at DESC, id DESC LIMIT 100`, id)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AutomationRun])
	if err != nil {
		return err
	}
	if list == nil {
		list = []AutomationRun{}
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

// ---- running -----------------------------------------------------------------------

// rowAdded starts row_added automations (after the request; never fails it).
func (s *Service) rowAdded(ctx context.Context, dbp *Page, row *Page, actor uuid.UUID) {
	s.fireAutomations(ctx, dbp.ID, row.ID, "row_added", nil, nil)
}

// rowChanged starts property_edited automations for the properties that
// changed.
func (s *Service) rowChanged(ctx context.Context, dbp *Page, before, after *Page, changed []string, actor uuid.UUID) {
	s.fireAutomations(ctx, dbp.ID, after.ID, "property_edited", changed, after.Props)
}

func (s *Service) fireAutomations(ctx context.Context, dbID, rowID uuid.UUID, kind string, changed []string, props map[string]any) {
	depth := automationDepth(ctx)
	if depth >= maxAutomationDepth {
		return
	}
	var any bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM docs_automations WHERE database_id = $1 AND enabled AND trigger->>'type' = $2)`, dbID, kind).Scan(&any); err != nil || !any {
		return
	}
	// A fresh context: automations run as their creator, never with the
	// trigger's agent marker or request values.
	bg.Go(context.Background(), "docs.automations", func(ctx context.Context) error {
		ctx = context.WithValue(ctx, automationDepthKey{}, depth+1)
		rows, err := s.d.Pool.Query(ctx, `SELECT `+automationCols+` FROM docs_automations WHERE database_id = $1 AND enabled AND trigger->>'type' = $2 ORDER BY created_at`, dbID, kind)
		if err != nil {
			return err
		}
		list, err := scanAutomations(rows)
		if err != nil {
			return err
		}
		for _, a := range list {
			if kind == "property_edited" {
				if !slices.Contains(changed, a.Trigger.Property) {
					continue
				}
				if len(a.Trigger.Values) > 0 && !triggerValueMatches(a.Trigger.Values, props[a.Trigger.Property]) {
					continue
				}
			}
			s.runAutomation(ctx, a, &rowID)
		}
		return nil
	})
}

// triggerValueMatches: the new value is one of the trigger's values (an
// option id, a checkbox state, text) or, for lists, contains one.
func triggerValueMatches(values []json.RawMessage, v any) bool {
	var have []string
	switch x := v.(type) {
	case string:
		have = []string{x}
	case bool:
		have = []string{strconv.FormatBool(x)}
	case float64:
		have = []string{fFmtNum(x)}
	case []any:
		have = fStringList(x)
	}
	for _, raw := range values {
		var want any
		_ = json.Unmarshal(raw, &want)
		w := fmt.Sprint(want)
		if b, ok := want.(bool); ok {
			w = strconv.FormatBool(b)
		}
		if want == nil && len(have) == 0 {
			return true
		}
		if slices.Contains(have, w) {
			return true
		}
	}
	return false
}

func (s *Service) logRun(ctx context.Context, automationID uuid.UUID, rowID *uuid.UUID, status, detail string) {
	if utf8.RuneCountInString(detail) > 500 {
		detail = string([]rune(detail)[:500])
	}
	_, _ = s.d.Pool.Exec(ctx, `INSERT INTO docs_automation_runs (automation_id, row_id, status, detail) VALUES ($1, $2, $3, $4)`, automationID, rowID, status, detail)
	_, _ = s.d.Pool.Exec(ctx, `UPDATE docs_automations SET last_run_at = now() WHERE id = $1`, automationID)
}

func (s *Service) pause(ctx context.Context, a Automation, reason string) {
	_, _ = s.d.Pool.Exec(ctx, `UPDATE docs_automations SET enabled = false, paused_reason = $2, updated_at = now() WHERE id = $1`, a.ID, reason)
}

// runAutomation runs one automation for a row (nil on a schedule: every
// matching row for edits, once for the rest) as its creator.
func (s *Service) runAutomation(ctx context.Context, a Automation, rowID *uuid.UUID) {
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM docs_automation_runs WHERE automation_id = $1 AND created_at > now() - interval '1 minute'`, a.ID).Scan(&recent); err != nil {
		return
	}
	if recent >= maxAutomationRunMin {
		s.logRun(ctx, a.ID, rowID, "skipped", "Ran 60 times in the last minute; skipped to stop a loop.")
		return
	}
	if a.CreatedBy == nil {
		s.pause(ctx, a, "The person who set it up left.")
		return
	}
	me := *a.CreatedBy
	dbp, err := requirePage(ctx, s.d.Pool, me, a.DatabaseID, access.Edit)
	if err != nil || dbp.DeletedAt != nil {
		s.pause(ctx, a, "The person it runs as can no longer edit this database.")
		s.logRun(ctx, a.ID, rowID, "failed", "Paused: its creator can no longer edit this database.")
		return
	}
	targets := []*Page{}
	if rowID != nil {
		row, err := requirePage(ctx, s.d.Pool, me, *rowID, access.View)
		if err != nil || row.DeletedAt != nil {
			s.logRun(ctx, a.ID, rowID, "skipped", "The row is gone or its creator can't open it.")
			return
		}
		targets = append(targets, row)
	}
	// Conditions, checked as the creator.
	v := viewer{me: me, loc: loadTZ(a.Trigger.TZ)}
	if len(a.Conditions) > 0 || rowID == nil {
		rq := rowQuery{db: dbp, filters: a.Conditions, me: me, tz: a.Trigger.TZ, noMeta: true, limit: 500}
		if rowID != nil {
			c := s.newComputer(ctx, v)
			c.setSchema(dbp.ID, dbp.Schema)
			r := pageRow(targets[0])
			c.addRow(r)
			if !c.matches(dbp.ID, dbp.Schema, r, Filter{Conj: "and", Filters: a.Conditions}) {
				return // not a match: no run to log
			}
		} else if hasRowStep(a.Actions) {
			page, err := s.queryRows(ctx, rq)
			if err != nil {
				s.logRun(ctx, a.ID, nil, "failed", "Couldn't read the rows: "+err.Error())
				return
			}
			for _, it := range page.Items {
				if row, err := loadPage(ctx, s.d.Pool, it.ID, false); err == nil {
					row.MyLevel = it.MyLevel
					targets = append(targets, row)
				}
			}
		}
	}
	var done []string
	for _, act := range a.Actions {
		switch act.Type {
		case "set_property":
			var val any
			_ = json.Unmarshal(act.Value, &val)
			raw, _ := json.Marshal(resolveDynamic(val, me, a.Trigger.TZ))
			for _, row := range targets {
				if row.MyLevel < access.Edit {
					if lvl, err := access.LevelOf(ctx, s.d.Pool, me, Kind, row.ID); err != nil || lvl < access.Edit {
						continue
					}
				}
				if _, err := s.setRow(ctx, me, row, rowValuesReq{Values: map[string]json.RawMessage{act.Property: raw}, TZ: a.Trigger.TZ}); err != nil {
					s.logRun(ctx, a.ID, &row.ID, "failed", "Edit property: "+errText(err))
					return
				}
			}
			done = append(done, "edited a property")
		case "add_row":
			if act.DatabaseID == nil {
				continue
			}
			if _, err := s.newRow(ctx, me, *act.DatabaseID, createRowReq{Values: act.Values, TemplateID: act.TemplateID, TZ: a.Trigger.TZ}); err != nil {
				s.logRun(ctx, a.ID, rowID, "failed", "Add row: "+errText(err))
				return
			}
			done = append(done, "added a row")
		case "notify":
			n := s.notifyStep(ctx, a, act, dbp, targets)
			done = append(done, fmt.Sprintf("notified %d", n))
		case "webhook":
			if err := s.webhookStep(ctx, a, act, dbp, targets); err != nil {
				s.logRun(ctx, a.ID, rowID, "failed", "Webhook: "+err.Error())
				return
			}
			done = append(done, "sent a webhook")
		}
	}
	s.logRun(ctx, a.ID, rowID, "ok", strings.Join(done, ", "))
}

func hasRowStep(acts []AutomationAction) bool {
	return slices.ContainsFunc(acts, func(a AutomationAction) bool {
		return a.Type == "set_property" || a.Type == "notify" || a.Type == "webhook"
	})
}

func errText(err error) string {
	if he, ok := err.(*httpx.Error); ok {
		return he.Message
	}
	return "something went wrong"
}

// notifyStep tells the chosen people (and people in a person property of
// the row), never anyone who can't open the row or database.
func (s *Service) notifyStep(ctx context.Context, a Automation, act AutomationAction, dbp *Page, rows []*Page) int {
	sent := 0
	subjects := rows
	if len(subjects) == 0 {
		subjects = []*Page{nil}
	}
	for _, row := range subjects {
		people := append([]uuid.UUID{}, act.People...)
		target := dbp.ID
		title := displayTitle(dbp.Title)
		if row != nil {
			target, title = row.ID, displayTitle(row.Title)
			if p, ok := findProp(dbp.Schema, act.PersonProperty); ok {
				switch p.Type {
				case "person":
					people = append(people, personValues([]Property{p}, row.Props)...)
				case "created_by":
					if row.CreatedBy != nil {
						people = append(people, *row.CreatedBy)
					}
				case "last_edited_by":
					if row.UpdatedBy != nil {
						people = append(people, *row.UpdatedBy)
					}
				}
			}
		}
		body := act.Message
		if body == "" {
			body = a.Name
		}
		for _, u := range dedupeIDs(people) {
			if !s.canView(ctx, u, target) {
				continue
			}
			s.send(ctx, notify.Notification{UserID: u, Kind: NotifyAutomation, Title: title, Body: body, Link: "/docs/" + target.String(),
				Data: map[string]any{"page_id": target, "automation_id": a.ID}})
			sent++
		}
	}
	return sent
}

// webhookStep posts the rows (or the run, on a schedule without rows) as
// JSON, signed: X-Signature: sha256=<hex HMAC of "<timestamp>.<body>"> with
// the automation's secret, X-Timestamp: unix seconds.
func (s *Service) webhookStep(ctx context.Context, a Automation, act AutomationAction, dbp *Page, rows []*Page) error {
	type rowOut struct {
		ID         uuid.UUID         `json:"id"`
		Title      string            `json:"title"`
		URL        string            `json:"url"`
		Properties map[string]string `json:"properties"`
	}
	payload := map[string]any{"automation_id": a.ID, "automation": a.Name, "database_id": dbp.ID, "database": displayTitle(dbp.Title),
		"trigger": a.Trigger.Type, "sent_at": time.Now().UTC().Format(time.RFC3339)}
	c := s.newComputer(ctx, viewer{me: *a.CreatedBy, loc: loadTZ(a.Trigger.TZ)})
	c.setSchema(dbp.ID, dbp.Schema)
	var list []rowOut
	for _, row := range rows {
		r := pageRow(row)
		c.addRow(r)
		props := map[string]string{}
		for _, p := range dbp.Schema {
			if p.Type == "title" || p.Type == "button" {
				continue
			}
			if t := fValueText(c.Value(dbp.ID, r.ID, p), c); t != "" {
				props[p.Name] = t
			}
		}
		list = append(list, rowOut{ID: row.ID, Title: displayTitle(row.Title), URL: s.d.Cfg.PublicWebURL + "/docs/" + row.ID.String(), Properties: props})
	}
	if list != nil {
		payload["rows"] = list
	}
	body, _ := json.Marshal(payload)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(a.Secret))
	mac.Write([]byte(ts + "." + string(body)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, act.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Scaffold App automations")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	dev := s.d.Cfg.IsDevelopment()
	client := safehttp.Client(safehttp.Options{Timeout: 10 * time.Second, MaxBody: 64 << 10, AllowPrivate: dev, AllowHTTP: dev})
	resp, err := client.Do(req)
	if err != nil {
		if safehttp.IsBlocked(err) {
			return fmt.Errorf("that address isn't allowed")
		}
		return fmt.Errorf("couldn't reach it")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("it answered %d", resp.StatusCode)
	}
	return nil
}

// runScheduledAutomations runs schedules that are due (each claims its next
// time first, so another instance never runs it twice).
func (s *Service) runScheduledAutomations(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT `+automationCols+` FROM docs_automations WHERE enabled AND next_run_at <= now() ORDER BY next_run_at LIMIT 50`)
	if err != nil {
		return err
	}
	list, err := scanAutomations(rows)
	if err != nil {
		return err
	}
	for _, a := range list {
		t := a.Trigger
		next := nextRepeat(Repeat{Every: t.Every, Interval: 1, Weekdays: t.Weekdays, Time: t.Time, TZ: t.TZ}, time.Now())
		tag, err := s.d.Pool.Exec(ctx, `UPDATE docs_automations SET next_run_at = $2 WHERE id = $1 AND next_run_at <= now()`, a.ID, next)
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		s.runAutomation(context.WithValue(ctx, automationDepthKey{}, 1), a, nil)
	}
	return nil
}
