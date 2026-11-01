package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// ---- org bootstrap and structural resources -------------------------------------

// Record access modes (crm_settings.record_access).
const (
	AccessEveryoneEdits = "everyone_edits"
	AccessEveryoneViews = "everyone_views"
	AccessOwnersTeams   = "owners_teams"
)

// modeGeneral is the org-wide level on the workspace resource for a mode.
func modeGeneral(mode string) access.Level {
	switch mode {
	case AccessEveryoneEdits:
		return access.Edit
	case AccessEveryoneViews:
		return access.Comment
	}
	return access.None
}

func workspaceRef(orgID uuid.UUID) access.Ref { return access.Ref{Kind: KindWorkspace, ID: orgID} }

// bootstrap creates an org's settings row, its workspace resource and, the
// first time only, a default pipeline. Idempotent; cached per process.
func (s *Service) bootstrap(ctx context.Context, orgID uuid.UUID) error {
	if _, ok := s.booted.Load(orgID); ok {
		return nil
	}
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO crm_settings (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
		if err != nil {
			return err
		}
		var mode string
		if err := tx.QueryRow(ctx, `SELECT record_access FROM crm_settings WHERE org_id = $1`, orgID).Scan(&mode); err != nil {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindWorkspace, ID: orgID, OrgID: orgID, General: modeGeneral(mode)}); err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			_, err := createPipelineTx(ctx, tx, orgID, uuid.Nil, "Sales pipeline", defaultStages())
			return err
		}
		return nil
	})
	if err == nil {
		s.booted.Store(orgID, true)
	}
	return err
}

func recordAccessMode(ctx context.Context, q db.Querier, orgID uuid.UUID) (string, error) {
	var mode string
	err := q.QueryRow(ctx, `SELECT record_access FROM crm_settings WHERE org_id = $1`, orgID).Scan(&mode)
	if db.IsNoRows(err) {
		return AccessEveryoneEdits, nil
	}
	return mode, err
}

// parentFor returns the access parent of a record: its team's crm.team
// resource (created on first use, with the team's grant when the org gives
// owners and teams edit), else the workspace.
func parentFor(ctx context.Context, tx pgx.Tx, orgID uuid.UUID, teamID *uuid.UUID) (access.Ref, error) {
	if teamID == nil {
		return workspaceRef(orgID), nil
	}
	ref := access.Ref{Kind: KindTeam, ID: *teamID}
	if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindTeam, ID: *teamID, OrgID: orgID, Parent: ptr(workspaceRef(orgID))}); err != nil {
		return ref, err
	}
	mode, err := recordAccessMode(ctx, tx, orgID)
	if err != nil {
		return ref, err
	}
	if mode == AccessOwnersTeams {
		if err := setTeamGrant(ctx, tx, *teamID, true); err != nil {
			return ref, err
		}
	}
	return ref, nil
}

// setTeamGrant gives (or removes) a team edit on its own crm.team resource,
// through access.SetGrant / RemoveGrant. crm sets grants only on its
// structural resources (workspace and team); record sharing goes through the
// access feature's routes.
func setTeamGrant(ctx context.Context, q db.Querier, teamID uuid.UUID, on bool) error {
	ref, team := access.Ref{Kind: KindTeam, ID: teamID}, access.SubjectRef{Type: "team", ID: teamID}
	if !on {
		return access.RemoveGrant(ctx, q, ref, team)
	}
	return access.SetGrant(ctx, q, ref, team, access.Edit, uuid.Nil)
}

func ptr[T any](v T) *T { return &v }

// firstTeam is the owner's team a new or reassigned record goes to (their
// first team by name), or nil.
func firstTeam(ctx context.Context, q db.Querier, orgID, userID uuid.UUID) (*uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT t.id FROM access_teams t JOIN access_team_members m ON m.team_id = t.id AND m.user_id = $2
		WHERE t.org_id = $1 ORDER BY lower(t.name), t.id LIMIT 1`, orgID, userID).Scan(&id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func isOrgMember(ctx context.Context, q db.Querier, orgID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1 AND user_id = $2)`, orgID, userID).Scan(&ok)
	return ok, err
}

// ---- events ------------------------------------------------------------------------

// change is one field change on a timeline entry.
type change struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func addEvent(ctx context.Context, q db.Querier, orgID uuid.UUID, o objectType, id uuid.UUID, kind string, actor uuid.UUID, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `INSERT INTO crm_events (org_id, record_type, record_id, kind, actor_id, agent, data) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		orgID, string(o), id, kind, nullUUID(actor), agentName(ctx), b)
	return err
}

// ---- reads -------------------------------------------------------------------------

// listRecords: GET /v1/crm/orgs/{orgID}/{companies|contacts|deals}
func (s *Service) listRecords(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		orgID := orgParam(r)
		spec, err := parseListSpec(r.URL.Query().Get)
		if err != nil {
			return err
		}
		props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
		if err != nil {
			return err
		}
		page, err := s.queryRecords(ctx, s.d.Pool, me(r), orgID, o, props, spec, true)
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

// RecordDetail is GET /v1/crm/{type}/{id}.
type RecordDetail struct {
	Record       recordish    `json:"record"`
	Associations Associations `json:"associations"`
	CanDelete    bool         `json:"can_delete"`
}

func (s *Service) getRecord(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := r.Context()
		id, err := idParam(r, "id")
		if err != nil {
			return err
		}
		d, err := s.recordDetail(ctx, me(r), o, id)
		if err != nil {
			return err
		}
		httpx.OK(w, d)
		return nil
	}
}

func (s *Service) recordDetail(ctx context.Context, userID uuid.UUID, o objectType, id uuid.UUID) (*RecordDetail, error) {
	orgID, lvl, err := s.requireRecord(ctx, s.d.Pool, userID, o, id, access.View)
	if err != nil {
		return nil, err
	}
	rec, err := s.getOne(ctx, s.d.Pool, userID, o, id)
	if err != nil {
		return nil, err
	}
	assoc, err := s.associations(ctx, s.d.Pool, userID, o, id)
	if err != nil {
		return nil, err
	}
	return &RecordDetail{Record: rec, Associations: *assoc,
		CanDelete: lvl >= access.Edit && !access.ViaAgent(ctx) && allowedHere(ctx, s.d.Pool, orgID, userID, PermDelete)}, nil
}

// ---- writes ------------------------------------------------------------------------

// pending is what a create or update will write.
type pending struct {
	cols     map[string]any // column → value
	props    map[string]any
	propsSet bool
	owner    *uuid.UUID // with ownerSet: nil unassigns
	ownerSet bool
	team     *uuid.UUID
	teamSet  bool
	stage    *stageInfo // deals: the new stage
	changes  []change
	currency string // deals: the currency amounts are in
}

type stageInfo struct {
	ID, PipelineID        uuid.UUID
	Name, Outcome         string
	Probability, Position int
}

func loadStage(ctx context.Context, q db.Querier, orgID, stageID uuid.UUID) (*stageInfo, error) {
	st := &stageInfo{ID: stageID}
	err := q.QueryRow(ctx, `SELECT pipeline_id, name, outcome, probability, position FROM crm_stages WHERE id = $1 AND org_id = $2`,
		stageID, orgID).Scan(&st.PipelineID, &st.Name, &st.Outcome, &st.Probability, &st.Position)
	if db.IsNoRows(err) {
		return nil, verr("Choose a stage from this %s's pipelines.", orgs.Noun())
	}
	return st, err
}

// current values of a record, as raw value and display text, for diffs.
func currentValue(rec recordish, p Property) (any, string) {
	b := rec.base()
	if !p.Builtin {
		v := b.Props[p.Key]
		if p.Type == "user" {
			return v, b.PropsDisplay[p.Key]
		}
		return v, displayValue(p, v, nil)
	}
	switch p.Key {
	case "owner":
		if b.Owner == nil {
			return nil, ""
		}
		return b.Owner.ID.String(), b.Owner.Name
	case "team":
		if b.Team == nil {
			return nil, ""
		}
		return b.Team.ID.String(), b.Team.Name
	case "tags":
		names := []string{}
		ids := []string{}
		for _, t := range b.Tags {
			names, ids = append(names, t.Name), append(ids, t.ID.String())
		}
		return strings.Join(ids, ","), strings.Join(names, ", ")
	}
	switch v := rec.(type) {
	case *Company:
		switch p.Key {
		case "name":
			return v.Name, v.Name
		case "domain":
			return deref(v.Domain), deref(v.Domain)
		case "industry":
			return v.Industry, v.Industry
		case "employees":
			if v.Employees == nil {
				return nil, ""
			}
			return *v.Employees, fmt.Sprint(*v.Employees)
		case "phone":
			return v.Phone, v.Phone
		case "city":
			return v.City, v.City
		case "country":
			return v.Country, v.Country
		case "description":
			return v.Description, clip(v.Description, 140)
		}
	case *Contact:
		switch p.Key {
		case "first_name":
			return v.FirstName, v.FirstName
		case "last_name":
			return v.LastName, v.LastName
		case "emails":
			return strings.Join(v.Emails, ","), strings.Join(v.Emails, ", ")
		case "phones":
			return strings.Join(v.Phones, ","), strings.Join(v.Phones, ", ")
		case "job_title":
			return v.JobTitle, v.JobTitle
		case "lifecycle_stage":
			return v.LifecycleStage, optionLabel(lifecycleOptions, v.LifecycleStage)
		case "city":
			return v.City, v.City
		case "country":
			return v.Country, v.Country
		}
	case *Deal:
		switch p.Key {
		case "name":
			return v.Name, v.Name
		case "amount":
			if v.Amount == nil {
				return nil, ""
			}
			return *v.Amount, formatMoney(*v.Amount, v.Currency)
		case "currency":
			return v.Currency, v.Currency
		case "close_date":
			return deref(v.CloseDate), deref(v.CloseDate)
		case "stage":
			return v.Stage.ID.String(), v.Stage.Name
		case "company":
			if v.Company == nil {
				return nil, ""
			}
			return v.Company.ID.String(), v.Company.Name
		}
	}
	return nil, ""
}

func sameValue(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// applyFields validates input fields against the object's properties and the
// current record (nil on create) and fills p. me is the caller.
func (s *Service) applyFields(ctx context.Context, q db.Querier, orgID, userID uuid.UUID, o objectType, props []Property, cur recordish, in map[string]json.RawMessage, p *pending) error {
	p.cols = map[string]any{}
	p.props = map[string]any{}
	if cur != nil {
		for k, v := range cur.base().Props {
			p.props[k] = v
		}
	}
	memberCache := map[uuid.UUID]bool{}
	isMember := func(id uuid.UUID) bool {
		if v, ok := memberCache[id]; ok {
			return v
		}
		ok, err := isOrgMember(ctx, q, orgID, id)
		memberCache[id] = err == nil && ok
		return memberCache[id]
	}
	if d, ok := cur.(*Deal); ok {
		p.currency = d.Currency
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	// currency first, so an amount in the same request displays in it
	slices.SortFunc(keys, func(a, b string) int {
		if a == "currency" || b == "currency" {
			return map[bool]int{true: -1, false: 1}[a == "currency"]
		}
		return strings.Compare(a, b)
	})
	for _, key := range keys {
		raw := in[key]
		prop, ok := findProp(props, key)
		if !ok {
			return verr("%s has no field \"%s\".", o.titleCase(), key)
		}
		if !prop.Editable {
			return verr("%s can't be changed here.", prop.Label)
		}
		var before any
		var beforeText string
		if cur != nil {
			before, beforeText = currentValue(cur, prop)
		}
		after, afterText, err := s.parseField(ctx, q, orgID, userID, o, prop, raw, isMember, p)
		if err != nil {
			return err
		}
		if cur != nil && sameValue(normalizeCmp(before), normalizeCmp(after)) {
			continue
		}
		if cur != nil || (after != nil && afterText != "") {
			p.changes = append(p.changes, change{Key: prop.Key, Label: prop.Label, Before: beforeText, After: afterText})
		}
	}
	return nil
}

func normalizeCmp(v any) any {
	switch x := v.(type) {
	case string:
		if x == "" {
			return nil
		}
	case *string:
		if x == nil || *x == "" {
			return nil
		}
		return *x
	case *int64:
		if x == nil {
			return nil
		}
		return *x
	case *int:
		if x == nil {
			return nil
		}
		return *x
	case int:
		return int64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	case []string:
		if len(x) == 0 {
			return nil
		}
	}
	return v
}

// parseField parses one input field, records what to write in p and returns
// the value to compare and its display text.
func (s *Service) parseField(ctx context.Context, q db.Querier, orgID, userID uuid.UUID, o objectType, prop Property, raw json.RawMessage,
	isMember func(uuid.UUID) bool, p *pending) (any, string, error) {
	null := len(raw) == 0 || string(raw) == "null"
	if !prop.Builtin {
		v, err := parseCustomValue(prop, raw, isMember)
		if err != nil {
			return nil, "", err
		}
		if v == nil {
			delete(p.props, prop.Key)
		} else {
			p.props[prop.Key] = v
		}
		p.propsSet = true
		text := displayValue(prop, v, nil)
		if prop.Type == "user" && v != nil {
			if names, err := s.peopleNames(ctx, q, map[string]bool{v.(string): true}); err == nil {
				text = names[v.(string)]
			}
		}
		return v, text, nil
	}
	str := func(max int) (string, error) {
		if null {
			return "", nil
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", verr("%s must be text.", prop.Label)
		}
		v = strings.TrimSpace(v)
		if len([]rune(v)) > max {
			return "", verr("%s can be at most %d characters.", prop.Label, max)
		}
		return v, nil
	}
	optUUID := func() (*uuid.UUID, error) {
		if null {
			return nil, nil
		}
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, verr("%s must be an id.", prop.Label)
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, verr("%s must be an id.", prop.Label)
		}
		return &id, nil
	}
	strList := func(max int) ([]string, error) {
		if null {
			return []string{}, nil
		}
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, verr("%s must be a list.", prop.Label)
		}
		if len(v) > max {
			return nil, verr("Add at most %d to %s.", max, prop.Label)
		}
		return v, nil
	}

	switch prop.Key {
	case "owner":
		id, err := optUUID()
		if err != nil {
			return nil, "", err
		}
		p.owner, p.ownerSet = id, true
		if id == nil {
			return nil, "", nil
		}
		if !isMember(*id) {
			return nil, "", verr("Choose a member of this %s as the owner.", orgs.Noun())
		}
		names, err := s.peopleNames(ctx, q, map[string]bool{id.String(): true})
		if err != nil {
			return nil, "", err
		}
		return id.String(), names[id.String()], nil
	case "team":
		id, err := optUUID()
		if err != nil {
			return nil, "", err
		}
		p.team, p.teamSet = id, true
		if id == nil {
			return nil, "", nil
		}
		var name string
		if err := q.QueryRow(ctx, `SELECT name FROM access_teams WHERE id = $1 AND org_id = $2`, *id, orgID).Scan(&name); err != nil {
			if db.IsNoRows(err) {
				return nil, "", verr("Choose a team from this %s.", orgs.Noun())
			}
			return nil, "", err
		}
		return id.String(), name, nil
	case "tags":
		vals, err := strList(50)
		if err != nil {
			return nil, "", err
		}
		ids := []uuid.UUID{}
		for _, v := range vals {
			id, err := uuid.Parse(v)
			if err != nil {
				return nil, "", verr("Tags must be tag ids.")
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		names := []string{}
		if len(ids) > 0 {
			rows, err := q.Query(ctx, `SELECT id, name FROM crm_tags WHERE org_id = $1 AND id = ANY ($2)`, orgID, ids)
			if err != nil {
				return nil, "", err
			}
			found := map[uuid.UUID]string{}
			for rows.Next() {
				var id uuid.UUID
				var n string
				if err := rows.Scan(&id, &n); err != nil {
					rows.Close()
					return nil, "", err
				}
				found[id] = n
			}
			rows.Close()
			if len(found) != len(ids) {
				return nil, "", verr("Choose tags from this %s.", orgs.Noun())
			}
			for _, id := range ids {
				names = append(names, found[id])
			}
		}
		p.cols["tag_ids"] = ids
		strs := make([]string, len(ids))
		for i, id := range ids {
			strs[i] = id.String()
		}
		return strings.Join(strs, ","), strings.Join(names, ", "), nil
	case "domain":
		v, err := str(253)
		if err != nil {
			return nil, "", err
		}
		d, err := cleanDomain(v)
		if err != nil {
			return nil, "", verr("Enter a domain like example.com.")
		}
		if d == "" {
			p.cols["domain"] = nil
			return nil, "", nil
		}
		p.cols["domain"] = d
		return d, d, nil
	case "emails":
		vals, err := strList(10)
		if err != nil {
			return nil, "", err
		}
		out := []string{}
		for _, v := range vals {
			e, err := cleanEmail(v)
			if err != nil {
				return nil, "", verr("“%s” isn't an email address.", clip(v, 60))
			}
			if !slices.Contains(out, e) {
				out = append(out, e)
			}
		}
		p.cols["emails"] = out
		return strings.Join(out, ","), strings.Join(out, ", "), nil
	case "phones":
		vals, err := strList(10)
		if err != nil {
			return nil, "", err
		}
		out := []string{}
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if !phoneRe.MatchString(v) {
				return nil, "", verr("“%s” isn't a phone number.", clip(v, 40))
			}
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
		p.cols["phones"] = out
		return strings.Join(out, ","), strings.Join(out, ", "), nil
	case "employees":
		if null {
			p.cols["employees"] = nil
			return nil, "", nil
		}
		var n int
		if err := json.Unmarshal(raw, &n); err != nil || n < 0 || n > 10_000_000 {
			return nil, "", verr("Employees must be a whole number.")
		}
		p.cols["employees"] = n
		return n, fmt.Sprint(n), nil
	case "amount":
		if null {
			p.cols["amount"] = nil
			return nil, "", nil
		}
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil || n < 0 || n > 1e15 {
			return nil, "", verr("Amount must be a whole number of minor units (cents), 0 or more.")
		}
		p.cols["amount"] = n
		if p.currency == "" {
			return n, moneyDecimal(n, "USD"), nil
		}
		return n, formatMoney(n, p.currency), nil
	case "currency":
		v, err := str(3)
		if err != nil {
			return nil, "", err
		}
		v = strings.ToUpper(v)
		if !currencyRe.MatchString(v) {
			return nil, "", verr("Currency must be a 3-letter code like USD.")
		}
		p.cols["currency"], p.currency = v, v
		return v, v, nil
	case "close_date":
		v, err := str(10)
		if err != nil {
			return nil, "", err
		}
		if v == "" {
			p.cols["close_date"] = nil
			return nil, "", nil
		}
		if !validDate(v) {
			return nil, "", verr("Close date must be a date like 2026-11-30.")
		}
		p.cols["close_date"] = v
		return v, v, nil
	case "stage":
		id, err := optUUID()
		if err != nil || id == nil {
			return nil, "", verr("Choose a stage.")
		}
		st, err := loadStage(ctx, q, orgID, *id)
		if err != nil {
			return nil, "", err
		}
		p.stage = st
		return st.ID.String(), st.Name, nil
	case "company":
		id, err := optUUID()
		if err != nil {
			return nil, "", err
		}
		if id == nil {
			p.cols["company_id"] = nil
			return nil, "", nil
		}
		refs, err := s.visibleRefs(ctx, q, userID, objCompany, []uuid.UUID{*id})
		if err != nil {
			return nil, "", err
		}
		ref, ok := refs[*id]
		var coOrg uuid.UUID
		if ok {
			_ = q.QueryRow(ctx, `SELECT org_id FROM crm_companies WHERE id = $1`, *id).Scan(&coOrg)
		}
		if !ok || coOrg != orgID {
			return nil, "", verr("Choose a company you can see in this %s.", orgs.Noun())
		}
		p.cols["company_id"] = *id
		return id.String(), ref.Name, nil
	case "lifecycle_stage":
		v, err := str(20)
		if err != nil {
			return nil, "", err
		}
		if !hasOption(lifecycleOptions, v) {
			return nil, "", verr("Choose a lifecycle stage.")
		}
		p.cols["lifecycle_stage"] = v
		return v, optionLabel(lifecycleOptions, v), nil
	}
	// Plain text columns.
	max := 200
	if prop.Type == "long_text" {
		max = 5000
	}
	v, err := str(max)
	if err != nil {
		return nil, "", err
	}
	if prop.Type == "phone" && v != "" && !phoneRe.MatchString(v) {
		return nil, "", verr("%s must be a phone number.", prop.Label)
	}
	if prop.Required && v == "" {
		return nil, "", verr("%s can't be empty.", prop.Label)
	}
	p.cols[prop.col] = v
	if prop.Type == "long_text" {
		return v, clip(v, 140), nil
	}
	return v, v, nil
}

// decodeFields reads a create or update body: field values by property key
// (plus, on create, associations and the duplicate override).
func decodeFields(r *http.Request) (map[string]json.RawMessage, error) {
	var in map[string]json.RawMessage
	if err := httpx.Decode(r, &in); err != nil {
		return nil, err
	}
	if in == nil {
		in = map[string]json.RawMessage{}
	}
	return in, nil
}

func takeBool(in map[string]json.RawMessage, key string) bool {
	raw, ok := in[key]
	delete(in, key)
	var b bool
	return ok && json.Unmarshal(raw, &b) == nil && b
}

func takeUUID(in map[string]json.RawMessage, key string) (*uuid.UUID, error) {
	raw, ok := in[key]
	delete(in, key)
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, verr("%s must be an id.", key)
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil, verr("%s must be an id.", key)
	}
	return &id, nil
}

func takeUUIDs(in map[string]json.RawMessage, key string) ([]uuid.UUID, error) {
	raw, ok := in[key]
	delete(in, key)
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err != nil || len(ss) > 100 {
		return nil, verr("%s must be a list of ids.", key)
	}
	out := []uuid.UUID{}
	for _, s := range ss {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, verr("%s must be a list of ids.", key)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// asHTTP turns field validation errors into 400s.
func asHTTP(err error) error {
	if ve, ok := err.(valueError); ok {
		return httpx.BadRequest(ve.msg)
	}
	return err
}

func (s *Service) createRecord(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		in, err := decodeFields(r)
		if err != nil {
			return err
		}
		rec, err := s.create(r.Context(), me(r), orgParam(r), o, in, false)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusCreated, rec)
		return nil
	}
}

// create makes a record from field values (also used by import). It checks
// visible duplicates unless allow_duplicate is set.
func (s *Service) create(ctx context.Context, userID, orgID uuid.UUID, o objectType, in map[string]json.RawMessage, imported bool) (recordish, error) {
	allowDup := takeBool(in, "allow_duplicate")
	companyID, err := takeUUID(in, "company_id")
	if err != nil {
		return nil, asHTTP(err)
	}
	contactIDs, err := takeUUIDs(in, "contact_ids")
	if err != nil {
		return nil, asHTTP(err)
	}
	pipelineID, err := takeUUID(in, "pipeline_id")
	if err != nil {
		return nil, asHTTP(err)
	}
	if o == objDeal && companyID != nil {
		in["company"] = json.RawMessage(`"` + companyID.String() + `"`)
		companyID = nil
	}
	var id uuid.UUID
	var p pending
	var stagePipelines []uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		props, err := s.loadProperties(ctx, tx, orgID, o)
		if err != nil {
			return err
		}
		if err := s.applyFields(ctx, tx, orgID, userID, o, props, nil, in, &p); err != nil {
			return asHTTP(err)
		}
		if !p.ownerSet {
			p.owner, p.ownerSet = &userID, true
		}
		if !p.teamSet && p.owner != nil {
			if p.team, err = firstTeam(ctx, tx, orgID, *p.owner); err != nil {
				return err
			}
		}
		cols := map[string]any{"org_id": orgID, "created_by": userID, "team_id": p.team}
		for k, v := range p.cols {
			cols[k] = v
		}
		if p.propsSet || len(p.props) > 0 {
			cols["props"] = p.props
		}
		switch o {
		case objCompany:
			if n, _ := cols["name"].(string); n == "" {
				return httpx.BadRequest("Enter the company's name.")
			}
		case objContact:
			first, _ := cols["first_name"].(string)
			last, _ := cols["last_name"].(string)
			emails, _ := cols["emails"].([]string)
			if first == "" && last == "" && len(emails) == 0 {
				return httpx.BadRequest("Enter a name or an email address.")
			}
		case objDeal:
			if n, _ := cols["name"].(string); n == "" {
				return httpx.BadRequest("Enter the deal's name.")
			}
			if p.stage == nil {
				st, err := s.defaultStage(ctx, tx, orgID, pipelineID)
				if err != nil {
					return err
				}
				p.stage = st
			} else if pipelineID != nil && *pipelineID != p.stage.PipelineID {
				return httpx.BadRequest("That stage belongs to another pipeline.")
			}
			cols["pipeline_id"], cols["stage_id"] = p.stage.PipelineID, p.stage.ID
			if p.stage.Outcome != "open" {
				cols["closed_at"] = true
			}
			if _, ok := cols["currency"]; !ok {
				var cur string
				if err := tx.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&cur); err != nil {
					return err
				}
				cols["currency"] = cur
			}
			stagePipelines = append(stagePipelines, p.stage.PipelineID)
		}
		if !allowDup {
			if dups, err := s.findDuplicates(ctx, tx, userID, orgID, o, cols, uuid.Nil); err != nil {
				return err
			} else if len(dups) > 0 {
				e := httpx.NewError(http.StatusConflict, "duplicate", duplicateMessage(o, dups))
				e.Details = map[string]any{"matches": dups}
				return e
			}
		}
		if id, err = insertRecord(ctx, tx, o, cols); err != nil {
			return err
		}
		parent, err := parentFor(ctx, tx, orgID, p.team)
		if err != nil {
			return err
		}
		owner := uuid.Nil
		if p.owner != nil {
			owner = *p.owner
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: o.kind(), ID: id, OrgID: orgID, OwnerID: owner, Parent: &parent}); err != nil {
			return err
		}
		if o == objDeal {
			if _, err := tx.Exec(ctx, `INSERT INTO crm_deal_stages (deal_id, org_id, pipeline_id, stage_id, stage_name, entered_by)
				VALUES ($1, $2, $3, $4, $5, $6)`, id, orgID, p.stage.PipelineID, p.stage.ID, p.stage.Name, userID); err != nil {
				return err
			}
		}
		kind := "created"
		if imported {
			kind = "imported"
		}
		if err := addEvent(ctx, tx, orgID, o, id, kind, userID, nil); err != nil {
			return err
		}
		// Associations named at creation: the caller must be able to view them.
		if o == objContact && companyID != nil {
			if err := s.linkContactCompany(ctx, tx, userID, orgID, id, *companyID, true); err != nil {
				return asHTTP(err)
			}
		}
		for _, cid := range contactIDs {
			if o != objDeal {
				break
			}
			if err := s.linkDealContact(ctx, tx, userID, orgID, id, cid); err != nil {
				return asHTTP(err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.boardChanged(ctx, stagePipelines...)
	if p.owner != nil && *p.owner != userID && !imported {
		s.notifyAssigned(ctx, userID, *p.owner, o, id)
	}
	return s.getOne(ctx, s.d.Pool, userID, o, id)
}

// insertRecord writes the row from a column map (column names come from the
// property catalog, never from input).
func insertRecord(ctx context.Context, tx pgx.Tx, o objectType, cols map[string]any) (uuid.UUID, error) {
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	slices.Sort(names)
	vals := make([]string, len(names))
	args := []any{}
	for i, n := range names {
		if !validColumn(o, n) {
			return uuid.Nil, fmt.Errorf("crm: unknown column %s", n)
		}
		if n == "closed_at" { // set to the insert time
			vals[i] = "now()"
			continue
		}
		args = append(args, cols[n])
		vals[i] = fmt.Sprintf("$%d", len(args))
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `INSERT INTO `+o.table()+` (`+strings.Join(names, ", ")+`) VALUES (`+strings.Join(vals, ", ")+`) RETURNING id`, args...).Scan(&id)
	return id, err
}

var columns = map[objectType][]string{
	objCompany: {"org_id", "created_by", "team_id", "props", "tag_ids", "name", "domain", "industry", "employees", "phone", "city", "country", "description"},
	objContact: {"org_id", "created_by", "team_id", "props", "tag_ids", "first_name", "last_name", "emails", "phones", "job_title", "lifecycle_stage", "city", "country"},
	objDeal: {"org_id", "created_by", "team_id", "props", "tag_ids", "name", "amount", "currency", "pipeline_id", "stage_id", "close_date", "company_id",
		"closed_at", "stage_entered_at"},
}

func validColumn(o objectType, c string) bool { return slices.Contains(columns[o], c) }

func (s *Service) defaultStage(ctx context.Context, q db.Querier, orgID uuid.UUID, pipelineID *uuid.UUID) (*stageInfo, error) {
	var stageID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT st.id FROM crm_stages st JOIN crm_pipelines pl ON pl.id = st.pipeline_id
		WHERE pl.org_id = $1 AND ($2::uuid IS NULL OR pl.id = $2) AND st.outcome = 'open'
		ORDER BY pl.position, pl.id, st.position, st.id LIMIT 1`, orgID, pipelineID).Scan(&stageID)
	if db.IsNoRows(err) {
		return nil, httpx.BadRequest("Add a pipeline with an open stage in CRM settings first.")
	}
	if err != nil {
		return nil, err
	}
	return loadStage(ctx, q, orgID, stageID)
}

func (s *Service) updateRecord(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := idParam(r, "id")
		if err != nil {
			return err
		}
		in, err := decodeFields(r)
		if err != nil {
			return err
		}
		rec, err := s.update(r.Context(), me(r), o, id, in)
		if err != nil {
			return err
		}
		httpx.OK(w, rec)
		return nil
	}
}

// update applies field changes (also used by bulk actions, import and the
// update-field agent tool). It needs edit on the record.
func (s *Service) update(ctx context.Context, userID uuid.UUID, o objectType, id uuid.UUID, in map[string]json.RawMessage) (recordish, error) {
	return s.updateWith(ctx, userID, o, id, in, false)
}

// updateWith is update; quiet skips the owner notification (bulk sends one summary).
func (s *Service) updateWith(ctx context.Context, userID uuid.UUID, o objectType, id uuid.UUID, in map[string]json.RawMessage, quiet bool) (recordish, error) {
	var (
		orgID      uuid.UUID
		p          pending
		prevOwner  *uuid.UUID
		accessMove bool
		pipelines  []uuid.UUID
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM `+o.table()+` WHERE id = $1 FOR UPDATE`, id); err != nil {
			return err
		}
		var err error
		if orgID, _, err = s.requireRecord(ctx, tx, userID, o, id, access.Edit); err != nil {
			return err
		}
		cur, err := s.getOne(ctx, tx, userID, o, id)
		if err != nil {
			return err
		}
		if cur.base().Owner != nil {
			prevOwner = &cur.base().Owner.ID
		}
		props, err := s.loadProperties(ctx, tx, orgID, o)
		if err != nil {
			return err
		}
		if err := s.applyFields(ctx, tx, orgID, userID, o, props, cur, in, &p); err != nil {
			return asHTTP(err)
		}
		if len(p.changes) == 0 {
			return nil
		}
		// Owner changes move the record to the new owner's team unless the
		// team was set too.
		ownerChanged := false
		if p.ownerSet {
			before := uuid.Nil
			if prevOwner != nil {
				before = *prevOwner
			}
			after := uuid.Nil
			if p.owner != nil {
				after = *p.owner
			}
			ownerChanged = before != after
			if ownerChanged && !p.teamSet && p.owner != nil {
				if p.team, err = firstTeam(ctx, tx, orgID, *p.owner); err != nil {
					return err
				}
				var curTeam *uuid.UUID
				if cur.base().Team != nil {
					curTeam = &cur.base().Team.ID
				}
				if !equalPtr(curTeam, p.team) {
					p.teamSet = true
					name := ""
					if p.team != nil {
						_ = tx.QueryRow(ctx, `SELECT name FROM access_teams WHERE id = $1`, *p.team).Scan(&name)
					}
					p.changes = append(p.changes, change{Key: "team", Label: "Team", Before: teamName(cur), After: name})
				}
			}
		}
		if p.teamSet {
			p.cols["team_id"] = p.team
		}
		if p.propsSet {
			p.cols["props"] = p.props
		}
		var stageChange *change
		if p.stage != nil {
			d := cur.(*Deal)
			if p.stage.ID != d.Stage.ID {
				pipelines = append(pipelines, d.PipelineID, p.stage.PipelineID)
				if err := moveDealStageTx(ctx, tx, userID, orgID, d, p.stage); err != nil {
					return err
				}
				for i, c := range p.changes {
					if c.Key == "stage" {
						stageChange = &p.changes[i]
					}
				}
			}
		}
		if len(p.cols) > 0 {
			if err := updateColumns(ctx, tx, o, id, p.cols); err != nil {
				return err
			}
		}
		if ownerChanged {
			owner := uuid.Nil
			if p.owner != nil {
				owner = *p.owner
			}
			if err := access.SetOwner(ctx, tx, access.Ref{Kind: o.kind(), ID: id}, owner); err != nil {
				return err
			}
			accessMove = true
		}
		if p.teamSet {
			parent, err := parentFor(ctx, tx, orgID, p.team)
			if err != nil {
				return err
			}
			if err := access.MoveResource(ctx, tx, access.Ref{Kind: o.kind(), ID: id}, &parent); err != nil {
				return err
			}
			accessMove = true
		}
		// The stage move has its own timeline entry.
		changes := []change{}
		for _, c := range p.changes {
			if stageChange == nil || c.Key != "stage" {
				changes = append(changes, c)
			}
		}
		if len(changes) > 0 {
			if err := addEvent(ctx, tx, orgID, o, id, "updated", userID, map[string]any{"changes": changes}); err != nil {
				return err
			}
		}
		if d, ok := cur.(*Deal); ok {
			pipelines = append(pipelines, d.PipelineID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(p.changes) > 0 {
		s.recordChanged(ctx, o, id, true)
		s.boardChanged(ctx, pipelines...)
		if accessMove {
			access.Changed(ctx, access.Ref{Kind: o.kind(), ID: id}, nil)
		}
		if !quiet && p.ownerSet && p.owner != nil && *p.owner != userID && !equalPtr(prevOwner, p.owner) {
			s.notifyAssigned(ctx, userID, *p.owner, o, id)
		}
	}
	return s.getOne(ctx, s.d.Pool, userID, o, id)
}

func teamName(rec recordish) string {
	if t := rec.base().Team; t != nil {
		return t.Name
	}
	return ""
}

func equalPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func updateColumns(ctx context.Context, tx pgx.Tx, o objectType, id uuid.UUID, cols map[string]any) error {
	names := make([]string, 0, len(cols))
	for k := range cols {
		names = append(names, k)
	}
	slices.Sort(names)
	sets := []string{"updated_at = now()"}
	args := []any{id}
	for _, n := range names {
		if !validColumn(o, n) {
			return fmt.Errorf("crm: unknown column %s", n)
		}
		args = append(args, cols[n])
		sets = append(sets, fmt.Sprintf("%s = $%d", n, len(args)))
	}
	_, err := tx.Exec(ctx, `UPDATE `+o.table()+` SET `+strings.Join(sets, ", ")+` WHERE id = $1`, args...)
	return err
}

func (s *Service) notifyAssigned(ctx context.Context, actor, owner uuid.UUID, o objectType, id uuid.UUID) {
	var actorName, title string
	_ = s.d.Pool.QueryRow(ctx, `SELECT display_name FROM users WHERE id = $1`, actor).Scan(&actorName)
	title, _ = recordTitle(ctx, s.d.Pool, o, id)
	s.send(ctx, notify.Notification{UserID: owner, Kind: NotifyAssigned, Title: actorName + " made you the owner of “" + clip(title, 80) + "”",
		Link: o.link(id), Data: map[string]any{"type": o, "id": id}})
}

// ---- delete --------------------------------------------------------------------------

func (s *Service) deleteRecord(o objectType) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := idParam(r, "id")
		if err != nil {
			return err
		}
		ctx := r.Context()
		var pipeline uuid.UUID
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			orgID, _, err := s.requireRecord(ctx, tx, me(r), o, id, access.Edit)
			if err != nil {
				return err
			}
			if !allowedHere(ctx, tx, orgID, me(r), PermDelete) {
				return permissionRequired(PermDelete)
			}
			title, _ := recordTitle(ctx, tx, o, id)
			if o == objDeal {
				_ = tx.QueryRow(ctx, `SELECT pipeline_id FROM crm_deals WHERE id = $1`, id).Scan(&pipeline)
			}
			if err := deleteRecordTx(ctx, tx, o, id); err != nil {
				return err
			}
			return audit.Log(ctx, tx, me(r), "crm.record.delete", o.kind(), id.String(), map[string]any{"org_id": orgID, "title": title})
		})
		if err != nil {
			return err
		}
		s.afterDelete(ctx, o, []uuid.UUID{id}, pipeline)
		httpx.NoContent(w)
		return nil
	}
}

func permissionRequired(key string) *httpx.Error {
	title := key
	if p, ok := access.PermissionOf(key); ok {
		title = p.Title
	}
	e := httpx.NewError(http.StatusForbidden, "permission_required", "You don't have the \""+title+"\" permission in this "+orgs.Noun()+". Ask an admin.")
	e.Details = map[string]any{"permission": key}
	return e
}

// deleteRecordTx removes a record, its timeline, the activities that were
// only on it, and its access resource (grants go with it).
func deleteRecordTx(ctx context.Context, tx pgx.Tx, o objectType, id uuid.UUID) error {
	stmts := []string{
		`DELETE FROM crm_activity_records WHERE record_type = $1 AND record_id = $2`,
		`DELETE FROM crm_activities a WHERE a.record_type = $1 AND a.record_id = $2
			AND NOT EXISTS (SELECT 1 FROM crm_activity_records l WHERE l.activity_id = a.id)`,
		`UPDATE crm_activities a SET record_type = l.record_type, record_id = l.record_id, updated_at = now()
			FROM (SELECT DISTINCT ON (activity_id) activity_id, record_type, record_id FROM crm_activity_records
			      ORDER BY activity_id, created_at) l
			WHERE a.id = l.activity_id AND a.record_type = $1 AND a.record_id = $2`,
		`DELETE FROM crm_events WHERE record_type = $1 AND record_id = $2`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(ctx, q, string(o), id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM `+o.table()+` WHERE id = $1`, id); err != nil {
		return err
	}
	return access.DeleteResource(ctx, tx, access.Ref{Kind: o.kind(), ID: id})
}

func (s *Service) afterDelete(ctx context.Context, o objectType, ids []uuid.UUID, pipelines ...uuid.UUID) {
	for _, id := range ids {
		topic := realtime.Topic("crm", id)
		s.publish(ctx, topic, EventRecordDeleted, map[string]any{"type": o, "id": id})
		_ = realtime.RevokeTopic(context.WithoutCancel(ctx), s.d.Broker, topic)
	}
	s.boardChanged(ctx, pipelines...)
}
