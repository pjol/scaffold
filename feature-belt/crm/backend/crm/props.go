package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/orgs"
)

// Option is one choice of a select property.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Property is one field of an object type: built in (columns) or custom
// (crm_properties, values in props jsonb). The client builds columns,
// filters, forms and the import mapping from this list.
type Property struct {
	ID         *uuid.UUID `json:"id"` // custom only
	Key        string     `json:"key"`
	Label      string     `json:"label"`
	Type       string     `json:"type"`
	Options    []Option   `json:"options"`
	Builtin    bool       `json:"builtin"`
	Editable   bool       `json:"editable"`
	Filterable bool       `json:"filterable"`
	Sortable   bool       `json:"sortable"`
	Required   bool       `json:"required"`
	Importable bool       `json:"importable"`
	Position   int        `json:"position"`

	col      string // built-in column written by PATCH (generic types)
	expr     string // filter expression; {K} is the custom key parameter
	sortExpr string
	sortType string // text | numeric | date | timestamptz
}

// Custom property types an org may add.
var customTypes = []string{"text", "number", "date", "select", "multi_select", "user", "url", "email", "phone", "currency", "checkbox"}

var propKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

var lifecycleOptions = []Option{
	{"subscriber", "Subscriber"}, {"lead", "Lead"}, {"mql", "Marketing qualified lead"}, {"sql", "Sales qualified lead"},
	{"opportunity", "Opportunity"}, {"customer", "Customer"}, {"evangelist", "Evangelist"}, {"other", "Other"},
}

func bp(key, label, typ string, mods ...func(*Property)) Property {
	p := Property{Key: key, Label: label, Type: typ, Builtin: true, Options: []Option{}, Filterable: true, Sortable: true}
	for _, m := range mods {
		m(&p)
	}
	return p
}

// col: a plain editable column with the same filter and sort expression.
func col(c, sortType string) func(*Property) {
	return func(p *Property) {
		p.col, p.expr, p.sortType, p.Editable, p.Importable = c, "r."+c, sortType, true, true
		p.sortExpr = p.expr
		if sortType == "text" {
			p.sortExpr = "lower(r." + c + ")"
		}
	}
}

func expr(e, sortExpr, sortType string) func(*Property) {
	return func(p *Property) { p.expr, p.sortExpr, p.sortType = e, sortExpr, sortType }
}

func editable(p *Property) { p.Editable, p.Importable = true, true }
func required(p *Property) { p.Required = true }
func noSort(p *Property)   { p.Sortable = false }
func noImport(p *Property) { p.Importable = false }
func options(o []Option) func(*Property) {
	return func(p *Property) { p.Options = o }
}

// common fields every object has, after its own.
func commonProps() []Property {
	return []Property{
		bp("owner", "Owner", "user", expr("ar.owner_id", "lower(ou.display_name)", "text"), editable),
		bp("team", "Team", "team", expr("r.team_id", "lower(t.name)", "text"), editable),
		bp("tags", "Tags", "tags", expr("r.tag_ids", "", ""), noSort, editable),
		bp("created_at", "Created", "datetime", expr("r.created_at", "r.created_at", "timestamptz")),
		bp("updated_at", "Last updated", "datetime", expr("r.updated_at", "r.updated_at", "timestamptz")),
		bp("last_activity_at", "Last activity", "datetime", expr("r.last_activity_at", "r.last_activity_at", "timestamptz")),
	}
}

func builtinProps(o objectType) []Property {
	var list []Property
	switch o {
	case objCompany:
		list = []Property{
			bp("name", "Name", "text", col("name", "text"), required),
			bp("domain", "Domain", "domain", expr("COALESCE(r.domain, '')", "COALESCE(r.domain, '')", "text"), editable),
			bp("industry", "Industry", "text", col("industry", "text")),
			bp("employees", "Employees", "number", col("employees", "numeric")),
			bp("phone", "Phone", "phone", col("phone", "text")),
			bp("city", "City", "text", col("city", "text")),
			bp("country", "Country", "text", col("country", "text")),
			bp("description", "Description", "long_text", col("description", "text"), noSort),
		}
	case objContact:
		list = []Property{
			bp("name", "Name", "text", expr("btrim(r.first_name || ' ' || r.last_name)", "lower(btrim(r.first_name || ' ' || r.last_name))", "text"), noImport),
			bp("first_name", "First name", "text", col("first_name", "text")),
			bp("last_name", "Last name", "text", col("last_name", "text")),
			bp("emails", "Email", "emails", expr("r.emails", "COALESCE(r.emails[1], '')", "text"), editable),
			bp("phones", "Phone", "phones", expr("r.phones", "COALESCE(r.phones[1], '')", "text"), editable),
			bp("job_title", "Job title", "text", col("job_title", "text")),
			bp("company", "Company", "company", expr("company", `(SELECT lower(co.name) FROM crm_contact_companies cc JOIN crm_companies co ON co.id = cc.company_id
				WHERE cc.contact_id = r.id AND cc.is_primary)`, "text"), func(p *Property) { p.Importable = true }),
			bp("lifecycle_stage", "Lifecycle stage", "select", col("lifecycle_stage", "text"), options(lifecycleOptions),
				func(p *Property) {
					p.sortExpr = "array_position(ARRAY['subscriber','lead','mql','sql','opportunity','customer','evangelist','other'], r.lifecycle_stage)"
					p.sortType = "numeric"
				}),
			bp("city", "City", "text", col("city", "text")),
			bp("country", "Country", "text", col("country", "text")),
		}
	case objDeal:
		list = []Property{
			bp("name", "Name", "text", col("name", "text"), required),
			bp("amount", "Amount", "currency", col("amount", "numeric")),
			bp("currency", "Currency", "currency_code", col("currency", "text")),
			bp("pipeline", "Pipeline", "pipeline", expr("r.pipeline_id", "lower(pl.name)", "text"), func(p *Property) { p.Importable = true }),
			bp("stage", "Stage", "stage", expr("r.stage_id", "st.position", "numeric"), editable),
			bp("close_date", "Close date", "date", col("close_date", "date")),
			bp("probability", "Probability", "number", expr("st.probability", "st.probability", "numeric")),
			bp("weighted_amount", "Weighted amount", "currency", expr("(r.amount * st.probability / 100)", "(r.amount * st.probability / 100)", "numeric")),
			bp("company", "Company", "company", expr("r.company_id", "(SELECT lower(co.name) FROM crm_companies co WHERE co.id = r.company_id)", "text"), editable),
			bp("stage_entered_at", "Entered stage", "datetime", expr("r.stage_entered_at", "r.stage_entered_at", "timestamptz")),
			bp("closed_at", "Closed", "datetime", expr("r.closed_at", "r.closed_at", "timestamptz")),
		}
	}
	list = append(list, commonProps()...)
	for i := range list {
		list[i].Position = i
	}
	return list
}

// customExpr returns the filter and sort expression of a custom property.
func customExpr(typ string) (expr, sortExpr, sortType string) {
	switch typ {
	case "number", "currency":
		return "(r.props->>{K})::numeric", "(r.props->>{K})::numeric", "numeric"
	case "date":
		return "(r.props->>{K})::date", "(r.props->>{K})::date", "date"
	case "checkbox":
		return "COALESCE((r.props->>{K})::boolean, false)", "COALESCE((r.props->>{K})::boolean, false)::int", "numeric"
	case "multi_select":
		return "(r.props->{K})", "", ""
	default:
		return "(r.props->>{K})", "lower(r.props->>{K})", "text"
	}
}

// loadProperties returns built-in then custom properties of an object type.
func (s *Service) loadProperties(ctx context.Context, q db.Querier, orgID uuid.UUID, o objectType) ([]Property, error) {
	list := builtinProps(o)
	rows, err := q.Query(ctx, `SELECT id, key, label, type, options FROM crm_properties
		WHERE org_id = $1 AND object_type = $2 ORDER BY position, lower(label), id`, orgID, string(o))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	n := len(list)
	for rows.Next() {
		var (
			id   uuid.UUID
			p    Property
			opts []byte
		)
		if err := rows.Scan(&id, &p.Key, &p.Label, &p.Type, &opts); err != nil {
			return nil, err
		}
		p.ID, p.Builtin, p.Editable, p.Filterable, p.Importable, p.Position = &id, false, true, true, true, n
		n++
		p.Options = []Option{}
		_ = json.Unmarshal(opts, &p.Options)
		p.expr, p.sortExpr, p.sortType = customExpr(p.Type)
		p.Sortable = p.sortExpr != ""
		list = append(list, p)
	}
	return list, rows.Err()
}

func findProp(list []Property, key string) (Property, bool) {
	for _, p := range list {
		if p.Key == key {
			return p, true
		}
	}
	return Property{}, false
}

// ---- values ------------------------------------------------------------------

var (
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
	domainRe   = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	phoneRe    = regexp.MustCompile(`^[0-9+()\-.\s/xXextEXT#*]{3,40}$`)
)

// valueError is a validation failure on one field, shown next to it.
type valueError struct{ msg string }

func (e valueError) Error() string { return e.msg }

func verr(format string, a ...any) error { return valueError{fmt.Sprintf(format, a...)} }

// parseCustomValue validates a custom property's JSON value and returns what
// is stored in props (nil clears it).
func parseCustomValue(p Property, raw json.RawMessage, isMember func(uuid.UUID) bool) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	switch p.Type {
	case "text", "url", "email", "phone":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, verr("%s must be text.", p.Label)
		}
		return normalizeText(p, s)
	case "number":
		var n json.Number
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&n); err != nil {
			return nil, verr("%s must be a number.", p.Label)
		}
		f, err := n.Float64()
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || math.Abs(f) > 1e15 {
			return nil, verr("%s must be a number.", p.Label)
		}
		return n, nil
	case "currency":
		var n int64
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, verr("%s must be a whole amount in minor units (cents).", p.Label)
		}
		return n, nil
	case "date":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || !validDate(s) {
			return nil, verr("%s must be a date like 2026-11-30.", p.Label)
		}
		return s, nil
	case "checkbox":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, verr("%s must be true or false.", p.Label)
		}
		return b, nil
	case "select":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || !hasOption(p.Options, s) {
			return nil, verr("Choose one of the options for %s.", p.Label)
		}
		return s, nil
	case "multi_select":
		var vals []string
		if err := json.Unmarshal(raw, &vals); err != nil {
			return nil, verr("%s must be a list of options.", p.Label)
		}
		out := []string{}
		for _, v := range vals {
			if !hasOption(p.Options, v) {
				return nil, verr("Choose options of %s from its list.", p.Label)
			}
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return out, nil
	case "user":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, verr("%s must be a person.", p.Label)
		}
		id, err := uuid.Parse(s)
		if err != nil || !isMember(id) {
			return nil, verr("Choose a member of this %s for %s.", orgs.Noun(), p.Label)
		}
		return id.String(), nil
	}
	return nil, verr("%s can't be set.", p.Label)
}

// normalizeText trims and checks text-like values; "" clears.
func normalizeText(p Property, s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	switch p.Type {
	case "url":
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > 2000 {
			return nil, verr("%s must be a web address starting with https://.", p.Label)
		}
	case "email":
		e, err := cleanEmail(s)
		if err != nil {
			return nil, verr("%s must be an email address.", p.Label)
		}
		s = e
	case "phone":
		if !phoneRe.MatchString(s) {
			return nil, verr("%s must be a phone number.", p.Label)
		}
	default:
		if len([]rune(s)) > 1000 {
			return nil, verr("%s can be at most 1,000 characters.", p.Label)
		}
	}
	return s, nil
}

func hasOption(opts []Option, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

func optionLabel(opts []Option, v string) string {
	for _, o := range opts {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

func validDate(s string) bool {
	if !dateRe.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func cleanEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@")+1:], ".") || len(s) > 254 {
		return "", fmt.Errorf("invalid email")
	}
	return s, nil
}

// cleanDomain turns "https://www.Example.com/about" into "example.com".
func cleanDomain(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Host
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:]
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "www."), ".")
	if h, _, ok := strings.Cut(s, ":"); ok {
		s = h
	}
	if !domainRe.MatchString(s) {
		return "", fmt.Errorf("invalid domain")
	}
	return s, nil
}

// freeMailDomains are never used to match a contact to a company.
var freeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "outlook.com": true, "hotmail.com": true,
	"live.com": true, "icloud.com": true, "me.com": true, "aol.com": true, "proton.me": true, "protonmail.com": true,
	"gmx.com": true, "mail.com": true, "yandex.com": true, "fastmail.com": true, "hey.com": true,
}

// ---- money -------------------------------------------------------------------

// currencyExp is the number of minor-unit digits (ISO 4217); default 2.
var currencyExp = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0, "KRW": 0, "PYG": 0, "RWF": 0,
	"UGX": 0, "VND": 0, "VUV": 0, "XAF": 0, "XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

func exponent(cur string) int {
	if e, ok := currencyExp[cur]; ok {
		return e
	}
	return 2
}

// formatMoney renders minor units as a plain decimal ("1234.50 USD").
func formatMoney(minor int64, cur string) string {
	return moneyDecimal(minor, cur) + " " + cur
}

func moneyDecimal(minor int64, cur string) string {
	e := exponent(cur)
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := strconv.FormatInt(minor, 10)
	if e > 0 {
		for len(s) <= e {
			s = "0" + s
		}
		s = s[:len(s)-e] + "." + s[len(s)-e:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// parseMoney reads "1,234.5", "$1234", "1 234,50" style amounts into minor units.
func parseMoney(s, cur string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "$€£¥₹ ")
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), cur))
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	// "1.234,56" (comma decimals) when the last separator is a comma with 1–3 digits after.
	if i := strings.LastIndex(s, ","); i >= 0 && i > strings.LastIndex(s, ".") && len(s)-i-1 <= exponent(cur) && len(s)-i-1 > 0 && len(s)-i-1 != 3 {
		s = strings.ReplaceAll(s[:i], ".", "") + "." + s[i+1:]
	}
	s = strings.ReplaceAll(s, ",", "")
	whole, frac, _ := strings.Cut(s, ".")
	e := exponent(cur)
	if len(frac) > e {
		return 0, fmt.Errorf("too many decimals")
	}
	for len(frac) < e {
		frac += "0"
	}
	if whole == "" {
		whole = "0"
	}
	if strings.HasPrefix(whole, "-") {
		return 0, fmt.Errorf("negative")
	}
	v, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("not a number")
	}
	return v, nil
}

// ---- display text --------------------------------------------------------------

// displayValue renders a custom value for events and CSV.
func displayValue(p Property, v any, people map[string]string) string {
	if v == nil {
		return ""
	}
	switch p.Type {
	case "select":
		return optionLabel(p.Options, fmt.Sprint(v))
	case "multi_select":
		var parts []string
		switch vals := v.(type) {
		case []string:
			for _, x := range vals {
				parts = append(parts, optionLabel(p.Options, x))
			}
		case []any:
			for _, x := range vals {
				parts = append(parts, optionLabel(p.Options, fmt.Sprint(x)))
			}
		}
		return strings.Join(parts, ", ")
	case "checkbox":
		if b, ok := v.(bool); ok && b {
			return "Yes"
		}
		return "No"
	case "user":
		if n, ok := people[fmt.Sprint(v)]; ok {
			return n
		}
		return "Former member"
	case "currency":
		switch n := v.(type) {
		case float64:
			return moneyDecimal(int64(n), "")
		case json.Number:
			i, _ := n.Int64()
			return moneyDecimal(i, "")
		case int64:
			return moneyDecimal(n, "")
		}
	}
	return fmt.Sprint(v)
}
