package docs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Property is one column of a database. The title property (id "title") is
// the row page's title; stored values live in the row's props keyed by
// property id. Computed types (formula, rollup, created/edited time and by,
// button) store nothing: they are evaluated for the viewer when read.
type Property struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Options     []Option `json:"options,omitempty"` // select, multi_select, status
	// Numbers: format (number, number_commas, percent or a currency code;
	// currencies are stored in integer minor units), display (number, bar,
	// ring) with the value that fills a bar or ring, and fixed decimals.
	NumberFormat  string   `json:"number_format,omitempty"`
	NumberDisplay string   `json:"number_display,omitempty"`
	NumberMax     *float64 `json:"number_max,omitempty"`
	Decimals      *int     `json:"decimals,omitempty"`
	// Dates (and created / edited times, date formulas): how they read.
	DateFormat string `json:"date_format,omitempty"` // full | short | mdy | dmy | ymd | relative
	TimeFormat string `json:"time_format,omitempty"` // 12h | 24h

	Formula  *FormulaConfig  `json:"formula,omitempty"`
	Relation *RelationConfig `json:"relation,omitempty"`
	Rollup   *RollupConfig   `json:"rollup,omitempty"`
	UniqueID *UniqueIDConfig `json:"unique_id,omitempty"`
	Button   *ButtonConfig   `json:"button,omitempty"`

	// ReadOnly: shown but not changed here (a connected database's
	// formulas, rollups, relations, people…). Never set on the app's own.
	ReadOnly bool `json:"read_only,omitempty"`
	// Source is the platform's property type for a connected database.
	Source string `json:"-"`
}

// Option is a select, multi-select or status choice. Status options belong
// to a group: todo, doing or done.
type Option struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Group string `json:"group,omitempty"`
}

// FormulaConfig: the expression as written (properties by name) and its
// result type, set by the server when the schema is saved.
type FormulaConfig struct {
	Expression string `json:"expression"`
	Type       string `json:"type,omitempty"`
	// Error is the compile error when a property it uses changed under it
	// (server-set; the formula evaluates to nothing until fixed).
	Error string `json:"error,omitempty"`
}

// RelationConfig links rows to rows of a database (possibly the same one).
// A two-way relation has a mirror property on the other database that the
// server keeps in sync; MirrorName names it when it's created.
type RelationConfig struct {
	DatabaseID     uuid.UUID `json:"database_id"`
	TwoWay         bool      `json:"two_way,omitempty"`
	MirrorProperty string    `json:"mirror_property,omitempty"` // server-set
	MirrorName     string    `json:"mirror_name,omitempty"`     // input only
	Limit          int       `json:"limit,omitempty"`           // 0: no limit, 1: one row
}

// RollupConfig aggregates a property of the rows a relation links to. Type
// is the result type (server-set); Target is the related property as the
// viewer gets it, for formatting (server-filled when read, never stored).
type RollupConfig struct {
	Relation string    `json:"relation"`
	Property string    `json:"property"`
	Function string    `json:"function"`
	Type     string    `json:"type,omitempty"`
	Target   *Property `json:"target,omitempty"`
}

// UniqueIDConfig: rows are numbered per database (race-safe counter); the
// prefix reads "TASK-12".
type UniqueIDConfig struct {
	Prefix string `json:"prefix,omitempty"`
}

// ButtonConfig: a button in every row that runs its actions as the person
// who clicks it.
type ButtonConfig struct {
	Label   string         `json:"label"`
	Actions []ButtonAction `json:"actions"`
}

// ButtonAction is one step: set_property on this row ({property, value};
// value may be "@me", "@today" or "@now"), add_row to a database
// ({database_id, values, template_id}) or open_url ({url}).
type ButtonAction struct {
	Type       string                     `json:"type"`
	Property   string                     `json:"property,omitempty"`
	Value      json.RawMessage            `json:"value,omitempty"`
	DatabaseID *uuid.UUID                 `json:"database_id,omitempty"`
	Values     map[string]json.RawMessage `json:"values,omitempty"`
	TemplateID *uuid.UUID                 `json:"template_id,omitempty"`
	URL        string                     `json:"url,omitempty"`
}

var (
	propTypes = []string{"title", "text", "number", "select", "multi_select", "status", "date", "person", "checkbox", "url", "email",
		"phone", "files", "formula", "relation", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "unique_id", "button"}
	// computedTypes store no value in props.
	computedTypes = []string{"formula", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "button"}
	// optionColors: Notion's hues (the tokens' swatch set) and docs 1.1's tones.
	optionColors = []string{"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red",
		"neutral", "primary", "accent", "success", "warning", "danger", "info"}
	statusGroups = []string{"todo", "doing", "done"}
	dateFormats  = []string{"full", "short", "mdy", "dmy", "ymd", "relative"}
	propIDRE     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,24}$`)
	emailRE      = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	phoneRE      = regexp.MustCompile(`^\+?[0-9 ().\-/]{3,30}(\s*(x|ext\.?)\s*[0-9]{1,6})?$`)
	dateValueRE  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2})?$`)
	prefixRE     = regexp.MustCompile(`^[A-Za-z0-9]{0,10}$`)
	// reminders: before a timed date, or for a date alone, at 09:00 on the
	// day ("at_time") or that long before it.
	reminderOffsets = map[string]time.Duration{"at_time": 0, "5m": 5 * time.Minute, "10m": 10 * time.Minute, "15m": 15 * time.Minute,
		"30m": 30 * time.Minute, "1h": time.Hour, "2h": 2 * time.Hour, "1d": 24 * time.Hour, "2d": 48 * time.Hour, "1w": 7 * 24 * time.Hour}
)

// currencies maps a currency format (ISO 4217, lower case) to its minor-unit
// exponent: values are stored as integers of the minor unit.
var currencies = map[string]int{
	"usd": 2, "eur": 2, "gbp": 2, "jpy": 0, "cny": 2, "inr": 2, "krw": 0, "brl": 2, "cad": 2, "aud": 2, "nzd": 2, "chf": 2,
	"sek": 2, "nok": 2, "dkk": 2, "pln": 2, "czk": 2, "huf": 2, "ron": 2, "try": 2, "ils": 2, "aed": 2, "sar": 2, "zar": 2,
	"mxn": 2, "ars": 2, "clp": 0, "cop": 2, "pen": 2, "sgd": 2, "hkd": 2, "twd": 2, "thb": 2, "idr": 2, "php": 2, "myr": 2,
	"vnd": 0, "ngn": 2, "egp": 2, "kes": 2, "uah": 2, "rub": 2,
}

// legacyCurrency names formats docs 1.1 stored.
var legacyCurrency = map[string]string{"dollar": "usd", "euro": "eur", "pound": "gbp"}

// currencyExp returns the minor-unit exponent of a currency format.
func currencyExp(format string) (int, bool) {
	e, ok := currencies[format]
	return e, ok
}

// numberScale is what a stored number is divided by to read it (100 for
// cents, 1 otherwise).
func numberScale(p Property) float64 {
	if e, ok := currencyExp(p.NumberFormat); ok {
		return math.Pow10(e)
	}
	return 1
}

const (
	maxProperties = 60
	maxOptions    = 100
	maxViews      = 30
	maxRows       = 10000
	maxTextValue  = 2000
	maxFormula    = 4000
	maxRelated    = 500 // ids in one relation value
	maxFiles      = 20
)

// shortID makes a property or option id.
func shortID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var b [8]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b[:])
}

func defaultSchema() []Property {
	return []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: shortID(), Name: "Tags", Type: "multi_select", Options: []Option{}},
	}
}

func statusProperty(name string) Property {
	return Property{ID: shortID(), Name: name, Type: "status", Options: []Option{
		{ID: shortID(), Name: "Not started", Color: "gray", Group: "todo"},
		{ID: shortID(), Name: "In progress", Color: "blue", Group: "doing"},
		{ID: shortID(), Name: "Done", Color: "green", Group: "done"},
	}}
}

func findProp(schema []Property, id string) (Property, bool) {
	for _, p := range schema {
		if p.ID == id {
			return p, true
		}
	}
	return Property{}, false
}

// findPropByName matches an id or a name (case-insensitive), for agents and
// formulas.
func findPropByName(schema []Property, name string) (Property, bool) {
	if p, ok := findProp(schema, name); ok {
		return p, true
	}
	n := strings.TrimSpace(name)
	for _, p := range schema {
		if strings.EqualFold(p.Name, n) {
			return p, true
		}
	}
	return Property{}, false
}

func hasOption(p Property, id string) bool {
	return slices.ContainsFunc(p.Options, func(o Option) bool { return o.ID == id })
}

func isComputed(p Property) bool { return slices.Contains(computedTypes, p.Type) }

// writable: a person can set the value (not computed, not the counter).
func writable(p Property) bool { return !isComputed(p) && p.Type != "unique_id" && !p.ReadOnly }

// personValues returns the people a row's person properties name.
func personValues(schema []Property, props map[string]any) []uuid.UUID {
	var out []uuid.UUID
	for _, p := range schema {
		if p.Type != "person" {
			continue
		}
		list, _ := props[p.ID].([]any)
		for _, v := range list {
			if s, ok := v.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// relationValues returns the row ids a relation value lists.
func relationValues(v any) []uuid.UUID {
	var out []uuid.UUID
	switch list := v.(type) {
	case []any:
		for _, x := range list {
			if s, ok := x.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					out = append(out, id)
				}
			}
		}
	case []string:
		for _, s := range list {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// ---- schema validation -------------------------------------------------------------

// validateSchema checks the property list's shape (each type's settings);
// relations, rollups and formulas are checked against other databases and
// each other by the caller (checkComputed), which needs the database.
func validateSchema(in []Property) ([]Property, error) {
	if len(in) == 0 || len(in) > maxProperties {
		return nil, httpx.BadRequest(fmt.Sprintf("A database has 1 to %d properties.", maxProperties))
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	titles := 0
	out := make([]Property, 0, len(in))
	for _, p := range in {
		p.Name = strings.TrimSpace(cleanText(p.Name))
		p.Description = strings.TrimSpace(cleanText(p.Description))
		p.ReadOnly, p.Source = false, ""
		if p.ID == "" {
			p.ID = shortID()
		}
		if !propIDRE.MatchString(p.ID) || seen[p.ID] {
			return nil, httpx.BadRequest("Each property needs its own id.")
		}
		seen[p.ID] = true
		if p.Name == "" || utf8.RuneCountInString(p.Name) > 100 {
			return nil, httpx.BadRequest("Give each property a name under 100 characters.")
		}
		if names[strings.ToLower(p.Name)] {
			return nil, httpx.BadRequest("Two properties are called " + strconv.Quote(p.Name) + ". Give each its own name.")
		}
		names[strings.ToLower(p.Name)] = true
		if utf8.RuneCountInString(p.Description) > 500 {
			return nil, httpx.BadRequest(p.Name + ": keep the description under 500 characters.")
		}
		if !slices.Contains(propTypes, p.Type) {
			return nil, httpx.BadRequest("Unknown property type " + strconv.Quote(p.Type) + ".")
		}
		if p.Type == "title" {
			titles++
			if p.ID != "title" {
				return nil, httpx.BadRequest("The title property's id is \"title\".")
			}
		}
		if err := cleanPropertySettings(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if titles != 1 {
		return nil, httpx.BadRequest("A database has exactly one title property.")
	}
	return out, nil
}

// cleanPropertySettings keeps only the settings a type uses and fills
// defaults.
func cleanPropertySettings(p *Property) error {
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	numeric := p.Type == "number" || p.Type == "formula" || p.Type == "rollup"
	if numeric {
		if c, ok := legacyCurrency[p.NumberFormat]; ok {
			p.NumberFormat = c
		}
		if _, cur := currencyExp(p.NumberFormat); !cur && p.NumberFormat != "number_commas" && p.NumberFormat != "percent" {
			p.NumberFormat = "number"
		}
		if p.NumberDisplay != "bar" && p.NumberDisplay != "ring" {
			p.NumberDisplay = ""
			p.NumberMax = nil
		} else if p.NumberMax == nil || *p.NumberMax <= 0 || math.IsInf(*p.NumberMax, 0) || math.IsNaN(*p.NumberMax) {
			p.NumberMax = ptr(100.0)
		}
		if p.Decimals != nil && (*p.Decimals < 0 || *p.Decimals > 8) {
			return bad("decimals go from 0 to 8.")
		}
		if p.Type != "number" && p.NumberFormat == "number" && p.NumberDisplay == "" && p.Decimals == nil {
			p.NumberFormat = ""
		}
	} else {
		p.NumberFormat, p.NumberDisplay, p.NumberMax, p.Decimals = "", "", nil, nil
	}
	dated := p.Type == "date" || p.Type == "created_time" || p.Type == "last_edited_time" || p.Type == "formula" || p.Type == "rollup"
	if dated {
		if !slices.Contains(dateFormats, p.DateFormat) {
			p.DateFormat = ""
		}
		if p.TimeFormat != "12h" && p.TimeFormat != "24h" {
			p.TimeFormat = ""
		}
	} else {
		p.DateFormat, p.TimeFormat = "", ""
	}
	switch p.Type {
	case "select", "multi_select", "status":
		if len(p.Options) > maxOptions {
			return bad(fmt.Sprintf("a property has up to %d options.", maxOptions))
		}
		ids := map[string]bool{}
		opts := make([]Option, 0, len(p.Options))
		for _, o := range p.Options {
			o.Name = strings.TrimSpace(cleanText(o.Name))
			if o.Name == "" || utf8.RuneCountInString(o.Name) > 100 {
				return bad("give each option a name under 100 characters.")
			}
			if o.ID == "" {
				o.ID = shortID()
			}
			if !propIDRE.MatchString(o.ID) || ids[o.ID] {
				return bad("each option needs its own id.")
			}
			ids[o.ID] = true
			if !slices.Contains(optionColors, o.Color) {
				o.Color = "default"
			}
			if p.Type == "status" {
				if !slices.Contains(statusGroups, o.Group) {
					o.Group = "todo"
				}
			} else {
				o.Group = ""
			}
			opts = append(opts, o)
		}
		p.Options = opts
	default:
		p.Options = nil
	}
	if p.Type != "formula" {
		p.Formula = nil
	} else {
		if p.Formula == nil {
			p.Formula = &FormulaConfig{}
		}
		p.Formula.Expression = strings.TrimSpace(p.Formula.Expression)
		p.Formula.Error = ""
		if len(p.Formula.Expression) > maxFormula {
			return bad(fmt.Sprintf("keep the formula under %d characters.", maxFormula))
		}
	}
	if p.Type != "relation" {
		p.Relation = nil
	} else {
		if p.Relation == nil || p.Relation.DatabaseID == uuid.Nil {
			return bad("choose the database it relates to.")
		}
		if p.Relation.Limit != 1 {
			p.Relation.Limit = 0
		}
		p.Relation.MirrorName = strings.TrimSpace(cleanText(p.Relation.MirrorName))
		if utf8.RuneCountInString(p.Relation.MirrorName) > 100 {
			return bad("give the related property a name under 100 characters.")
		}
	}
	if p.Type != "rollup" {
		p.Rollup = nil
	} else {
		if p.Rollup == nil || p.Rollup.Relation == "" || p.Rollup.Property == "" {
			return bad("choose a relation and the property to roll up.")
		}
		if !slices.Contains(rollupFunctions, p.Rollup.Function) {
			p.Rollup.Function = "show_original"
		}
		p.Rollup.Target = nil
	}
	if p.Type != "unique_id" {
		p.UniqueID = nil
	} else {
		if p.UniqueID == nil {
			p.UniqueID = &UniqueIDConfig{}
		}
		p.UniqueID.Prefix = strings.ToUpper(strings.TrimSpace(p.UniqueID.Prefix))
		if !prefixRE.MatchString(p.UniqueID.Prefix) {
			return bad("the prefix is up to 10 letters or digits.")
		}
	}
	if p.Type != "button" {
		p.Button = nil
	} else {
		if p.Button == nil {
			p.Button = &ButtonConfig{}
		}
		p.Button.Label = strings.TrimSpace(cleanText(p.Button.Label))
		if p.Button.Label == "" {
			p.Button.Label = p.Name
		}
		if utf8.RuneCountInString(p.Button.Label) > 60 {
			return bad("keep the button label under 60 characters.")
		}
		if len(p.Button.Actions) > 10 {
			return bad("a button runs up to 10 actions.")
		}
		for i, a := range p.Button.Actions {
			switch a.Type {
			case "set_property":
				if a.Property == "" {
					return bad("choose the property each step sets.")
				}
				a.DatabaseID, a.Values, a.TemplateID, a.URL = nil, nil, nil, ""
			case "add_row":
				if a.DatabaseID == nil {
					return bad("choose the database a step adds a row to.")
				}
				a.Property, a.Value, a.URL = "", nil, ""
			case "open_url":
				u := strings.TrimSpace(a.URL)
				if u != "" && !strings.Contains(u, "://") {
					u = "https://" + u
				}
				u = safeLink(u)
				if u == "" || strings.HasPrefix(u, "mailto:") {
					return bad("enter a web address to open.")
				}
				a = ButtonAction{Type: "open_url", URL: u}
			default:
				return bad("a step sets a property, adds a row or opens a link.")
			}
			p.Button.Actions[i] = a
		}
	}
	return nil
}

// ---- row values --------------------------------------------------------------------

// cleanValue validates one stored row value for its property (not
// relations: cleanRelation). nil clears it. rowID is the row being written
// (uuid.Nil for a new row: files can't be attached yet).
func (s *Service) cleanValue(ctx context.Context, orgID uuid.UUID, p Property, raw json.RawMessage) (any, error) {
	return s.cleanRowValue(ctx, orgID, uuid.Nil, p, raw)
}

func (s *Service) cleanRowValue(ctx context.Context, orgID, rowID uuid.UUID, p Property, raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	if !writable(p) {
		return nil, bad("is worked out for you and can't be set.")
	}
	switch p.Type {
	case "text", "url", "email", "phone":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad("enter text.")
		}
		v = strings.TrimSpace(cleanText(v))
		if v == "" {
			return nil, nil
		}
		if utf8.RuneCountInString(v) > maxTextValue {
			return nil, bad(fmt.Sprintf("keep it under %d characters.", maxTextValue))
		}
		switch p.Type {
		case "url":
			if !strings.Contains(v, "://") {
				v = "https://" + v
			}
			if v = safeLink(v); v == "" || strings.HasPrefix(v, "mailto:") {
				return nil, bad("enter a web address.")
			}
		case "email":
			if !emailRE.MatchString(v) {
				return nil, bad("enter an email address.")
			}
		case "phone":
			if !phoneRE.MatchString(v) {
				return nil, bad("enter a phone number.")
			}
		}
		return v, nil
	case "number":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, bad("enter a number.")
		}
		if _, cur := currencyExp(p.NumberFormat); cur {
			v = math.Round(v) // minor units
			if math.Abs(v) > 9e15 {
				return nil, bad("that amount is too large.")
			}
		}
		return v, nil
	case "checkbox":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, bad("must be true or false.")
		}
		return v, nil
	case "select", "status":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil || !hasOption(p, v) {
			return nil, bad("choose one of its options.")
		}
		return v, nil
	case "multi_select":
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil || len(v) > maxOptions {
			return nil, bad("choose from its options.")
		}
		out := []string{}
		for _, id := range v {
			if !hasOption(p, id) {
				return nil, bad("choose from its options.")
			}
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out, nil
	case "date":
		return cleanDateValue(p, raw)
	case "person":
		var v []uuid.UUID
		if err := json.Unmarshal(raw, &v); err != nil || len(v) > 20 {
			return nil, bad("choose up to 20 people.")
		}
		v = dedupeIDs(v)
		if len(v) == 0 {
			return []string{}, nil
		}
		var n int
		if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND user_id = ANY ($2)`, orgID, v).Scan(&n); err != nil {
			return nil, err
		}
		if n != len(v) {
			return nil, bad("choose people in this " + orgs.Noun() + ".")
		}
		out := make([]string, len(v))
		for i, id := range v {
			out[i] = id.String()
		}
		return out, nil
	case "files":
		return s.cleanFilesValue(ctx, rowID, p, raw)
	case "relation":
		return nil, bad("set relations with row ids.") // cleanRelation handles them
	}
	return nil, bad("can't be set.")
}

// DateValue is a date property's value: a day or a local date-time, an
// optional end, the IANA time zone the times are in, and a reminder.
type DateValue struct {
	Start  string `json:"start"`
	End    string `json:"end,omitempty"`
	TZ     string `json:"tz,omitempty"`
	Remind string `json:"remind,omitempty"`
}

func cleanDateValue(p Property, raw json.RawMessage) (any, error) {
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	var v DateValue
	if err := json.Unmarshal(raw, &v); err != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil, bad("enter a date.")
		}
		v.Start = s
	}
	if !validDateValue(v.Start) || (v.End != "" && (!validDateValue(v.End) || v.End < v.Start)) {
		return nil, bad("enter a date (and an end after the start).")
	}
	if v.TZ != "" {
		if len(v.TZ) > 64 {
			return nil, bad("that time zone isn't known.")
		}
		if _, err := time.LoadLocation(v.TZ); err != nil {
			return nil, bad("that time zone isn't known.")
		}
	}
	if v.Remind != "" {
		if _, ok := reminderOffsets[v.Remind]; !ok {
			return nil, bad("choose a reminder from the list.")
		}
	}
	out := map[string]any{"start": v.Start}
	if v.End != "" {
		out["end"] = v.End
	}
	if v.TZ != "" {
		out["tz"] = v.TZ
	}
	if v.Remind != "" {
		out["remind"] = v.Remind
	}
	return out, nil
}

func validDateValue(s string) bool {
	if !dateValueRE.MatchString(s) {
		return false
	}
	layout := "2006-01-02"
	if len(s) > 10 {
		layout = "2006-01-02T15:04"
	}
	_, err := time.Parse(layout, s)
	return err == nil
}

// FileValue is one entry of a files property: an upload on the row's page
// (file_id) or a link (url).
type FileValue struct {
	FileID *uuid.UUID `json:"file_id,omitempty"`
	URL    string     `json:"url,omitempty"`
	Name   string     `json:"name"`
	Mime   string     `json:"mime,omitempty"`
}

func (s *Service) cleanFilesValue(ctx context.Context, rowID uuid.UUID, p Property, raw json.RawMessage) (any, error) {
	bad := func(msg string) error { return httpx.BadRequest(p.Name + ": " + msg) }
	var list []FileValue
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, bad("add files or links.")
	}
	if len(list) > maxFiles {
		return nil, bad(fmt.Sprintf("holds up to %d files.", maxFiles))
	}
	out := []map[string]any{}
	for _, f := range list {
		name := strings.TrimSpace(cleanText(f.Name))
		if utf8.RuneCountInString(name) > 200 {
			name = string([]rune(name)[:200])
		}
		switch {
		case f.FileID != nil:
			if rowID == uuid.Nil {
				return nil, bad("upload files after the row exists.")
			}
			var stored, mime string
			err := s.d.Pool.QueryRow(ctx, `SELECT name, content_type FROM docs_files WHERE id = $1 AND page_id = $2`, *f.FileID, rowID).Scan(&stored, &mime)
			if err != nil {
				return nil, bad("that file isn't attached to this row.")
			}
			if name == "" {
				name = stored
			}
			out = append(out, map[string]any{"file_id": f.FileID.String(), "name": name, "mime": mime})
		case f.URL != "":
			u := safeLink(strings.TrimSpace(f.URL))
			if u == "" || strings.HasPrefix(u, "mailto:") {
				return nil, bad("enter a web address.")
			}
			if name == "" {
				name = u
			}
			out = append(out, map[string]any{"url": u, "name": name})
		default:
			return nil, bad("add files or links.")
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// fileIDs returns the uploads a files value names.
func fileIDs(v any) []uuid.UUID {
	var out []uuid.UUID
	list, _ := v.([]any)
	for _, x := range list {
		m, _ := x.(map[string]any)
		if s, _ := m["file_id"].(string); s != "" {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}
