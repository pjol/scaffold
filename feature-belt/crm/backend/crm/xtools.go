package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Agents on connected CRMs. Reads go through the same tools as the app's
// records (an xref id, or source: <connection id>) and are narrowed by the
// connection's own agent scope (app.ConnectionScope) as well as the CRM's.
// Writes are separate tools per platform, Source set to the platform, so the
// assistant asks first and treats the output as third-party content; each
// names its record as Target kind crm.external, id
// <connection id>:<company|contact|deal>:<platform id>, previews the exact
// change, and checks the connection's scope in both Preview (suggest) and
// Handler (edit). Agents never change owners or team visibility.

// agentAllowsX checks the connection's agent scope for one connected record.
func (s *Service) agentAllowsX(ctx context.Context, caller *auth.Identity, connID string, o objectType, pid string, need app.AgentLevel) error {
	ok, err := app.AgentAllows(ctx, s.d.Modules, caller, app.ConnectionScope(connID), KindExternal, agentTargetID(connID, o, pid), need)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	switch need {
	case app.AgentRead:
		return httpx.NewError(http.StatusForbidden, "agent_scope_off",
			"The person hasn't given AI agents access to this connected account. They can change it in Settings → AI agents.")
	case app.AgentSuggest:
		return httpx.NewError(http.StatusForbidden, "agent_scope_read_only",
			"The person lets AI agents read this connected account but not change it. They can allow suggestions or edits in Settings → AI agents.")
	}
	return httpx.NewError(http.StatusForbidden, "agent_scope_suggest_only",
		"The person allows only suggestions for this connected account, so the change can't run directly. They can allow edits in Settings → AI agents.")
}

// connAgentReadable reports whether an agent may read anything in a
// connected account (always true for people and without an agent policy).
func (s *Service) connAgentReadable(ctx context.Context, caller *auth.Identity, connID string) bool {
	if _, ok := auth.AgentFrom(ctx); !ok {
		return true
	}
	p := app.FindAgentPolicy(s.d.Modules)
	if p == nil {
		return true
	}
	_, top, err := p.FeatureLevel(ctx, caller, app.ConnectionScope(connID))
	return err == nil && top >= app.AgentRead
}

// agentFilterHits drops connected search hits the connection's agent scope hides.
func (s *Service) agentFilterHits(ctx context.Context, caller *auth.Identity, hits []XSearchHit) ([]XSearchHit, error) {
	if _, ok := auth.AgentFrom(ctx); !ok || len(hits) == 0 {
		return hits, nil
	}
	byConn := map[string][]string{}
	for _, h := range hits {
		c, pid, _ := parseXRef(h.ID)
		byConn[c] = append(byConn[c], agentTargetID(c, h.Type, pid))
	}
	allowed := map[string]bool{}
	for c, ids := range byConn {
		m, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(c), KindExternal, ids)
		if err != nil {
			return nil, err
		}
		for id, ok := range m {
			allowed[id] = ok
		}
	}
	out := hits[:0]
	for _, h := range hits {
		c, pid, _ := parseXRef(h.ID)
		if allowed[agentTargetID(c, h.Type, pid)] {
			out = append(out, h)
		}
	}
	return out, nil
}

// agentFilterPage drops connected rows the connection's agent scope hides.
func (s *Service) agentFilterPage(ctx context.Context, caller *auth.Identity, connID string, o objectType, page *xPage) error {
	if _, ok := auth.AgentFrom(ctx); !ok || len(page.Items) == 0 {
		return nil
	}
	ids := make([]string, len(page.Items))
	for i, it := range page.Items {
		ids[i] = agentTargetID(connID, o, it.xbase().platformID)
	}
	m, err := app.AgentReadable(ctx, s.d.Modules, caller, app.ConnectionScope(connID), KindExternal, ids)
	if err != nil {
		return err
	}
	kept := page.Items[:0]
	for i, it := range page.Items {
		if m[ids[i]] {
			kept = append(kept, it)
		}
	}
	page.Items = kept
	return nil
}

// xToolRecord resolves a connected record id for a tool (xref from a read tool).
func (s *Service) xToolRecord(ctx context.Context, caller *auth.Identity, recordID string) (Source, string, error) {
	connID, pid, ok := parseXRef(recordID)
	if !ok {
		return nil, "", httpx.BadRequest("record_id is a connected record's id as a read tool returned it.")
	}
	src, err := s.sourceForRecord(ctx, caller.UserID, connID)
	return src, pid, err
}

// xRecordDetail is the connected branch of crm_get_*.
func (s *Service) xRecordDetail(ctx context.Context, caller *auth.Identity, o objectType, id string) (any, error) {
	src, pid, err := s.xToolRecord(ctx, caller, id)
	if err != nil {
		return nil, err
	}
	if err := s.agentAllowsX(ctx, caller, src.Connection().ID, o, pid, app.AgentRead); err != nil {
		return nil, err
	}
	d, err := src.Get(ctx, o, pid)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"record": d.Record, "associations": d.Associations, "link": "/crm/" + o.path() + "/" + id,
		"source": src.Ref(), "write_tools": "crm_" + src.Connection().Provider + "_*"}
	if deal, ok := d.Record.(*XDeal); ok {
		if pls, err := src.Pipelines(ctx); err == nil {
			for _, p := range pls {
				if p.ID == deal.PipelineID {
					out["pipeline_stages"] = p.Stages
				}
			}
		}
	}
	return out, nil
}

// ---- write tools -----------------------------------------------------------------

type xToolInput struct {
	RecordType      string          `json:"record_type"`
	RecordID        string          `json:"record_id"`
	DealID          string          `json:"deal_id"`
	StageID         string          `json:"stage_id"`
	Field           string          `json:"field"`
	Value           json.RawMessage `json:"value"`
	Title           string          `json:"title"`
	Body            string          `json:"body"`
	DueAt           *time.Time      `json:"due_at"`
	ReminderAt      *time.Time      `json:"reminder_at"`
	AssigneeID      *string         `json:"assignee_id"`
	Priority        *string         `json:"priority"`
	Outcome         *string         `json:"outcome"`
	Direction       *string         `json:"direction"`
	DurationMinutes *int            `json:"duration_minutes"`
	OccurredAt      *time.Time      `json:"occurred_at"`
}

func (in xToolInput) object() (objectType, error) {
	if in.DealID != "" {
		return objDeal, nil
	}
	o, ok := parseObject(in.RecordType)
	if !ok {
		return "", httpx.BadRequest("record_type is company, contact or deal.")
	}
	return o, nil
}

func (in xToolInput) recordID() string {
	if in.DealID != "" {
		return in.DealID
	}
	return in.RecordID
}

// xTarget is the Target of connected write tools: kind crm.external (also
// for an empty input), id <connection id>:<type>:<platform id>.
func xTarget(input json.RawMessage) (string, string) {
	var in xToolInput
	_ = json.Unmarshal(input, &in)
	o, err := in.object()
	connID, pid, ok := parseXRef(in.recordID())
	if err != nil || !ok {
		return KindExternal, ""
	}
	return KindExternal, agentTargetID(connID, o, pid)
}

// xWrite resolves a write tool's record: the platform must match the tool,
// the connection's agent scope must reach need.
func (s *Service) xWrite(ctx context.Context, caller *auth.Identity, provider string, input json.RawMessage, need app.AgentLevel) (*xToolInput, Source, objectType, string, error) {
	var in xToolInput
	if err := decodeInput(input, &in); err != nil {
		return nil, nil, "", "", err
	}
	o, err := in.object()
	if err != nil {
		return nil, nil, "", "", err
	}
	src, pid, err := s.xToolRecord(ctx, caller, in.recordID())
	if err != nil {
		return nil, nil, "", "", err
	}
	if p := src.Connection().Provider; p != provider {
		return nil, nil, "", "", httpx.BadRequest(fmt.Sprintf("That record is in %s; use the crm_%s_… tools.", providerLabel(p), p))
	}
	if err := s.agentAllowsX(ctx, caller, src.Connection().ID, o, pid, need); err != nil {
		return nil, nil, "", "", err
	}
	if !src.CanWrite() {
		return nil, nil, "", "", httpx.NewError(http.StatusConflict, app.ConnectionScopeMissing,
			"The person hasn't turned on editing for "+providerLabel(provider)+" in Settings → Connections.")
	}
	return &in, src, o, pid, nil
}

// errXAccessField: owners and team visibility decide who sees a record on the platform.
var errXAccessField = httpx.NewError(http.StatusForbidden, "agent_forbidden",
	"AI agents can't change a record's owner or who can see it. Ask the person to do it in the app or on the platform.")

func xAccessField(provider, key string) bool {
	if accessFields[key] {
		return true
	}
	switch provider {
	case "hubspot":
		return hsAccessProps[key]
	case "salesforce":
		return sfAccessFields[key]
	}
	return false
}

func (s *Service) connectedTools() []app.Tool {
	var out []app.Tool
	for _, p := range crmProviders {
		p := p
		rec := `"record_type":{"type":"string","enum":["company","contact","deal"]},"record_id":{"type":"string","description":"The connected record's id as crm_search or crm_get_* returned it."}`
		where := " in " + p.Label + " (a record from the person's connected " + p.Label + " account)"
		out = append(out,
			app.Tool{
				Name: "crm_" + p.ID + "_update_field",
				Description: "Change one field of a record" + where + ", by property key (from crm_get_*: name, amount, close_date, a custom property key…). " +
					"value is the new value as JSON (null clears it). Amounts are minor units (cents). " + p.Label + " validates the value. Owners and visibility can't be changed.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{` + rec + `,"field":{"type":"string"},"value":{}},"required":["record_type","record_id","field","value"],"additionalProperties":false}`),
				Source:      p.Label,
				Target:      xTarget,
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					return s.xPreviewField(ctx, caller, p.ID, input)
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					in, src, o, pid, err := s.xWrite(ctx, caller, p.ID, input, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					if xAccessField(p.ID, in.Field) {
						return nil, errXAccessField
					}
					if len(in.Value) == 0 {
						in.Value = json.RawMessage("null")
					}
					return src.Update(ctx, o, pid, map[string]json.RawMessage{in.Field: in.Value})
				},
			},
			app.Tool{
				Name:        "crm_" + p.ID + "_move_deal_stage",
				Description: "Move a deal" + where + " to another stage of its pipeline (stage ids come from crm_get_deal's pipeline_stages).",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","description":"The connected deal's id as a read tool returned it."},"stage_id":{"type":"string"}},"required":["deal_id","stage_id"],"additionalProperties":false}`),
				Source:      p.Label,
				Target:      xTarget,
				Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
					return s.xPreviewStage(ctx, caller, p.ID, input)
				},
				Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
					in, src, _, pid, err := s.xWrite(ctx, caller, p.ID, input, app.AgentEdit)
					if err != nil {
						return nil, err
					}
					return src.MoveStage(ctx, pid, in.StageID)
				},
			},
			s.xActivityTool(p, "note", "crm_"+p.ID+"_add_note", "Add a note to the timeline of a record"+where+".",
				`{"type":"object","properties":{`+rec+`,"body":{"type":"string","maxLength":20000}},"required":["record_type","record_id","body"],"additionalProperties":false}`),
			s.xActivityTool(p, "task", "crm_"+p.ID+"_create_task", "Create a task on a record"+where+", owned by the person unless assignee_id names another owner (from crm_list_sources owners).",
				`{"type":"object","properties":{`+rec+`,"title":{"type":"string","maxLength":200},"body":{"type":"string"},"due_at":{"type":"string","format":"date-time"},
				"reminder_at":{"type":"string","format":"date-time"},"assignee_id":{"type":"string"},"priority":{"type":"string","enum":["none","low","medium","high"]}},
				"required":["record_type","record_id","title"],"additionalProperties":false}`),
			s.xActivityTool(p, "call", "crm_"+p.ID+"_log_call", "Log a call on a record"+where+": what was said, outcome, direction, duration and when.",
				`{"type":"object","properties":{`+rec+`,"body":{"type":"string"},"outcome":{"type":"string","enum":["connected","no_answer","left_voicemail","busy","wrong_number"]},
				"direction":{"type":"string","enum":["inbound","outbound"]},"duration_minutes":{"type":"integer","minimum":0,"maximum":1440},"occurred_at":{"type":"string","format":"date-time"}},
				"required":["record_type","record_id"],"additionalProperties":false}`),
		)
	}
	return out
}

func (s *Service) xActivityTool(p providerInfo, typ, name, desc, schema string) app.Tool {
	return app.Tool{
		Name: name, Description: desc, InputSchema: json.RawMessage(schema), Source: p.Label, Target: xTarget,
		Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
			in, src, o, pid, err := s.xWrite(ctx, caller, p.ID, input, app.AgentSuggest)
			if err != nil {
				return app.ToolPreview{}, err
			}
			return s.xPreviewActivity(ctx, src, o, pid, typ, in)
		},
		Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
			in, src, o, pid, err := s.xWrite(ctx, caller, p.ID, input, app.AgentEdit)
			if err != nil {
				return nil, err
			}
			return src.LogActivity(ctx, o, pid, in.request(typ))
		},
	}
}

func (in xToolInput) request(typ string) xActivityReq {
	body := in.Body
	req := xActivityReq{Type: typ, Body: &body}
	switch typ {
	case "task":
		title := in.Title
		req.Subject, req.DueAt, req.ReminderAt, req.AssigneeID, req.Priority = &title, in.DueAt, in.ReminderAt, in.AssigneeID, in.Priority
	case "call":
		req.Outcome, req.Direction, req.DurationMinutes, req.OccurredAt = in.Outcome, in.Direction, in.DurationMinutes, in.OccurredAt
	}
	return req
}

func xTitle(rec xrecordish) string { return rec.xbase().Title }

func (s *Service) xPreviewActivity(ctx context.Context, src Source, o objectType, pid, typ string, in *xToolInput) (app.ToolPreview, error) {
	req := in.request(typ)
	if err := req.validate(); err != nil {
		return app.ToolPreview{}, err
	}
	d, err := src.Get(ctx, o, pid)
	if err != nil {
		return app.ToolPreview{}, err
	}
	title, label := xTitle(d.Record), providerLabel(src.Connection().Provider)
	link := "/crm/" + o.path() + "/" + xref(src.Connection().ID, pid)
	const stamp = "Mon Jan 2 2006, 15:04 UTC"
	withBody := func(lines []string) string {
		out := strings.Join(lines, "\n")
		if b := strings.TrimSpace(in.Body); b != "" {
			out = strings.TrimSpace(out + "\n\n" + b)
		}
		return out
	}
	switch typ {
	case "note":
		return app.ToolPreview{Summary: "Add a note to “" + title + "” in " + label, After: in.Body, Link: link}, nil
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
		if in.AssigneeID != nil && *in.AssigneeID != "" {
			owners, err := src.Owners(ctx)
			if err != nil {
				return app.ToolPreview{}, err
			}
			name := ""
			for _, u := range owners {
				if u.ID == *in.AssigneeID {
					name = u.Name
				}
			}
			if name == "" {
				return app.ToolPreview{}, fieldValueError("Assignee", "That person isn't an owner in "+label+".")
			}
			lines = append(lines, "Assigned to "+name)
		}
		return app.ToolPreview{Summary: "Create the task “" + clip(in.Title, 80) + "” on “" + title + "” in " + label, After: withBody(lines), Link: link}, nil
	}
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
	return app.ToolPreview{Summary: "Log a call on “" + title + "” in " + label, After: withBody(lines), Link: link}, nil
}

func (s *Service) xPreviewStage(ctx context.Context, caller *auth.Identity, provider string, input json.RawMessage) (app.ToolPreview, error) {
	in, src, _, pid, err := s.xWrite(ctx, caller, provider, input, app.AgentSuggest)
	if err != nil {
		return app.ToolPreview{}, err
	}
	d, err := src.Get(ctx, objDeal, pid)
	if err != nil {
		return app.ToolPreview{}, err
	}
	deal := d.Record.(*XDeal)
	pls, err := src.Pipelines(ctx)
	if err != nil {
		return app.ToolPreview{}, err
	}
	for _, pl := range pls {
		if pl.ID != deal.PipelineID {
			continue
		}
		st, ok := pl.stage(in.StageID)
		if !ok {
			break
		}
		return app.ToolPreview{Summary: fmt.Sprintf("Move “%s” from %s to %s in %s", deal.Name, deal.Stage.Name, st.Name, providerLabel(provider)),
			Before: deal.Stage.Name, After: st.Name, Link: "/crm/deals/" + xref(src.Connection().ID, pid)}, nil
	}
	return app.ToolPreview{}, fieldValueError("Stage", "That stage isn't in the deal's pipeline.")
}

// xPreviewField computes the exact before and after of a connected field change.
func (s *Service) xPreviewField(ctx context.Context, caller *auth.Identity, provider string, input json.RawMessage) (app.ToolPreview, error) {
	in, src, o, pid, err := s.xWrite(ctx, caller, provider, input, app.AgentSuggest)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if in.Field == "" {
		return app.ToolPreview{}, httpx.BadRequest("field is a property key.")
	}
	if xAccessField(provider, in.Field) {
		return app.ToolPreview{}, errXAccessField
	}
	props, err := src.Properties(ctx, o)
	if err != nil {
		return app.ToolPreview{}, err
	}
	p, ok := findProp(props, in.Field)
	if !ok {
		return app.ToolPreview{}, fieldValueError(in.Field, "There's no field “"+in.Field+"” on "+providerLabel(provider)+" "+o.labelPlural()+".")
	}
	if !p.Editable {
		return app.ToolPreview{}, fieldValueError(p.Label, p.Label+" can't be changed from here.")
	}
	d, err := src.Get(ctx, o, pid)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var owners []XUserRef
	if p.Type == "user" {
		if owners, err = src.Owners(ctx); err != nil {
			return app.ToolPreview{}, err
		}
	}
	before := xFieldText(d.Record, p)
	after, err := xValueText(p, in.Value, xCurrency(d.Record), owners)
	if err != nil {
		return app.ToolPreview{}, err
	}
	title := xTitle(d.Record)
	link := "/crm/" + o.path() + "/" + xref(src.Connection().ID, pid)
	if before == after {
		return app.ToolPreview{Summary: fmt.Sprintf("%s of “%s” is already %s", p.Label, title, quoteOrEmpty(before)), Before: before, After: after, Link: link}, nil
	}
	return app.ToolPreview{Summary: fmt.Sprintf("Change %s of “%s” in %s from %s to %s", p.Label, title, providerLabel(provider), quoteOrEmpty(before), quoteOrEmpty(after)),
		Before: before, After: after, Link: link}, nil
}

func xCurrency(rec xrecordish) string {
	if d, ok := rec.(*XDeal); ok && d.Currency != "" {
		return d.Currency
	}
	return "USD"
}

// xFieldText is a connected record's current value of p, as people read it.
func xFieldText(rec xrecordish, p Property) string {
	b := rec.xbase()
	if !p.Builtin {
		v, ok := b.Props[p.Key]
		if !ok || v == nil {
			return ""
		}
		if n, ok := b.PropsDisplay[p.Key]; ok {
			return n
		}
		switch p.Type {
		case "select":
			return optionLabel(p.Options, asText(v))
		case "multi_select":
			labels := []string{}
			for _, x := range asStrings(v) {
				labels = append(labels, optionLabel(p.Options, x))
			}
			return strings.Join(labels, ", ")
		case "checkbox":
			if v == true {
				return "Yes"
			}
			return "No"
		case "currency":
			if n, ok := v.(int64); ok {
				return formatMoney(n, xCurrency(rec))
			}
		}
		return asText(v)
	}
	switch p.Key {
	case "owner":
		if b.Owner != nil {
			return b.Owner.Name
		}
		return ""
	}
	raw, _ := json.Marshal(rec)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	switch p.Key {
	case "amount":
		if d, ok := rec.(*XDeal); ok && d.Amount != nil {
			return formatMoney(*d.Amount, d.Currency)
		}
		return ""
	case "stage":
		if d, ok := rec.(*XDeal); ok {
			return d.Stage.Name
		}
	case "emails", "phones":
		return strings.Join(asStrings(m[p.Key]), ", ")
	case "lifecycle_stage":
		return optionLabel(p.Options, asText(m[p.Key]))
	}
	return asText(m[p.Key])
}

// xValueText checks a new value against p's type and returns how it reads.
func xValueText(p Property, raw json.RawMessage, currency string, owners []XUserRef) (string, error) {
	v, err := jsonValue(raw)
	if err != nil {
		return "", fieldValueError(p.Label, p.Label+" isn't a valid value.")
	}
	if v == nil {
		return "", nil
	}
	switch p.Type {
	case "currency":
		n, err := strconv.ParseInt(asText(v), 10, 64)
		if err != nil {
			return "", fieldValueError(p.Label, p.Label+" is an amount in minor units (cents).")
		}
		return formatMoney(n, currency), nil
	case "number":
		if _, ok := parseFloat(asText(v)); !ok {
			return "", fieldValueError(p.Label, p.Label+" is a number.")
		}
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			return "", fieldValueError(p.Label, p.Label+" is true or false.")
		}
		if b {
			return "Yes", nil
		}
		return "No", nil
	case "date":
		if !validDate(asText(v)) {
			return "", fieldValueError(p.Label, p.Label+" is a date like 2026-11-30.")
		}
	case "select":
		s := asText(v)
		if len(p.Options) > 0 && !hasOption(p.Options, s) {
			return "", fieldValueError(p.Label, "“"+s+"” isn't an option of "+p.Label+".")
		}
		if len(p.Options) > 0 {
			return optionLabel(p.Options, s), nil
		}
	case "multi_select":
		labels := []string{}
		for _, x := range asStrings(v) {
			if !hasOption(p.Options, x) {
				return "", fieldValueError(p.Label, "“"+x+"” isn't an option of "+p.Label+".")
			}
			labels = append(labels, optionLabel(p.Options, x))
		}
		return strings.Join(labels, ", "), nil
	case "user":
		id := asText(v)
		for _, u := range owners {
			if u.ID == id {
				return u.Name, nil
			}
		}
		return "", fieldValueError(p.Label, "That person isn't an owner on the platform.")
	case "emails", "phones":
		return strings.Join(asStrings(v), ", "), nil
	case "domain":
		d, err := cleanDomain(asText(v))
		if err != nil {
			return "", fieldValueError(p.Label, err.Error())
		}
		return d, nil
	}
	return asText(v), nil
}

// ---- read tools: the connected branches -------------------------------------------------

// crm_list_sources: where the person's CRM records come from.
func (s *Service) listSourcesTool() app.Tool {
	return app.Tool{
		Name: "crm_list_sources",
		Description: "List where the user's CRM records come from in a workspace: the app's own CRM and connected HubSpot or Salesforce accounts " +
			"(ids to pass as source), whether each can be edited from here, and the platform's owners when owners is true.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"org_id":{"type":"string","format":"uuid"},"owners":{"type":"boolean"}},"additionalProperties":false}`),
		ReadOnly:    true,
		Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
			var in struct {
				OrgID  *uuid.UUID `json:"org_id"`
				Owners bool       `json:"owners"`
			}
			if err := decodeInput(input, &in); err != nil {
				return nil, err
			}
			orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
			if err != nil {
				return nil, err
			}
			out, err := s.sources(ctx, caller.UserID, orgID)
			if err != nil {
				return nil, err
			}
			res := map[string]any{"mode": out.Mode, "sources": []any{}}
			list := []any{}
			for _, fs := range out.Sources {
				if fs.Type == "connected" {
					if !s.connAgentReadable(ctx, caller, fs.ID) {
						continue
					}
				}
				item := map[string]any{"id": fs.ID, "type": fs.Type, "label": fs.Label, "status": fs.Status, "writable": fs.Writable}
				if fs.Type == "connected" && in.Owners && fs.Status == "active" {
					if src, err := s.source(ctx, caller.UserID, orgID, fs.ID); err == nil {
						if owners, err := src.Owners(ctx); err == nil {
							item["owners"] = owners
						}
					}
				}
				list = append(list, item)
			}
			res["sources"] = list
			return res, nil
		},
	}
}

// xListDeals is crm_list_deals on a connected account.
func (s *Service) xListDeals(ctx context.Context, caller *auth.Identity, orgID uuid.UUID, connID, pipelineID, stageID, owner, status, closeFrom, closeTo, cursor string, limit int) (any, error) {
	src, err := s.source(ctx, caller.UserID, orgID, connID)
	if err != nil {
		return nil, err
	}
	if !s.connAgentReadable(ctx, caller, connID) {
		return nil, httpx.NewError(http.StatusForbidden, "agent_scope_off",
			"The person hasn't given AI agents access to this connected account. They can change it in Settings → AI agents.")
	}
	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	spec := listSpec{SortKey: "close_date", SortDir: "asc", Cursor: cursor, Limit: limit, xPipeline: pipelineID, xOutcome: status}
	if spec.Limit <= 0 || spec.Limit > 100 {
		spec.Limit = 25
	}
	if stageID != "" {
		spec.xStages = []string{stageID}
	}
	if owner != "" {
		spec.Filters = append(spec.Filters, Filter{Key: "owner", Op: "is_any", Value: raw([]string{owner})})
	}
	if closeFrom != "" {
		from, err := time.Parse("2006-01-02", closeFrom)
		if err != nil {
			return nil, httpx.BadRequest("close_from must be a date like 2026-11-30.")
		}
		spec.Filters = append(spec.Filters, Filter{Key: "close_date", Op: "after", Value: raw(from.AddDate(0, 0, -1).Format("2006-01-02"))})
	}
	if closeTo != "" {
		to, err := time.Parse("2006-01-02", closeTo)
		if err != nil {
			return nil, httpx.BadRequest("close_to must be a date like 2026-11-30.")
		}
		spec.Filters = append(spec.Filters, Filter{Key: "close_date", Op: "before", Value: raw(to.AddDate(0, 0, 1).Format("2006-01-02"))})
	}
	page, err := src.List(ctx, objDeal, spec)
	if err != nil {
		return nil, err
	}
	if err := s.agentFilterPage(ctx, caller, connID, objDeal, page); err != nil {
		return nil, err
	}
	return page, nil
}

// xTimelineTool is crm_list_activities on a connected record.
func (s *Service) xTimelineTool(ctx context.Context, caller *auth.Identity, o objectType, recordID, filter, cursor string, limit int) (any, error) {
	src, pid, err := s.xToolRecord(ctx, caller, recordID)
	if err != nil {
		return nil, err
	}
	if err := s.agentAllowsX(ctx, caller, src.Connection().ID, o, pid, app.AgentRead); err != nil {
		return nil, err
	}
	return src.Timeline(ctx, o, pid, filter, cursor, limit)
}

// xSearchTool is crm_search across connected accounts (source all or one id).
func (s *Service) xSearchTool(ctx context.Context, caller *auth.Identity, orgIDs []uuid.UUID, source, q string, types []objectType, limit int) ([]XSearchHit, error) {
	out := []XSearchHit{}
	seen := map[string]bool{}
	for _, orgID := range orgIDs {
		st, err := s.sourceSettings(ctx, orgID)
		if err != nil || !st.allowsConnected() {
			continue
		}
		var hits []XSearchHit
		if source == SourceAll {
			hits = s.connectedSearch(ctx, caller.UserID, orgID, st, q, types, limit)
		} else {
			src, err := s.source(ctx, caller.UserID, orgID, source)
			if err != nil {
				continue
			}
			if hits, err = src.Search(ctx, q, types, limit); err != nil {
				return nil, err
			}
		}
		hits, err = s.agentFilterHits(ctx, caller, hits)
		if err != nil {
			return nil, err
		}
		for _, h := range hits {
			if !seen[h.ID] {
				seen[h.ID] = true
				out = append(out, h)
			}
		}
	}
	return out, nil
}

var _ = access.View
