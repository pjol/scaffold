package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// HubSpot adapter: CRM v3 objects and search, associations v4, pipelines,
// properties, owners and engagements (notes, calls, emails, meetings,
// tasks), read and written with the person's token. Endpoints: "api"
// (default https://api.hubapi.com) and "app" (the HubSpot UI, for "Open in
// HubSpot"; default from the account's uiDomain).

const (
	hubspotAPI     = "https://api.hubapi.com"
	hsSearchMax    = 200  // HubSpot's search page limit
	hsScanMax      = 1000 // deals summed for a board or dashboard before totals are partial
	hsAssocPerType = 200  // engagements read per type for a timeline
	hsMaxFilters   = 6
)

type hubspot struct {
	*xconn
	api string
}

func newHubSpot(x *xconn) *hubspot { return &hubspot{xconn: x, api: x.endpoint("api", hubspotAPI)} }

// hubspotAppBase is the HubSpot UI host for a connection.
func hubspotAppBase(c app.Connection) string {
	if v := c.Endpoints["app"]; v != "" {
		return v
	}
	return "https://app.hubspot.com"
}

var hsObjectName = map[objectType]string{objCompany: "companies", objContact: "contacts", objDeal: "deals"}
var hsTypeID = map[objectType]string{objCompany: "0-2", objContact: "0-1", objDeal: "0-3"}

// Default association type ids (HUBSPOT_DEFINED) from an engagement to a record.
var hsEngagementAssoc = map[string]map[objectType]int{
	"notes":    {objContact: 202, objCompany: 190, objDeal: 214},
	"calls":    {objContact: 194, objCompany: 182, objDeal: 206},
	"tasks":    {objContact: 204, objCompany: 192, objDeal: 216},
	"meetings": {objContact: 200, objCompany: 188, objDeal: 212},
	"emails":   {objContact: 198, objCompany: 186, objDeal: 210},
}

// Primary-company association types: contact → company (1), deal → company (5).
var hsPrimaryCompany = map[int]bool{1: true, 5: true}

// hsField maps a CRM property key to a HubSpot property.
type hsField struct{ key, prop string }

var hsBuiltin = map[objectType][]hsField{
	objCompany: {{"name", "name"}, {"domain", "domain"}, {"industry", "industry"}, {"employees", "numberofemployees"}, {"phone", "phone"},
		{"city", "city"}, {"country", "country"}, {"description", "description"}, {"owner", "hubspot_owner_id"},
		{"created_at", "createdate"}, {"updated_at", "hs_lastmodifieddate"}, {"last_activity_at", "notes_last_updated"}},
	objContact: {{"first_name", "firstname"}, {"last_name", "lastname"}, {"emails", "email"}, {"phones", "phone"}, {"job_title", "jobtitle"},
		{"lifecycle_stage", "lifecyclestage"}, {"city", "city"}, {"country", "country"}, {"owner", "hubspot_owner_id"},
		{"created_at", "createdate"}, {"updated_at", "lastmodifieddate"}, {"last_activity_at", "notes_last_updated"}},
	objDeal: {{"name", "dealname"}, {"amount", "amount"}, {"currency", "deal_currency_code"}, {"pipeline", "pipeline"}, {"stage", "dealstage"},
		{"close_date", "closedate"}, {"owner", "hubspot_owner_id"},
		{"created_at", "createdate"}, {"updated_at", "hs_lastmodifieddate"}, {"last_activity_at", "notes_last_updated"}},
}

var hsExtraRead = map[objectType][]string{
	objContact: {"mobilephone", "associatedcompanyid"},
	objDeal:    {"hs_v2_date_entered_current_stage"},
}

// Properties agents never change: who owns a record and which teams see it.
var hsAccessProps = map[string]bool{"hubspot_owner_id": true, "hubspot_team_id": true, "hs_all_owner_ids": true,
	"hs_all_team_ids": true, "hs_all_accessible_team_ids": true, "hs_shared_team_ids": true, "hs_shared_user_ids": true}

func hsPropOf(o objectType, key string) string {
	for _, f := range hsBuiltin[o] {
		if f.key == key {
			return f.prop
		}
	}
	return ""
}

// HubSpot's standard call outcomes (hs_call_disposition ids).
var hsDispositions = map[string]string{
	"f240bbac-87c9-4f6e-bf70-924b57d47db7": "connected", "73a0d17f-1163-4015-bdd5-ec830791da20": "no_answer",
	"b2cf5968-551e-4856-9783-52b3da59a7d0": "left_voicemail", "9d9162e7-6cf3-4944-bf63-4dff82258764": "busy",
	"17b47fee-58de-441e-a44c-c6300d46f273": "wrong_number",
}

func hsDisposition(outcome string) string {
	for k, v := range hsDispositions {
		if v == outcome {
			return k
		}
	}
	return ""
}

var hsLifecycleIn = map[string]string{"marketingqualifiedlead": "mql", "salesqualifiedlead": "sql"}

func hsLifecycle(v string) string {
	if m, ok := hsLifecycleIn[v]; ok {
		return m
	}
	return v
}

func hsLifecycleOut(v string) string {
	for k, m := range hsLifecycleIn {
		if m == v {
			return k
		}
	}
	return v
}

// ---- wire types ------------------------------------------------------------------

type hsObject struct {
	ID         string             `json:"id"`
	Properties map[string]*string `json:"properties"`
	CreatedAt  string             `json:"createdAt"`
	UpdatedAt  string             `json:"updatedAt"`
}

func (o hsObject) p(name string) string {
	if v := o.Properties[name]; v != nil {
		return *v
	}
	return ""
}

type hsPaging struct {
	Next *struct {
		After string `json:"after"`
	} `json:"next"`
}

type hsSearchResult struct {
	Total   int        `json:"total"`
	Results []hsObject `json:"results"`
	Paging  *hsPaging  `json:"paging"`
}

func (r hsSearchResult) after() string {
	if r.Paging != nil && r.Paging.Next != nil {
		return r.Paging.Next.After
	}
	return ""
}

type hsProperty struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	Type           string `json:"type"`
	FieldType      string `json:"fieldType"`
	Hidden         bool   `json:"hidden"`
	HubspotDefined bool   `json:"hubspotDefined"`
	Calculated     bool   `json:"calculated"`
	DisplayOrder   int    `json:"displayOrder"`
	Options        []struct {
		Label  string `json:"label"`
		Value  string `json:"value"`
		Hidden bool   `json:"hidden"`
	} `json:"options"`
	ModificationMetadata struct {
		ReadOnlyValue bool `json:"readOnlyValue"`
	} `json:"modificationMetadata"`
}

type hsAccount struct {
	PortalID        int64  `json:"portalId"`
	CompanyCurrency string `json:"companyCurrency"`
	UIDomain        string `json:"uiDomain"`
}

// ---- requests ------------------------------------------------------------------------

func hubspotParse(_ int, body []byte) (string, map[string]string) {
	var e struct {
		Message  string `json:"message"`
		Category string `json:"category"`
		Errors   []struct {
			Message string              `json:"message"`
			Context map[string][]string `json:"context"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil {
		return "", nil
	}
	fields := map[string]string{}
	msgs := []string{}
	for _, x := range e.Errors {
		if x.Message != "" {
			msgs = append(msgs, x.Message)
		}
		for _, name := range x.Context["propertyName"] {
			fields[name] = x.Message
		}
	}
	msg := e.Message
	if len(msgs) > 0 && (msg == "" || strings.HasPrefix(msg, "Property values were not valid")) {
		msg = strings.Join(msgs, " ")
	}
	return strings.TrimSpace(msg), fields
}

func (h *hubspot) call(ctx context.Context, hc *http.Client, method, path string, query url.Values, body, out any) error {
	if hc == nil {
		hc = h.read
	}
	return xreq{hc: hc, label: "HubSpot", method: method, url: h.api + path, query: query, body: body, parse: hubspotParse}.do(ctx, out)
}

func (h *hubspot) account(ctx context.Context) (hsAccount, error) {
	return cached(h.xconn, "hs.account", func() (hsAccount, error) {
		var a hsAccount
		err := h.call(ctx, nil, http.MethodGet, "/account-info/v3/details", nil, nil, &a)
		if a.CompanyCurrency == "" {
			a.CompanyCurrency = "USD"
		}
		return a, err
	})
}

func (h *hubspot) portalID(ctx context.Context) string {
	if h.conn.AccountID != "" {
		return h.conn.AccountID
	}
	if a, err := h.account(ctx); err == nil && a.PortalID != 0 {
		return strconv.FormatInt(a.PortalID, 10)
	}
	return ""
}

// AccountID is the HubSpot portal id of the connection.
func (h *hubspot) AccountID(ctx context.Context) string { return h.portalID(ctx) }

// RecordURL opens a record in HubSpot.
func (h *hubspot) RecordURL(o objectType, id string) string {
	base := hubspotAppBase(h.conn)
	if a, ok := sourceCache.get(h.userID.String() + "|" + h.conn.ID + "|hs.account"); ok && h.conn.Endpoints["app"] == "" {
		if acc, ok := a.(hsAccount); ok && acc.UIDomain != "" {
			base = "https://" + acc.UIDomain
		}
	}
	portal := h.conn.AccountID
	if portal == "" {
		if a, ok := sourceCache.get(h.userID.String() + "|" + h.conn.ID + "|hs.account"); ok {
			if acc, ok := a.(hsAccount); ok && acc.PortalID != 0 {
				portal = strconv.FormatInt(acc.PortalID, 10)
			}
		}
	}
	if portal == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/contacts/" + url.PathEscape(portal) + "/record/" + hsTypeID[o] + "/" + url.PathEscape(id)
}

func (h *hubspot) recordRef(o objectType, platformID string) *app.SourceRef {
	return withURL(h.ref, h.RecordURL(o, platformID))
}

// ---- schema ---------------------------------------------------------------------------

func (h *hubspot) rawProperties(ctx context.Context, o objectType) ([]hsProperty, error) {
	return cached(h.xconn, "hs.props."+string(o), func() ([]hsProperty, error) {
		var res struct {
			Results []hsProperty `json:"results"`
		}
		err := h.call(ctx, nil, http.MethodGet, "/crm/v3/properties/"+hsObjectName[o], nil, nil, &res)
		return res.Results, err
	})
}

// hsType maps a HubSpot property onto a CRM property type.
func hsType(p hsProperty) string {
	switch {
	case p.Name == "hubspot_owner_id" || p.FieldType == "owner" || p.ExternalOwner():
		return "user"
	case p.Type == "enumeration" && p.FieldType == "checkbox":
		return "multi_select"
	case p.Type == "enumeration":
		return "select"
	case p.Type == "bool":
		return "checkbox"
	case p.Type == "number":
		return "number"
	case p.Type == "date":
		return "date"
	case p.Type == "datetime":
		return "datetime"
	case p.FieldType == "textarea":
		return "long_text"
	case p.FieldType == "phonenumber":
		return "phone"
	}
	return "text"
}

// ExternalOwner is true for HubSpot owner-valued properties other than the owner itself.
func (p hsProperty) ExternalOwner() bool {
	return p.Type == "enumeration" && strings.HasSuffix(p.Name, "_owner_id")
}

// Properties are the CRM's built-in fields this object has in HubSpot, then
// the portal's own (custom, visible) properties, typed for the CRM's editors.
func (h *hubspot) Properties(ctx context.Context, o objectType) ([]Property, error) {
	raw, err := h.rawProperties(ctx, o)
	if err != nil {
		return nil, err
	}
	byName := map[string]hsProperty{}
	for _, p := range raw {
		byName[p.Name] = p
	}
	out := []Property{}
	for _, bp := range builtinProps(o) {
		name := hsPropOf(o, bp.Key)
		if o == objContact && bp.Key == "name" {
			bp.Editable, bp.Filterable, bp.Sortable, bp.Importable = false, false, true, false
			out = append(out, bp)
			continue
		}
		if o == objContact && bp.Key == "company" {
			bp.Editable, bp.Filterable, bp.Sortable, bp.Importable = false, false, false, false
			out = append(out, bp)
			continue
		}
		if o == objDeal && bp.Key == "company" {
			bp.Editable, bp.Filterable, bp.Sortable, bp.Importable = false, false, false, false
			out = append(out, bp)
			continue
		}
		if o == objDeal && (bp.Key == "probability" || bp.Key == "weighted_amount" || bp.Key == "stage_entered_at" || bp.Key == "closed_at") {
			bp.Editable, bp.Filterable, bp.Sortable, bp.Importable = false, false, false, false
			out = append(out, bp)
			continue
		}
		if name == "" {
			continue
		}
		hp, ok := byName[name]
		bp.Importable = false
		if ok {
			bp.Editable = bp.Editable && !hp.ModificationMetadata.ReadOnlyValue && !hp.Calculated
			if bp.Key == "lifecycle_stage" && len(hp.Options) > 0 {
				bp.Options = []Option{}
				for _, op := range hp.Options {
					if !op.Hidden {
						bp.Options = append(bp.Options, Option{Value: hsLifecycle(op.Value), Label: op.Label})
					}
				}
			}
		}
		if bp.Key == "owner" {
			bp.Editable = true
		}
		if bp.Key == "currency" || bp.Key == "pipeline" {
			bp.Editable = false
		}
		out = append(out, bp)
	}
	custom := []hsProperty{}
	for _, p := range raw {
		if !p.HubspotDefined && !p.Hidden {
			custom = append(custom, p)
		}
	}
	sort.SliceStable(custom, func(i, j int) bool { return custom[i].DisplayOrder < custom[j].DisplayOrder })
	for _, p := range custom {
		prop := Property{Key: p.Name, Label: p.Label, Type: hsType(p), Options: []Option{}, Editable: !p.ModificationMetadata.ReadOnlyValue && !p.Calculated,
			Filterable: true, Sortable: p.Type != "enumeration" || p.FieldType != "checkbox"}
		for _, op := range p.Options {
			if !op.Hidden {
				prop.Options = append(prop.Options, Option{Value: op.Value, Label: op.Label})
			}
		}
		out = append(out, prop)
	}
	for i := range out {
		out[i].Position = i
	}
	return out, nil
}

func (h *hubspot) readProps(ctx context.Context, o objectType) []string {
	set := []string{}
	for _, f := range hsBuiltin[o] {
		set = append(set, f.prop)
	}
	set = append(set, hsExtraRead[o]...)
	if raw, err := h.rawProperties(ctx, o); err == nil {
		for _, p := range raw {
			if !p.HubspotDefined && !p.Hidden {
				set = append(set, p.Name)
			}
		}
	}
	return set
}

// Pipelines are HubSpot's deal pipelines with their stages.
func (h *hubspot) Pipelines(ctx context.Context) ([]XPipeline, error) {
	return cached(h.xconn, "hs.pipelines", func() ([]XPipeline, error) {
		var res struct {
			Results []struct {
				ID           string `json:"id"`
				Label        string `json:"label"`
				DisplayOrder int    `json:"displayOrder"`
				Archived     bool   `json:"archived"`
				Stages       []struct {
					ID           string `json:"id"`
					Label        string `json:"label"`
					DisplayOrder int    `json:"displayOrder"`
					Archived     bool   `json:"archived"`
					Metadata     struct {
						IsClosed    string `json:"isClosed"`
						Probability string `json:"probability"`
					} `json:"metadata"`
				} `json:"stages"`
			} `json:"results"`
		}
		if err := h.call(ctx, nil, http.MethodGet, "/crm/v3/pipelines/deals", nil, nil, &res); err != nil {
			return nil, err
		}
		out := []XPipeline{}
		for _, p := range res.Results {
			if p.Archived {
				continue
			}
			pl := XPipeline{ID: p.ID, OrgID: h.orgID, Name: p.Label, Position: p.DisplayOrder, Stages: []XStage{}}
			sort.SliceStable(p.Stages, func(i, j int) bool { return p.Stages[i].DisplayOrder < p.Stages[j].DisplayOrder })
			for i, st := range p.Stages {
				if st.Archived {
					continue
				}
				prob, _ := parseFloat(st.Metadata.Probability)
				outcome := "open"
				if st.Metadata.IsClosed == "true" {
					outcome = "lost"
					if prob >= 1 {
						outcome = "won"
					}
				}
				pl.Stages = append(pl.Stages, XStage{ID: st.ID, Name: st.Label, Position: i, Probability: int(prob*100 + 0.5), Outcome: outcome})
			}
			out = append(out, pl)
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
		return out, nil
	})
}

func (h *hubspot) pipeline(ctx context.Context, id string) (*XPipeline, error) {
	list, err := h.Pipelines(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id || (id == "" && i == 0) {
			return &list[i], nil
		}
	}
	if id == "" {
		return nil, httpx.NotFound("This HubSpot account has no deal pipelines.")
	}
	return nil, httpx.NotFound("That HubSpot pipeline isn't there, or this account can't see it.")
}

// ownerMap is the portal's owners by id (HubSpot users who own records).
func (h *hubspot) ownerMap(ctx context.Context) (map[string]XUserRef, error) {
	return cached(h.xconn, "hs.owners", func() (map[string]XUserRef, error) {
		out := map[string]XUserRef{}
		after := ""
		for range 10 {
			q := url.Values{"limit": {"100"}}
			if after != "" {
				q.Set("after", after)
			}
			var res struct {
				Results []struct {
					ID        string `json:"id"`
					Email     string `json:"email"`
					FirstName string `json:"firstName"`
					LastName  string `json:"lastName"`
				} `json:"results"`
				Paging *hsPaging `json:"paging"`
			}
			if err := h.call(ctx, nil, http.MethodGet, "/crm/v3/owners", q, nil, &res); err != nil {
				return nil, err
			}
			for _, o := range res.Results {
				name := strings.TrimSpace(o.FirstName + " " + o.LastName)
				if name == "" {
					name = o.Email
				}
				out[o.ID] = XUserRef{ID: o.ID, Name: name, Email: o.Email}
			}
			if res.Paging == nil || res.Paging.Next == nil {
				break
			}
			after = res.Paging.Next.After
		}
		return out, nil
	})
}

// Owners lists HubSpot owners by name (owner pickers, task assignees).
func (h *hubspot) Owners(ctx context.Context) ([]XUserRef, error) {
	m, err := h.ownerMap(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]XUserRef, 0, len(m))
	for _, u := range m {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// me is the HubSpot owner id of the connected account ("" when HubSpot
// can't tell: the account's email isn't an owner).
func (h *hubspot) me(ctx context.Context) string {
	id, _ := cached(h.xconn, "hs.me", func() (string, error) {
		email := strings.TrimSpace(h.conn.Account)
		if !strings.Contains(email, "@") {
			return "", nil
		}
		m, err := h.ownerMap(ctx)
		if err != nil {
			return "", err
		}
		for _, u := range m {
			if strings.EqualFold(u.Email, email) {
				return u.ID, nil
			}
		}
		return "", nil
	})
	return id
}

func (h *hubspot) ownerRef(owners map[string]XUserRef, id string) *XUserRef {
	if id == "" {
		return nil
	}
	if u, ok := owners[id]; ok {
		return &u
	}
	return &XUserRef{ID: id, Name: "HubSpot user " + id}
}

// ---- mapping ------------------------------------------------------------------------

// hsMapping is what turning HubSpot objects into CRM records needs.
type hsMapping struct {
	props     []Property
	owners    map[string]XUserRef
	pipelines []XPipeline
	currency  string
	companies map[string]XRecordRef // platform id → company (contacts' and deals' primary company)
}

func (h *hubspot) mapping(ctx context.Context, o objectType) (*hsMapping, error) {
	m := &hsMapping{companies: map[string]XRecordRef{}}
	var errs [4]error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); m.props, errs[0] = h.Properties(ctx, o) }()
	go func() { defer wg.Done(); m.owners, errs[1] = h.ownerMap(ctx) }()
	go func() {
		defer wg.Done()
		var a hsAccount
		a, errs[2] = h.account(ctx)
		m.currency = a.CompanyCurrency
	}()
	wg.Wait()
	if o == objDeal {
		m.pipelines, errs[3] = h.Pipelines(ctx)
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	if m.currency == "" {
		m.currency = "USD"
	}
	return m, nil
}

func (h *hubspot) toRecord(o objectType, obj hsObject, m *hsMapping) xrecordish {
	base := XRecord{ID: xref(h.conn.ID, obj.ID), OrgID: h.orgID, Type: o, Tags: []Tag{}, Props: map[string]any{}, PropsDisplay: map[string]string{},
		MyLevel: h.level(), Source: h.recordRef(o, obj.ID), platformID: obj.ID}
	base.Owner = h.ownerRef(m.owners, obj.p("hubspot_owner_id"))
	if t, ok := parseTime(obj.p("createdate")); ok {
		base.CreatedAt = t
	} else if t, ok := parseTime(obj.CreatedAt); ok {
		base.CreatedAt = t
	}
	if t, ok := parseTime(obj.UpdatedAt); ok {
		base.UpdatedAt = t
	} else {
		base.UpdatedAt = base.CreatedAt
	}
	base.LastActivityAt = timePtr(obj.p("notes_last_updated"))
	for _, p := range m.props {
		if p.Builtin {
			continue
		}
		raw := obj.p(p.Key)
		if raw == "" {
			continue
		}
		switch p.Type {
		case "number":
			if f, ok := parseFloat(raw); ok {
				base.Props[p.Key] = f
			}
		case "checkbox":
			base.Props[p.Key] = raw == "true"
		case "multi_select":
			base.Props[p.Key] = strings.Split(raw, ";")
		case "date":
			if d := dateOnly(raw); d != nil {
				base.Props[p.Key] = *d
			}
		case "user":
			base.Props[p.Key] = raw
			if u := h.ownerRef(m.owners, raw); u != nil {
				base.PropsDisplay[p.Key] = u.Name
			}
		default:
			base.Props[p.Key] = raw
		}
	}
	switch o {
	case objCompany:
		c := &XCompany{XRecord: base, Name: obj.p("name"), Domain: domainOf(obj.p("domain")), Industry: obj.p("industry"),
			Phone: obj.p("phone"), City: obj.p("city"), Country: obj.p("country"), Description: obj.p("description")}
		if n, ok := parseInt(obj.p("numberofemployees")); ok {
			c.Employees = &n
		}
		c.Title = c.Name
		if c.Title == "" {
			c.Title = "Unnamed company"
		}
		return c
	case objContact:
		c := &XContact{XRecord: base, FirstName: obj.p("firstname"), LastName: obj.p("lastname"), Emails: nonEmpty(obj.p("email")),
			Phones: nonEmpty(obj.p("phone"), obj.p("mobilephone")), JobTitle: obj.p("jobtitle"), LifecycleStage: hsLifecycle(obj.p("lifecyclestage")),
			City: obj.p("city"), Country: obj.p("country")}
		c.Name = contactName(c.FirstName, c.LastName, c.Emails)
		c.Title = c.Name
		if id := obj.p("associatedcompanyid"); id != "" {
			if ref, ok := m.companies[id]; ok {
				c.Company = &ref
			}
		}
		return c
	default:
		d := &XDeal{XRecord: base, Name: obj.p("dealname"), Currency: obj.p("deal_currency_code"), PipelineID: obj.p("pipeline"), CloseDate: dateOnly(obj.p("closedate"))}
		if d.Currency == "" {
			d.Currency = m.currency
		}
		d.Title = d.Name
		if d.Title == "" {
			d.Title = "Unnamed deal"
		}
		stageID := obj.p("dealstage")
		for _, pl := range m.pipelines {
			if pl.ID == d.PipelineID {
				d.PipelineName = pl.Name
				if st, ok := pl.stage(stageID); ok {
					d.Stage = st.ref()
				}
			}
		}
		if d.Stage.ID == "" {
			d.Stage = XStageRef{ID: stageID, Name: stageID, Outcome: "open"}
		}
		if f, ok := parseFloat(obj.p("amount")); ok {
			minor := moneyMinor(f, d.Currency)
			d.Amount = &minor
			w := minor * int64(d.Stage.Probability) / 100
			d.Weighted = &w
		}
		d.StageEnteredAt = d.UpdatedAt
		if t, ok := parseTime(obj.p("hs_v2_date_entered_current_stage")); ok {
			d.StageEnteredAt = t
		}
		if d.Stage.Outcome != "open" && d.CloseDate != nil {
			d.ClosedAt = timePtr(*d.CloseDate)
		}
		if ref, ok := m.companies["deal:"+obj.ID]; ok {
			d.Company = &ref
		}
		return d
	}
}

// fillCompanies resolves the primary companies a page of contacts or deals
// shows (contacts: associatedcompanyid; deals: the v4 deal → company link).
func (h *hubspot) fillCompanies(ctx context.Context, o objectType, objs []hsObject, m *hsMapping) error {
	ids := map[string]bool{}
	dealCompany := map[string]string{}
	switch o {
	case objContact:
		for _, obj := range objs {
			if id := obj.p("associatedcompanyid"); id != "" {
				ids[id] = true
			}
		}
	case objDeal:
		if len(objs) == 0 {
			return nil
		}
		links, err := h.batchAssociations(ctx, "deals", "companies", idsOf(objs))
		if err != nil {
			return err
		}
		for from, to := range links {
			if len(to) == 0 {
				continue
			}
			pick := to[0]
			for _, t := range to {
				if t.primary {
					pick = t
				}
			}
			dealCompany[from] = pick.id
			ids[pick.id] = true
		}
	default:
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	comps, err := h.batchRead(ctx, "companies", list, []string{"name", "domain"})
	if err != nil {
		return err
	}
	for _, c := range comps {
		m.companies[c.ID] = XRecordRef{Type: objCompany, ID: xref(h.conn.ID, c.ID), Name: c.p("name")}
	}
	for deal, comp := range dealCompany {
		if ref, ok := m.companies[comp]; ok {
			m.companies["deal:"+deal] = ref
		}
	}
	return nil
}

func idsOf(objs []hsObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}

func (h *hubspot) batchRead(ctx context.Context, object string, ids, props []string) ([]hsObject, error) {
	out := []hsObject{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		inputs := make([]map[string]string, 0, end-start)
		for _, id := range ids[start:end] {
			inputs = append(inputs, map[string]string{"id": id})
		}
		var res struct {
			Results []hsObject `json:"results"`
		}
		if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/"+object+"/batch/read", nil,
			map[string]any{"properties": props, "inputs": inputs}, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Results...)
	}
	return out, nil
}

type hsLink struct {
	id      string
	primary bool
	types   []int
}

// associations lists the ids object/id links to in toObject (v4, paged).
func (h *hubspot) associations(ctx context.Context, object, id, toObject string, max int) ([]hsLink, error) {
	out := []hsLink{}
	after := ""
	for len(out) < max {
		q := url.Values{"limit": {"500"}}
		if after != "" {
			q.Set("after", after)
		}
		var res struct {
			Results []struct {
				ToObjectID json.Number `json:"toObjectId"`
				Types      []struct {
					TypeID int `json:"typeId"`
				} `json:"associationTypes"`
			} `json:"results"`
			Paging *hsPaging `json:"paging"`
		}
		if err := h.call(ctx, nil, http.MethodGet, "/crm/v4/objects/"+object+"/"+url.PathEscape(id)+"/associations/"+toObject, q, nil, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Results {
			l := hsLink{id: r.ToObjectID.String()}
			for _, t := range r.Types {
				l.types = append(l.types, t.TypeID)
				if hsPrimaryCompany[t.TypeID] {
					l.primary = true
				}
			}
			out = append(out, l)
		}
		if res.Paging == nil || res.Paging.Next == nil {
			break
		}
		after = res.Paging.Next.After
	}
	if len(out) > max {
		out = out[:max]
	}
	return out, nil
}

// batchAssociations reads links for many records at once (v4 batch read).
func (h *hubspot) batchAssociations(ctx context.Context, object, toObject string, ids []string) (map[string][]hsLink, error) {
	out := map[string][]hsLink{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		inputs := make([]map[string]string, 0, end-start)
		for _, id := range ids[start:end] {
			inputs = append(inputs, map[string]string{"id": id})
		}
		var res struct {
			Results []struct {
				From struct {
					ID string `json:"id"`
				} `json:"from"`
				To []struct {
					ToObjectID json.Number `json:"toObjectId"`
					Types      []struct {
						TypeID int `json:"typeId"`
					} `json:"associationTypes"`
				} `json:"to"`
			} `json:"results"`
		}
		if err := h.call(ctx, nil, http.MethodPost, "/crm/v4/associations/"+object+"/"+toObject+"/batch/read", nil,
			map[string]any{"inputs": inputs}, &res); err != nil {
			return nil, err
		}
		for _, r := range res.Results {
			for _, t := range r.To {
				l := hsLink{id: t.ToObjectID.String()}
				for _, ty := range t.Types {
					l.types = append(l.types, ty.TypeID)
					if hsPrimaryCompany[ty.TypeID] {
						l.primary = true
					}
				}
				out[r.From.ID] = append(out[r.From.ID], l)
			}
		}
	}
	return out, nil
}

// ---- lists ---------------------------------------------------------------------------

type hsCursor struct {
	After string `json:"a"`
}

// List runs HubSpot's search for o with the list's filters, search text and
// sort. HubSpot's total is the number of matches this account can see.
func (h *hubspot) List(ctx context.Context, o objectType, spec listSpec) (*xPage, error) {
	m, err := h.mapping(ctx, o)
	if err != nil {
		return nil, err
	}
	filters, err := h.filters(ctx, o, m.props, spec)
	if err != nil {
		return nil, err
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = 50
	}
	body := map[string]any{"properties": h.readProps(ctx, o), "limit": min(limit, hsSearchMax)}
	if len(filters) > 0 {
		body["filterGroups"] = []map[string]any{{"filters": filters}}
	}
	if q := strings.TrimSpace(spec.Q); q != "" {
		body["query"] = q
	}
	if sorts := h.sorts(o, m.props, spec); sorts != nil {
		body["sorts"] = sorts
	}
	if spec.Cursor != "" {
		var c hsCursor
		if err := httpx.DecodeCursor(spec.Cursor, &c); err != nil || c.After == "" {
			return nil, httpx.BadRequest("That page cursor isn't valid.")
		}
		body["after"] = c.After
	}
	var res hsSearchResult
	if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/"+hsObjectName[o]+"/search", nil, body, &res); err != nil {
		return nil, err
	}
	if err := h.fillCompanies(ctx, o, res.Results, m); err != nil {
		return nil, err
	}
	page := &xPage{Items: []xrecordish{}}
	for _, obj := range res.Results {
		page.Items = append(page.Items, h.toRecord(o, obj, m))
	}
	if a := res.after(); a != "" && len(res.Results) > 0 {
		page.NextCursor = httpx.EncodeCursor(hsCursor{After: a})
	}
	if spec.Cursor == "" {
		n := res.Total
		page.Total = &n
	}
	return page, nil
}

func (h *hubspot) sorts(o objectType, props []Property, spec listSpec) []map[string]string {
	key := spec.SortKey
	if key == "" {
		return []map[string]string{{"propertyName": "createdate", "direction": "DESCENDING"}}
	}
	name := ""
	switch {
	case o == objContact && key == "name":
		name = "firstname"
	default:
		name = hsPropOf(o, key)
		if name == "" {
			if p, ok := findProp(props, key); ok && !p.Builtin {
				name = p.Key
			}
		}
	}
	if name == "" {
		return nil
	}
	dir := "ASCENDING"
	if spec.SortDir == "desc" {
		dir = "DESCENDING"
	}
	return []map[string]string{{"propertyName": name, "direction": dir}}
}

func errFilterUnsupported(platform, label string) *httpx.Error {
	return httpx.NewError(http.StatusBadRequest, "filter_unsupported", "Filtering on "+label+" isn't available for "+platform+" records.")
}

// filters turns the list's conditions into one HubSpot filter group (AND).
func (h *hubspot) filters(ctx context.Context, o objectType, props []Property, spec listSpec) ([]map[string]any, error) {
	out := []map[string]any{}
	loc, _ := time.LoadLocation(spec.TZ)
	if loc == nil {
		loc = time.UTC
	}
	if spec.xPipeline != "" {
		out = append(out, map[string]any{"propertyName": "pipeline", "operator": "EQ", "value": spec.xPipeline})
	}
	if len(spec.xStages) > 0 {
		out = append(out, map[string]any{"propertyName": "dealstage", "operator": "IN", "values": spec.xStages})
	}
	if spec.xOutcome != "" {
		pls, err := h.Pipelines(ctx)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for _, pl := range pls {
			if spec.xPipeline != "" && pl.ID != spec.xPipeline {
				continue
			}
			for _, st := range pl.Stages {
				if st.Outcome == spec.xOutcome {
					ids = append(ids, st.ID)
				}
			}
		}
		if len(ids) == 0 {
			ids = []string{"-"}
		}
		out = append(out, map[string]any{"propertyName": "dealstage", "operator": "IN", "values": ids})
	}
	for _, f := range spec.Filters {
		p, ok := findProp(props, f.Key)
		if !ok {
			return nil, errFilterUnsupported("HubSpot", f.Key)
		}
		name := hsPropOf(o, f.Key)
		if !p.Builtin {
			name = p.Key
		}
		if name == "" || !p.Filterable {
			return nil, errFilterUnsupported("HubSpot", p.Label)
		}
		v, _ := jsonValue(f.Value)
		item := map[string]any{"propertyName": name}
		val := func(x any) string {
			s := asText(x)
			switch p.Type {
			case "currency":
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					return moneyDecimal(n, "USD")
				}
			case "user":
				if s == "me" {
					return h.me(ctx)
				}
			case "date", "datetime":
				if t, ok := parseTime(s); ok {
					return strconv.FormatInt(t.UnixMilli(), 10)
				}
			case "select":
				if f.Key == "lifecycle_stage" {
					return hsLifecycleOut(s)
				}
			}
			return s
		}
		vals := func() []string {
			list := asStrings(v)
			out := make([]string, 0, len(list))
			for _, x := range list {
				out = append(out, val(x))
			}
			return out
		}
		day := func(s string) (time.Time, time.Time, bool) {
			t, err := time.ParseInLocation("2006-01-02", s, loc)
			if err != nil {
				return time.Time{}, time.Time{}, false
			}
			if p.Type == "date" {
				t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			}
			return t, t.AddDate(0, 0, 1), true
		}
		ms := func(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
		switch f.Op {
		case "eq":
			item["operator"], item["value"] = "EQ", val(v)
		case "neq":
			item["operator"], item["value"] = "NEQ", val(v)
		case "contains":
			item["operator"], item["value"] = "CONTAINS_TOKEN", "*"+asText(v)+"*"
		case "not_contains":
			item["operator"], item["value"] = "NOT_CONTAINS_TOKEN", "*"+asText(v)+"*"
		case "empty":
			item["operator"] = "NOT_HAS_PROPERTY"
		case "not_empty":
			item["operator"] = "HAS_PROPERTY"
		case "gt", "gte", "lt", "lte":
			item["operator"], item["value"] = strings.ToUpper(f.Op), val(v)
		case "is_any", "has_any":
			item["operator"], item["values"] = "IN", vals()
		case "is_none", "has_none":
			item["operator"], item["values"] = "NOT_IN", vals()
		case "is_true":
			item["operator"], item["value"] = "EQ", "true"
		case "is_false":
			item["operator"], item["value"] = "NEQ", "true"
		case "on", "before", "after":
			from, to, ok := day(asText(v))
			if !ok {
				return nil, httpx.BadRequest(p.Label + " needs a date like 2026-11-30.")
			}
			switch f.Op {
			case "on":
				item["operator"], item["value"], item["highValue"] = "BETWEEN", ms(from), ms(to.Add(-time.Millisecond))
			case "before":
				item["operator"], item["value"] = "LT", ms(from)
			default:
				item["operator"], item["value"] = "GTE", ms(to)
			}
		case "last_days", "next_days":
			n, _ := strconv.Atoi(asText(v))
			if n <= 0 {
				return nil, httpx.BadRequest(p.Label + " needs a number of days.")
			}
			now := time.Now().In(loc)
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			if f.Op == "last_days" {
				item["operator"], item["value"], item["highValue"] = "BETWEEN", ms(start.AddDate(0, 0, -n)), ms(now)
			} else {
				item["operator"], item["value"], item["highValue"] = "BETWEEN", ms(now), ms(start.AddDate(0, 0, n+1))
			}
		default:
			return nil, errFilterUnsupported("HubSpot", p.Label+" ("+f.Op+")")
		}
		if (f.Key == "owner" || p.Type == "user") && item["value"] == "" && (f.Op == "eq" || f.Op == "neq") {
			return nil, httpx.BadRequest("HubSpot doesn't list this account as an owner, so “me” can't be used here.")
		}
		out = append(out, item)
	}
	if len(out) > hsMaxFilters {
		return nil, httpx.BadRequest(fmt.Sprintf("HubSpot takes up to %d filters at once.", hsMaxFilters))
	}
	return out, nil
}

// ---- one record ----------------------------------------------------------------------

func (h *hubspot) readOne(ctx context.Context, o objectType, id string) (hsObject, error) {
	var obj hsObject
	err := h.call(ctx, nil, http.MethodGet, "/crm/v3/objects/"+hsObjectName[o]+"/"+url.PathEscape(id),
		url.Values{"properties": {strings.Join(h.readProps(ctx, o), ",")}}, nil, &obj)
	return obj, err
}

// Get reads one record and the companies, contacts and deals it is linked to.
func (h *hubspot) Get(ctx context.Context, o objectType, id string) (*XRecordDetail, error) {
	m, err := h.mapping(ctx, o)
	if err != nil {
		return nil, err
	}
	obj, err := h.readOne(ctx, o, id)
	if err != nil {
		return nil, err
	}
	assoc, primary, err := h.assocOf(ctx, o, id)
	if err != nil {
		return nil, err
	}
	if primary != nil {
		if o == objDeal {
			m.companies["deal:"+id] = *primary
		} else {
			m.companies[obj.p("associatedcompanyid")] = *primary
		}
	} else if err := h.fillCompanies(ctx, o, []hsObject{obj}, m); err != nil {
		return nil, err
	}
	return &XRecordDetail{Record: h.toRecord(o, obj, m), Associations: *assoc}, nil
}

// assocOf reads a record's linked records (v4) with their names.
func (h *hubspot) assocOf(ctx context.Context, o objectType, id string) (*XAssociations, *XRecordRef, error) {
	out := &XAssociations{Companies: []XAssocCompany{}, Contacts: []XAssocContact{}, Deals: []XAssocDeal{}}
	var primary *XRecordRef
	from := hsObjectName[o]
	for _, other := range objectTypes {
		if other == o {
			continue
		}
		links, err := h.associations(ctx, from, id, hsObjectName[other], 100)
		if err != nil {
			return nil, nil, err
		}
		if len(links) == 0 {
			continue
		}
		ids := make([]string, len(links))
		prim := map[string]bool{}
		for i, l := range links {
			ids[i] = l.id
			prim[l.id] = l.primary
		}
		switch other {
		case objCompany:
			objs, err := h.batchRead(ctx, "companies", ids, []string{"name", "domain"})
			if err != nil {
				return nil, nil, err
			}
			for _, c := range objs {
				out.Companies = append(out.Companies, XAssocCompany{ID: xref(h.conn.ID, c.ID), Name: c.p("name"), Domain: domainOf(c.p("domain")), IsPrimary: prim[c.ID]})
				if prim[c.ID] || (primary == nil && len(objs) == 1) {
					primary = &XRecordRef{Type: objCompany, ID: xref(h.conn.ID, c.ID), Name: c.p("name")}
				}
			}
		case objContact:
			objs, err := h.batchRead(ctx, "contacts", ids, []string{"firstname", "lastname", "email", "phone", "jobtitle"})
			if err != nil {
				return nil, nil, err
			}
			for _, c := range objs {
				emails := nonEmpty(c.p("email"))
				out.Contacts = append(out.Contacts, XAssocContact{ID: xref(h.conn.ID, c.ID), Name: contactName(c.p("firstname"), c.p("lastname"), emails),
					Email: strPtr(c.p("email")), Phone: strPtr(c.p("phone")), JobTitle: c.p("jobtitle")})
			}
		case objDeal:
			objs, err := h.batchRead(ctx, "deals", ids, []string{"dealname", "amount", "deal_currency_code", "pipeline", "dealstage", "closedate"})
			if err != nil {
				return nil, nil, err
			}
			pls, err := h.Pipelines(ctx)
			if err != nil {
				return nil, nil, err
			}
			acc, _ := h.account(ctx)
			for _, d := range objs {
				ad := XAssocDeal{ID: xref(h.conn.ID, d.ID), Name: d.p("dealname"), Currency: d.p("deal_currency_code"), CloseDate: dateOnly(d.p("closedate")),
					StageName: d.p("dealstage"), StageOutcome: "open"}
				if ad.Currency == "" {
					ad.Currency = acc.CompanyCurrency
				}
				for _, pl := range pls {
					if pl.ID == d.p("pipeline") {
						if st, ok := pl.stage(d.p("dealstage")); ok {
							ad.StageName, ad.StageOutcome = st.Name, st.Outcome
						}
					}
				}
				if f, ok := parseFloat(d.p("amount")); ok {
					n := moneyMinor(f, ad.Currency)
					ad.Amount = &n
				}
				out.Deals = append(out.Deals, ad)
			}
		}
	}
	return out, primary, nil
}

// ---- timeline ----------------------------------------------------------------------------

var hsEngagementTypes = []string{"notes", "calls", "emails", "meetings", "tasks"}

var hsEngagementProps = map[string][]string{
	"notes":    {"hs_note_body", "hs_timestamp", "hubspot_owner_id", "hs_createdate", "hs_lastmodifieddate"},
	"calls":    {"hs_call_title", "hs_call_body", "hs_call_duration", "hs_call_disposition", "hs_call_direction", "hs_timestamp", "hubspot_owner_id", "hs_createdate", "hs_lastmodifieddate"},
	"emails":   {"hs_email_subject", "hs_email_text", "hs_email_direction", "hs_timestamp", "hubspot_owner_id", "hs_createdate", "hs_lastmodifieddate"},
	"meetings": {"hs_meeting_title", "hs_meeting_body", "hs_meeting_start_time", "hs_meeting_end_time", "hs_meeting_outcome", "hs_timestamp", "hubspot_owner_id", "hs_createdate", "hs_lastmodifieddate"},
	"tasks":    {"hs_task_subject", "hs_task_body", "hs_task_status", "hs_task_priority", "hs_timestamp", "hs_task_completion_date", "hs_task_reminders", "hubspot_owner_id", "hs_createdate", "hs_lastmodifieddate"},
}

var hsTypeOf = map[string]string{"notes": "note", "calls": "call", "emails": "email", "meetings": "meeting", "tasks": "task"}

func (h *hubspot) toActivity(kind string, obj hsObject, owners map[string]XUserRef, records []XRecordRef) *XActivity {
	a := &XActivity{ID: xref(h.conn.ID, kind+"."+obj.ID), OrgID: h.orgID, Type: hsTypeOf[kind], Records: records, Mentions: []Mention{},
		Priority: "none", Source: h.ref}
	if a.Records == nil {
		a.Records = []XRecordRef{}
	}
	created := timePtr(obj.p("hs_createdate"))
	if created == nil {
		created = timePtr(obj.CreatedAt)
	}
	if created != nil {
		a.CreatedAt = *created
	}
	a.UpdatedAt = a.CreatedAt
	if t := timePtr(obj.p("hs_lastmodifieddate")); t != nil {
		a.UpdatedAt = *t
	}
	if t := timePtr(obj.p("hs_timestamp")); t != nil {
		a.OccurredAt = *t
	} else {
		a.OccurredAt = a.CreatedAt
	}
	owner := h.ownerRef(owners, obj.p("hubspot_owner_id"))
	a.CreatedBy = owner
	switch kind {
	case "notes":
		a.Body = htmlText(obj.p("hs_note_body"))
	case "calls":
		a.Subject, a.Body = obj.p("hs_call_title"), htmlText(obj.p("hs_call_body"))
		if ms, ok := parseInt(obj.p("hs_call_duration")); ok {
			n := ms / 60000
			a.DurationMinutes = &n
		}
		if o, ok := hsDispositions[obj.p("hs_call_disposition")]; ok {
			a.Outcome = &o
		}
		switch obj.p("hs_call_direction") {
		case "INBOUND":
			a.Direction = strPtr("inbound")
		case "OUTBOUND":
			a.Direction = strPtr("outbound")
		}
	case "emails":
		a.Subject, a.Body = obj.p("hs_email_subject"), obj.p("hs_email_text")
		if obj.p("hs_email_direction") == "INCOMING_EMAIL" {
			a.Direction = strPtr("inbound")
		} else if obj.p("hs_email_direction") != "" {
			a.Direction = strPtr("outbound")
		}
	case "meetings":
		a.Subject, a.Body = obj.p("hs_meeting_title"), htmlText(obj.p("hs_meeting_body"))
		if t := timePtr(obj.p("hs_meeting_start_time")); t != nil {
			a.OccurredAt = *t
			if e := timePtr(obj.p("hs_meeting_end_time")); e != nil && e.After(*t) {
				n := int(e.Sub(*t).Minutes())
				a.DurationMinutes = &n
			}
		}
		if o := strings.ToLower(obj.p("hs_meeting_outcome")); o != "" {
			o = strings.ReplaceAll(o, "no_show", "no_show")
			a.Outcome = &o
		}
	case "tasks":
		a.Subject, a.Body = obj.p("hs_task_subject"), htmlText(obj.p("hs_task_body"))
		a.DueAt = timePtr(obj.p("hs_timestamp"))
		a.Assignee = owner
		if obj.p("hs_task_status") == "COMPLETED" {
			done := timePtr(obj.p("hs_task_completion_date"))
			if done == nil {
				done = &a.UpdatedAt
			}
			a.DoneAt = done
		}
		if p := strings.ToLower(obj.p("hs_task_priority")); slices.Contains(priorities, p) {
			a.Priority = p
		}
		if r := strings.Split(obj.p("hs_task_reminders"), ";"); len(r) > 0 && r[0] != "" {
			a.ReminderAt = timePtr(r[0])
		}
		a.CanEdit = h.write
	}
	return a
}

// htmlText turns HubSpot's HTML bodies into the plain text the timeline shows.
func htmlText(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	r := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</p>", "\n", "</div>", "\n", "</li>", "\n")
	s = r.Replace(s)
	var b strings.Builder
	in := false
	for _, c := range s {
		switch {
		case c == '<':
			in = true
		case c == '>':
			in = false
		case !in:
			b.WriteRune(c)
		}
	}
	out := strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#39;", "'").Replace(b.String())
	return strings.TrimSpace(out)
}

// Timeline merges the record's engagements, newest first.
func (h *hubspot) Timeline(ctx context.Context, o objectType, id, filter, cursor string, limit int) (*XTimelinePage, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	kinds := hsEngagementTypes
	switch filter {
	case "", "all", "activities":
	case "changes":
		return &XTimelinePage{Items: []XTimelineItem{}, Upcoming: []*XActivity{}}, nil
	default:
		k := strings.TrimSuffix(filter, "s") + "s"
		if !slices.Contains(hsEngagementTypes, k) {
			return nil, httpx.BadRequest("filter is all, activities, notes, calls, emails, meetings, tasks or changes.")
		}
		kinds = []string{k}
	}
	owners, err := h.ownerMap(ctx)
	if err != nil {
		return nil, err
	}
	self := XRecordRef{Type: o, ID: xref(h.conn.ID, id)}
	all := []*XActivity{}
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for _, kind := range kinds {
		wg.Add(1)
		go func(kind string) {
			defer wg.Done()
			links, err := h.associations(ctx, hsObjectName[o], id, kind, hsAssocPerType)
			if err == nil && len(links) > 0 {
				ids := make([]string, len(links))
				for i, l := range links {
					ids[i] = l.id
				}
				var objs []hsObject
				objs, err = h.batchRead(ctx, kind, ids, hsEngagementProps[kind])
				if err == nil {
					mu.Lock()
					for _, obj := range objs {
						all = append(all, h.toActivity(kind, obj, owners, []XRecordRef{self}))
					}
					mu.Unlock()
				}
			}
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(kind)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return pageTimeline(all, cursor, limit), nil
}

// pageTimeline sorts activities newest first, pages them, and lists open
// tasks (soonest due first) on the first page.
func pageTimeline(all []*XActivity, cursor string, limit int) *XTimelinePage {
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].OccurredAt.Equal(all[j].OccurredAt) {
			return all[i].OccurredAt.After(all[j].OccurredAt)
		}
		return all[i].ID > all[j].ID
	})
	off := readOffset(cursor)
	page := &XTimelinePage{Items: []XTimelineItem{}}
	for i := off; i < len(all) && i < off+limit; i++ {
		page.Items = append(page.Items, XTimelineItem{ID: all[i].ID, Kind: "activity", At: all[i].OccurredAt, Activity: all[i]})
	}
	if off+limit < len(all) {
		page.NextCursor = httpx.EncodeCursor(offsetCursor{O: off + limit})
	}
	if cursor == "" {
		page.Upcoming = []*XActivity{}
		for _, a := range all {
			if a.Type == "task" && a.DoneAt == nil {
				page.Upcoming = append(page.Upcoming, a)
			}
		}
		sort.SliceStable(page.Upcoming, func(i, j int) bool {
			a, b := page.Upcoming[i].DueAt, page.Upcoming[j].DueAt
			if a == nil || b == nil {
				return a != nil
			}
			return a.Before(*b)
		})
	}
	return page
}

// ---- board and dashboard -----------------------------------------------------------------------

// scanDeals reads up to hsScanMax deals matching filters (amount, stage,
// currency only) and HubSpot's total.
func (h *hubspot) scanDeals(ctx context.Context, filters []map[string]any, q string, props []string) ([]hsObject, int, error) {
	out := []hsObject{}
	total, after := 0, ""
	for len(out) < hsScanMax {
		body := map[string]any{"properties": props, "limit": hsSearchMax,
			"sorts": []map[string]string{{"propertyName": "closedate", "direction": "ASCENDING"}}}
		if len(filters) > 0 {
			body["filterGroups"] = []map[string]any{{"filters": filters}}
		}
		if q != "" {
			body["query"] = q
		}
		if after != "" {
			body["after"] = after
		}
		var res hsSearchResult
		if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/deals/search", nil, body, &res); err != nil {
			return nil, 0, err
		}
		total = res.Total
		out = append(out, res.Results...)
		if after = res.after(); after == "" || len(res.Results) == 0 {
			break
		}
	}
	return out, total, nil
}

// searchTotal is HubSpot's count of matches for filters on object.
func (h *hubspot) searchTotal(ctx context.Context, object, q string, filters []map[string]any) (int, error) {
	body := map[string]any{"limit": 1, "properties": []string{"hs_object_id"}}
	if q != "" {
		body["query"] = q
	}
	if len(filters) > 0 {
		body["filterGroups"] = []map[string]any{{"filters": filters}}
	}
	var res hsSearchResult
	err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/"+object+"/search", nil, body, &res)
	return res.Total, err
}

// Board shows a HubSpot pipeline's stages with the account's deals: counts
// from HubSpot's totals, sums over up to 1,000 deals (partial past that).
func (h *hubspot) Board(ctx context.Context, pipelineID string, spec listSpec) (*XBoard, error) {
	pl, err := h.pipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	m, err := h.mapping(ctx, objDeal)
	if err != nil {
		return nil, err
	}
	spec.xPipeline = pl.ID
	filters, err := h.filters(ctx, objDeal, m.props, spec)
	if err != nil {
		return nil, err
	}
	q := strings.TrimSpace(spec.Q)
	objs, total, err := h.scanDeals(ctx, filters, q, h.readProps(ctx, objDeal))
	if err != nil {
		return nil, err
	}
	board := &XBoard{Pipeline: *pl, Columns: []XBoardColumn{}, Open: []MoneyTotal{}, Source: h.ref, Partial: total > len(objs)}
	byStage := map[string][]hsObject{}
	for _, o := range objs {
		byStage[o.p("dealstage")] = append(byStage[o.p("dealstage")], o)
	}
	cards := []hsObject{}
	for _, st := range pl.Stages {
		list := byStage[st.ID]
		if len(list) > boardColumnSize {
			list = list[:boardColumnSize]
		}
		cards = append(cards, list...)
	}
	if err := h.fillCompanies(ctx, objDeal, cards, m); err != nil {
		return nil, err
	}
	for _, st := range pl.Stages {
		col := XBoardColumn{Stage: st, Totals: []MoneyTotal{}, Deals: []*XDeal{}}
		for i, o := range byStage[st.ID] {
			d := h.toRecord(objDeal, o, m).(*XDeal)
			if d.Amount != nil {
				col.Totals = addTotal(col.Totals, d.Currency, *d.Amount, *d.Weighted)
				if st.Outcome == "open" {
					board.Open = addTotal(board.Open, d.Currency, *d.Amount, *d.Weighted)
				}
			}
			if i < boardColumnSize {
				col.Deals = append(col.Deals, d)
			}
		}
		col.Count = len(byStage[st.ID])
		if board.Partial {
			n, err := h.searchTotal(ctx, "deals", q, append(slices.Clone(filters), map[string]any{"propertyName": "dealstage", "operator": "EQ", "value": st.ID}))
			if err != nil {
				return nil, err
			}
			col.Count = n
		}
		board.Columns = append(board.Columns, col)
	}
	return board, nil
}

// Dashboard computes the CRM dashboard from what the account can see.
func (h *hubspot) Dashboard(ctx context.Context, pipelineID string, loc *time.Location) (*XDashboard, error) {
	board, err := h.Board(ctx, pipelineID, listSpec{})
	if err != nil {
		return nil, err
	}
	acc, _ := h.account(ctx)
	out := &XDashboard{Pipeline: board.Pipeline, Currency: acc.CompanyCurrency, Open: []XStageSummary{}, OpenTotals: board.Open,
		Won: ClosedSummary{Totals: []MoneyTotal{}}, Lost: ClosedSummary{Totals: []MoneyTotal{}}, Activity: []XOwnerActivity{}, Source: h.ref}
	if out.Currency == "" {
		out.Currency = "USD"
	}
	var won, lost []string
	for _, c := range board.Columns {
		switch c.Stage.Outcome {
		case "open":
			out.Open = append(out.Open, XStageSummary{StageID: c.Stage.ID, Name: c.Stage.Name, Count: c.Count, Totals: c.Totals})
		case "won":
			won = append(won, c.Stage.ID)
		case "lost":
			lost = append(lost, c.Stage.ID)
		}
	}
	_, _, weekStart, monthStart, monthEnd := timeBounds(loc)
	ms := func(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
	closed := func(stages []string) (ClosedSummary, error) {
		sum := ClosedSummary{Totals: []MoneyTotal{}}
		if len(stages) == 0 {
			return sum, nil
		}
		objs, total, err := h.scanDeals(ctx, []map[string]any{
			{"propertyName": "pipeline", "operator": "EQ", "value": board.Pipeline.ID},
			{"propertyName": "dealstage", "operator": "IN", "values": stages},
			{"propertyName": "closedate", "operator": "BETWEEN", "value": ms(monthStart), "highValue": ms(monthEnd.Add(-time.Millisecond))},
		}, "", []string{"amount", "deal_currency_code"})
		if err != nil {
			return sum, err
		}
		sum.Count = total
		for _, o := range objs {
			cur := o.p("deal_currency_code")
			if cur == "" {
				cur = out.Currency
			}
			if f, ok := parseFloat(o.p("amount")); ok {
				n := moneyMinor(f, cur)
				sum.Totals = addTotal(sum.Totals, cur, n, n)
			}
		}
		return sum, nil
	}
	if out.Won, err = closed(won); err != nil {
		return nil, err
	}
	if out.Lost, err = closed(lost); err != nil {
		return nil, err
	}
	// Activity this week, per owner.
	owners, err := h.ownerMap(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]*XOwnerActivity{}
	bump := func(owner string, f func(a *XOwnerActivity)) {
		if owner == "" {
			return
		}
		a, ok := counts[owner]
		if !ok {
			a = &XOwnerActivity{User: *h.ownerRef(owners, owner)}
			counts[owner] = a
		}
		f(a)
		a.Total++
	}
	for _, kind := range []string{"notes", "calls", "emails", "meetings", "tasks"} {
		filters := []map[string]any{{"propertyName": "hs_timestamp", "operator": "GTE", "value": ms(weekStart)}}
		if kind == "tasks" {
			filters = []map[string]any{{"propertyName": "hs_task_status", "operator": "EQ", "value": "COMPLETED"},
				{"propertyName": "hs_task_completion_date", "operator": "GTE", "value": ms(weekStart)}}
		}
		objs, err := h.scan(ctx, kind, filters, []string{"hubspot_owner_id"})
		if err != nil {
			return nil, err
		}
		for _, o := range objs {
			bump(o.p("hubspot_owner_id"), func(a *XOwnerActivity) {
				switch kind {
				case "notes":
					a.Notes++
				case "calls":
					a.Calls++
				case "emails":
					a.Emails++
				case "meetings":
					a.Meetings++
				default:
					a.Tasks++
				}
			})
		}
	}
	for _, a := range counts {
		out.Activity = append(out.Activity, *a)
	}
	sort.Slice(out.Activity, func(i, j int) bool {
		if out.Activity[i].Total != out.Activity[j].Total {
			return out.Activity[i].Total > out.Activity[j].Total
		}
		return out.Activity[i].User.Name < out.Activity[j].User.Name
	})
	if len(out.Activity) > 10 {
		out.Activity = out.Activity[:10]
	}
	out.Tasks, err = h.taskSummary(ctx, loc)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// scan reads up to hsScanMax objects of a type matching filters.
func (h *hubspot) scan(ctx context.Context, object string, filters []map[string]any, props []string) ([]hsObject, error) {
	out := []hsObject{}
	after := ""
	for len(out) < hsScanMax {
		body := map[string]any{"properties": props, "limit": hsSearchMax, "filterGroups": []map[string]any{{"filters": filters}}}
		if after != "" {
			body["after"] = after
		}
		var res hsSearchResult
		if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/"+object+"/search", nil, body, &res); err != nil {
			return nil, err
		}
		out = append(out, res.Results...)
		if after = res.after(); after == "" || len(res.Results) == 0 {
			break
		}
	}
	return out, nil
}

// ---- tasks -------------------------------------------------------------------------------

func (h *hubspot) taskFilters(ctx context.Context, bucket string, loc *time.Location) ([]map[string]any, []map[string]string, error) {
	dayStart, dayEnd, _, _, _ := timeBounds(loc)
	ms := func(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
	filters := []map[string]any{}
	if me := h.me(ctx); me != "" {
		filters = append(filters, map[string]any{"propertyName": "hubspot_owner_id", "operator": "EQ", "value": me})
	}
	asc := []map[string]string{{"propertyName": "hs_timestamp", "direction": "ASCENDING"}}
	open := map[string]any{"propertyName": "hs_task_status", "operator": "NEQ", "value": "COMPLETED"}
	switch bucket {
	case "done":
		return append(filters, map[string]any{"propertyName": "hs_task_status", "operator": "EQ", "value": "COMPLETED"}),
			[]map[string]string{{"propertyName": "hs_task_completion_date", "direction": "DESCENDING"}}, nil
	case "overdue":
		return append(filters, open, map[string]any{"propertyName": "hs_timestamp", "operator": "LT", "value": ms(dayStart)}), asc, nil
	case "today":
		return append(filters, open, map[string]any{"propertyName": "hs_timestamp", "operator": "BETWEEN", "value": ms(dayStart), "highValue": ms(dayEnd.Add(-time.Millisecond))}), asc, nil
	case "upcoming":
		return append(filters, open, map[string]any{"propertyName": "hs_timestamp", "operator": "GTE", "value": ms(dayEnd)}), asc, nil
	case "", "open":
		return append(filters, open), asc, nil
	}
	return nil, nil, httpx.BadRequest("bucket is open, overdue, today, upcoming or done.")
}

// Tasks lists the account's HubSpot tasks in a bucket, with the records each is on.
func (h *hubspot) Tasks(ctx context.Context, bucket string, loc *time.Location, cursor string, limit int) (*xActivityPage, error) {
	filters, sorts, err := h.taskFilters(ctx, bucket, loc)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > hsSearchMax {
		limit = 50
	}
	body := map[string]any{"properties": hsEngagementProps["tasks"], "limit": limit, "filterGroups": []map[string]any{{"filters": filters}}, "sorts": sorts}
	if cursor != "" {
		var c hsCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil {
			return nil, httpx.BadRequest("That page cursor isn't valid.")
		}
		body["after"] = c.After
	}
	var res hsSearchResult
	if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/tasks/search", nil, body, &res); err != nil {
		return nil, err
	}
	owners, err := h.ownerMap(ctx)
	if err != nil {
		return nil, err
	}
	records, err := h.engagementRecords(ctx, "tasks", idsOf(res.Results))
	if err != nil {
		return nil, err
	}
	page := &xActivityPage{Items: []*XActivity{}}
	for _, obj := range res.Results {
		page.Items = append(page.Items, h.toActivity("tasks", obj, owners, records[obj.ID]))
	}
	if a := res.after(); a != "" && len(res.Results) > 0 {
		page.NextCursor = httpx.EncodeCursor(hsCursor{After: a})
	}
	return page, nil
}

// engagementRecords names the records each engagement is linked to.
func (h *hubspot) engagementRecords(ctx context.Context, kind string, ids []string) (map[string][]XRecordRef, error) {
	out := map[string][]XRecordRef{}
	if len(ids) == 0 {
		return out, nil
	}
	for _, o := range []objectType{objContact, objCompany, objDeal} {
		links, err := h.batchAssociations(ctx, kind, hsObjectName[o], ids)
		if err != nil {
			return nil, err
		}
		need := map[string]bool{}
		for _, ls := range links {
			for _, l := range ls {
				need[l.id] = true
			}
		}
		if len(need) == 0 {
			continue
		}
		list := make([]string, 0, len(need))
		for id := range need {
			list = append(list, id)
		}
		props := map[objectType][]string{objContact: {"firstname", "lastname", "email"}, objCompany: {"name"}, objDeal: {"dealname"}}[o]
		objs, err := h.batchRead(ctx, hsObjectName[o], list, props)
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		for _, x := range objs {
			switch o {
			case objContact:
				names[x.ID] = contactName(x.p("firstname"), x.p("lastname"), nonEmpty(x.p("email")))
			case objCompany:
				names[x.ID] = x.p("name")
			default:
				names[x.ID] = x.p("dealname")
			}
		}
		for from, ls := range links {
			for _, l := range ls {
				if n, ok := names[l.id]; ok {
					out[from] = append(out[from], XRecordRef{Type: o, ID: xref(h.conn.ID, l.id), Name: n})
				}
			}
		}
	}
	return out, nil
}

// TaskSummary counts the account's open HubSpot tasks by when they're due.
func (h *hubspot) TaskSummary(ctx context.Context, loc *time.Location) (TaskSummary, error) {
	return h.taskSummary(ctx, loc)
}

func (h *hubspot) taskSummary(ctx context.Context, loc *time.Location) (TaskSummary, error) {
	var out TaskSummary
	for _, b := range []string{"overdue", "today", "upcoming"} {
		filters, _, err := h.taskFilters(ctx, b, loc)
		if err != nil {
			return out, err
		}
		n, err := h.searchTotal(ctx, "tasks", "", filters)
		if err != nil {
			return out, err
		}
		switch b {
		case "overdue":
			out.Overdue = n
		case "today":
			out.Today = n
		default:
			out.Upcoming = n
		}
	}
	return out, nil
}

// SetTaskDone completes or reopens a HubSpot task.
func (h *hubspot) SetTaskDone(ctx context.Context, activityID string, done bool) (*XActivity, error) {
	kind, id, ok := strings.Cut(activityID, ".")
	if !ok || kind != "tasks" {
		return nil, httpx.BadRequest("Only tasks can be marked done.")
	}
	hc, err := h.writer(ctx)
	if err != nil {
		return nil, err
	}
	status := "NOT_STARTED"
	if done {
		status = "COMPLETED"
	}
	var obj hsObject
	if err := h.call(ctx, hc, http.MethodPatch, "/crm/v3/objects/tasks/"+url.PathEscape(id), nil,
		map[string]any{"properties": map[string]string{"hs_task_status": status}}, &obj); err != nil {
		return nil, err
	}
	var full hsObject
	if err := h.call(ctx, nil, http.MethodGet, "/crm/v3/objects/tasks/"+url.PathEscape(id),
		url.Values{"properties": {strings.Join(hsEngagementProps["tasks"], ",")}}, nil, &full); err != nil {
		return nil, err
	}
	owners, _ := h.ownerMap(ctx)
	recs, _ := h.engagementRecords(ctx, "tasks", []string{id})
	return h.toActivity("tasks", full, owners, recs[id]), nil
}

// ---- search ----------------------------------------------------------------------------------

// Search runs HubSpot's search per type with the text.
func (h *hubspot) Search(ctx context.Context, q string, types []objectType, limit int) ([]XSearchHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(types) == 0 {
		types = objectTypes
	}
	out := []XSearchHit{}
	var pls []XPipeline
	for _, o := range types {
		props := map[objectType][]string{
			objCompany: {"name", "domain", "hs_lastmodifieddate"},
			objContact: {"firstname", "lastname", "email", "lastmodifieddate"},
			objDeal:    {"dealname", "dealstage", "pipeline", "hs_lastmodifieddate"},
		}[o]
		body := map[string]any{"properties": props, "limit": limit,
			"sorts": []map[string]string{{"propertyName": hsPropOf(o, "updated_at"), "direction": "DESCENDING"}}}
		if q = strings.TrimSpace(q); q != "" {
			body["query"] = q
		}
		var res hsSearchResult
		if err := h.call(ctx, nil, http.MethodPost, "/crm/v3/objects/"+hsObjectName[o]+"/search", nil, body, &res); err != nil {
			return nil, err
		}
		if o == objDeal && len(res.Results) > 0 && pls == nil {
			pls, _ = h.Pipelines(ctx)
		}
		for i, obj := range res.Results {
			hit := XSearchHit{Type: o, ID: xref(h.conn.ID, obj.ID), OrgID: h.orgID, Score: 0.6 - float64(i)*0.01, Source: h.recordRef(o, obj.ID)}
			if t, ok := parseTime(obj.UpdatedAt); ok {
				hit.At = t
			}
			switch o {
			case objCompany:
				hit.Title, hit.Detail = obj.p("name"), obj.p("domain")
			case objContact:
				hit.Title, hit.Detail = contactName(obj.p("firstname"), obj.p("lastname"), nonEmpty(obj.p("email"))), obj.p("email")
			default:
				hit.Title, hit.Detail = obj.p("dealname"), obj.p("dealstage")
				for _, pl := range pls {
					if st, ok := pl.stage(obj.p("dealstage")); ok && pl.ID == obj.p("pipeline") {
						hit.Detail = st.Name
					}
				}
			}
			out = append(out, hit)
		}
	}
	return out, nil
}

// ---- writes ------------------------------------------------------------------------------------

// toHubSpot converts field values (CRM property keys) into HubSpot property
// values, refusing fields this account can't change from here.
func (h *hubspot) toHubSpot(ctx context.Context, o objectType, props []Property, fields map[string]json.RawMessage, currency string) (map[string]string, error) {
	out := map[string]string{}
	for key, raw := range fields {
		p, ok := findProp(props, key)
		if !ok {
			return nil, fieldValueError(key, "There's no field “"+key+"” on HubSpot "+o.labelPlural()+".")
		}
		if !p.Editable {
			return nil, fieldValueError(p.Label, p.Label+" can't be changed from here.")
		}
		name := hsPropOf(o, key)
		if !p.Builtin {
			name = p.Key
		}
		v, err := jsonValue(raw)
		if err != nil {
			return nil, fieldValueError(p.Label, p.Label+" isn't a valid value.")
		}
		if v == nil {
			out[name] = ""
			continue
		}
		switch p.Type {
		case "currency":
			n, err := strconv.ParseInt(asText(v), 10, 64)
			if err != nil {
				return nil, fieldValueError(p.Label, p.Label+" is an amount in minor units (cents).")
			}
			out[name] = moneyDecimalString(n, currency)
		case "number":
			if _, ok := parseFloat(asText(v)); !ok {
				return nil, fieldValueError(p.Label, p.Label+" is a number.")
			}
			out[name] = asText(v)
		case "checkbox":
			b, ok := v.(bool)
			if !ok {
				return nil, fieldValueError(p.Label, p.Label+" is true or false.")
			}
			out[name] = strconv.FormatBool(b)
		case "multi_select":
			list := asStrings(v)
			for _, x := range list {
				if !hasOption(p.Options, x) {
					return nil, fieldValueError(p.Label, "“"+x+"” isn't an option of "+p.Label+".")
				}
			}
			out[name] = strings.Join(list, ";")
		case "select":
			s := asText(v)
			if len(p.Options) > 0 && !hasOption(p.Options, s) {
				return nil, fieldValueError(p.Label, "“"+s+"” isn't an option of "+p.Label+".")
			}
			if key == "lifecycle_stage" {
				s = hsLifecycleOut(s)
			}
			out[name] = s
		case "date":
			s := asText(v)
			if !validDate(s) {
				return nil, fieldValueError(p.Label, p.Label+" is a date like 2026-11-30.")
			}
			out[name] = s
		case "emails", "phones":
			list := asStrings(v)
			if len(list) == 0 {
				out[name] = ""
			} else {
				out[name] = list[0]
			}
		case "domain":
			d, err := cleanDomain(asText(v))
			if err != nil {
				return nil, fieldValueError(p.Label, err.Error())
			}
			out[name] = d
		case "user":
			id := asText(v)
			owners, err := h.ownerMap(ctx)
			if err != nil {
				return nil, err
			}
			if _, ok := owners[id]; !ok {
				return nil, fieldValueError(p.Label, "That person isn't a HubSpot owner in this account.")
			}
			out[name] = id
		case "stage":
			out[name] = asText(v)
		default:
			out[name] = asText(v)
		}
	}
	return out, nil
}

// Update writes field changes to HubSpot (HubSpot validates them; its
// message comes back as a 422 naming the field).
func (h *hubspot) Update(ctx context.Context, o objectType, id string, fields map[string]json.RawMessage) (xrecordish, error) {
	if len(fields) == 0 {
		d, err := h.Get(ctx, o, id)
		if err != nil {
			return nil, err
		}
		return d.Record, nil
	}
	hc, err := h.writer(ctx)
	if err != nil {
		return nil, err
	}
	m, err := h.mapping(ctx, o)
	if err != nil {
		return nil, err
	}
	cur := m.currency
	if o == objDeal {
		if obj, err := h.readOne(ctx, o, id); err == nil && obj.p("deal_currency_code") != "" {
			cur = obj.p("deal_currency_code")
		}
	}
	if raw, ok := fields["stage"]; ok && o == objDeal {
		var stageID string
		_ = json.Unmarshal(raw, &stageID)
		if _, err := h.MoveStage(ctx, id, stageID); err != nil {
			return nil, err
		}
		delete(fields, "stage")
	}
	props, err := h.toHubSpot(ctx, o, m.props, fields, cur)
	if err != nil {
		return nil, err
	}
	if len(props) > 0 {
		if err := h.call(ctx, hc, http.MethodPatch, "/crm/v3/objects/"+hsObjectName[o]+"/"+url.PathEscape(id), nil,
			map[string]any{"properties": props}, nil); err != nil {
			return nil, err
		}
	}
	d, err := h.Get(ctx, o, id)
	if err != nil {
		return nil, err
	}
	return d.Record, nil
}

// Create adds a record in HubSpot (deals land in the first stage of the
// first pipeline unless pipeline/stage say otherwise).
func (h *hubspot) Create(ctx context.Context, o objectType, fields map[string]json.RawMessage) (xrecordish, error) {
	hc, err := h.writer(ctx)
	if err != nil {
		return nil, err
	}
	m, err := h.mapping(ctx, o)
	if err != nil {
		return nil, err
	}
	var pipelineID, stageID string
	if o == objDeal {
		if raw, ok := fields["pipeline"]; ok {
			_ = json.Unmarshal(raw, &pipelineID)
			delete(fields, "pipeline")
		}
		if raw, ok := fields["stage"]; ok {
			_ = json.Unmarshal(raw, &stageID)
			delete(fields, "stage")
		}
		pl, err := h.pipeline(ctx, pipelineID)
		if err != nil {
			return nil, err
		}
		if stageID == "" && len(pl.Stages) > 0 {
			stageID = pl.Stages[0].ID
		}
		if _, ok := pl.stage(stageID); !ok {
			return nil, fieldValueError("Stage", "That stage isn't in “"+pl.Name+"”.")
		}
		pipelineID = pl.ID
	}
	editable := make([]Property, 0, len(m.props))
	for _, p := range m.props {
		if p.Key == "first_name" || p.Key == "last_name" || p.Key == "name" || p.Key == "emails" {
			p.Editable = true
		}
		editable = append(editable, p)
	}
	props, err := h.toHubSpot(ctx, o, editable, fields, m.currency)
	if err != nil {
		return nil, err
	}
	switch o {
	case objCompany, objDeal:
		if strings.TrimSpace(props[hsPropOf(o, "name")]) == "" {
			return nil, fieldValueError("Name", "Give it a name.")
		}
	case objContact:
		if props["email"] == "" && props["firstname"] == "" && props["lastname"] == "" {
			return nil, fieldValueError("Name", "Give the contact a name or an email.")
		}
	}
	if o == objDeal {
		props["pipeline"], props["dealstage"] = pipelineID, stageID
	}
	if _, ok := props["hubspot_owner_id"]; !ok {
		if me := h.me(ctx); me != "" {
			props["hubspot_owner_id"] = me
		}
	}
	var obj hsObject
	if err := h.call(ctx, hc, http.MethodPost, "/crm/v3/objects/"+hsObjectName[o], nil, map[string]any{"properties": props}, &obj); err != nil {
		return nil, err
	}
	d, err := h.Get(ctx, o, obj.ID)
	if err != nil {
		return nil, err
	}
	return d.Record, nil
}

// MoveStage moves a deal to a stage of its pipeline.
func (h *hubspot) MoveStage(ctx context.Context, id, stageID string) (xrecordish, error) {
	hc, err := h.writer(ctx)
	if err != nil {
		return nil, err
	}
	obj, err := h.readOne(ctx, objDeal, id)
	if err != nil {
		return nil, err
	}
	pl, err := h.pipeline(ctx, obj.p("pipeline"))
	if err != nil {
		return nil, err
	}
	if _, ok := pl.stage(stageID); !ok {
		return nil, fieldValueError("Stage", "That stage isn't in “"+pl.Name+"”.")
	}
	if err := h.call(ctx, hc, http.MethodPatch, "/crm/v3/objects/deals/"+url.PathEscape(id), nil,
		map[string]any{"properties": map[string]string{"dealstage": stageID}}, nil); err != nil {
		return nil, err
	}
	d, err := h.Get(ctx, objDeal, id)
	if err != nil {
		return nil, err
	}
	return d.Record, nil
}

// LogActivity creates a note, call or task in HubSpot, linked to the record.
func (h *hubspot) LogActivity(ctx context.Context, o objectType, id string, req xActivityReq) (*XActivity, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	hc, err := h.writer(ctx)
	if err != nil {
		return nil, err
	}
	kind := req.Type + "s"
	now := time.Now().UTC()
	ms := func(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }
	body := func() string {
		if req.Body == nil {
			return ""
		}
		return strings.TrimSpace(*req.Body)
	}
	props := map[string]string{}
	owner := h.me(ctx)
	switch req.Type {
	case "note":
		props["hs_note_body"], props["hs_timestamp"] = body(), ms(now)
	case "call":
		at := now
		if req.OccurredAt != nil {
			at = req.OccurredAt.UTC()
		}
		props["hs_call_body"], props["hs_timestamp"], props["hs_call_status"] = body(), ms(at), "COMPLETED"
		if req.Subject != nil {
			props["hs_call_title"] = *req.Subject
		}
		if req.DurationMinutes != nil {
			props["hs_call_duration"] = strconv.Itoa(*req.DurationMinutes * 60000)
		}
		if req.Direction != nil {
			props["hs_call_direction"] = strings.ToUpper(*req.Direction)
		}
		if req.Outcome != nil {
			props["hs_call_disposition"] = hsDisposition(*req.Outcome)
		}
	case "task":
		due := now.Add(24 * time.Hour)
		if req.DueAt != nil {
			due = req.DueAt.UTC()
		}
		props["hs_task_subject"], props["hs_task_body"], props["hs_timestamp"], props["hs_task_status"] = strings.TrimSpace(*req.Subject), body(), ms(due), "NOT_STARTED"
		if req.Priority != nil && *req.Priority != "none" {
			props["hs_task_priority"] = strings.ToUpper(*req.Priority)
		}
		if req.ReminderAt != nil {
			props["hs_task_reminders"] = ms(req.ReminderAt.UTC())
		}
		if req.AssigneeID != nil && *req.AssigneeID != "" {
			owners, err := h.ownerMap(ctx)
			if err != nil {
				return nil, err
			}
			if _, ok := owners[*req.AssigneeID]; !ok {
				return nil, fieldValueError("Assignee", "That person isn't a HubSpot owner in this account.")
			}
			owner = *req.AssigneeID
		}
	}
	if owner != "" {
		props["hubspot_owner_id"] = owner
	}
	assoc := []map[string]any{{"to": map[string]string{"id": id},
		"types": []map[string]any{{"associationCategory": "HUBSPOT_DEFINED", "associationTypeId": hsEngagementAssoc[kind][o]}}}}
	var obj hsObject
	if err := h.call(ctx, hc, http.MethodPost, "/crm/v3/objects/"+kind, nil, map[string]any{"properties": props, "associations": assoc}, &obj); err != nil {
		return nil, err
	}
	if obj.Properties == nil {
		obj.Properties = map[string]*string{}
	}
	for k, v := range props {
		if obj.Properties[k] == nil {
			obj.Properties[k] = &v
		}
	}
	owners, _ := h.ownerMap(ctx)
	name := ""
	if d, err := h.readOne(ctx, o, id); err == nil {
		switch o {
		case objCompany:
			name = d.p("name")
		case objContact:
			name = contactName(d.p("firstname"), d.p("lastname"), nonEmpty(d.p("email")))
		default:
			name = d.p("dealname")
		}
	}
	return h.toActivity(kind, obj, owners, []XRecordRef{{Type: o, ID: xref(h.conn.ID, id), Name: name}}), nil
}

// readTitle is a record's display name (previews).
func (h *hubspot) readTitle(ctx context.Context, o objectType, id string) (string, error) {
	d, err := h.readOne(ctx, o, id)
	if err != nil {
		return "", err
	}
	switch o {
	case objCompany:
		return d.p("name"), nil
	case objContact:
		return contactName(d.p("firstname"), d.p("lastname"), nonEmpty(d.p("email"))), nil
	}
	return d.p("dealname"), nil
}
