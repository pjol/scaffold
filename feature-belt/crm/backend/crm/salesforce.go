package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// Salesforce adapter: REST API on the connection's own instance
// (Endpoints "instance", or "api"): SOQL queries paged with nextRecordsUrl,
// sObject describe for fields, PATCH/POST for writes, OpportunityStage for
// the pipeline, Task/Event/Note for the timeline, User for owners. The
// org's sharing model, profile and field-level security decide what each
// person's queries return, aggregates included.

const (
	sfAPIVersion = "v62.0"
	sfBatchSize  = 200
	sfPipelineID = "opportunity"
)

var sfIDRe = regexp.MustCompile(`^[a-zA-Z0-9]{15}([a-zA-Z0-9]{3})?$`)
var sfFieldRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)?$`)

type salesforce struct {
	*xconn
	base string // instance URL, e.g. https://acme.my.salesforce.com
}

func newSalesforce(x *xconn) (Source, error) {
	base := x.endpoint("instance", x.endpoint("api", ""))
	if base == "" {
		return nil, httpx.NewError(http.StatusConflict, app.ConnectionNeedsAuth, "This Salesforce connection has no instance address. Reconnect it in Settings → Connections.")
	}
	return &salesforce{xconn: x, base: base}, nil
}

var sfObject = map[objectType]string{objCompany: "Account", objContact: "Contact", objDeal: "Opportunity"}

var sfBuiltin = map[objectType][]hsField{
	objCompany: {{"name", "Name"}, {"domain", "Website"}, {"industry", "Industry"}, {"employees", "NumberOfEmployees"}, {"phone", "Phone"},
		{"city", "BillingCity"}, {"country", "BillingCountry"}, {"description", "Description"}, {"owner", "OwnerId"},
		{"created_at", "CreatedDate"}, {"updated_at", "LastModifiedDate"}, {"last_activity_at", "LastActivityDate"}},
	objContact: {{"name", "Name"}, {"first_name", "FirstName"}, {"last_name", "LastName"}, {"emails", "Email"}, {"phones", "Phone"},
		{"job_title", "Title"}, {"city", "MailingCity"}, {"country", "MailingCountry"}, {"owner", "OwnerId"}, {"company", "AccountId"},
		{"created_at", "CreatedDate"}, {"updated_at", "LastModifiedDate"}, {"last_activity_at", "LastActivityDate"}},
	objDeal: {{"name", "Name"}, {"amount", "Amount"}, {"currency", "CurrencyIsoCode"}, {"stage", "StageName"}, {"close_date", "CloseDate"},
		{"owner", "OwnerId"}, {"company", "AccountId"}, {"probability", "Probability"}, {"stage_entered_at", "LastStageChangeDate"},
		{"created_at", "CreatedDate"}, {"updated_at", "LastModifiedDate"}, {"last_activity_at", "LastActivityDate"}},
}

var sfExtraRead = map[objectType][]string{
	objContact: {"MobilePhone", "Account.Name"},
	objDeal:    {"Account.Name", "IsClosed", "IsWon"},
}

// Fields agents never change: who owns a record.
var sfAccessFields = map[string]bool{"OwnerId": true}

func sfFieldOf(o objectType, key string) string {
	for _, f := range sfBuiltin[o] {
		if f.key == key {
			return f.prop
		}
	}
	return ""
}

// soqlString quotes a SOQL string literal.
func soqlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\n", `\n`, "\r", `\r`, "\t", `\t`, `"`, `\"`)
	return "'" + r.Replace(s) + "'"
}

// soqlLike quotes a LIKE pattern matching s anywhere.
func soqlLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, `%`, `\%`, `_`, `\_`)
	return "'%" + r.Replace(s) + "%'"
}

func sfParse(_ int, body []byte) (string, map[string]string) {
	var list []struct {
		Message   string   `json:"message"`
		ErrorCode string   `json:"errorCode"`
		Fields    []string `json:"fields"`
	}
	if json.Unmarshal(body, &list) != nil {
		return "", nil
	}
	msgs := []string{}
	fields := map[string]string{}
	for _, e := range list {
		msgs = append(msgs, e.Message)
		for _, f := range e.Fields {
			fields[f] = e.Message
		}
	}
	return strings.Join(msgs, " "), fields
}

func sfLimited(status int, body []byte) bool {
	return status == http.StatusForbidden && strings.Contains(string(body), "REQUEST_LIMIT_EXCEEDED")
}

func (sf *salesforce) call(ctx context.Context, hc *http.Client, method, path string, query url.Values, body, out any) error {
	if hc == nil {
		hc = sf.read
	}
	h := http.Header{}
	if method == http.MethodGet && strings.Contains(path, "/query") {
		h.Set("Sforce-Query-Options", "batchSize="+strconv.Itoa(sfBatchSize))
	}
	return xreq{hc: hc, label: "Salesforce", method: method, url: sf.base + path, query: query, body: body, header: h,
		parse: sfParse, limited: sfLimited}.do(ctx, out)
}

func (sf *salesforce) data(p string) string { return "/services/data/" + sfAPIVersion + p }

// sfResult is a SOQL answer: up to sfBatchSize records and where the rest are.
type sfResult struct {
	TotalSize      int              `json:"totalSize"`
	Done           bool             `json:"done"`
	NextRecordsURL string           `json:"nextRecordsUrl"`
	Records        []map[string]any `json:"records"`
}

func (sf *salesforce) query(ctx context.Context, soql string) (*sfResult, error) {
	var res sfResult
	err := sf.call(ctx, nil, http.MethodGet, sf.data("/query"), url.Values{"q": {soql}}, nil, &res)
	return &res, err
}

// queryAt fetches a batch by path: a nextRecordsUrl, or the query itself.
func (sf *salesforce) queryAt(ctx context.Context, path string) (*sfResult, error) {
	if !strings.HasPrefix(path, "/services/data/") {
		return nil, httpx.BadRequest("That page cursor isn't valid.")
	}
	var res sfResult
	err := sf.call(ctx, nil, http.MethodGet, path, nil, nil, &res)
	return &res, err
}

// queryAll follows nextRecordsUrl up to max records.
func (sf *salesforce) queryAll(ctx context.Context, soql string, max int) ([]map[string]any, int, error) {
	res, err := sf.query(ctx, soql)
	if err != nil {
		return nil, 0, err
	}
	out := res.Records
	total := res.TotalSize
	for !res.Done && res.NextRecordsURL != "" && len(out) < max {
		if res, err = sf.queryAt(ctx, res.NextRecordsURL); err != nil {
			return nil, 0, err
		}
		out = append(out, res.Records...)
	}
	if len(out) > max {
		out = out[:max]
	}
	return out, total, nil
}

func rs(rec map[string]any, field string) string {
	if a, b, ok := strings.Cut(field, "."); ok {
		if sub, ok := rec[a].(map[string]any); ok {
			return rs(sub, b)
		}
		return ""
	}
	switch v := rec[field].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func rf(rec map[string]any, field string) (float64, bool) {
	switch v := rec[field].(type) {
	case float64:
		return v, true
	case string:
		return parseFloat(v)
	}
	return 0, false
}

func rb(rec map[string]any, field string) bool { v, _ := rec[field].(bool); return v }

func (sf *salesforce) RecordURL(o objectType, id string) string {
	return sf.base + "/lightning/r/" + sfObject[o] + "/" + url.PathEscape(id) + "/view"
}

func (sf *salesforce) recordRef(o objectType, id string) *app.SourceRef {
	return withURL(sf.ref, sf.RecordURL(o, id))
}

// ---- schema ---------------------------------------------------------------------------

type sfField struct {
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Type           string   `json:"type"`
	Updateable     bool     `json:"updateable"`
	Createable     bool     `json:"createable"`
	Custom         bool     `json:"custom"`
	Filterable     bool     `json:"filterable"`
	Sortable       bool     `json:"sortable"`
	Calculated     bool     `json:"calculated"`
	ReferenceTo    []string `json:"referenceTo"`
	PicklistValues []struct {
		Value  string `json:"value"`
		Label  string `json:"label"`
		Active bool   `json:"active"`
	} `json:"picklistValues"`
}

func (sf *salesforce) describe(ctx context.Context, o objectType) (map[string]sfField, []sfField, error) {
	list, err := cached(sf.xconn, "sf.describe."+string(o), func() ([]sfField, error) {
		var res struct {
			Fields []sfField `json:"fields"`
		}
		err := sf.call(ctx, nil, http.MethodGet, sf.data("/sobjects/"+sfObject[o]+"/describe"), nil, nil, &res)
		return res.Fields, err
	})
	if err != nil {
		return nil, nil, err
	}
	m := make(map[string]sfField, len(list))
	for _, f := range list {
		m[f.Name] = f
	}
	return m, list, nil
}

func sfType(f sfField) string {
	switch f.Type {
	case "textarea":
		return "long_text"
	case "picklist":
		return "select"
	case "multipicklist":
		return "multi_select"
	case "boolean":
		return "checkbox"
	case "currency":
		return "currency"
	case "double", "int", "percent", "long":
		return "number"
	case "date":
		return "date"
	case "datetime":
		return "datetime"
	case "phone":
		return "phone"
	case "email":
		return "email"
	case "url":
		return "url"
	case "reference":
		if slices.Contains(f.ReferenceTo, "User") {
			return "user"
		}
	}
	return "text"
}

// Properties are the CRM's built-in fields this object has in Salesforce
// (field-level security hides what the person can't read), then its custom
// fields, typed for the CRM's editors.
func (sf *salesforce) Properties(ctx context.Context, o objectType) ([]Property, error) {
	fields, list, err := sf.describe(ctx, o)
	if err != nil {
		return nil, err
	}
	out := []Property{}
	for _, bp := range builtinProps(o) {
		name := sfFieldOf(o, bp.Key)
		f, ok := fields[name]
		if name == "" || !ok {
			if o == objDeal && bp.Key == "weighted_amount" {
				bp.Editable, bp.Filterable, bp.Sortable, bp.Importable = false, false, false, false
				out = append(out, bp)
			}
			continue
		}
		bp.Importable = false
		bp.Editable = f.Updateable && !f.Calculated
		bp.Filterable = bp.Filterable && f.Filterable
		bp.Sortable = bp.Sortable && f.Sortable
		switch bp.Key {
		case "company", "currency", "probability", "stage_entered_at":
			bp.Editable = false
		case "name":
			if o == objContact {
				bp.Editable = false
			}
		}
		out = append(out, bp)
	}
	for _, f := range list {
		if !f.Custom {
			continue
		}
		p := Property{Key: f.Name, Label: f.Label, Type: sfType(f), Options: []Option{}, Editable: f.Updateable && !f.Calculated,
			Filterable: f.Filterable, Sortable: f.Sortable}
		for _, v := range f.PicklistValues {
			if v.Active {
				p.Options = append(p.Options, Option{Value: v.Value, Label: v.Label})
			}
		}
		out = append(out, p)
	}
	for i := range out {
		out[i].Position = i
	}
	return out, nil
}

// readFields is the SELECT list for o: built-ins the describe has, related
// names, and custom fields.
func (sf *salesforce) readFields(ctx context.Context, o objectType) ([]string, error) {
	fields, list, err := sf.describe(ctx, o)
	if err != nil {
		return nil, err
	}
	set := []string{"Id"}
	for _, f := range sfBuiltin[o] {
		if _, ok := fields[f.prop]; ok && !slices.Contains(set, f.prop) {
			set = append(set, f.prop)
		}
	}
	if _, ok := fields["OwnerId"]; ok {
		set = append(set, "Owner.Name")
	}
	for _, f := range sfExtraRead[o] {
		base, _, _ := strings.Cut(f, ".")
		if _, ok := fields[base]; ok || strings.Contains(f, ".") {
			if strings.Contains(f, ".") {
				if _, ok := fields[base+"Id"]; !ok {
					continue
				}
			}
			set = append(set, f)
		}
	}
	if o == objDeal {
		if _, ok := fields["ExpectedRevenue"]; ok {
			set = append(set, "ExpectedRevenue")
		}
	}
	for _, f := range list {
		if f.Custom && sfFieldRe.MatchString(f.Name) {
			set = append(set, f.Name)
		}
	}
	return set, nil
}

// Pipelines is the org's one opportunity pipeline: its active stages.
func (sf *salesforce) Pipelines(ctx context.Context) ([]XPipeline, error) {
	return cached(sf.xconn, "sf.pipelines", func() ([]XPipeline, error) {
		res, err := sf.query(ctx, "SELECT ApiName, MasterLabel, DefaultProbability, IsClosed, IsWon, SortOrder FROM OpportunityStage WHERE IsActive = true ORDER BY SortOrder")
		if err != nil {
			return nil, err
		}
		pl := XPipeline{ID: sfPipelineID, OrgID: sf.orgID, Name: "Opportunities", Stages: []XStage{}}
		for i, r := range res.Records {
			id := rs(r, "ApiName")
			if id == "" {
				id = rs(r, "MasterLabel")
			}
			prob, _ := rf(r, "DefaultProbability")
			outcome := "open"
			if rb(r, "IsClosed") {
				outcome = "lost"
				if rb(r, "IsWon") {
					outcome = "won"
				}
			}
			pl.Stages = append(pl.Stages, XStage{ID: id, Name: rs(r, "MasterLabel"), Position: i, Probability: int(prob + 0.5), Outcome: outcome})
		}
		return []XPipeline{pl}, nil
	})
}

func (sf *salesforce) pipeline(ctx context.Context, id string) (*XPipeline, error) {
	list, err := sf.Pipelines(ctx)
	if err != nil {
		return nil, err
	}
	if id != "" && id != sfPipelineID {
		return nil, httpx.NotFound("Salesforce has one opportunity pipeline here.")
	}
	return &list[0], nil
}

func (sf *salesforce) currency(ctx context.Context) string {
	c, _ := cached(sf.xconn, "sf.currency", func() (string, error) {
		res, err := sf.query(ctx, "SELECT DefaultCurrencyIsoCode FROM Organization LIMIT 1")
		if err != nil || len(res.Records) == 0 {
			return "USD", nil
		}
		if c := rs(res.Records[0], "DefaultCurrencyIsoCode"); c != "" {
			return c, nil
		}
		return "USD", nil
	})
	return c
}

func (sf *salesforce) ownerMap(ctx context.Context) (map[string]XUserRef, error) {
	return cached(sf.xconn, "sf.owners", func() (map[string]XUserRef, error) {
		recs, _, err := sf.queryAll(ctx, "SELECT Id, Name, Email FROM User WHERE IsActive = true ORDER BY Name", 2000)
		if err != nil {
			return nil, err
		}
		out := map[string]XUserRef{}
		for _, r := range recs {
			out[rs(r, "Id")] = XUserRef{ID: rs(r, "Id"), Name: rs(r, "Name"), Email: rs(r, "Email")}
		}
		return out, nil
	})
}

// Owners lists the org's active users.
func (sf *salesforce) Owners(ctx context.Context) ([]XUserRef, error) {
	m, err := sf.ownerMap(ctx)
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

type sfUserInfo struct {
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`
}

func (sf *salesforce) userInfo(ctx context.Context) sfUserInfo {
	info, _ := cached(sf.xconn, "sf.userinfo", func() (sfUserInfo, error) {
		var info sfUserInfo
		err := sf.call(ctx, nil, http.MethodGet, "/services/oauth2/userinfo", nil, nil, &info)
		return info, err
	})
	return info
}

// me is the connected Salesforce user's id.
func (sf *salesforce) me(ctx context.Context) string { return sf.userInfo(ctx).UserID }

// AccountID is the Salesforce org id of the connection.
func (sf *salesforce) AccountID(ctx context.Context) string {
	if sf.conn.AccountID != "" {
		return sf.conn.AccountID
	}
	return sf.userInfo(ctx).OrganizationID
}

// ---- mapping ---------------------------------------------------------------------------

func (sf *salesforce) toRecord(o objectType, r map[string]any, props []Property, pls []XPipeline, currency string) xrecordish {
	id := rs(r, "Id")
	base := XRecord{ID: xref(sf.conn.ID, id), OrgID: sf.orgID, Type: o, Tags: []Tag{}, Props: map[string]any{}, PropsDisplay: map[string]string{},
		MyLevel: sf.level(), Source: sf.recordRef(o, id), platformID: id}
	if owner := rs(r, "OwnerId"); owner != "" {
		base.Owner = &XUserRef{ID: owner, Name: rs(r, "Owner.Name")}
		if base.Owner.Name == "" {
			base.Owner.Name = "Salesforce user"
		}
	}
	if t, ok := parseTime(rs(r, "CreatedDate")); ok {
		base.CreatedAt = t
	}
	base.UpdatedAt = base.CreatedAt
	if t, ok := parseTime(rs(r, "LastModifiedDate")); ok {
		base.UpdatedAt = t
	}
	base.LastActivityAt = timePtr(rs(r, "LastActivityDate"))
	for _, p := range props {
		if p.Builtin {
			continue
		}
		v, ok := r[p.Key]
		if !ok || v == nil {
			continue
		}
		switch p.Type {
		case "multi_select":
			base.Props[p.Key] = strings.Split(rs(r, p.Key), ";")
		case "currency":
			if f, ok := rf(r, p.Key); ok {
				base.Props[p.Key] = moneyMinor(f, currency)
			}
		case "date":
			if d := dateOnly(rs(r, p.Key)); d != nil {
				base.Props[p.Key] = *d
			}
		default:
			base.Props[p.Key] = v
		}
	}
	switch o {
	case objCompany:
		c := &XCompany{XRecord: base, Name: rs(r, "Name"), Domain: domainOf(rs(r, "Website")), Industry: rs(r, "Industry"), Phone: rs(r, "Phone"),
			City: rs(r, "BillingCity"), Country: rs(r, "BillingCountry"), Description: rs(r, "Description")}
		if n, ok := rf(r, "NumberOfEmployees"); ok {
			e := int(n)
			c.Employees = &e
		}
		c.Title = c.Name
		return c
	case objContact:
		c := &XContact{XRecord: base, FirstName: rs(r, "FirstName"), LastName: rs(r, "LastName"), Emails: nonEmpty(rs(r, "Email")),
			Phones: nonEmpty(rs(r, "Phone"), rs(r, "MobilePhone")), JobTitle: rs(r, "Title"), LifecycleStage: "other",
			City: rs(r, "MailingCity"), Country: rs(r, "MailingCountry")}
		c.Name = rs(r, "Name")
		if c.Name == "" {
			c.Name = contactName(c.FirstName, c.LastName, c.Emails)
		}
		c.Title = c.Name
		if acc := rs(r, "AccountId"); acc != "" {
			c.Company = &XRecordRef{Type: objCompany, ID: xref(sf.conn.ID, acc), Name: rs(r, "Account.Name")}
		}
		return c
	default:
		d := &XDeal{XRecord: base, Name: rs(r, "Name"), Currency: rs(r, "CurrencyIsoCode"), PipelineID: sfPipelineID, CloseDate: dateOnly(rs(r, "CloseDate"))}
		if d.Currency == "" {
			d.Currency = currency
		}
		d.Title = d.Name
		stage := rs(r, "StageName")
		if len(pls) > 0 {
			d.PipelineName = pls[0].Name
			if st, ok := pls[0].stage(stage); ok {
				d.Stage = st.ref()
			}
		}
		if d.Stage.ID == "" {
			d.Stage = XStageRef{ID: stage, Name: stage, Outcome: "open"}
			if rb(r, "IsClosed") {
				d.Stage.Outcome = "lost"
				if rb(r, "IsWon") {
					d.Stage.Outcome = "won"
				}
			}
		}
		if p, ok := rf(r, "Probability"); ok {
			d.Stage.Probability = int(p + 0.5)
		}
		if f, ok := rf(r, "Amount"); ok {
			n := moneyMinor(f, d.Currency)
			d.Amount = &n
			w := n * int64(d.Stage.Probability) / 100
			d.Weighted = &w
		}
		d.StageEnteredAt = d.UpdatedAt
		if t, ok := parseTime(rs(r, "LastStageChangeDate")); ok {
			d.StageEnteredAt = t
		}
		if d.Stage.Outcome != "open" && d.CloseDate != nil {
			d.ClosedAt = timePtr(*d.CloseDate)
		}
		if acc := rs(r, "AccountId"); acc != "" {
			d.Company = &XRecordRef{Type: objCompany, ID: xref(sf.conn.ID, acc), Name: rs(r, "Account.Name")}
		}
		return d
	}
}

// ---- lists ---------------------------------------------------------------------------------

type sfCursor struct {
	U string `json:"u"` // batch path: the query, or a nextRecordsUrl
	O int    `json:"o"` // records of that batch already shown
}

// List runs a SOQL query with the list's filters, search text and sort,
// paged by nextRecordsUrl; totalSize is what the person's sharing lets them see.
func (sf *salesforce) List(ctx context.Context, o objectType, spec listSpec) (*xPage, error) {
	props, err := sf.Properties(ctx, o)
	if err != nil {
		return nil, err
	}
	var pls []XPipeline
	if o == objDeal {
		if pls, err = sf.Pipelines(ctx); err != nil {
			return nil, err
		}
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, sfBatchSize)
	path, off := "", 0
	if spec.Cursor != "" {
		var c sfCursor
		if err := httpx.DecodeCursor(spec.Cursor, &c); err != nil || c.U == "" {
			return nil, httpx.BadRequest("That page cursor isn't valid.")
		}
		path, off = c.U, c.O
	} else {
		soql, err := sf.listSOQL(ctx, o, props, spec)
		if err != nil {
			return nil, err
		}
		path = sf.data("/query") + "?q=" + url.QueryEscape(soql)
	}
	res, err := sf.queryAt(ctx, path)
	if err != nil {
		return nil, err
	}
	cur := sf.currency(ctx)
	page := &xPage{Items: []xrecordish{}}
	end := min(off+limit, len(res.Records))
	for _, r := range res.Records[min(off, len(res.Records)):end] {
		page.Items = append(page.Items, sf.toRecord(o, r, props, pls, cur))
	}
	switch {
	case end < len(res.Records):
		page.NextCursor = httpx.EncodeCursor(sfCursor{U: path, O: end})
	case !res.Done && res.NextRecordsURL != "":
		page.NextCursor = httpx.EncodeCursor(sfCursor{U: res.NextRecordsURL})
	}
	if spec.Cursor == "" {
		n := res.TotalSize
		page.Total = &n
	}
	return page, nil
}

func (sf *salesforce) listSOQL(ctx context.Context, o objectType, props []Property, spec listSpec) (string, error) {
	fields, err := sf.readFields(ctx, o)
	if err != nil {
		return "", err
	}
	where, err := sf.where(ctx, o, props, spec)
	if err != nil {
		return "", err
	}
	q := "SELECT " + strings.Join(fields, ", ") + " FROM " + sfObject[o]
	if where != "" {
		q += " WHERE " + where
	}
	return q + " " + sf.orderBy(o, props, spec), nil
}

func (sf *salesforce) orderBy(o objectType, props []Property, spec listSpec) string {
	dir := "ASC"
	if spec.SortDir == "desc" {
		dir = "DESC"
	}
	field := ""
	if spec.SortKey != "" {
		if p, ok := findProp(props, spec.SortKey); ok && p.Sortable {
			field = sfFieldOf(o, p.Key)
			if !p.Builtin {
				field = p.Key
			}
		}
	}
	if field == "" || !sfFieldRe.MatchString(field) {
		return "ORDER BY CreatedDate DESC, Id DESC"
	}
	return "ORDER BY " + field + " " + dir + " NULLS LAST, Id " + dir
}

// where builds the SOQL condition for a list: every filter must hold.
func (sf *salesforce) where(ctx context.Context, o objectType, props []Property, spec listSpec) (string, error) {
	conds := []string{}
	loc, _ := time.LoadLocation(spec.TZ)
	if loc == nil {
		loc = time.UTC
	}
	if q := strings.TrimSpace(spec.Q); q != "" {
		switch o {
		case objCompany:
			conds = append(conds, "(Name LIKE "+soqlLike(q)+" OR Website LIKE "+soqlLike(q)+")")
		case objContact:
			conds = append(conds, "(Name LIKE "+soqlLike(q)+" OR Email LIKE "+soqlLike(q)+")")
		default:
			conds = append(conds, "Name LIKE "+soqlLike(q))
		}
	}
	if len(spec.xStages) > 0 {
		vals := make([]string, len(spec.xStages))
		for i, v := range spec.xStages {
			vals[i] = soqlString(v)
		}
		conds = append(conds, "StageName IN ("+strings.Join(vals, ", ")+")")
	}
	switch spec.xOutcome {
	case "open":
		conds = append(conds, "IsClosed = false")
	case "won":
		conds = append(conds, "IsWon = true")
	case "lost":
		conds = append(conds, "IsClosed = true AND IsWon = false")
	}
	for _, f := range spec.Filters {
		p, ok := findProp(props, f.Key)
		if !ok || !p.Filterable {
			return "", errFilterUnsupported("Salesforce", f.Key)
		}
		field := sfFieldOf(o, p.Key)
		if !p.Builtin {
			field = p.Key
		}
		if field == "" || !sfFieldRe.MatchString(field) {
			return "", errFilterUnsupported("Salesforce", p.Label)
		}
		v, _ := jsonValue(f.Value)
		lit := func(x any) string {
			s := asText(x)
			switch p.Type {
			case "currency":
				if n, err := strconv.ParseInt(s, 10, 64); err == nil {
					return moneyDecimal(n, "USD")
				}
				return "0"
			case "number":
				if _, ok := parseFloat(s); ok {
					return s
				}
				return "0"
			case "user":
				if s == "me" {
					s = sf.me(ctx)
				}
			case "date":
				if validDate(s) {
					return s
				}
			case "datetime":
				if t, ok := parseTime(s); ok {
					return t.UTC().Format("2006-01-02T15:04:05Z")
				}
			}
			return soqlString(s)
		}
		list := func() string {
			items := asStrings(v)
			out := make([]string, 0, len(items))
			for _, x := range items {
				out = append(out, lit(x))
			}
			if len(out) == 0 {
				out = append(out, "''")
			}
			return strings.Join(out, ", ")
		}
		dayBounds := func(s string) (string, string, bool) {
			t, err := time.ParseInLocation("2006-01-02", s, loc)
			if err != nil {
				return "", "", false
			}
			if p.Type == "date" {
				return t.Format("2006-01-02"), t.AddDate(0, 0, 1).Format("2006-01-02"), true
			}
			return t.UTC().Format("2006-01-02T15:04:05Z"), t.AddDate(0, 0, 1).UTC().Format("2006-01-02T15:04:05Z"), true
		}
		switch f.Op {
		case "eq":
			conds = append(conds, field+" = "+lit(v))
		case "neq":
			conds = append(conds, field+" != "+lit(v))
		case "contains":
			conds = append(conds, field+" LIKE "+soqlLike(asText(v)))
		case "not_contains":
			conds = append(conds, "(NOT "+field+" LIKE "+soqlLike(asText(v))+")")
		case "empty":
			conds = append(conds, field+" = null")
		case "not_empty":
			conds = append(conds, field+" != null")
		case "gt":
			conds = append(conds, field+" > "+lit(v))
		case "gte":
			conds = append(conds, field+" >= "+lit(v))
		case "lt":
			conds = append(conds, field+" < "+lit(v))
		case "lte":
			conds = append(conds, field+" <= "+lit(v))
		case "is_any":
			conds = append(conds, field+" IN ("+list()+")")
		case "is_none":
			conds = append(conds, field+" NOT IN ("+list()+")")
		case "has_any":
			conds = append(conds, field+" INCLUDES ("+list()+")")
		case "has_none":
			conds = append(conds, field+" EXCLUDES ("+list()+")")
		case "is_true":
			conds = append(conds, field+" = true")
		case "is_false":
			conds = append(conds, field+" = false")
		case "on", "before", "after":
			from, to, ok := dayBounds(asText(v))
			if !ok {
				return "", httpx.BadRequest(p.Label + " needs a date like 2026-11-30.")
			}
			switch f.Op {
			case "on":
				conds = append(conds, "("+field+" >= "+from+" AND "+field+" < "+to+")")
			case "before":
				conds = append(conds, field+" < "+from)
			default:
				conds = append(conds, field+" >= "+to)
			}
		case "last_days", "next_days":
			n, _ := strconv.Atoi(asText(v))
			if n <= 0 {
				return "", httpx.BadRequest(p.Label + " needs a number of days.")
			}
			now := time.Now().In(loc)
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			fmtT := func(t time.Time) string {
				if p.Type == "date" {
					return t.Format("2006-01-02")
				}
				return t.UTC().Format("2006-01-02T15:04:05Z")
			}
			if f.Op == "last_days" {
				conds = append(conds, "("+field+" >= "+fmtT(start.AddDate(0, 0, -n))+" AND "+field+" <= "+fmtT(now)+")")
			} else {
				conds = append(conds, "("+field+" >= "+fmtT(start)+" AND "+field+" < "+fmtT(start.AddDate(0, 0, n+1))+")")
			}
		default:
			return "", errFilterUnsupported("Salesforce", p.Label+" ("+f.Op+")")
		}
	}
	return strings.Join(conds, " AND "), nil
}

// ---- one record ------------------------------------------------------------------------------

func (sf *salesforce) readOne(ctx context.Context, o objectType, id string) (map[string]any, error) {
	if !sfIDRe.MatchString(id) {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	fields, err := sf.readFields(ctx, o)
	if err != nil {
		return nil, err
	}
	res, err := sf.query(ctx, "SELECT "+strings.Join(fields, ", ")+" FROM "+sfObject[o]+" WHERE Id = "+soqlString(id)+" LIMIT 1")
	if err != nil {
		return nil, err
	}
	if len(res.Records) == 0 {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	return res.Records[0], nil
}

// Get reads a record and what's linked to it (accounts' contacts and
// opportunities; contact roles between contacts and opportunities).
func (sf *salesforce) Get(ctx context.Context, o objectType, id string) (*XRecordDetail, error) {
	props, err := sf.Properties(ctx, o)
	if err != nil {
		return nil, err
	}
	r, err := sf.readOne(ctx, o, id)
	if err != nil {
		return nil, err
	}
	var pls []XPipeline
	if pls, err = sf.Pipelines(ctx); err != nil {
		return nil, err
	}
	cur := sf.currency(ctx)
	rec := sf.toRecord(o, r, props, pls, cur)
	a := XAssociations{Companies: []XAssocCompany{}, Contacts: []XAssocContact{}, Deals: []XAssocDeal{}}
	qid := soqlString(id)
	deal := func(r map[string]any, prefix string) XAssocDeal {
		d := XAssocDeal{ID: xref(sf.conn.ID, rs(r, prefix+"Id")), Name: rs(r, prefix+"Name"), Currency: cur, CloseDate: dateOnly(rs(r, prefix+"CloseDate")),
			StageName: rs(r, prefix+"StageName"), StageOutcome: "open"}
		if st, ok := pls[0].stage(d.StageName); ok {
			d.StageName, d.StageOutcome = st.Name, st.Outcome
		}
		sub := r
		if prefix != "" {
			sub, _ = r[strings.TrimSuffix(prefix, ".")].(map[string]any)
		}
		if f, ok := rf(sub, "Amount"); ok {
			n := moneyMinor(f, cur)
			d.Amount = &n
		}
		return d
	}
	switch o {
	case objCompany:
		cs, err := sf.query(ctx, "SELECT Id, Name, Email, Phone, Title FROM Contact WHERE AccountId = "+qid+" ORDER BY Name LIMIT 100")
		if err != nil {
			return nil, err
		}
		for _, c := range cs.Records {
			a.Contacts = append(a.Contacts, XAssocContact{ID: xref(sf.conn.ID, rs(c, "Id")), Name: rs(c, "Name"), Email: strPtr(rs(c, "Email")), Phone: strPtr(rs(c, "Phone")), JobTitle: rs(c, "Title")})
		}
		ds, err := sf.query(ctx, "SELECT Id, Name, Amount, StageName, CloseDate FROM Opportunity WHERE AccountId = "+qid+" ORDER BY CloseDate DESC LIMIT 100")
		if err != nil {
			return nil, err
		}
		for _, d := range ds.Records {
			a.Deals = append(a.Deals, deal(d, ""))
		}
	case objContact, objDeal:
		if acc := rs(r, "AccountId"); acc != "" {
			ac, err := sf.query(ctx, "SELECT Id, Name, Website FROM Account WHERE Id = "+soqlString(acc)+" LIMIT 1")
			if err != nil {
				return nil, err
			}
			for _, c := range ac.Records {
				a.Companies = append(a.Companies, XAssocCompany{ID: xref(sf.conn.ID, rs(c, "Id")), Name: rs(c, "Name"), Domain: domainOf(rs(c, "Website")), IsPrimary: true})
			}
		}
		if o == objContact {
			roles, err := sf.query(ctx, "SELECT OpportunityId, Opportunity.Id, Opportunity.Name, Opportunity.Amount, Opportunity.StageName, Opportunity.CloseDate FROM OpportunityContactRole WHERE ContactId = "+qid+" LIMIT 100")
			if err != nil {
				return nil, err
			}
			for _, rr := range roles.Records {
				if rs(rr, "Opportunity.Id") != "" {
					a.Deals = append(a.Deals, deal(rr, "Opportunity."))
				}
			}
		} else {
			roles, err := sf.query(ctx, "SELECT ContactId, Contact.Id, Contact.Name, Contact.Email, Contact.Phone, Contact.Title FROM OpportunityContactRole WHERE OpportunityId = "+qid+" LIMIT 100")
			if err != nil {
				return nil, err
			}
			for _, rr := range roles.Records {
				if rs(rr, "Contact.Id") != "" {
					a.Contacts = append(a.Contacts, XAssocContact{ID: xref(sf.conn.ID, rs(rr, "Contact.Id")), Name: rs(rr, "Contact.Name"),
						Email: strPtr(rs(rr, "Contact.Email")), Phone: strPtr(rs(rr, "Contact.Phone")), JobTitle: rs(rr, "Contact.Title")})
				}
			}
		}
	}
	return &XRecordDetail{Record: rec, Associations: a}, nil
}

// ---- timeline ------------------------------------------------------------------------------

const sfTaskFields = "Id, Subject, Description, ActivityDate, Status, Priority, TaskSubtype, CallDurationInSeconds, CallDisposition, CallType, IsClosed, " +
	"OwnerId, Owner.Name, CreatedDate, LastModifiedDate, CompletedDateTime, ReminderDateTime, WhoId, WhatId, Who.Name, Who.Type, What.Name, What.Type"

func (sf *salesforce) taskActivity(r map[string]any, records []XRecordRef) *XActivity {
	a := &XActivity{OrgID: sf.orgID, Records: records, Mentions: []Mention{}, Priority: "none", Source: sf.ref, Subject: rs(r, "Subject"), Body: rs(r, "Description")}
	if a.Records == nil {
		a.Records = sf.taskRecords(r)
	}
	a.ID = xref(sf.conn.ID, "Task."+rs(r, "Id"))
	if t, ok := parseTime(rs(r, "CreatedDate")); ok {
		a.CreatedAt = t
	}
	a.UpdatedAt = a.CreatedAt
	if t, ok := parseTime(rs(r, "LastModifiedDate")); ok {
		a.UpdatedAt = t
	}
	a.OccurredAt = a.CreatedAt
	if owner := rs(r, "OwnerId"); owner != "" {
		a.CreatedBy = &XUserRef{ID: owner, Name: rs(r, "Owner.Name")}
	}
	switch rs(r, "TaskSubtype") {
	case "Call":
		a.Type = "call"
		if t := timePtr(rs(r, "CompletedDateTime")); t != nil {
			a.OccurredAt = *t
		}
		if s, ok := rf(r, "CallDurationInSeconds"); ok {
			n := int(s) / 60
			a.DurationMinutes = &n
		}
		switch rs(r, "CallType") {
		case "Inbound":
			a.Direction = strPtr("inbound")
		case "Outbound":
			a.Direction = strPtr("outbound")
		}
		if d := strings.ToLower(strings.ReplaceAll(rs(r, "CallDisposition"), " ", "_")); slices.Contains(outcomes["call"], d) {
			a.Outcome = &d
		}
	case "Email":
		a.Type = "email"
	default:
		a.Type = "task"
		a.DueAt = timePtr(rs(r, "ActivityDate"))
		if a.DueAt != nil {
			a.OccurredAt = *a.DueAt
		}
		a.Assignee = a.CreatedBy
		if rb(r, "IsClosed") {
			done := timePtr(rs(r, "CompletedDateTime"))
			if done == nil {
				done = &a.UpdatedAt
			}
			a.DoneAt = done
		}
		switch rs(r, "Priority") {
		case "High":
			a.Priority = "high"
		case "Normal":
			a.Priority = "medium"
		case "Low":
			a.Priority = "low"
		}
		a.ReminderAt = timePtr(rs(r, "ReminderDateTime"))
		a.CanEdit = sf.write
	}
	return a
}

func (sf *salesforce) taskRecords(r map[string]any) []XRecordRef {
	out := []XRecordRef{}
	if id := rs(r, "WhoId"); id != "" && rs(r, "Who.Type") == "Contact" {
		out = append(out, XRecordRef{Type: objContact, ID: xref(sf.conn.ID, id), Name: rs(r, "Who.Name")})
	}
	if id := rs(r, "WhatId"); id != "" {
		switch rs(r, "What.Type") {
		case "Account":
			out = append(out, XRecordRef{Type: objCompany, ID: xref(sf.conn.ID, id), Name: rs(r, "What.Name")})
		case "Opportunity":
			out = append(out, XRecordRef{Type: objDeal, ID: xref(sf.conn.ID, id), Name: rs(r, "What.Name")})
		}
	}
	return out
}

// Timeline merges the record's Tasks (calls, emails, tasks), Events
// (meetings) and Notes, newest first.
func (sf *salesforce) Timeline(ctx context.Context, o objectType, id, filter, cursor string, limit int) (*XTimelinePage, error) {
	if !sfIDRe.MatchString(id) {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	want := map[string]bool{}
	switch filter {
	case "", "all", "activities":
		want = map[string]bool{"note": true, "call": true, "email": true, "meeting": true, "task": true}
	case "changes":
		return &XTimelinePage{Items: []XTimelineItem{}, Upcoming: []*XActivity{}}, nil
	case "notes", "calls", "emails", "meetings", "tasks":
		want[strings.TrimSuffix(filter, "s")] = true
	default:
		return nil, httpx.BadRequest("filter is all, activities, notes, calls, emails, meetings, tasks or changes.")
	}
	qid := soqlString(id)
	link := "WhoId = " + qid + " OR WhatId = " + qid
	self := []XRecordRef{{Type: o, ID: xref(sf.conn.ID, id)}}
	all := []*XActivity{}
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	run := func(f func() ([]*XActivity, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			list, err := f()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			all = append(all, list...)
		}()
	}
	if want["call"] || want["email"] || want["task"] {
		run(func() ([]*XActivity, error) {
			res, err := sf.query(ctx, "SELECT "+sfTaskFields+" FROM Task WHERE "+link+" ORDER BY CreatedDate DESC LIMIT 200")
			if err != nil {
				return nil, err
			}
			out := []*XActivity{}
			for _, r := range res.Records {
				a := sf.taskActivity(r, self)
				if want[a.Type] {
					out = append(out, a)
				}
			}
			return out, nil
		})
	}
	if want["meeting"] {
		run(func() ([]*XActivity, error) {
			res, err := sf.query(ctx, "SELECT Id, Subject, Description, StartDateTime, EndDateTime, DurationInMinutes, OwnerId, Owner.Name, CreatedDate, LastModifiedDate FROM Event WHERE "+
				link+" ORDER BY StartDateTime DESC LIMIT 200")
			if err != nil {
				return nil, err
			}
			out := []*XActivity{}
			for _, r := range res.Records {
				a := &XActivity{ID: xref(sf.conn.ID, "Event."+rs(r, "Id")), OrgID: sf.orgID, Type: "meeting", Records: self, Mentions: []Mention{}, Priority: "none",
					Subject: rs(r, "Subject"), Body: rs(r, "Description"), Source: sf.ref}
				if t, ok := parseTime(rs(r, "CreatedDate")); ok {
					a.CreatedAt, a.UpdatedAt, a.OccurredAt = t, t, t
				}
				if t, ok := parseTime(rs(r, "StartDateTime")); ok {
					a.OccurredAt = t
				}
				if n, ok := rf(r, "DurationInMinutes"); ok {
					m := int(n)
					a.DurationMinutes = &m
				}
				if owner := rs(r, "OwnerId"); owner != "" {
					a.CreatedBy = &XUserRef{ID: owner, Name: rs(r, "Owner.Name")}
				}
				out = append(out, a)
			}
			return out, nil
		})
	}
	if want["note"] {
		run(func() ([]*XActivity, error) {
			res, err := sf.query(ctx, "SELECT Id, Title, Body, OwnerId, Owner.Name, CreatedDate, LastModifiedDate FROM Note WHERE ParentId = "+qid+" ORDER BY CreatedDate DESC LIMIT 200")
			if err != nil {
				return nil, err
			}
			out := []*XActivity{}
			for _, r := range res.Records {
				a := &XActivity{ID: xref(sf.conn.ID, "Note."+rs(r, "Id")), OrgID: sf.orgID, Type: "note", Records: self, Mentions: []Mention{}, Priority: "none",
					Subject: rs(r, "Title"), Body: rs(r, "Body"), Source: sf.ref}
				if a.Body == "" {
					a.Body, a.Subject = a.Subject, ""
				}
				if t, ok := parseTime(rs(r, "CreatedDate")); ok {
					a.CreatedAt, a.UpdatedAt, a.OccurredAt = t, t, t
				}
				if owner := rs(r, "OwnerId"); owner != "" {
					a.CreatedBy = &XUserRef{ID: owner, Name: rs(r, "Owner.Name")}
				}
				out = append(out, a)
			}
			return out, nil
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return pageTimeline(all, cursor, limit), nil
}

// ---- board and dashboard -------------------------------------------------------------------------

type sfAgg struct {
	count            int
	amount, weighted int64
}

// stageAggregates sums opportunities per stage with one aggregate query.
func (sf *salesforce) stageAggregates(ctx context.Context, where, cur string) (map[string]sfAgg, error) {
	q := "SELECT StageName, COUNT(Id) cnt, SUM(Amount) amt, SUM(ExpectedRevenue) wt FROM Opportunity"
	if where != "" {
		q += " WHERE " + where
	}
	res, err := sf.query(ctx, q+" GROUP BY StageName")
	if err != nil {
		return nil, err
	}
	out := map[string]sfAgg{}
	for _, r := range res.Records {
		n, _ := rf(r, "cnt")
		amt, _ := rf(r, "amt")
		wt, _ := rf(r, "wt")
		out[rs(r, "StageName")] = sfAgg{count: int(n), amount: moneyMinor(amt, cur), weighted: moneyMinor(wt, cur)}
	}
	return out, nil
}

// Board shows the opportunity stages with exact counts and sums from an
// aggregate query and the first cards of each stage.
func (sf *salesforce) Board(ctx context.Context, pipelineID string, spec listSpec) (*XBoard, error) {
	pl, err := sf.pipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	props, err := sf.Properties(ctx, objDeal)
	if err != nil {
		return nil, err
	}
	where, err := sf.where(ctx, objDeal, props, spec)
	if err != nil {
		return nil, err
	}
	cur := sf.currency(ctx)
	agg, err := sf.stageAggregates(ctx, where, cur)
	if err != nil {
		return nil, err
	}
	fields, err := sf.readFields(ctx, objDeal)
	if err != nil {
		return nil, err
	}
	board := &XBoard{Pipeline: *pl, Columns: make([]XBoardColumn, len(pl.Stages)), Open: []MoneyTotal{}, Source: sf.ref}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	errs := make([]error, len(pl.Stages))
	for i, st := range pl.Stages {
		a := agg[st.ID]
		col := XBoardColumn{Stage: st, Count: a.count, Totals: []MoneyTotal{}, Deals: []*XDeal{}}
		if a.count > 0 && (a.amount != 0 || a.weighted != 0) {
			col.Totals = addTotal(col.Totals, cur, a.amount, a.weighted)
		}
		if st.Outcome == "open" && (a.amount != 0 || a.weighted != 0) {
			board.Open = addTotal(board.Open, cur, a.amount, a.weighted)
		}
		board.Columns[i] = col
		if a.count == 0 {
			continue
		}
		wg.Add(1)
		go func(i int, st XStage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cond := "StageName = " + soqlString(st.ID)
			if where != "" {
				cond = where + " AND " + cond
			}
			res, err := sf.query(ctx, "SELECT "+strings.Join(fields, ", ")+" FROM Opportunity WHERE "+cond+
				" ORDER BY CloseDate ASC NULLS LAST, Id ASC LIMIT "+strconv.Itoa(boardColumnSize))
			if err != nil {
				errs[i] = err
				return
			}
			for _, r := range res.Records {
				board.Columns[i].Deals = append(board.Columns[i].Deals, sf.toRecord(objDeal, r, props, []XPipeline{*pl}, cur).(*XDeal))
			}
		}(i, st)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return board, nil
}

// Dashboard computes the CRM dashboard with aggregate queries the person's
// sharing applies to.
func (sf *salesforce) Dashboard(ctx context.Context, pipelineID string, loc *time.Location) (*XDashboard, error) {
	pl, err := sf.pipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	cur := sf.currency(ctx)
	agg, err := sf.stageAggregates(ctx, "", cur)
	if err != nil {
		return nil, err
	}
	out := &XDashboard{Pipeline: *pl, Currency: cur, Open: []XStageSummary{}, OpenTotals: []MoneyTotal{}, Won: ClosedSummary{Totals: []MoneyTotal{}},
		Lost: ClosedSummary{Totals: []MoneyTotal{}}, Activity: []XOwnerActivity{}, Source: sf.ref}
	for _, st := range pl.Stages {
		if st.Outcome != "open" {
			continue
		}
		a := agg[st.ID]
		s := XStageSummary{StageID: st.ID, Name: st.Name, Count: a.count, Totals: []MoneyTotal{}}
		if a.count > 0 {
			s.Totals = addTotal(s.Totals, cur, a.amount, a.weighted)
			out.OpenTotals = addTotal(out.OpenTotals, cur, a.amount, a.weighted)
		}
		out.Open = append(out.Open, s)
	}
	_, _, weekStart, monthStart, monthEnd := timeBounds(loc)
	month := "CloseDate >= " + monthStart.Format("2006-01-02") + " AND CloseDate < " + monthEnd.Format("2006-01-02")
	closed := func(cond string) (ClosedSummary, error) {
		res, err := sf.query(ctx, "SELECT COUNT(Id) cnt, SUM(Amount) amt FROM Opportunity WHERE "+cond+" AND "+month)
		sum := ClosedSummary{Totals: []MoneyTotal{}}
		if err != nil || len(res.Records) == 0 {
			return sum, err
		}
		n, _ := rf(res.Records[0], "cnt")
		amt, _ := rf(res.Records[0], "amt")
		sum.Count = int(n)
		if n > 0 {
			sum.Totals = addTotal(sum.Totals, cur, moneyMinor(amt, cur), moneyMinor(amt, cur))
		}
		return sum, nil
	}
	if out.Won, err = closed("IsWon = true"); err != nil {
		return nil, err
	}
	if out.Lost, err = closed("IsClosed = true AND IsWon = false"); err != nil {
		return nil, err
	}
	since := weekStart.UTC().Format("2006-01-02T15:04:05Z")
	owners, err := sf.ownerMap(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]*XOwnerActivity{}
	add := func(owner string, n int, f func(a *XOwnerActivity, n int)) {
		if owner == "" || n == 0 {
			return
		}
		a, ok := counts[owner]
		if !ok {
			u, ok := owners[owner]
			if !ok {
				u = XUserRef{ID: owner, Name: "Salesforce user"}
			}
			a = &XOwnerActivity{User: u}
			counts[owner] = a
		}
		f(a, n)
		a.Total += n
	}
	tasks, err := sf.query(ctx, "SELECT OwnerId, TaskSubtype, COUNT(Id) cnt FROM Task WHERE CreatedDate >= "+since+" AND TaskSubtype IN ('Call', 'Email') GROUP BY OwnerId, TaskSubtype")
	if err != nil {
		return nil, err
	}
	for _, r := range tasks.Records {
		n, _ := rf(r, "cnt")
		sub := rs(r, "TaskSubtype")
		add(rs(r, "OwnerId"), int(n), func(a *XOwnerActivity, n int) {
			if sub == "Call" {
				a.Calls += n
			} else {
				a.Emails += n
			}
		})
	}
	done, err := sf.query(ctx, "SELECT OwnerId, COUNT(Id) cnt FROM Task WHERE IsClosed = true AND TaskSubtype = 'Task' AND LastModifiedDate >= "+since+" GROUP BY OwnerId")
	if err != nil {
		return nil, err
	}
	for _, r := range done.Records {
		n, _ := rf(r, "cnt")
		add(rs(r, "OwnerId"), int(n), func(a *XOwnerActivity, n int) { a.Tasks += n })
	}
	for _, spec := range []struct {
		obj  string
		bump func(a *XOwnerActivity, n int)
	}{
		{"Event", func(a *XOwnerActivity, n int) { a.Meetings += n }},
		{"Note", func(a *XOwnerActivity, n int) { a.Notes += n }},
	} {
		res, err := sf.query(ctx, "SELECT OwnerId, COUNT(Id) cnt FROM "+spec.obj+" WHERE CreatedDate >= "+since+" GROUP BY OwnerId")
		if err != nil {
			return nil, err
		}
		for _, r := range res.Records {
			n, _ := rf(r, "cnt")
			add(rs(r, "OwnerId"), int(n), spec.bump)
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
	if out.Tasks, err = sf.taskSummary(ctx, loc); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- tasks -------------------------------------------------------------------------------------

func (sf *salesforce) taskWhere(ctx context.Context, bucket string, loc *time.Location) (string, string, error) {
	dayStart, _, _, _, _ := timeBounds(loc)
	today := dayStart.Format("2006-01-02")
	conds := []string{"(TaskSubtype = 'Task' OR TaskSubtype = null)"}
	if me := sf.me(ctx); me != "" {
		conds = append(conds, "OwnerId = "+soqlString(me))
	}
	order := "ORDER BY ActivityDate ASC NULLS LAST, Id ASC"
	switch bucket {
	case "done":
		conds = append(conds, "IsClosed = true")
		order = "ORDER BY LastModifiedDate DESC, Id DESC"
	case "overdue":
		conds = append(conds, "IsClosed = false", "ActivityDate < "+today)
	case "today":
		conds = append(conds, "IsClosed = false", "ActivityDate = "+today)
	case "upcoming":
		conds = append(conds, "IsClosed = false", "(ActivityDate > "+today+" OR ActivityDate = null)")
	case "", "open":
		conds = append(conds, "IsClosed = false")
	default:
		return "", "", httpx.BadRequest("bucket is open, overdue, today, upcoming or done.")
	}
	return strings.Join(conds, " AND "), order, nil
}

// Tasks lists the person's Salesforce tasks in a bucket.
func (sf *salesforce) Tasks(ctx context.Context, bucket string, loc *time.Location, cursor string, limit int) (*xActivityPage, error) {
	if limit <= 0 || limit > sfBatchSize {
		limit = 50
	}
	path, off := "", 0
	if cursor != "" {
		var c sfCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil || c.U == "" {
			return nil, httpx.BadRequest("That page cursor isn't valid.")
		}
		path, off = c.U, c.O
	} else {
		where, order, err := sf.taskWhere(ctx, bucket, loc)
		if err != nil {
			return nil, err
		}
		path = sf.data("/query") + "?q=" + url.QueryEscape("SELECT "+sfTaskFields+" FROM Task WHERE "+where+" "+order)
	}
	res, err := sf.queryAt(ctx, path)
	if err != nil {
		return nil, err
	}
	page := &xActivityPage{Items: []*XActivity{}}
	end := min(off+limit, len(res.Records))
	for _, r := range res.Records[min(off, len(res.Records)):end] {
		page.Items = append(page.Items, sf.taskActivity(r, nil))
	}
	switch {
	case end < len(res.Records):
		page.NextCursor = httpx.EncodeCursor(sfCursor{U: path, O: end})
	case !res.Done && res.NextRecordsURL != "":
		page.NextCursor = httpx.EncodeCursor(sfCursor{U: res.NextRecordsURL})
	}
	return page, nil
}

// TaskSummary counts the person's open Salesforce tasks by when they're due.
func (sf *salesforce) TaskSummary(ctx context.Context, loc *time.Location) (TaskSummary, error) {
	return sf.taskSummary(ctx, loc)
}

func (sf *salesforce) taskSummary(ctx context.Context, loc *time.Location) (TaskSummary, error) {
	var out TaskSummary
	for _, b := range []string{"overdue", "today", "upcoming"} {
		where, _, err := sf.taskWhere(ctx, b, loc)
		if err != nil {
			return out, err
		}
		res, err := sf.query(ctx, "SELECT COUNT() FROM Task WHERE "+where)
		if err != nil {
			return out, err
		}
		switch b {
		case "overdue":
			out.Overdue = res.TotalSize
		case "today":
			out.Today = res.TotalSize
		default:
			out.Upcoming = res.TotalSize
		}
	}
	return out, nil
}

func (sf *salesforce) readTask(ctx context.Context, id string) (*XActivity, error) {
	res, err := sf.query(ctx, "SELECT "+sfTaskFields+" FROM Task WHERE Id = "+soqlString(id)+" LIMIT 1")
	if err != nil {
		return nil, err
	}
	if len(res.Records) == 0 {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	return sf.taskActivity(res.Records[0], nil), nil
}

// SetTaskDone completes or reopens a Salesforce task.
func (sf *salesforce) SetTaskDone(ctx context.Context, activityID string, done bool) (*XActivity, error) {
	kind, id, ok := strings.Cut(activityID, ".")
	if !ok || kind != "Task" || !sfIDRe.MatchString(id) {
		return nil, httpx.BadRequest("Only tasks can be marked done.")
	}
	hc, err := sf.writer(ctx)
	if err != nil {
		return nil, err
	}
	status := "Not Started"
	if done {
		status = "Completed"
	}
	if err := sf.call(ctx, hc, http.MethodPatch, sf.data("/sobjects/Task/"+id), nil, map[string]string{"Status": status}, nil); err != nil {
		return nil, err
	}
	return sf.readTask(ctx, id)
}

// ---- search ----------------------------------------------------------------------------------------

// Search uses Salesforce's parameterized search (SOSL), or the most
// recently changed records when there's no text.
func (sf *salesforce) Search(ctx context.Context, q string, types []objectType, limit int) ([]XSearchHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if len(types) == 0 {
		types = objectTypes
	}
	fieldsOf := map[objectType]string{objCompany: "Id,Name,Website,LastModifiedDate", objContact: "Id,Name,Email,LastModifiedDate", objDeal: "Id,Name,StageName,LastModifiedDate"}
	typeOf := map[string]objectType{"Account": objCompany, "Contact": objContact, "Opportunity": objDeal}
	var recs []map[string]any
	q = strings.TrimSpace(q)
	if len([]rune(q)) >= 2 {
		params := url.Values{"q": {q}, "overallLimit": {strconv.Itoa(limit * len(types))}}
		for _, o := range types {
			params.Add("sobject", sfObject[o])
			params.Set(sfObject[o]+".fields", fieldsOf[o])
			params.Set(sfObject[o]+".limit", strconv.Itoa(limit))
		}
		var res struct {
			SearchRecords []map[string]any `json:"searchRecords"`
		}
		if err := sf.call(ctx, nil, http.MethodGet, sf.data("/parameterizedSearch/"), params, nil, &res); err != nil {
			return nil, err
		}
		recs = res.SearchRecords
	} else {
		for _, o := range types {
			res, err := sf.query(ctx, "SELECT "+fieldsOf[o]+" FROM "+sfObject[o]+" ORDER BY LastModifiedDate DESC LIMIT "+strconv.Itoa(limit))
			if err != nil {
				return nil, err
			}
			for _, r := range res.Records {
				r["attributes"] = map[string]any{"type": sfObject[o]}
				recs = append(recs, r)
			}
		}
	}
	var pls []XPipeline
	out := []XSearchHit{}
	for i, r := range recs {
		attrs, _ := r["attributes"].(map[string]any)
		t, _ := attrs["type"].(string)
		o, ok := typeOf[t]
		if !ok || !slices.Contains(types, o) {
			continue
		}
		id := rs(r, "Id")
		hit := XSearchHit{Type: o, ID: xref(sf.conn.ID, id), OrgID: sf.orgID, Title: rs(r, "Name"), Score: 0.6 - float64(i)*0.01, Source: sf.recordRef(o, id)}
		if at, ok := parseTime(rs(r, "LastModifiedDate")); ok {
			hit.At = at
		}
		switch o {
		case objCompany:
			if d := domainOf(rs(r, "Website")); d != nil {
				hit.Detail = *d
			}
		case objContact:
			hit.Detail = rs(r, "Email")
		default:
			hit.Detail = rs(r, "StageName")
			if pls == nil {
				pls, _ = sf.Pipelines(ctx)
			}
			if len(pls) > 0 {
				if st, ok := pls[0].stage(hit.Detail); ok {
					hit.Detail = st.Name
				}
			}
		}
		out = append(out, hit)
	}
	return out, nil
}

// ---- writes ---------------------------------------------------------------------------------------

// toSalesforce converts field values (CRM property keys) into sObject
// field values, refusing fields this account can't change.
func (sf *salesforce) toSalesforce(ctx context.Context, o objectType, props []Property, fields map[string]json.RawMessage, currency string, creating bool) (map[string]any, error) {
	out := map[string]any{}
	describe, _, err := sf.describe(ctx, o)
	if err != nil {
		return nil, err
	}
	for key, raw := range fields {
		p, ok := findProp(props, key)
		if !ok {
			return nil, fieldValueError(key, "There's no field “"+key+"” on Salesforce "+o.labelPlural()+".")
		}
		name := sfFieldOf(o, key)
		if !p.Builtin {
			name = p.Key
		}
		f, known := describe[name]
		if !known || !sfFieldRe.MatchString(name) {
			return nil, fieldValueError(p.Label, p.Label+" can't be changed from here.")
		}
		if creating && !f.Createable || !creating && !p.Editable {
			return nil, fieldValueError(p.Label, p.Label+" can't be changed from here.")
		}
		v, err := jsonValue(raw)
		if err != nil {
			return nil, fieldValueError(p.Label, p.Label+" isn't a valid value.")
		}
		if v == nil {
			out[name] = nil
			continue
		}
		switch p.Type {
		case "currency":
			n, err := strconv.ParseInt(asText(v), 10, 64)
			if err != nil {
				return nil, fieldValueError(p.Label, p.Label+" is an amount in minor units (cents).")
			}
			x, _ := strconv.ParseFloat(moneyDecimal(n, currency), 64)
			out[name] = x
		case "number":
			x, ok := parseFloat(asText(v))
			if !ok {
				return nil, fieldValueError(p.Label, p.Label+" is a number.")
			}
			out[name] = x
		case "checkbox":
			b, ok := v.(bool)
			if !ok {
				return nil, fieldValueError(p.Label, p.Label+" is true or false.")
			}
			out[name] = b
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
				out[name] = nil
			} else {
				out[name] = list[0]
			}
		case "user":
			id := asText(v)
			owners, err := sf.ownerMap(ctx)
			if err != nil {
				return nil, err
			}
			if _, ok := owners[id]; !ok {
				return nil, fieldValueError(p.Label, "That person isn't an active Salesforce user.")
			}
			out[name] = id
		case "stage":
			pl, err := sf.pipeline(ctx, "")
			if err != nil {
				return nil, err
			}
			s := asText(v)
			if _, ok := pl.stage(s); !ok {
				return nil, fieldValueError(p.Label, "“"+s+"” isn't an opportunity stage.")
			}
			out[name] = s
		default:
			out[name] = asText(v)
		}
	}
	return out, nil
}

// Update PATCHes the record; Salesforce's validation rules and field
// security answer with a message naming the field.
func (sf *salesforce) Update(ctx context.Context, o objectType, id string, fields map[string]json.RawMessage) (xrecordish, error) {
	if !sfIDRe.MatchString(id) {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	if len(fields) > 0 {
		hc, err := sf.writer(ctx)
		if err != nil {
			return nil, err
		}
		props, err := sf.Properties(ctx, o)
		if err != nil {
			return nil, err
		}
		body, err := sf.toSalesforce(ctx, o, props, fields, sf.currency(ctx), false)
		if err != nil {
			return nil, err
		}
		if err := sf.call(ctx, hc, http.MethodPatch, sf.data("/sobjects/"+sfObject[o]+"/"+id), nil, body, nil); err != nil {
			return nil, err
		}
	}
	d, err := sf.Get(ctx, o, id)
	if err != nil {
		return nil, err
	}
	return d.Record, nil
}

// Create inserts a record (opportunities default to the first stage and a
// close date 30 days out, which Salesforce requires).
func (sf *salesforce) Create(ctx context.Context, o objectType, fields map[string]json.RawMessage) (xrecordish, error) {
	hc, err := sf.writer(ctx)
	if err != nil {
		return nil, err
	}
	props, err := sf.Properties(ctx, o)
	if err != nil {
		return nil, err
	}
	delete(fields, "pipeline")
	body, err := sf.toSalesforce(ctx, o, props, fields, sf.currency(ctx), true)
	if err != nil {
		return nil, err
	}
	switch o {
	case objCompany, objDeal:
		if s, _ := body["Name"].(string); strings.TrimSpace(s) == "" {
			return nil, fieldValueError("Name", "Give it a name.")
		}
	case objContact:
		if s, _ := body["LastName"].(string); strings.TrimSpace(s) == "" {
			return nil, fieldValueError("Last name", "Salesforce needs a last name for a contact.")
		}
	}
	if o == objDeal {
		if _, ok := body["StageName"]; !ok {
			pl, err := sf.pipeline(ctx, "")
			if err != nil {
				return nil, err
			}
			if len(pl.Stages) == 0 {
				return nil, fieldValueError("Stage", "This Salesforce org has no active opportunity stages.")
			}
			body["StageName"] = pl.Stages[0].ID
		}
		if _, ok := body["CloseDate"]; !ok {
			body["CloseDate"] = time.Now().AddDate(0, 0, 30).Format("2006-01-02")
		}
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := sf.call(ctx, hc, http.MethodPost, sf.data("/sobjects/"+sfObject[o]), nil, body, &res); err != nil {
		return nil, err
	}
	d, err := sf.Get(ctx, o, res.ID)
	if err != nil {
		return nil, err
	}
	return d.Record, nil
}

// MoveStage sets an opportunity's stage.
func (sf *salesforce) MoveStage(ctx context.Context, id, stageID string) (xrecordish, error) {
	raw, _ := json.Marshal(stageID)
	return sf.Update(ctx, objDeal, id, map[string]json.RawMessage{"stage": raw})
}

// LogActivity creates a Note, or a Task for a call (TaskSubtype Call) or a
// to-do, on the record.
func (sf *salesforce) LogActivity(ctx context.Context, o objectType, id string, req xActivityReq) (*XActivity, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	if !sfIDRe.MatchString(id) {
		return nil, httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	hc, err := sf.writer(ctx)
	if err != nil {
		return nil, err
	}
	body := ""
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
	}
	link := map[string]any{}
	if o == objContact {
		link["WhoId"] = id
	} else {
		link["WhatId"] = id
	}
	self := []XRecordRef{{Type: o, ID: xref(sf.conn.ID, id)}}
	if title, err := sf.readTitle(ctx, o, id); err == nil {
		self[0].Name = title
	}
	var res struct {
		ID string `json:"id"`
	}
	switch req.Type {
	case "note":
		title := strings.SplitN(body, "\n", 2)[0]
		title = clip(title, 80)
		if err := sf.call(ctx, hc, http.MethodPost, sf.data("/sobjects/Note"), nil, map[string]any{"ParentId": id, "Title": title, "Body": body}, &res); err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		a := &XActivity{ID: xref(sf.conn.ID, "Note."+res.ID), OrgID: sf.orgID, Type: "note", Records: self, Mentions: []Mention{}, Priority: "none",
			Body: body, OccurredAt: now, CreatedAt: now, UpdatedAt: now, Source: sf.ref}
		if me := sf.me(ctx); me != "" {
			if owners, err := sf.ownerMap(ctx); err == nil {
				if u, ok := owners[me]; ok {
					a.CreatedBy = &u
				}
			}
		}
		return a, nil
	case "call":
		at := time.Now().UTC()
		if req.OccurredAt != nil {
			at = req.OccurredAt.UTC()
		}
		t := map[string]any{"Subject": "Call", "Description": body, "TaskSubtype": "Call", "Status": "Completed", "ActivityDate": at.Format("2006-01-02")}
		if req.Subject != nil && strings.TrimSpace(*req.Subject) != "" {
			t["Subject"] = strings.TrimSpace(*req.Subject)
		}
		if req.DurationMinutes != nil {
			t["CallDurationInSeconds"] = *req.DurationMinutes * 60
		}
		if req.Direction != nil {
			t["CallType"] = map[string]string{"inbound": "Inbound", "outbound": "Outbound"}[*req.Direction]
		}
		if req.Outcome != nil {
			t["CallDisposition"] = strings.ReplaceAll(*req.Outcome, "_", " ")
		}
		maps(t, link)
		if err := sf.call(ctx, hc, http.MethodPost, sf.data("/sobjects/Task"), nil, t, &res); err != nil {
			return nil, err
		}
	default:
		t := map[string]any{"Subject": strings.TrimSpace(*req.Subject), "Description": body, "Status": "Not Started"}
		if req.DueAt != nil {
			t["ActivityDate"] = req.DueAt.UTC().Format("2006-01-02")
		}
		if req.Priority != nil {
			t["Priority"] = map[string]string{"none": "Normal", "low": "Low", "medium": "Normal", "high": "High"}[*req.Priority]
		}
		if req.ReminderAt != nil {
			t["IsReminderSet"], t["ReminderDateTime"] = true, req.ReminderAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		if req.AssigneeID != nil && *req.AssigneeID != "" {
			owners, err := sf.ownerMap(ctx)
			if err != nil {
				return nil, err
			}
			if _, ok := owners[*req.AssigneeID]; !ok {
				return nil, fieldValueError("Assignee", "That person isn't an active Salesforce user.")
			}
			t["OwnerId"] = *req.AssigneeID
		}
		maps(t, link)
		if err := sf.call(ctx, hc, http.MethodPost, sf.data("/sobjects/Task"), nil, t, &res); err != nil {
			return nil, err
		}
	}
	a, err := sf.readTask(ctx, res.ID)
	if err != nil {
		return nil, err
	}
	if len(a.Records) == 0 {
		a.Records = self
	}
	return a, nil
}

func maps(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}

// readTitle is a record's display name (previews, activities).
func (sf *salesforce) readTitle(ctx context.Context, o objectType, id string) (string, error) {
	if !sfIDRe.MatchString(id) {
		return "", httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	res, err := sf.query(ctx, "SELECT Name FROM "+sfObject[o]+" WHERE Id = "+soqlString(id)+" LIMIT 1")
	if err != nil {
		return "", err
	}
	if len(res.Records) == 0 {
		return "", httpx.NotFound("Not found in Salesforce, or this account can't see it.")
	}
	return rs(res.Records[0], "Name"), nil
}

var _ = fmt.Sprintf
