package docs

import (
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// maxFormulaJoin caps text a formula builds by joining (concatenation, join).
const maxFormulaJoin = 100_000

// fBuiltin is one formula function. Arity counts a method's receiver.
// lambda: argument 1 is evaluated once per element of argument 0 with
// `current` and `index` bound. Functions with lazy evaluate their own
// arguments (short-circuits, lambdas); the rest get evaluated arguments.
type fBuiltin struct {
	min, max int
	lambda   bool
	check    func(c *fChecker, n *fnode, t []sType) (sType, error)
	eval     func(ev *fEvaluator, n *fnode, v []fValue) fValue
	lazy     func(ev *fEvaluator, n *fnode) fValue
}

// fBuiltins is filled in init (the table refers to the checker, which looks
// functions up here).
var fBuiltins map[string]*fBuiltin

// ---- evaluator ------------------------------------------------------------------------

type fEvalVar struct {
	name string
	val  fValue
}

type fEvaluator struct {
	env   fEnv
	db    uuid.UUID
	row   uuid.UUID
	steps int
	vars  []fEvalVar
}

type fEvalAbort struct{}

// Eval evaluates the formula for one row. Runtime problems give no value;
// it never panics.
func (f *Formula) Eval(env fEnv, dbID, rowID uuid.UUID) (out fValue) {
	if f == nil || f.root == nil || env == nil {
		return vEmpty
	}
	defer func() {
		if r := recover(); r != nil {
			out = vEmpty
		}
	}()
	ev := &fEvaluator{env: env, db: dbID, row: rowID}
	return fFinite(ev.eval(f.root))
}

func (ev *fEvaluator) loc() *time.Location {
	if l := ev.env.Location(); l != nil {
		return l
	}
	return time.UTC
}

func (ev *fEvaluator) eval(n *fnode) fValue {
	ev.steps++
	if ev.steps > maxFormulaSteps {
		panic(fEvalAbort{})
	}
	switch n.kind {
	case fnLit:
		return n.val
	case fnList:
		out := make([]fValue, 0, len(n.args))
		for _, a := range n.args {
			if v := ev.eval(a); v.Kind != fEmpty {
				out = append(out, v)
			}
		}
		return vList(out)
	case fnVar:
		for i := len(ev.vars) - 1; i >= 0; i-- {
			if ev.vars[i].name == n.name {
				return ev.vars[i].val
			}
		}
		return vEmpty
	case fnUnary:
		x := ev.eval(n.args[0])
		if n.op == "-" {
			if x.Kind != fNumber {
				return vEmpty
			}
			return vNum(-x.Num)
		}
		return vBool(!fTruthy(x))
	case fnBinary:
		return ev.binary(n)
	case fnTernary:
		if fTruthy(ev.eval(n.args[0])) {
			return ev.eval(n.args[1])
		}
		return ev.eval(n.args[2])
	case fnCall:
		return ev.call(n)
	}
	return vEmpty
}

func (ev *fEvaluator) call(n *fnode) fValue {
	if n.name == "prop" && n.prop != nil {
		if !n.method {
			return ev.env.Value(ev.db, ev.row, *n.prop)
		}
		page := ev.eval(n.args[0])
		if page.Kind != fPage {
			return vEmpty
		}
		return ev.env.Value(page.DB, page.ID, *n.prop)
	}
	if n.fn == nil { // let / lets
		base := len(ev.vars)
		defer func() { ev.vars = ev.vars[:base] }()
		for i := 0; i+1 < len(n.args); i += 2 {
			ev.vars = append(ev.vars, fEvalVar{n.args[i].name, ev.eval(n.args[i+1])})
		}
		return ev.eval(n.args[len(n.args)-1])
	}
	if n.fn.lazy != nil {
		return fFinite(n.fn.lazy(ev, n))
	}
	vals := make([]fValue, len(n.args))
	for i, a := range n.args {
		vals[i] = ev.eval(a)
	}
	return fFinite(n.fn.eval(ev, n, vals))
}

// lambda runs argument 1 for each element of the list in argument 0; fn
// returns false to stop.
func (ev *fEvaluator) lambda(n *fnode, fn func(i int, el, res fValue) bool) {
	list := ev.eval(n.args[0])
	if list.Kind != fList {
		return
	}
	for i, el := range list.List {
		ev.vars = append(ev.vars, fEvalVar{"current", el}, fEvalVar{"index", vNum(float64(i))})
		res := ev.eval(n.args[1])
		ev.vars = ev.vars[:len(ev.vars)-2]
		if !fn(i, el, res) {
			return
		}
	}
}

func (ev *fEvaluator) binary(n *fnode) fValue {
	switch n.op {
	case "and":
		return vBool(fTruthy(ev.eval(n.args[0])) && fTruthy(ev.eval(n.args[1])))
	case "or":
		return vBool(fTruthy(ev.eval(n.args[0])) || fTruthy(ev.eval(n.args[1])))
	}
	a, b := ev.eval(n.args[0]), ev.eval(n.args[1])
	switch n.op {
	case "+":
		return ev.plus(a, b)
	case "-", "*", "/", "%", "^":
		return fArith(n.op, a, b)
	case "==":
		return vBool(fEqual(a, b))
	case "!=":
		return vBool(!fEqual(a, b))
	case "<", "<=", ">", ">=":
		c, ok := fOrdered(a, b)
		if !ok {
			return vBool(false)
		}
		switch n.op {
		case "<":
			return vBool(c < 0)
		case "<=":
			return vBool(c <= 0)
		case ">":
			return vBool(c > 0)
		}
		return vBool(c >= 0)
	}
	return vEmpty
}

// plus adds numbers or joins text (the other side read as text). An empty
// side counts as 0 next to a number.
func (ev *fEvaluator) plus(a, b fValue) fValue {
	if a.Kind == fText || b.Kind == fText {
		s := fValueText(a, ev.env) + fValueText(b, ev.env)
		if len(s) > maxFormulaJoin {
			return vEmpty
		}
		return vText(s)
	}
	return fArith("+", a, b)
}

func fArith(op string, a, b fValue) fValue {
	if a.Kind == fEmpty && b.Kind == fEmpty {
		return vEmpty
	}
	if (a.Kind != fNumber && a.Kind != fEmpty) || (b.Kind != fNumber && b.Kind != fEmpty) {
		return vEmpty
	}
	x, y := a.Num, b.Num
	switch op {
	case "+":
		return fNumC(x + y)
	case "-":
		return fNumC(x - y)
	case "*":
		return fNumC(x * y)
	case "/":
		if y == 0 {
			return vEmpty
		}
		return fNumC(x / y)
	case "%":
		if y == 0 {
			return vEmpty
		}
		return fNumC(math.Mod(x, y))
	case "^":
		return fNumC(math.Pow(x, y))
	}
	return vEmpty
}

// fOrdered compares two values of the same orderable kind.
func fOrdered(a, b fValue) (int, bool) {
	if a.Kind != b.Kind {
		return 0, false
	}
	switch a.Kind {
	case fNumber:
		return fCmpFloat(a.Num, b.Num), true
	case fText:
		return strings.Compare(a.Str, b.Str), true
	case fDate:
		return a.Date.Start.Compare(b.Date.Start), true
	}
	return 0, false
}

func fTruthy(v fValue) bool { return v.Kind == fBool && v.Bool }

func fNumC(n float64) fValue {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return vEmpty
	}
	return vNum(n)
}

// fFinite turns NaN and infinities into no value.
func fFinite(v fValue) fValue {
	if v.Kind == fNumber {
		return fNumC(v.Num)
	}
	return v
}

func fIsBlank(v fValue) bool {
	return v.Kind == fEmpty || (v.Kind == fText && v.Str == "") || (v.Kind == fList && len(v.List) == 0)
}

// fEqual is formula equality: blank values (empty, "", []) equal each
// other; otherwise kinds must match.
func fEqual(a, b fValue) bool {
	if fIsBlank(a) || fIsBlank(b) {
		return fIsBlank(a) && fIsBlank(b)
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case fNumber:
		return a.Num == b.Num
	case fText:
		return a.Str == b.Str
	case fBool:
		return a.Bool == b.Bool
	case fDate:
		if !a.Date.Start.Equal(b.Date.Start) || (a.Date.End == nil) != (b.Date.End == nil) {
			return false
		}
		return a.Date.End == nil || a.Date.End.Equal(*b.Date.End)
	case fPerson, fPage:
		return a.ID == b.ID
	case fList:
		if len(a.List) != len(b.List) {
			return false
		}
		for i := range a.List {
			if !fEqual(a.List[i], b.List[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func fCmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// fCompare orders two values for sorting: by kind first, then numbers,
// text (ignoring case), false before true, dates by start, people and
// pages by id, lists by length then element by element.
func fCompare(a, b fValue) int {
	if a.Kind != b.Kind {
		return fCmpFloat(float64(a.Kind), float64(b.Kind))
	}
	switch a.Kind {
	case fNumber:
		return fCmpFloat(a.Num, b.Num)
	case fText:
		return strings.Compare(strings.ToLower(a.Str), strings.ToLower(b.Str))
	case fBool:
		switch {
		case a.Bool == b.Bool:
			return 0
		case !a.Bool:
			return -1
		}
		return 1
	case fDate:
		return a.Date.Start.Compare(b.Date.Start)
	case fPerson, fPage:
		return strings.Compare(a.ID.String(), b.ID.String())
	case fList:
		if c := fCmpFloat(float64(len(a.List)), float64(len(b.List))); c != 0 {
			return c
		}
		for i := range a.List {
			if c := fCompare(a.List[i], b.List[i]); c != 0 {
				return c
			}
		}
	}
	return 0
}

// ---- output --------------------------------------------------------------------------

// fFmtNum writes a number without float noise (15 significant digits).
func fFmtNum(n float64) string {
	r, err := strconv.ParseFloat(strconv.FormatFloat(n, 'g', 15, 64), 64)
	if err != nil {
		r = n
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func fFmtDateText(d fDateValue) string {
	layout := "2006-01-02"
	if d.HasTime {
		layout = "2006-01-02 15:04"
	}
	s := d.Start.Format(layout)
	if d.End != nil {
		s += " → " + d.End.Format(layout)
	}
	return s
}

// fValueJSON is a value as clients get it.
func fValueJSON(v fValue) any {
	budget := 20_000
	return fJSONValue(v, &budget)
}

func fJSONValue(v fValue, budget *int) any {
	*budget--
	switch v.Kind {
	case fNumber:
		if math.IsNaN(v.Num) || math.IsInf(v.Num, 0) {
			return nil
		}
		return v.Num
	case fText:
		return v.Str
	case fBool:
		return v.Bool
	case fDate:
		layout := "2006-01-02"
		if v.Date.HasTime {
			layout = "2006-01-02T15:04"
		}
		m := map[string]any{"start": v.Date.Start.Format(layout)}
		if v.Date.End != nil {
			m["end"] = v.Date.End.Format(layout)
		}
		return m
	case fPerson:
		return map[string]any{"person": v.ID.String()}
	case fPage:
		return map[string]any{"page": v.ID.String()}
	case fList:
		out := make([]any, 0, len(v.List))
		for _, x := range v.List {
			if *budget <= 0 {
				break
			}
			if j := fJSONValue(x, budget); j != nil {
				out = append(out, j)
			}
		}
		return out
	}
	return nil
}

var fTitleProp = Property{ID: "title", Name: "Name", Type: "title"}

// fValueText is a value as plain text.
func fValueText(v fValue, env fEnv) string {
	var b strings.Builder
	fWriteText(&b, v, env)
	return b.String()
}

func fWriteText(b *strings.Builder, v fValue, env fEnv) {
	if b.Len() > maxFormulaJoin {
		return
	}
	switch v.Kind {
	case fNumber:
		b.WriteString(fFmtNum(v.Num))
	case fText:
		b.WriteString(v.Str)
	case fBool:
		b.WriteString(strconv.FormatBool(v.Bool))
	case fDate:
		b.WriteString(fFmtDateText(v.Date))
	case fPerson:
		if env != nil {
			b.WriteString(env.PersonName(v.ID))
		} else {
			b.WriteString(v.ID.String())
		}
	case fPage:
		title := ""
		if env != nil {
			if t := env.Value(v.DB, v.ID, fTitleProp); t.Kind == fText {
				title = t.Str
			}
		}
		if strings.TrimSpace(title) == "" {
			title = "Untitled"
		}
		b.WriteString(title)
	case fList:
		for i, x := range v.List {
			if i > 0 {
				b.WriteString(", ")
			}
			fWriteText(b, x, env)
		}
	}
}

// ---- stored values -------------------------------------------------------------------

var fTZCache sync.Map // name → *time.Location

func fLoadTZ(name string) *time.Location {
	if l, ok := fTZCache.Load(name); ok {
		return l.(*time.Location)
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	fTZCache.Store(name, l)
	return l
}

func fToFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case interface{ Float64() (float64, error) }:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func fStringList(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func fParseStored(s, tz string, loc *time.Location) (time.Time, bool, bool) {
	if len(s) == 10 {
		t, err := time.ParseInLocation("2006-01-02", s, loc)
		return t, false, err == nil
	}
	in := loc
	if tz != "" {
		if l := fLoadTZ(tz); l != nil {
			in = l
		}
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, in); err == nil {
			return t.In(loc), true, true
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.In(loc), true, true
	}
	return time.Time{}, false, false
}

func fDateFromMap(m map[string]any, loc *time.Location) fValue {
	start, _ := m["start"].(string)
	end, _ := m["end"].(string)
	tz, _ := m["tz"].(string)
	st, hasTime, ok := fParseStored(start, tz, loc)
	if !ok {
		return vEmpty
	}
	d := fDateValue{Start: st, HasTime: hasTime}
	if end != "" {
		if et, endTime, ok := fParseStored(end, tz, loc); ok {
			d.End = &et
			d.HasTime = d.HasTime || endTime
		}
	}
	return fValue{Kind: fDate, Date: d}
}

func fPersonID(v any) (uuid.UUID, bool) {
	switch x := v.(type) {
	case uuid.UUID:
		return x, x != uuid.Nil
	case *uuid.UUID:
		if x != nil {
			return *x, *x != uuid.Nil
		}
	case string:
		id, err := uuid.Parse(x)
		return id, err == nil
	}
	return uuid.Nil, false
}

// fromStored converts a stored row value of property p into a formula value
// (see propFType). visible filters relation targets (nil: all).
func fromStored(p Property, v any, loc *time.Location, visible func(uuid.UUID) bool) fValue {
	if fv, ok := v.(fValue); ok {
		return fv
	}
	if loc == nil {
		loc = time.UTC
	}
	optName := func(id string) (string, bool) {
		for _, o := range p.Options {
			if o.ID == id {
				return o.Name, true
			}
		}
		return "", false
	}
	switch p.Type {
	case "title", "text", "url", "email", "phone":
		if s, _ := v.(string); s != "" {
			return vText(s)
		}
	case "select", "status":
		if s, ok := v.(string); ok {
			if name, ok := optName(s); ok {
				return vText(name)
			}
		}
	case "multi_select":
		out := []fValue{}
		for _, id := range fStringList(v) {
			if name, ok := optName(id); ok {
				out = append(out, vText(name))
			}
		}
		return vList(out)
	case "number", "unique_id":
		f, ok := fToFloat(v)
		if !ok {
			return vEmpty
		}
		if p.Type == "number" {
			f /= numberScale(p)
		}
		return fNumC(f)
	case "checkbox":
		b, _ := v.(bool)
		return vBool(b)
	case "date":
		switch m := v.(type) {
		case map[string]any:
			return fDateFromMap(m, loc)
		case map[string]string:
			conv := make(map[string]any, len(m))
			for k, s := range m {
				conv[k] = s
			}
			return fDateFromMap(conv, loc)
		}
	case "created_time", "last_edited_time":
		switch t := v.(type) {
		case time.Time:
			return fValue{Kind: fDate, Date: fDateValue{Start: t.In(loc), HasTime: true}}
		case *time.Time:
			if t != nil {
				return fValue{Kind: fDate, Date: fDateValue{Start: t.In(loc), HasTime: true}}
			}
		case string:
			if tt, err := time.Parse(time.RFC3339Nano, t); err == nil {
				return fValue{Kind: fDate, Date: fDateValue{Start: tt.In(loc), HasTime: true}}
			}
		}
	case "person":
		out := []fValue{}
		for _, s := range fStringList(v) {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, vPerson(id))
			}
		}
		return vList(out)
	case "created_by", "last_edited_by":
		if id, ok := fPersonID(v); ok {
			return vPerson(id)
		}
	case "files":
		out := []fValue{}
		if list, ok := v.([]any); ok {
			for _, x := range list {
				if m, ok := x.(map[string]any); ok {
					if name, _ := m["name"].(string); name != "" {
						out = append(out, vText(name))
					}
				}
			}
		}
		return vList(out)
	case "relation":
		out := []fValue{}
		var db uuid.UUID
		if p.Relation != nil {
			db = p.Relation.DatabaseID
		}
		for _, id := range fRelationIDs(v) {
			if visible == nil || visible(id) {
				out = append(out, vPage(db, id))
			}
		}
		return vList(out)
	}
	return vEmpty
}

func fRelationIDs(v any) []uuid.UUID {
	var out []uuid.UUID
	switch x := v.(type) {
	case []uuid.UUID:
		return x
	default:
		for _, s := range fStringList(x) {
			if id, err := uuid.Parse(s); err == nil {
				out = append(out, id)
			}
		}
	}
	return out
}

// ---- helpers for builtins -------------------------------------------------------------

var (
	fkNum    = []fKind{fNumber}
	fkText   = []fKind{fText}
	fkBool   = []fKind{fBool}
	fkDate   = []fKind{fDate}
	fkPerson = []fKind{fPerson}
)

// fSig checks fixed argument kinds (nil: any) and returns ret.
func fSig(ret sType, kinds ...[]fKind) func(c *fChecker, n *fnode, t []sType) (sType, error) {
	return func(c *fChecker, n *fnode, t []sType) (sType, error) {
		for i := range t {
			if i < len(kinds) && kinds[i] != nil {
				if err := c.need(n.args[i], t[i], kinds[i]...); err != nil {
					return sAny, err
				}
			}
		}
		return ret, nil
	}
}

func fNum1(f func(float64) float64) *fBuiltin {
	return &fBuiltin{min: 1, max: 1, check: fSig(sNum, fkNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		if v[0].Kind != fNumber {
			return vEmpty
		}
		return fNumC(f(v[0].Num))
	}}
}

func fNum2(f func(a, b float64) fValue) *fBuiltin {
	return &fBuiltin{min: 2, max: 2, check: fSig(sNum, fkNum, fkNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		if v[0].Kind != fNumber || v[1].Kind != fNumber {
			return vEmpty
		}
		return f(v[0].Num, v[1].Num)
	}}
}

func fText1(f func(string) string) *fBuiltin {
	return &fBuiltin{min: 1, max: 1, check: fSig(sText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		if v[0].Kind != fText {
			return vEmpty
		}
		return vText(f(v[0].Str))
	}}
}

func fDateNum(f func(t time.Time) float64) *fBuiltin {
	return &fBuiltin{min: 1, max: 1, check: fSig(sNum, fkDate), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		if v[0].Kind != fDate {
			return vEmpty
		}
		return vNum(f(v[0].Date.Start))
	}}
}

// fCheckNumbers accepts numbers and lists of numbers.
func fCheckNumbers(c *fChecker, n *fnode, t []sType) (sType, error) {
	for i, a := range t {
		if a.Kind == fList {
			if err := c.needList(n.args[i], a, fNumber); err != nil {
				return sAny, err
			}
			continue
		}
		if err := c.need(n.args[i], a, fNumber, fList); err != nil {
			return sAny, err
		}
	}
	return sNum, nil
}

func fFlatNumbers(v []fValue) []float64 {
	var out []float64
	for _, x := range v {
		switch x.Kind {
		case fNumber:
			out = append(out, x.Num)
		case fList:
			for _, e := range x.List {
				if e.Kind == fNumber {
					out = append(out, e.Num)
				}
			}
		}
	}
	return out
}

func fNumbersFn(f func(nums []float64) fValue) *fBuiltin {
	return &fBuiltin{min: 1, max: -1, check: fCheckNumbers, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		return f(fFlatNumbers(v))
	}}
}

func fCapList(l []fValue) []fValue {
	if len(l) > maxFormulaList {
		return l[:maxFormulaList]
	}
	return l
}

// fLambdaBool checks a list function whose expression is a condition.
func fLambdaBool(ret func(list sType) sType) func(c *fChecker, n *fnode, t []sType) (sType, error) {
	return func(c *fChecker, n *fnode, t []sType) (sType, error) {
		if err := c.needList(n.args[0], t[0]); err != nil {
			return sAny, err
		}
		if err := c.need(n.args[1], t[1], fBool); err != nil {
			return sAny, err
		}
		return ret(t[0]), nil
	}
}

var (
	fRegexCache sync.Map // pattern → *regexp.Regexp
	fRegexCount atomic.Int64
)

func fRegexp(pattern string) *regexp.Regexp {
	if r, ok := fRegexCache.Load(pattern); ok {
		return r.(*regexp.Regexp)
	}
	if len(pattern) > 1000 {
		return nil
	}
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	if fRegexCount.Load() < 1000 {
		fRegexCache.Store(pattern, r)
		fRegexCount.Add(1)
	}
	return r
}

var fJSGroupRE = regexp.MustCompile(`\$(\d+|&)`)

// fJSReplacement turns JavaScript-style $1 and $& into Go's ${1} and ${0}.
func fJSReplacement(s string) string {
	return fJSGroupRE.ReplaceAllStringFunc(s, func(m string) string {
		if m == "$&" {
			return "${0}"
		}
		return "${" + m[1:] + "}"
	})
}

func fCheckRegexArgs(ret sType, kinds ...[]fKind) func(c *fChecker, n *fnode, t []sType) (sType, error) {
	base := fSig(ret, kinds...)
	return func(c *fChecker, n *fnode, t []sType) (sType, error) {
		if _, err := base(c, n, t); err != nil {
			return sAny, err
		}
		return ret, c.literalRegexp(n.args[1])
	}
}

// date units --------------------------------------------------------------------------

var fDateUnits = []string{"year", "quarter", "month", "week", "day", "hour", "minute"}

func fNormUnit(s string) (string, bool) {
	u := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "s")
	return u, slices.Contains(fDateUnits, u)
}

func fCheckUnit(c *fChecker, n *fnode) error {
	if n.kind == fnLit && n.val.Kind == fText {
		if _, ok := fNormUnit(n.val.Str); !ok {
			return fErr(n, "Unknown unit “%s”. Use years, quarters, months, weeks, days, hours or minutes.", n.val.Str)
		}
	}
	return nil
}

func fAddMonths(t time.Time, m int) time.Time {
	y, mo, d := t.Date()
	first := time.Date(y, mo+time.Month(m), 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
}

func fAddDate(t time.Time, n float64, unit string) (time.Time, bool) {
	limits := map[string]float64{"year": 10000, "quarter": 40000, "month": 120000, "week": 520000, "day": 3_650_000, "hour": 2_000_000, "minute": 100_000_000}
	if math.Abs(n) > limits[unit] {
		return t, false
	}
	var out time.Time
	switch unit {
	case "year":
		out = fAddMonths(t, int(n)*12)
	case "quarter":
		out = fAddMonths(t, int(n)*3)
	case "month":
		out = fAddMonths(t, int(n))
	case "week":
		out = t.AddDate(0, 0, int(n)*7)
	case "day":
		out = t.AddDate(0, 0, int(n))
	case "hour":
		out = t.Add(time.Duration(n * float64(time.Hour)))
	case "minute":
		out = t.Add(time.Duration(n * float64(time.Minute)))
	default:
		return t, false
	}
	if out.Year() < -9999 || out.Year() > 9999 {
		return t, false
	}
	return out, true
}

func fMonthsBetween(a, b time.Time) int {
	if a.Before(b) {
		return -fMonthsBetween(b, a)
	}
	m := (a.Year()-b.Year())*12 + int(a.Month()) - int(b.Month())
	if fAddMonths(b, m).After(a) {
		m--
	}
	return m
}

func fCivil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

func fDateBetween(a, b time.Time, unit string) float64 {
	switch unit {
	case "year":
		return float64(fMonthsBetween(a, b) / 12)
	case "quarter":
		return float64(fMonthsBetween(a, b) / 3)
	case "month":
		return float64(fMonthsBetween(a, b))
	case "week":
		return math.Trunc(fCivil(a).Sub(fCivil(b)).Hours() / (24 * 7))
	case "day":
		return math.Trunc(fCivil(a).Sub(fCivil(b)).Hours() / 24)
	case "hour":
		return math.Trunc(a.Sub(b).Hours())
	case "minute":
		return math.Trunc(a.Sub(b).Minutes())
	}
	return 0
}

var fDateTokens = []string{"YYYY", "MMMM", "dddd", "MMM", "ddd", "YY", "Do", "MM", "DD", "HH", "hh", "mm", "ss", "M", "D", "H", "h", "A", "a"}

// fFormatDate writes t with Moment-style tokens; [text] is literal.
func fFormatDate(t time.Time, layout string) string {
	var b strings.Builder
	for i := 0; i < len(layout); {
		if layout[i] == '[' {
			if end := strings.IndexByte(layout[i:], ']'); end > 0 {
				b.WriteString(layout[i+1 : i+end])
				i += end + 1
				continue
			}
		}
		matched := ""
		for _, tok := range fDateTokens {
			if strings.HasPrefix(layout[i:], tok) {
				matched = tok
				break
			}
		}
		if matched == "" {
			r, size := utf8.DecodeRuneInString(layout[i:])
			b.WriteRune(r)
			i += size
			continue
		}
		h12 := t.Hour() % 12
		if h12 == 0 {
			h12 = 12
		}
		switch matched {
		case "YYYY":
			b.WriteString(strconv.Itoa(t.Year()))
		case "YY":
			b.WriteString(t.Format("06"))
		case "MMMM":
			b.WriteString(t.Month().String())
		case "MMM":
			b.WriteString(t.Month().String()[:3])
		case "MM":
			b.WriteString(t.Format("01"))
		case "M":
			b.WriteString(strconv.Itoa(int(t.Month())))
		case "DD":
			b.WriteString(t.Format("02"))
		case "D":
			b.WriteString(strconv.Itoa(t.Day()))
		case "Do":
			b.WriteString(fOrdinal(t.Day()))
		case "dddd":
			b.WriteString(t.Weekday().String())
		case "ddd":
			b.WriteString(t.Weekday().String()[:3])
		case "HH":
			b.WriteString(t.Format("15"))
		case "H":
			b.WriteString(strconv.Itoa(t.Hour()))
		case "hh":
			b.WriteString(fPad2(h12))
		case "h":
			b.WriteString(strconv.Itoa(h12))
		case "mm":
			b.WriteString(t.Format("04"))
		case "ss":
			b.WriteString(t.Format("05"))
		case "A":
			b.WriteString(t.Format("PM"))
		case "a":
			b.WriteString(strings.ToLower(t.Format("PM")))
		}
		i += len(matched)
	}
	return b.String()
}

func fPad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func fOrdinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func fParseDateText(s string, loc *time.Location) (fDateValue, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return fDateValue{Start: t.In(loc), HasTime: true}, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return fDateValue{Start: t, HasTime: true}, true
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return fDateValue{Start: t}, true
	}
	return fDateValue{}, false
}

func fRuneSlice(s string, start, end int) string {
	r := []rune(s)
	start, end = fClampRange(start, end, len(r))
	return string(r[start:end])
}

// fClampRange resolves slice bounds (negative from the end) into [0, n].
func fClampRange(start, end, n int) (int, int) {
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	start = min(max(start, 0), n)
	end = min(max(end, 0), n)
	if end < start {
		end = start
	}
	return start, end
}

func fPadText(s string, n int, pad string, start bool) fValue {
	cur := utf8.RuneCountInString(s)
	if n > maxFormulaText {
		return vEmpty
	}
	if pad == "" || cur >= n {
		return vText(s)
	}
	need := n - cur
	fill := []rune(strings.Repeat(pad, need/utf8.RuneCountInString(pad)+1))[:need]
	if start {
		return vText(string(fill) + s)
	}
	return vText(s + string(fill))
}

func fRoundTo(x, places float64) float64 {
	p := math.Pow(10, math.Trunc(places))
	return math.Round(x*p) / p
}

func fMedian(nums []float64) fValue {
	if len(nums) == 0 {
		return vEmpty
	}
	s := slices.Clone(nums)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return vNum(s[m])
	}
	return vNum((s[m-1] + s[m]) / 2)
}

func fToNumber(v fValue) fValue {
	switch v.Kind {
	case fNumber:
		return v
	case fBool:
		if v.Bool {
			return vNum(1)
		}
		return vNum(0)
	case fText:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.Str), 64)
		if err != nil {
			return vEmpty
		}
		return fNumC(f)
	case fDate:
		return vNum(float64(v.Date.Start.UnixMilli()))
	}
	return vEmpty
}

func fEmptyValue(v fValue) bool {
	switch v.Kind {
	case fEmpty:
		return true
	case fText:
		return v.Str == ""
	case fNumber:
		return v.Num == 0
	case fBool:
		return !v.Bool
	case fList:
		return len(v.List) == 0
	}
	return false
}

func fCheckCompare(ret sType, orderedOnly bool) func(c *fChecker, n *fnode, t []sType) (sType, error) {
	return func(c *fChecker, n *fnode, t []sType) (sType, error) {
		if orderedOnly {
			for i := range t {
				if err := c.need(n.args[i], t[i], fNumber, fText, fDate); err != nil {
					return sAny, err
				}
			}
		}
		if t[0].Kind != fEmpty && t[1].Kind != fEmpty && t[0].Kind != t[1].Kind {
			return sAny, fErr(n, "Can't compare %s with %s.", t[0].label(), t[1].label())
		}
		return ret, nil
	}
}

func fCmpFn(test func(c int) bool) func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
	return func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
		c, ok := fOrdered(v[0], v[1])
		return vBool(ok && test(c))
	}
}

func fListSame(c *fChecker, n *fnode, t []sType) (sType, error) {
	if err := c.needList(n.args[0], t[0]); err != nil {
		return sAny, err
	}
	if t[0].Kind == fEmpty {
		return sList(sAny), nil
	}
	return t[0], nil
}

func fListElem(c *fChecker, n *fnode, t []sType) (sType, error) {
	if err := c.needList(n.args[0], t[0]); err != nil {
		return sAny, err
	}
	return t[0].elem(), nil
}

// ---- the function table --------------------------------------------------------------

func init() {
	fBuiltins = map[string]*fBuiltin{
		// logic
		"if": {min: 3, max: 3,
			check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
				if err := c.need(n.args[0], t[0], fBool); err != nil {
					return sAny, err
				}
				u, ok := fUnify(t[1], t[2])
				if !ok {
					return sAny, fErr(n.args[2], "if() gives %s in one case and %s in the other: both need the same kind.", t[1].label(), t[2].label())
				}
				return u, nil
			},
			lazy: func(ev *fEvaluator, n *fnode) fValue {
				if fTruthy(ev.eval(n.args[0])) {
					return ev.eval(n.args[1])
				}
				return ev.eval(n.args[2])
			}},
		"ifs": {min: 3, max: -1,
			check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
				if len(t)%2 == 0 {
					return sAny, fErr(n, "ifs() needs pairs of conditions and values, then a value for when none match.")
				}
				out := sAny
				for i := 0; i < len(t); i++ {
					if i%2 == 0 && i < len(t)-1 {
						if err := c.need(n.args[i], t[i], fBool); err != nil {
							return sAny, err
						}
						continue
					}
					u, ok := fUnify(out, t[i])
					if !ok {
						return sAny, fErr(n.args[i], "ifs() gives %s here and %s elsewhere: every value needs the same kind.", t[i].label(), out.label())
					}
					out = u
				}
				return out, nil
			},
			lazy: func(ev *fEvaluator, n *fnode) fValue {
				for i := 0; i+1 < len(n.args); i += 2 {
					if fTruthy(ev.eval(n.args[i])) {
						return ev.eval(n.args[i+1])
					}
				}
				return ev.eval(n.args[len(n.args)-1])
			}},
		"and": {min: 1, max: -1, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			for i := range t {
				if err := c.need(n.args[i], t[i], fBool); err != nil {
					return sAny, err
				}
			}
			return sBool, nil
		}, lazy: func(ev *fEvaluator, n *fnode) fValue {
			for _, a := range n.args {
				if !fTruthy(ev.eval(a)) {
					return vBool(false)
				}
			}
			return vBool(true)
		}},
		"or": {min: 1, max: -1, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			for i := range t {
				if err := c.need(n.args[i], t[i], fBool); err != nil {
					return sAny, err
				}
			}
			return sBool, nil
		}, lazy: func(ev *fEvaluator, n *fnode) fValue {
			for _, a := range n.args {
				if fTruthy(ev.eval(a)) {
					return vBool(true)
				}
			}
			return vBool(false)
		}},
		"not":       {min: 1, max: 1, check: fSig(sBool, fkBool), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return vBool(!fTruthy(v[0])) }},
		"empty":     {min: 1, max: 1, check: fSig(sBool), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return vBool(fEmptyValue(v[0])) }},
		"equal":     {min: 2, max: 2, check: fCheckCompare(sBool, false), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return vBool(fEqual(v[0], v[1])) }},
		"unequal":   {min: 2, max: 2, check: fCheckCompare(sBool, false), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return vBool(!fEqual(v[0], v[1])) }},
		"larger":    {min: 2, max: 2, check: fCheckCompare(sBool, true), eval: fCmpFn(func(c int) bool { return c > 0 })},
		"largerEq":  {min: 2, max: 2, check: fCheckCompare(sBool, true), eval: fCmpFn(func(c int) bool { return c >= 0 })},
		"smaller":   {min: 2, max: 2, check: fCheckCompare(sBool, true), eval: fCmpFn(func(c int) bool { return c < 0 })},
		"smallerEq": {min: 2, max: 2, check: fCheckCompare(sBool, true), eval: fCmpFn(func(c int) bool { return c <= 0 })},

		// text
		"length": {min: 1, max: 1, check: fSig(sNum, []fKind{fText, fList}), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			switch v[0].Kind {
			case fText:
				return vNum(float64(utf8.RuneCountInString(v[0].Str)))
			case fList:
				return vNum(float64(len(v[0].List)))
			}
			return vNum(0)
		}},
		"substring": {min: 2, max: 3, check: fSig(sText, fkText, fkNum, fkNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fText || v[1].Kind != fNumber {
				return vEmpty
			}
			start := int(v[1].Num)
			end := utf8.RuneCountInString(v[0].Str)
			if len(v) > 2 && v[2].Kind == fNumber {
				end = int(v[2].Num)
			}
			if start < 0 {
				start = 0
			}
			if end < 0 {
				end = 0
			}
			return vText(fRuneSlice(v[0].Str, start, end))
		}},
		"contains": {min: 2, max: 2, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if t[0].Kind == fList {
				return sBool, nil
			}
			return fSig(sBool, fkText, fkText)(c, n, t)
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind == fList {
				return vBool(slices.ContainsFunc(v[0].List, func(x fValue) bool { return fEqual(x, v[1]) }))
			}
			if v[0].Kind != fText {
				return vBool(false)
			}
			return vBool(strings.Contains(v[0].Str, v[1].Str))
		}},
		"test": {min: 2, max: 2, check: fCheckRegexArgs(sBool, fkText, fkText), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			re := fRegexp(v[1].Str)
			if re == nil || v[1].Kind != fText {
				return vEmpty
			}
			return vBool(re.MatchString(fValueText(v[0], ev.env)))
		}},
		"match": {min: 2, max: 2, check: fCheckRegexArgs(sList(sText), fkText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			re := fRegexp(v[1].Str)
			if re == nil || v[0].Kind != fText || v[1].Kind != fText {
				return vEmpty
			}
			out := []fValue{}
			for _, m := range re.FindAllString(v[0].Str, maxFormulaList) {
				out = append(out, vText(m))
			}
			return vList(out)
		}},
		"replace": {min: 3, max: 3, check: fCheckRegexArgs(sText, fkText, fkText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			re := fRegexp(v[1].Str)
			if re == nil || v[0].Kind != fText || v[1].Kind != fText {
				return vEmpty
			}
			loc := re.FindStringSubmatchIndex(v[0].Str)
			if loc == nil {
				return v[0]
			}
			var dst []byte
			dst = re.ExpandString(dst, fJSReplacement(v[2].Str), v[0].Str, loc)
			s := v[0].Str[:loc[0]] + string(dst) + v[0].Str[loc[1]:]
			if len(s) > maxFormulaJoin {
				return vEmpty
			}
			return vText(s)
		}},
		"replaceAll": {min: 3, max: 3, check: fCheckRegexArgs(sText, fkText, fkText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			re := fRegexp(v[1].Str)
			if re == nil || v[0].Kind != fText || v[1].Kind != fText {
				return vEmpty
			}
			s := re.ReplaceAllString(v[0].Str, fJSReplacement(v[2].Str))
			if len(s) > maxFormulaJoin {
				return vEmpty
			}
			return vText(s)
		}},
		"lower":   fText1(strings.ToLower),
		"upper":   fText1(strings.ToUpper),
		"trim":    fText1(strings.TrimSpace),
		"unstyle": fText1(func(s string) string { return s }),
		"repeat": {min: 2, max: 2, check: fSig(sText, fkText, fkNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fText || v[1].Kind != fNumber {
				return vEmpty
			}
			n := int(v[1].Num)
			if n <= 0 || v[0].Str == "" {
				return vText("")
			}
			if float64(utf8.RuneCountInString(v[0].Str))*float64(n) > maxFormulaText {
				return vEmpty
			}
			return vText(strings.Repeat(v[0].Str, n))
		}},
		"padStart": {min: 3, max: 3, check: fSig(sText, fkText, fkNum, fkText), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[1].Kind != fNumber {
				return vEmpty
			}
			return fPadText(fValueText(v[0], ev.env), int(v[1].Num), v[2].Str, true)
		}},
		"padEnd": {min: 3, max: 3, check: fSig(sText, fkText, fkNum, fkText), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[1].Kind != fNumber {
				return vEmpty
			}
			return fPadText(fValueText(v[0], ev.env), int(v[1].Num), v[2].Str, false)
		}},
		"split": {min: 2, max: 2, check: fSig(sList(sText), fkText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fText {
				return vList([]fValue{})
			}
			parts := strings.SplitN(v[0].Str, v[1].Str, maxFormulaList)
			out := make([]fValue, len(parts))
			for i, p := range parts {
				out[i] = vText(p)
			}
			return vList(out)
		}},
		"join": {min: 2, max: 2, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if err := c.needList(n.args[0], t[0]); err != nil {
				return sAny, err
			}
			return sText, c.need(n.args[1], t[1], fText)
		}, eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList {
				return vEmpty
			}
			var b strings.Builder
			for i, x := range v[0].List {
				if i > 0 {
					b.WriteString(v[1].Str)
				}
				fWriteText(&b, x, ev.env)
				if b.Len() > maxFormulaJoin {
					return vEmpty
				}
			}
			return vText(b.String())
		}},
		"concat": {min: 1, max: -1, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			lists := false
			for _, x := range t {
				if x.Kind == fList {
					lists = true
					break
				}
			}
			if !lists {
				for i := range t {
					if err := c.need(n.args[i], t[i], fText); err != nil {
						return sAny, err
					}
				}
				return sText, nil
			}
			el := sAny
			for i := range t {
				if err := c.needList(n.args[i], t[i]); err != nil {
					return sAny, err
				}
				u, ok := fUnify(el, t[i].elem())
				if !ok {
					return sAny, fErr(n.args[i], "concat() joins lists of one kind: this is %s.", t[i].label())
				}
				el = u
			}
			return sList(el), nil
		}, eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if slices.ContainsFunc(v, func(x fValue) bool { return x.Kind == fList }) {
				out := []fValue{}
				for _, x := range v {
					out = append(out, x.List...)
					if len(out) > maxFormulaList {
						break
					}
				}
				return vList(fCapList(out))
			}
			var b strings.Builder
			for _, x := range v {
				fWriteText(&b, x, ev.env)
			}
			if b.Len() > maxFormulaJoin {
				return vEmpty
			}
			return vText(b.String())
		}},
		"format": {min: 1, max: 1, check: fSig(sText), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			return vText(fValueText(v[0], ev.env))
		}},
		"toNumber": {min: 1, max: 1, check: fSig(sNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return fToNumber(v[0]) }},
		"link": {min: 2, max: 2, check: fSig(sText, fkText, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind == fText && v[0].Str != "" {
				return v[0]
			}
			return v[1]
		}},
		"style": {min: 1, max: -1, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			for i := range t {
				if err := c.need(n.args[i], t[i], fText); err != nil {
					return sAny, err
				}
			}
			return sText, nil
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return v[0] }},

		// numbers
		"abs":   fNum1(math.Abs),
		"ceil":  fNum1(math.Ceil),
		"floor": fNum1(math.Floor),
		"sqrt":  fNum1(math.Sqrt),
		"cbrt":  fNum1(math.Cbrt),
		"exp":   fNum1(math.Exp),
		"ln":    fNum1(math.Log),
		"log10": fNum1(math.Log10),
		"log2":  fNum1(math.Log2),
		"sign": fNum1(func(x float64) float64 {
			switch {
			case x > 0:
				return 1
			case x < 0:
				return -1
			}
			return 0
		}),
		"round": {min: 1, max: 2, check: fSig(sNum, fkNum, fkNum), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fNumber {
				return vEmpty
			}
			places := 0.0
			if len(v) > 1 && v[1].Kind == fNumber {
				places = math.Max(-15, math.Min(15, v[1].Num))
			}
			return fNumC(fRoundTo(v[0].Num, places))
		}},
		"pow":      fNum2(func(a, b float64) fValue { return fNumC(math.Pow(a, b)) }),
		"mod":      fNum2(func(a, b float64) fValue { return fArith("%", vNum(a), vNum(b)) }),
		"subtract": fNum2(func(a, b float64) fValue { return fNumC(a - b) }),
		"multiply": fNum2(func(a, b float64) fValue { return fNumC(a * b) }),
		"divide":   fNum2(func(a, b float64) fValue { return fArith("/", vNum(a), vNum(b)) }),
		"add": {min: 2, max: 2, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if t[0].Kind == fText || t[1].Kind == fText {
				return sText, nil
			}
			return fSig(sNum, fkNum, fkNum)(c, n, t)
		}, eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue { return ev.plus(v[0], v[1]) }},
		"min": fNumbersFn(func(nums []float64) fValue {
			if len(nums) == 0 {
				return vEmpty
			}
			return vNum(slices.Min(nums))
		}),
		"max": fNumbersFn(func(nums []float64) fValue {
			if len(nums) == 0 {
				return vEmpty
			}
			return vNum(slices.Max(nums))
		}),
		"sum": fNumbersFn(func(nums []float64) fValue {
			total := 0.0
			for _, x := range nums {
				total += x
			}
			return fNumC(total)
		}),
		"mean": fNumbersFn(func(nums []float64) fValue {
			if len(nums) == 0 {
				return vEmpty
			}
			total := 0.0
			for _, x := range nums {
				total += x
			}
			return fNumC(total / float64(len(nums)))
		}),
		"median": fNumbersFn(fMedian),
		"pi":     {min: 0, max: 0, check: fSig(sNum), eval: func(*fEvaluator, *fnode, []fValue) fValue { return vNum(math.Pi) }},
		"e":      {min: 0, max: 0, check: fSig(sNum), eval: func(*fEvaluator, *fnode, []fValue) fValue { return vNum(math.E) }},

		// dates
		"now": {min: 0, max: 0, check: fSig(sDate), eval: func(ev *fEvaluator, _ *fnode, _ []fValue) fValue {
			return fValue{Kind: fDate, Date: fDateValue{Start: ev.env.Now().In(ev.loc()), HasTime: true}}
		}},
		"today": {min: 0, max: 0, check: fSig(sDate), eval: func(ev *fEvaluator, _ *fnode, _ []fValue) fValue {
			t := ev.env.Now().In(ev.loc())
			return fValue{Kind: fDate, Date: fDateValue{Start: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, ev.loc())}}
		}},
		"minute": fDateNum(func(t time.Time) float64 { return float64(t.Minute()) }),
		"hour":   fDateNum(func(t time.Time) float64 { return float64(t.Hour()) }),
		"day": fDateNum(func(t time.Time) float64 {
			if t.Weekday() == time.Sunday {
				return 7
			}
			return float64(t.Weekday())
		}),
		"date":  fDateNum(func(t time.Time) float64 { return float64(t.Day()) }),
		"week":  fDateNum(func(t time.Time) float64 { _, w := t.ISOWeek(); return float64(w) }),
		"month": fDateNum(func(t time.Time) float64 { return float64(t.Month()) }),
		"year":  fDateNum(func(t time.Time) float64 { return float64(t.Year()) }),
		"dateAdd": {min: 3, max: 3, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if _, err := fSig(sDate, fkDate, fkNum, fkText)(c, n, t); err != nil {
				return sAny, err
			}
			return sDate, fCheckUnit(c, n.args[2])
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return fShiftDate(v, 1) }},
		"dateSubtract": {min: 3, max: 3, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if _, err := fSig(sDate, fkDate, fkNum, fkText)(c, n, t); err != nil {
				return sAny, err
			}
			return sDate, fCheckUnit(c, n.args[2])
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue { return fShiftDate(v, -1) }},
		"dateBetween": {min: 3, max: 3, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if _, err := fSig(sNum, fkDate, fkDate, fkText)(c, n, t); err != nil {
				return sAny, err
			}
			return sNum, fCheckUnit(c, n.args[2])
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			unit, ok := fNormUnit(v[2].Str)
			if v[0].Kind != fDate || v[1].Kind != fDate || !ok {
				return vEmpty
			}
			return vNum(fDateBetween(v[0].Date.Start, v[1].Date.Start, unit))
		}},
		"dateRange": {min: 2, max: 2, check: fSig(sDate, fkDate, fkDate), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fDate || v[1].Kind != fDate {
				return vEmpty
			}
			end := v[1].Date.Start
			if end.Before(v[0].Date.Start) {
				return vEmpty
			}
			return fValue{Kind: fDate, Date: fDateValue{Start: v[0].Date.Start, End: &end, HasTime: v[0].Date.HasTime || v[1].Date.HasTime}}
		}},
		"dateStart": {min: 1, max: 1, check: fSig(sDate, fkDate), eval: fDateStart},
		"dateEnd":   {min: 1, max: 1, check: fSig(sDate, fkDate), eval: fDateEnd},
		"start":     {min: 1, max: 1, check: fSig(sDate, fkDate), eval: fDateStart},
		"end":       {min: 1, max: 1, check: fSig(sDate, fkDate), eval: fDateEnd},
		"timestamp": {min: 1, max: 1, check: fSig(sNum, fkDate), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fDate {
				return vEmpty
			}
			return vNum(float64(v[0].Date.Start.UnixMilli()))
		}},
		"fromTimestamp": {min: 1, max: 1, check: fSig(sDate, fkNum), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fNumber || math.Abs(v[0].Num) > 2.5e14 {
				return vEmpty
			}
			return fValue{Kind: fDate, Date: fDateValue{Start: time.UnixMilli(int64(v[0].Num)).In(ev.loc()), HasTime: true}}
		}},
		"formatDate": {min: 1, max: 2, check: fSig(sText, fkDate, fkText), eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fDate {
				return vEmpty
			}
			layout := "MMMM D, YYYY"
			if len(v) > 1 && v[1].Kind == fText {
				layout = v[1].Str
			}
			if len(layout) > 200 {
				return vEmpty
			}
			s := fFormatDate(v[0].Date.Start, layout)
			if v[0].Date.End != nil {
				s += " → " + fFormatDate(*v[0].Date.End, layout)
			}
			return vText(s)
		}},
		"parseDate": {min: 1, max: 1, check: fSig(sDate, fkText), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fText {
				return vEmpty
			}
			d, ok := fParseDateText(v[0].Str, ev.loc())
			if !ok {
				return vEmpty
			}
			return fValue{Kind: fDate, Date: d}
		}},

		// lists
		"at": {min: 2, max: 2, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if err := c.need(n.args[1], t[1], fNumber); err != nil {
				return sAny, err
			}
			return fListElem(c, n, t)
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList || v[1].Kind != fNumber {
				return vEmpty
			}
			i := int(v[1].Num)
			if i < 0 {
				i += len(v[0].List)
			}
			if i < 0 || i >= len(v[0].List) {
				return vEmpty
			}
			return v[0].List[i]
		}},
		"first": {min: 1, max: 1, check: fListElem, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList || len(v[0].List) == 0 {
				return vEmpty
			}
			return v[0].List[0]
		}},
		"last": {min: 1, max: 1, check: fListElem, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList || len(v[0].List) == 0 {
				return vEmpty
			}
			return v[0].List[len(v[0].List)-1]
		}},
		"slice": {min: 2, max: 3, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			for i := 1; i < len(t); i++ {
				if err := c.need(n.args[i], t[i], fNumber); err != nil {
					return sAny, err
				}
			}
			if t[0].Kind == fText {
				return sText, nil
			}
			return fListSame(c, n, t)
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[1].Kind != fNumber {
				return vEmpty
			}
			switch v[0].Kind {
			case fText:
				end := utf8.RuneCountInString(v[0].Str)
				if len(v) > 2 && v[2].Kind == fNumber {
					end = int(v[2].Num)
				}
				return vText(fRuneSlice(v[0].Str, int(v[1].Num), end))
			case fList:
				end := len(v[0].List)
				if len(v) > 2 && v[2].Kind == fNumber {
					end = int(v[2].Num)
				}
				s, e := fClampRange(int(v[1].Num), end, len(v[0].List))
				return vList(slices.Clone(v[0].List[s:e]))
			}
			return vEmpty
		}},
		"sort": {min: 1, max: 2, lambda: true, check: fListSame, lazy: func(ev *fEvaluator, n *fnode) fValue {
			if len(n.args) == 1 {
				list := ev.eval(n.args[0])
				if list.Kind != fList {
					return vEmpty
				}
				out := slices.Clone(list.List)
				slices.SortStableFunc(out, fCompare)
				return vList(out)
			}
			type keyed struct{ el, key fValue }
			var ks []keyed
			ev.lambda(n, func(_ int, el, res fValue) bool {
				ks = append(ks, keyed{el, res})
				return true
			})
			slices.SortStableFunc(ks, func(a, b keyed) int { return fCompare(a.key, b.key) })
			out := make([]fValue, len(ks))
			for i, k := range ks {
				out[i] = k.el
			}
			return vList(out)
		}},
		"reverse": {min: 1, max: 1, check: fListSame, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList {
				return vEmpty
			}
			out := slices.Clone(v[0].List)
			slices.Reverse(out)
			return vList(out)
		}},
		"includes": {min: 2, max: 2, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			return sBool, c.needList(n.args[0], t[0])
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList {
				return vBool(false)
			}
			return vBool(slices.ContainsFunc(v[0].List, func(x fValue) bool { return fEqual(x, v[1]) }))
		}},
		"find": {min: 2, max: 2, lambda: true, check: fLambdaBool(func(l sType) sType { return l.elem() }), lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := vEmpty
			ev.lambda(n, func(_ int, el, res fValue) bool {
				if fTruthy(res) {
					out = el
					return false
				}
				return true
			})
			return out
		}},
		"findIndex": {min: 2, max: 2, lambda: true, check: fLambdaBool(func(sType) sType { return sNum }), lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := -1
			ev.lambda(n, func(i int, _, res fValue) bool {
				if fTruthy(res) {
					out = i
					return false
				}
				return true
			})
			return vNum(float64(out))
		}},
		"filter": {min: 2, max: 2, lambda: true, check: fLambdaBool(func(l sType) sType {
			if l.Kind == fEmpty {
				return sList(sAny)
			}
			return l
		}), lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := []fValue{}
			ev.lambda(n, func(_ int, el, res fValue) bool {
				if fTruthy(res) {
					out = append(out, el)
				}
				return true
			})
			return vList(out)
		}},
		"map": {min: 2, max: 2, lambda: true, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if err := c.needList(n.args[0], t[0]); err != nil {
				return sAny, err
			}
			return sList(t[1]), nil
		}, lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := []fValue{}
			ev.lambda(n, func(_ int, _, res fValue) bool {
				if res = fFinite(res); res.Kind != fEmpty {
					out = append(out, res)
				}
				return true
			})
			return vList(out)
		}},
		"some": {min: 2, max: 2, lambda: true, check: fLambdaBool(func(sType) sType { return sBool }), lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := false
			ev.lambda(n, func(_ int, _, res fValue) bool {
				out = fTruthy(res)
				return !out
			})
			return vBool(out)
		}},
		"every": {min: 2, max: 2, lambda: true, check: fLambdaBool(func(sType) sType { return sBool }), lazy: func(ev *fEvaluator, n *fnode) fValue {
			out := true
			ev.lambda(n, func(_ int, _, res fValue) bool {
				out = fTruthy(res)
				return out
			})
			return vBool(out)
		}},
		"flat": {min: 1, max: 1, check: func(c *fChecker, n *fnode, t []sType) (sType, error) {
			if err := c.needList(n.args[0], t[0]); err != nil {
				return sAny, err
			}
			if el := t[0].elem(); el.Kind == fList {
				return el, nil
			}
			return fListSame(c, n, t)
		}, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList {
				return vEmpty
			}
			out := []fValue{}
			for _, x := range v[0].List {
				if x.Kind == fList {
					out = append(out, x.List...)
				} else {
					out = append(out, x)
				}
				if len(out) > maxFormulaList {
					break
				}
			}
			return vList(fCapList(out))
		}},
		"unique": {min: 1, max: 1, check: fListSame, eval: func(_ *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fList {
				return vEmpty
			}
			out := []fValue{}
			for _, x := range v[0].List {
				if !slices.ContainsFunc(out, func(y fValue) bool { return fEqual(x, y) }) {
					out = append(out, x)
				}
			}
			return vList(out)
		}},

		// people and pages
		"name": {min: 1, max: 1, check: fSig(sText, fkPerson), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fPerson {
				return vEmpty
			}
			return vText(ev.env.PersonName(v[0].ID))
		}},
		"email": {min: 1, max: 1, check: fSig(sText, fkPerson), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if v[0].Kind != fPerson {
				return vEmpty
			}
			if s := ev.env.PersonEmail(v[0].ID); s != "" {
				return vText(s)
			}
			return vEmpty
		}},
		"id": {min: 0, max: 1, check: fSig(sText, []fKind{fPage}), eval: func(ev *fEvaluator, _ *fnode, v []fValue) fValue {
			if len(v) == 0 {
				return vText(ev.row.String())
			}
			if v[0].Kind != fPage {
				return vEmpty
			}
			return vText(v[0].ID.String())
		}},
	}
}

func fShiftDate(v []fValue, sign float64) fValue {
	unit, ok := fNormUnit(v[2].Str)
	if v[0].Kind != fDate || v[1].Kind != fNumber || !ok {
		return vEmpty
	}
	d := v[0].Date
	start, ok := fAddDate(d.Start, sign*v[1].Num, unit)
	if !ok {
		return vEmpty
	}
	out := fDateValue{Start: start, HasTime: d.HasTime || unit == "hour" || unit == "minute"}
	if d.End != nil {
		end, ok := fAddDate(*d.End, sign*v[1].Num, unit)
		if !ok {
			return vEmpty
		}
		out.End = &end
	}
	return fValue{Kind: fDate, Date: out}
}

func fDateStart(_ *fEvaluator, _ *fnode, v []fValue) fValue {
	if v[0].Kind != fDate {
		return vEmpty
	}
	return fValue{Kind: fDate, Date: fDateValue{Start: v[0].Date.Start, HasTime: v[0].Date.HasTime}}
}

func fDateEnd(_ *fEvaluator, _ *fnode, v []fValue) fValue {
	if v[0].Kind != fDate {
		return vEmpty
	}
	end := v[0].Date.Start
	if v[0].Date.End != nil {
		end = *v[0].Date.End
	}
	return fValue{Kind: fDate, Date: fDateValue{Start: end, HasTime: v[0].Date.HasTime}}
}
