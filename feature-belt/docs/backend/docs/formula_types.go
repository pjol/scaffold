package docs

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Formulas (Notion formulas 2.0) are compiled once per schema (parse +
// static type check against the database's properties) and evaluated per row
// at read time for the viewer: values are never stored, because a formula
// that walks a relation sees only the related rows the viewer can open.
//
// This file holds the shared value and type model; formula.go parses and
// type-checks, formula_eval.go evaluates.

// fKind is a formula value's kind.
type fKind uint8

const (
	fEmpty  fKind = iota // no value; statically "any"
	fNumber              // float64
	fText                // string
	fBool                // true / false
	fDate                // a date or date range, with or without a time
	fPerson              // a user id
	fPage                // a database row (relation targets)
	fList                // a list of values
)

var fKindNames = map[fKind]string{fEmpty: "empty", fNumber: "number", fText: "text", fBool: "boolean", fDate: "date", fPerson: "person", fPage: "page", fList: "list"}

// fType is a static type: a kind, plus the element type of a list.
type fType struct {
	Kind fKind
	Elem *fType
}

var (
	tEmpty  = fType{Kind: fEmpty}
	tNumber = fType{Kind: fNumber}
	tText   = fType{Kind: fText}
	tBool   = fType{Kind: fBool}
	tDate   = fType{Kind: fDate}
	tPerson = fType{Kind: fPerson}
	tPage   = fType{Kind: fPage}
)

func tList(elem fType) fType { return fType{Kind: fList, Elem: &elem} }

// String is the type's stored name: "number", "text", "boolean", "date",
// "person", "page", "empty", or "list:<element>".
func (t fType) String() string {
	if t.Kind == fList {
		if t.Elem == nil {
			return "list:empty"
		}
		return "list:" + t.Elem.String()
	}
	return fKindNames[t.Kind]
}

// parseFType reads a stored type name ("" and unknown names are empty).
func parseFType(s string) fType {
	if rest, ok := strings.CutPrefix(s, "list:"); ok {
		return tList(parseFType(rest))
	}
	for k, n := range fKindNames {
		if n == s && k != fList {
			return fType{Kind: k}
		}
	}
	return tEmpty
}

// fDateValue is a date value. Start (and End) are in the evaluation location;
// HasTime says whether the time of day is meaningful.
type fDateValue struct {
	Start   time.Time
	End     *time.Time
	HasTime bool
}

// fValue is a formula value. Kind fEmpty is "no value" (an empty property,
// a failed conversion); lists never contain empty values.
type fValue struct {
	Kind fKind
	Num  float64
	Str  string
	Bool bool
	Date fDateValue
	ID   uuid.UUID // person: user id; page: row id
	DB   uuid.UUID // page: the row's database
	List []fValue
}

var vEmpty = fValue{}

func vNum(n float64) fValue   { return fValue{Kind: fNumber, Num: n} }
func vText(s string) fValue   { return fValue{Kind: fText, Str: s} }
func vBool(b bool) fValue     { return fValue{Kind: fBool, Bool: b} }
func vList(l []fValue) fValue { return fValue{Kind: fList, List: l} }
func vPerson(id uuid.UUID) fValue {
	return fValue{Kind: fPerson, ID: id}
}
func vPage(db, id uuid.UUID) fValue { return fValue{Kind: fPage, DB: db, ID: id} }

// fSchemas gives the compiler the properties of a database (the formula's
// own, and relation targets for `.prop()` on pages).
type fSchemas interface {
	Schema(dbID uuid.UUID) ([]Property, bool)
}

// fEnv is what evaluation reads. The compute pipeline implements it, already
// narrowed to what the viewer can open: Value of a relation lists only
// related rows the viewer can open, and a page value's properties come from
// rows the viewer can open.
type fEnv interface {
	fSchemas
	// Value is a row's value for a property as a formula value (see
	// propFType for the mapping); formulas and rollups are evaluated.
	Value(dbID, rowID uuid.UUID, p Property) fValue
	PersonName(id uuid.UUID) string
	// PersonEmail is "" when the viewer may not see it.
	PersonEmail(id uuid.UUID) string
	// Now is the evaluation time in Location() (the viewer's time zone).
	Now() time.Time
	Location() *time.Location
}

// FormulaError is a compile (syntax or type) error at a byte offset of the
// expression.
type FormulaError struct {
	Pos int    `json:"pos"`
	Msg string `json:"message"`
}

func (e *FormulaError) Error() string { return e.Msg }

// propFType is the formula type of a property's values: title, text, url,
// email, phone, select and status are text; multi-select and files are
// lists of text; number and unique ID numbers (currencies in major units);
// checkbox boolean; date, created time and last edited time dates; person a
// list of people; created by and last edited by a person; relation a list of
// pages; formula and rollup their result type. Buttons have none.
func propFType(p Property) (fType, bool) {
	switch p.Type {
	case "title", "text", "url", "email", "phone", "select", "status":
		return tText, true
	case "multi_select", "files":
		return tList(tText), true
	case "number", "unique_id":
		return tNumber, true
	case "checkbox":
		return tBool, true
	case "date", "created_time", "last_edited_time":
		return tDate, true
	case "person":
		return tList(tPerson), true
	case "created_by", "last_edited_by":
		return tPerson, true
	case "relation":
		return tList(tPage), true
	case "formula":
		if p.Formula != nil {
			return parseFType(p.Formula.Type), true
		}
	case "rollup":
		if p.Rollup != nil {
			return parseFType(p.Rollup.Type), true
		}
	}
	return tEmpty, false
}
