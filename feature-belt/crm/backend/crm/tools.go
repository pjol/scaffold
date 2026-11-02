package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Tools run as the caller with the REST routes' checks: the same visible
// query (agent-capped through access.ViaAgent), access.Require on every id,
// and crm.access in the record's org. Writes are one precise change each,
// with Target and Preview; there is no delete, merge, export or sharing tool.
func (s *Service) Tools() []app.Tool {
	recordSchema := `"record_type":{"type":"string","enum":["company","contact","deal"]},"record_id":{"type":"string","format":"uuid"}`
	readRecordSchema := `"record_type":{"type":"string","enum":["company","contact","deal"]},"record_id":{"type":"string","description":"The app's own record id (UUID) or a connected record's id from crm_search."}`
	sourceSchema := `"source":{"type":"string","description":"Where to look: app (the app's own CRM), all, or a connected CRM's id from crm_list_sources."}`
	orgSchema := `"org_id":{"type":"string","format":"uuid","description":"` + orgs.NounTitle() + `; optional when the user uses the CRM in one ` + orgs.Noun() + `."}`
	return append([]app.Tool{
		{
			Name: "crm_search",
			Description: "Search CRM companies, contacts and deals the user can see by name, email, phone or domain: the app's own records and, " +
				"where the workspace connects HubSpot or Salesforce, the user's records there (each hit's source says where it lives).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"types":{"type":"array","items":{"type":"string","enum":["company","contact","deal"]}},` +
				orgSchema + `,"limit":{"type":"integer","minimum":1,"maximum":50},` + sourceSchema + `},"required":["query"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Query  string     `json:"query"`
					Types  []string   `json:"types"`
					OrgID  *uuid.UUID `json:"org_id"`
					Limit  int        `json:"limit"`
					Source string     `json:"source"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				orgIDs, err := s.crmOrgs(ctx, caller.UserID)
				if err != nil {
					return nil, err
				}
				if in.OrgID != nil {
					if err := s.requireOrgCRM(ctx, *in.OrgID, caller.UserID); err != nil {
						return nil, err
					}
					orgIDs = []uuid.UUID{*in.OrgID}
				}
				types := parseTypes(strings.Join(in.Types, ","))
				source := in.Source
				if source == "" {
					source = SourceAll
				}
				out := []any{}
				if source == SourceApp || source == SourceAll {
					appOrgs := []uuid.UUID{}
					for _, id := range orgIDs {
						if st, err := s.sourceSettings(ctx, id); err == nil && st.allowsApp() {
							appOrgs = append(appOrgs, id)
						}
					}
					hits, err := s.search(ctx, caller.UserID, appOrgs, in.Query, types, in.Limit)
					if err != nil {
						return nil, err
					}
					for _, h := range hits {
						out = append(out, h)
					}
				}
				if source != SourceApp {
					hits, err := s.xSearchTool(ctx, caller, orgIDs, source, in.Query, types, in.Limit)
					if err != nil {
						return nil, err
					}
					for _, h := range hits {
						out = append(out, h)
					}
				}
				return out, nil
			},
		},
		s.getTool(objContact, "crm_get_contact", "Get a CRM contact the user can see: fields, owner, tags, custom properties, and the companies and deals it is associated with."),
		s.getTool(objCompany, "crm_get_company", "Get a CRM company the user can see: fields, owner, tags, custom properties, and its contacts and deals."),
		s.getTool(objDeal, "crm_get_deal", "Get a CRM deal the user can see: amount, pipeline, stage, close date, owner, custom properties, and its company and contacts."),
		{
			Name: "crm_list_deals",
			Description: "List CRM deals the user can see, with filters for pipeline, stage, owner (\"me\" or a person's id), status and close date. " +
				"Returns deals, the number that match and a cursor for the next page.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,` + sourceSchema + `,
				"pipeline_id":{"type":"string"},"stage_id":{"type":"string"},
				"owner":{"type":"string","description":"me, or a person's user id"},"status":{"type":"string","enum":["open","won","lost"]},
				"close_from":{"type":"string","format":"date"},"close_to":{"type":"string","format":"date"},
				"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolListDeals,
		},
		{
			Name: "crm_list_activities",
			Description: "List the timeline of a CRM record the user can see, newest first: notes, calls, emails, meetings, tasks and changes. " +
				"Connected records (HubSpot, Salesforce) list their engagements from the platform.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + readRecordSchema + `,
				"filter":{"type":"string","enum":["all","activities","notes","calls","emails","meetings","tasks","changes"]},
				"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"}},"required":["record_type","record_id"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					RecordType string `json:"record_type"`
					RecordID   string `json:"record_id"`
					Filter     string `json:"filter"`
					Limit      int    `json:"limit"`
					Cursor     string `json:"cursor"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				o, ok := parseObject(in.RecordType)
				if !ok {
					return nil, httpx.BadRequest("record_type is company, contact or deal.")
				}
				recordID, err := uuid.Parse(in.RecordID)
				if err != nil {
					return s.xTimelineTool(ctx, caller, o, in.RecordID, in.Filter, in.Cursor, in.Limit)
				}
				page, err := s.loadTimeline(ctx, caller.UserID, o, recordID, in.Filter, in.Cursor, in.Limit)
				if err != nil {
					return nil, err
				}
				for _, it := range page.Items {
					if it.Activity != nil {
						it.Activity.Body = plainBody(it.Activity.Body, it.Activity.Mentions)
					}
				}
				return page, nil
			},
		},
		{
			Name:        "crm_list_my_tasks",
			Description: "List the user's open CRM tasks (overdue, due today, upcoming) or done ones, soonest first.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,
				"bucket":{"type":"string","enum":["open","overdue","today","upcoming","done"]},
				"tz":{"type":"string","description":"IANA time zone for today, like Europe/Berlin"},"cursor":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID  *uuid.UUID `json:"org_id"`
					Bucket string     `json:"bucket"`
					TZ     string     `json:"tz"`
					Cursor string     `json:"cursor"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				return s.tasks(ctx, caller.UserID, orgID, in.Bucket, "me", in.TZ, in.Cursor, 50)
			},
		},
		{
			Name: "crm_update_field",
			Description: "Change one field of a CRM record the user can edit, by property key (from crm_get_*: name, amount, close_date, " +
				"lifecycle_stage, a custom property key…). value is the new value as JSON (null clears it). Amounts are minor units (cents). " +
				"Owner and team decide who can open the record, so they can't be changed here.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + recordSchema + `,"field":{"type":"string"},"value":{}},
				"required":["record_type","record_id","field","value"],"additionalProperties":false}`),
			ReadOnly: false,
			Target:   recordTarget,
			Preview:  s.previewUpdateField,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				in, o, err := decodeFieldInput(input)
				if err != nil {
					return nil, err
				}
				return s.update(ctx, caller.UserID, o, in.RecordID, map[string]json.RawMessage{in.Field: in.Value})
			},
		},
		{
			Name:        "crm_move_deal_stage",
			Description: "Move a CRM deal the user can edit to another stage (stage ids come from crm_get_deal or crm_list_deals).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"},"stage_id":{"type":"string","format":"uuid"}},
				"required":["deal_id","stage_id"],"additionalProperties":false}`),
			ReadOnly: false,
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					DealID string `json:"deal_id"`
				}
				_ = json.Unmarshal(input, &in)
				return KindDeal, in.DealID
			},
			Preview: s.previewMoveStage,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					DealID  uuid.UUID `json:"deal_id"`
					StageID uuid.UUID `json:"stage_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.moveDeal(ctx, caller.UserID, in.DealID, in.StageID)
			},
		},
		{
			Name:        "crm_add_note",
			Description: "Add a note to the timeline of a CRM record the user can comment on.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + recordSchema + `,"body":{"type":"string","maxLength":20000}},
				"required":["record_type","record_id","body"],"additionalProperties":false}`),
			ReadOnly: false,
			Target:   recordTarget,
			Preview:  s.previewActivity("note"),
			Handler:  s.toolActivity("note"),
		},
		{
			Name:        "crm_create_task",
			Description: "Create a task on a CRM record the user can comment on, assigned to the user unless assignee_id names another member who can see the record.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + recordSchema + `,"title":{"type":"string","maxLength":200},"body":{"type":"string"},
				"due_at":{"type":"string","format":"date-time"},"reminder_at":{"type":"string","format":"date-time"},
				"assignee_id":{"type":"string","format":"uuid"},"priority":{"type":"string","enum":["none","low","medium","high"]}},
				"required":["record_type","record_id","title"],"additionalProperties":false}`),
			ReadOnly: false,
			Target:   recordTarget,
			Preview:  s.previewActivity("task"),
			Handler:  s.toolActivity("task"),
		},
		{
			Name:        "crm_log_call",
			Description: "Log a call on a CRM record the user can comment on: what was said, outcome, direction, duration and when it happened.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + recordSchema + `,"body":{"type":"string"},
				"outcome":{"type":"string","enum":["connected","no_answer","left_voicemail","busy","wrong_number"]},
				"direction":{"type":"string","enum":["inbound","outbound"]},"duration_minutes":{"type":"integer","minimum":0,"maximum":1440},
				"occurred_at":{"type":"string","format":"date-time"}},"required":["record_type","record_id"],"additionalProperties":false}`),
			ReadOnly: false,
			Target:   recordTarget,
			Preview:  s.previewActivity("call"),
			Handler:  s.toolActivity("call"),
		},
		s.listSourcesTool(),
	}, s.connectedTools()...)
}

func (s *Service) getTool(o objectType, name, desc string) app.Tool {
	return app.Tool{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The record's id: the app's own (a UUID) or a connected record's, as crm_search returned it."}},"required":["id"],"additionalProperties":false}`),
		ReadOnly:    true,
		Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
			var raw struct {
				ID string `json:"id"`
			}
			if err := decodeInput(input, &raw); err != nil {
				return nil, err
			}
			parsed, err := uuid.Parse(raw.ID)
			if err != nil {
				if _, _, ok := parseXRef(raw.ID); ok {
					return s.xRecordDetail(ctx, caller, o, raw.ID)
				}
				return nil, httpx.BadRequest("id must be a record id.")
			}
			in := struct{ ID uuid.UUID }{parsed}
			if in.ID == uuid.Nil {
				return nil, httpx.BadRequest("id must be a record id.")
			}
			d, err := s.recordDetail(ctx, caller.UserID, o, in.ID)
			if err != nil {
				return nil, err
			}
			out := map[string]any{"record": d.Record, "associations": d.Associations, "link": o.link(in.ID)}
			if deal, ok := d.Record.(*Deal); ok {
				pipelines, err := s.loadPipelines(ctx, s.d.Pool, deal.OrgID)
				if err != nil {
					return nil, err
				}
				for _, p := range pipelines {
					if p.ID == deal.PipelineID {
						out["pipeline_stages"] = p.Stages
					}
				}
			}
			return out, nil
		},
	}
}

// toolOrg picks the org a tool works in: the given one (checked), or the
// only one where the user uses the CRM.
func (s *Service) toolOrg(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) (uuid.UUID, error) {
	if orgID != nil {
		return *orgID, s.requireOrgCRM(ctx, *orgID, userID)
	}
	ids, err := s.crmOrgs(ctx, userID)
	if err != nil {
		return uuid.Nil, err
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, errNoCRM()
	case 1:
		return ids[0], s.bootstrap(ctx, ids[0])
	}
	names := []string{}
	rows, err := s.d.Pool.Query(ctx, `SELECT id, name FROM orgs WHERE id = ANY ($1) ORDER BY name`, ids)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			var n string
			if rows.Scan(&id, &n) == nil {
				names = append(names, fmt.Sprintf("%s (%s)", n, id))
			}
		}
		rows.Close()
	}
	return uuid.Nil, httpx.BadRequest("The user uses the CRM in several " + orgs.Nouns() + ". Pass org_id, one of: " + strings.Join(names, ", ") + ".")
}

func (s *Service) toolListDeals(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		OrgID      *uuid.UUID `json:"org_id"`
		Source     string     `json:"source"`
		PipelineID *string    `json:"pipeline_id"`
		StageID    *string    `json:"stage_id"`
		Owner      string     `json:"owner"`
		Status     string     `json:"status"`
		CloseFrom  string     `json:"close_from"`
		CloseTo    string     `json:"close_to"`
		Limit      int        `json:"limit"`
		Cursor     string     `json:"cursor"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
	if err != nil {
		return nil, err
	}
	if in.Source != "" && in.Source != SourceApp {
		if in.Source == SourceAll {
			return nil, httpx.BadRequest("Deals list one source at a time: app, or a connection id from crm_list_sources.")
		}
		if in.Status != "" && in.Status != "open" && in.Status != "won" && in.Status != "lost" {
			return nil, httpx.BadRequest("status is open, won or lost.")
		}
		return s.xListDeals(ctx, caller, orgID, in.Source, deref(in.PipelineID), deref(in.StageID), in.Owner, in.Status, in.CloseFrom, in.CloseTo, in.Cursor, in.Limit)
	}
	if st, err := s.sourceSettings(ctx, orgID); err == nil && !st.allowsApp() {
		return nil, httpx.BadRequest("This workspace works in its connected CRM: pass source, a connection id from crm_list_sources.")
	}
	var pipelineID, stageID *uuid.UUID
	for _, pair := range []struct {
		in  *string
		out **uuid.UUID
	}{{in.PipelineID, &pipelineID}, {in.StageID, &stageID}} {
		if pair.in != nil && *pair.in != "" {
			id, err := uuid.Parse(*pair.in)
			if err != nil {
				return nil, httpx.BadRequest("pipeline_id and stage_id of the app's own deals are ids from crm_get_deal.")
			}
			*pair.out = &id
		}
	}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	spec := listSpec{SortKey: "close_date", SortDir: "asc", Cursor: in.Cursor, Limit: in.Limit}
	if spec.Limit <= 0 || spec.Limit > 100 {
		spec.Limit = 25
	}
	if pipelineID != nil {
		spec.Filters = append(spec.Filters, Filter{Key: "pipeline", Op: "is_any", Value: raw([]string{pipelineID.String()})})
	}
	if stageID != nil {
		spec.Filters = append(spec.Filters, Filter{Key: "stage", Op: "is_any", Value: raw([]string{stageID.String()})})
	}
	if in.Owner != "" {
		spec.Filters = append(spec.Filters, Filter{Key: "owner", Op: "is_any", Value: raw([]string{in.Owner})})
	}
	if in.CloseFrom != "" {
		if !validDate(in.CloseFrom) {
			return nil, httpx.BadRequest("close_from must be a date like 2026-11-30.")
		}
		from, _ := time.Parse("2006-01-02", in.CloseFrom)
		spec.Filters = append(spec.Filters, Filter{Key: "close_date", Op: "after", Value: raw(from.AddDate(0, 0, -1).Format("2006-01-02"))})
	}
	if in.CloseTo != "" {
		if !validDate(in.CloseTo) {
			return nil, httpx.BadRequest("close_to must be a date like 2026-11-30.")
		}
		to, _ := time.Parse("2006-01-02", in.CloseTo)
		spec.Filters = append(spec.Filters, Filter{Key: "close_date", Op: "before", Value: raw(to.AddDate(0, 0, 1).Format("2006-01-02"))})
	}
	switch in.Status {
	case "":
	case "open", "won", "lost":
		status := in.Status
		spec.where = append(spec.where, func(b *sqlb) string { return "st.outcome = " + b.arg(status) })
	default:
		return nil, httpx.BadRequest("status is open, won or lost.")
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objDeal)
	if err != nil {
		return nil, err
	}
	return s.queryRecords(ctx, s.d.Pool, caller.UserID, orgID, objDeal, props, spec, true)
}

func recordTarget(input json.RawMessage) (string, string) {
	var in struct {
		RecordType string `json:"record_type"`
		RecordID   string `json:"record_id"`
	}
	_ = json.Unmarshal(input, &in)
	o, ok := parseObject(in.RecordType)
	if !ok {
		return "", ""
	}
	return o.kind(), in.RecordID
}

type fieldInput struct {
	RecordType string          `json:"record_type"`
	RecordID   uuid.UUID       `json:"record_id"`
	Field      string          `json:"field"`
	Value      json.RawMessage `json:"value"`
}

// accessFields are the fields that decide who can open a record: the owner
// (access owner, so manage) and the team (its access parent). Changing them
// is sharing, which agents never do, so crm_update_field refuses them for
// its preview, its run and an accepted suggestion alike.
var accessFields = map[string]bool{"owner": true, "team": true}

var errAccessField = httpx.NewError(http.StatusForbidden, "agent_forbidden",
	"AI agents can't change a record's owner or team: that changes who can open it. Ask the person to do it in the app.")

func decodeFieldInput(input json.RawMessage) (*fieldInput, objectType, error) {
	var in fieldInput
	if err := decodeInput(input, &in); err != nil {
		return nil, "", err
	}
	o, ok := parseObject(in.RecordType)
	if !ok {
		return nil, "", httpx.BadRequest("record_type is company, contact or deal.")
	}
	if in.Field == "" {
		return nil, "", httpx.BadRequest("field is a property key.")
	}
	if accessFields[in.Field] {
		return nil, "", errAccessField
	}
	if len(in.Value) == 0 {
		in.Value = json.RawMessage("null")
	}
	return &in, o, nil
}

// previewUpdateField computes the exact before and after the update would
// write, by running the same validation as the write without saving.
func (s *Service) previewUpdateField(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, o, err := decodeFieldInput(input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, o, in.RecordID, access.View)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cur, err := s.getOne(ctx, s.d.Pool, caller.UserID, o, in.RecordID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var p pending
	if err := s.applyFields(ctx, s.d.Pool, orgID, caller.UserID, o, props, cur, map[string]json.RawMessage{in.Field: in.Value}, &p); err != nil {
		return app.ToolPreview{}, asHTTP(err)
	}
	title := cur.base().Title
	prop, _ := findProp(props, in.Field)
	if len(p.changes) == 0 {
		_, text := currentValue(cur, prop)
		return app.ToolPreview{Summary: fmt.Sprintf("%s of “%s” is already %s", prop.Label, title, quoteOrEmpty(text)),
			Before: text, After: text, Link: o.link(in.RecordID)}, nil
	}
	c := p.changes[0]
	return app.ToolPreview{Summary: fmt.Sprintf("Change %s of “%s” from %s to %s", c.Label, title, quoteOrEmpty(c.Before), quoteOrEmpty(c.After)),
		Before: c.Before, After: c.After, Link: o.link(in.RecordID)}, nil
}

func quoteOrEmpty(s string) string {
	if s == "" {
		return "empty"
	}
	return "“" + clip(s, 80) + "”"
}

func (s *Service) previewMoveStage(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	var in struct {
		DealID  uuid.UUID `json:"deal_id"`
		StageID uuid.UUID `json:"stage_id"`
	}
	if err := decodeInput(input, &in); err != nil {
		return app.ToolPreview{}, err
	}
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, objDeal, in.DealID, access.View)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cur, err := s.getOne(ctx, s.d.Pool, caller.UserID, objDeal, in.DealID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	st, err := loadStage(ctx, s.d.Pool, orgID, in.StageID)
	if err != nil {
		return app.ToolPreview{}, asHTTP(err)
	}
	d := cur.(*Deal)
	return app.ToolPreview{Summary: fmt.Sprintf("Move “%s” from %s to %s", d.Name, d.Stage.Name, st.Name),
		Before: d.Stage.Name, After: st.Name, Link: objDeal.link(in.DealID)}, nil
}

type activityToolInput struct {
	RecordType      string     `json:"record_type"`
	RecordID        uuid.UUID  `json:"record_id"`
	Title           string     `json:"title"`
	Body            string     `json:"body"`
	DueAt           *time.Time `json:"due_at"`
	ReminderAt      *time.Time `json:"reminder_at"`
	AssigneeID      *uuid.UUID `json:"assignee_id"`
	Priority        *string    `json:"priority"`
	Outcome         *string    `json:"outcome"`
	Direction       *string    `json:"direction"`
	DurationMinutes *int       `json:"duration_minutes"`
	OccurredAt      *time.Time `json:"occurred_at"`
}

func (in activityToolInput) request(typ string) activityReq {
	req := activityReq{Type: typ, Body: &in.Body}
	switch typ {
	case "task":
		req.Subject, req.DueAt, req.ReminderAt, req.AssigneeID, req.Priority = &in.Title, in.DueAt, in.ReminderAt, in.AssigneeID, in.Priority
	case "call":
		req.Outcome, req.Direction, req.DurationMinutes, req.OccurredAt = in.Outcome, in.Direction, in.DurationMinutes, in.OccurredAt
	}
	return req
}

func (s *Service) toolActivity(typ string) func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	return func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
		var in activityToolInput
		if err := decodeInput(input, &in); err != nil {
			return nil, err
		}
		o, ok := parseObject(in.RecordType)
		if !ok {
			return nil, httpx.BadRequest("record_type is company, contact or deal.")
		}
		return s.logActivity(ctx, caller.UserID, o, in.RecordID, in.request(typ))
	}
}

func (s *Service) previewActivity(typ string) func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	return func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
		var in activityToolInput
		if err := decodeInput(input, &in); err != nil {
			return app.ToolPreview{}, err
		}
		o, ok := parseObject(in.RecordType)
		if !ok {
			return app.ToolPreview{}, httpx.BadRequest("record_type is company, contact or deal.")
		}
		orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, o, in.RecordID, access.View)
		if err != nil {
			return app.ToolPreview{}, err
		}
		title, err := recordTitle(ctx, s.d.Pool, o, in.RecordID)
		if err != nil {
			return app.ToolPreview{}, err
		}
		link := o.link(in.RecordID)
		const stamp = "Mon Jan 2 2006, 15:04 UTC"
		// Every field the write sets, one a line, then the text: what the
		// person accepts is exactly what runs.
		withBody := func(lines []string) string {
			out := strings.Join(lines, "\n")
			if b := strings.TrimSpace(in.Body); b != "" {
				out = strings.TrimSpace(out + "\n\n" + b)
			}
			return out
		}
		switch typ {
		case "note":
			return app.ToolPreview{Summary: "Add a note to “" + title + "”", After: in.Body, Link: link}, nil
		case "task":
			lines := []string{in.Title}
			if in.DueAt != nil {
				lines = append(lines, "Due "+in.DueAt.UTC().Format(stamp))
			}
			if in.ReminderAt != nil {
				lines = append(lines, "Reminder "+in.ReminderAt.UTC().Format(stamp))
			}
			if in.Priority != nil && *in.Priority != "none" {
				lines = append(lines, "Priority: "+*in.Priority)
			}
			if in.AssigneeID != nil && *in.AssigneeID != caller.UserID {
				// The same check the write makes, so a task nobody could see
				// is refused before the person reviews it.
				if err := s.checkAssignee(ctx, s.d.Pool, orgID, *in.AssigneeID, o, in.RecordID); err != nil {
					return app.ToolPreview{}, err
				}
				names, err := s.peopleNames(ctx, s.d.Pool, map[string]bool{in.AssigneeID.String(): true})
				if err != nil {
					return app.ToolPreview{}, err
				}
				lines = append(lines, "Assigned to "+names[in.AssigneeID.String()])
			}
			return app.ToolPreview{Summary: "Create the task “" + clip(in.Title, 80) + "” on “" + title + "”", After: withBody(lines), Link: link}, nil
		default:
			var lines []string
			if in.Outcome != nil {
				lines = append(lines, "Outcome: "+strings.ReplaceAll(*in.Outcome, "_", " "))
			}
			if in.Direction != nil {
				lines = append(lines, "Direction: "+*in.Direction)
			}
			if in.DurationMinutes != nil {
				lines = append(lines, fmt.Sprintf("Duration: %d min", *in.DurationMinutes))
			}
			if in.OccurredAt != nil {
				lines = append(lines, "When: "+in.OccurredAt.UTC().Format(stamp))
			}
			return app.ToolPreview{Summary: "Log a call on “" + title + "”", After: withBody(lines), Link: link}, nil
		}
	}
}
