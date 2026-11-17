package docs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// formulaTestEnv is two databases: Projects (relation Tasks → Tasks) and Tasks.
type formulaTestEnv struct {
	schemas map[uuid.UUID][]Property
	rows    map[uuid.UUID]map[string]any
	hidden  map[uuid.UUID]bool
	people  map[uuid.UUID]string
	emails  map[uuid.UUID]string
	now     time.Time
	loc     *time.Location
}

func (e *formulaTestEnv) Schema(db uuid.UUID) ([]Property, bool) {
	s, ok := e.schemas[db]
	return s, ok
}

func (e *formulaTestEnv) Value(_, row uuid.UUID, p Property) fValue {
	if e.hidden[row] {
		return vEmpty
	}
	props := e.rows[row]
	if p.Type == "title" {
		s, _ := props["title"].(string)
		if s == "" {
			return vEmpty
		}
		return vText(s)
	}
	return fromStored(p, props[p.ID], e.loc, func(id uuid.UUID) bool { return !e.hidden[id] })
}

func (e *formulaTestEnv) PersonName(id uuid.UUID) string  { return e.people[id] }
func (e *formulaTestEnv) PersonEmail(id uuid.UUID) string { return e.emails[id] }
func (e *formulaTestEnv) Now() time.Time                  { return e.now }
func (e *formulaTestEnv) Location() *time.Location        { return e.loc }

var (
	ftProjects = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	ftTasks    = uuid.MustParse("00000000-0000-0000-0000-0000000000a2")
	ftProject  = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
	ftTask1    = uuid.MustParse("00000000-0000-0000-0000-0000000000c1")
	ftTask2    = uuid.MustParse("00000000-0000-0000-0000-0000000000c2")
	ftTask3    = uuid.MustParse("00000000-0000-0000-0000-0000000000c3")
	ftSecret   = uuid.MustParse("00000000-0000-0000-0000-0000000000c4")
	ftAlice    = uuid.MustParse("00000000-0000-0000-0000-0000000000d1")
	ftBob      = uuid.MustParse("00000000-0000-0000-0000-0000000000d2")
)

func newFormulaTestEnv(t *testing.T) *formulaTestEnv {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	projects := []Property{
		{ID: "title", Name: "Name", Type: "title"},
		{ID: "tasks", Name: "Tasks", Type: "relation", Relation: &RelationConfig{DatabaseID: ftTasks}},
		{ID: "budget", Name: "Budget", Type: "number", NumberFormat: "usd"},
		{ID: "score", Name: "Score", Type: "number"},
		{ID: "zero", Name: "Zero", Type: "number"},
		{ID: "empty", Name: "Empty number", Type: "number"},
		{ID: "notes", Name: "Notes", Type: "text"},
		{ID: "blank", Name: "Blank", Type: "text"},
		{ID: "tags", Name: "Tags", Type: "multi_select", Options: []Option{{ID: "o1", Name: "Red"}, {ID: "o2", Name: "Blue"}}},
		{ID: "stage", Name: "Stage", Type: "status", Options: []Option{{ID: "s1", Name: "Doing"}}},
		{ID: "due", Name: "Due", Type: "date"},
		{ID: "meet", Name: "Meeting", Type: "date"},
		{ID: "span", Name: "Span", Type: "date"},
		{ID: "owner", Name: "Owner", Type: "person"},
		{ID: "lead", Name: "Lead", Type: "created_by"},
		{ID: "made", Name: "Created", Type: "created_time"},
		{ID: "flag", Name: "Flag", Type: "checkbox"},
		{ID: "files", Name: "Files", Type: "files"},
		{ID: "num", Name: "Num", Type: "unique_id"},
		{ID: "dbl", Name: "Double", Type: "formula", Formula: &FormulaConfig{Expression: `prop("Score") * 2`, Type: "number"}},
		{ID: "btn", Name: "Go", Type: "button"},
		{ID: "self", Name: "Self", Type: "formula", Formula: &FormulaConfig{}},
	}
	tasks := []Property{
		{ID: "title", Name: "Task", Type: "title"},
		{ID: "hours", Name: "Hours", Type: "number"},
		{ID: "done", Name: "Done", Type: "checkbox"},
		{ID: "project", Name: "Project", Type: "relation", Relation: &RelationConfig{DatabaseID: ftProjects}},
		{ID: "who", Name: "Assignee", Type: "person"},
	}
	return &formulaTestEnv{
		schemas: map[uuid.UUID][]Property{ftProjects: projects, ftTasks: tasks},
		rows: map[uuid.UUID]map[string]any{
			ftProject: {
				"title": "Launch", "tasks": []any{ftTask1.String(), ftTask2.String(), ftTask3.String(), ftSecret.String()},
				"budget": float64(12550), "score": float64(7), "zero": float64(0), "notes": "Hello World", "blank": "",
				"tags": []any{"o1", "o2"}, "stage": "s1",
				"due":   map[string]any{"start": "2026-11-02"},
				"meet":  map[string]any{"start": "2026-11-02T14:30", "tz": "America/Los_Angeles"},
				"span":  map[string]any{"start": "2026-01-31", "end": "2026-02-10"},
				"owner": []any{ftAlice.String(), ftBob.String()}, "lead": ftAlice.String(),
				"made": "2026-10-01T12:00:00Z", "flag": true,
				"files": []any{map[string]any{"name": "a.pdf"}, map[string]any{"url": "https://x.test", "name": "x"}},
				"num":   float64(12),
			},
			ftTask1:  {"title": "Design", "hours": float64(3), "done": true, "who": []any{ftAlice.String()}},
			ftTask2:  {"title": "Build", "hours": float64(5), "done": false},
			ftTask3:  {"title": "Ship", "hours": float64(2), "done": true, "who": []any{ftBob.String()}},
			ftSecret: {"title": "Secret", "hours": float64(100), "done": true},
		},
		hidden: map[uuid.UUID]bool{ftSecret: true},
		people: map[uuid.UUID]string{ftAlice: "Alice", ftBob: "Bob"},
		emails: map[uuid.UUID]string{ftAlice: "alice@example.test"},
		now:    time.Date(2026, 11, 2, 15, 4, 5, 0, ny),
		loc:    ny,
	}
}

// eval compiles and evaluates expr on the project row; it fails the test on
// a compile error.
func ftEval(t *testing.T, env *formulaTestEnv, expr string) (fValue, *Formula) {
	t.Helper()
	f, err := compileFormula(expr, ftProjects, "self", env)
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	return f.Eval(env, ftProjects, ftProject), f
}

func ftJSON(v fValue) string {
	b, _ := json.Marshal(fValueJSON(v))
	return string(b)
}

func TestFormulaValues(t *testing.T) {
	env := newFormulaTestEnv(t)
	cases := []struct{ expr, want, typ string }{
		// literals and arithmetic, precedence, associativity
		{`1 + 2 * 3`, `7`, "number"},
		{`(1 + 2) * 3`, `9`, "number"},
		{`2 ^ 3 ^ 2`, `512`, "number"},
		{`-2 ^ 2`, `-4`, "number"},
		{`2 ^ -1`, `0.5`, "number"},
		{`10 - 4 - 3`, `3`, "number"},
		{`20 / 4 / 5`, `1`, "number"},
		{`7 % 3`, `1`, "number"},
		{`1.5e3`, `1500`, "number"},
		{`1 / 0`, `null`, "number"},
		{`5 % 0`, `null`, "number"},
		{`"a" + 1`, `"a1"`, "text"},
		{`1 + "a"`, `"1a"`, "text"},
		{`"x" + true`, `"xtrue"`, "text"},
		{`'it\'s' + " \"ok\""`, `"it's \"ok\""`, "text"},
		{`"tab\there"`, `"tab\there"`, "text"},
		{`/* note */ 1 + /* two */ 2`, `3`, "number"},
		{`prop("Score") + prop("Empty number")`, `7`, "number"},
		{`prop("Empty number") + prop("Empty number")`, `null`, "number"},
		{`prop("Empty number") * 3`, `0`, "number"},
		// comparison and logic
		{`1 < 2 and 2 < 3`, `true`, "boolean"},
		{`1 < 2 && 3 < 2`, `false`, "boolean"},
		{`false or true`, `true`, "boolean"},
		{`false || false`, `false`, "boolean"},
		{`not true`, `false`, "boolean"},
		{`!false`, `true`, "boolean"},
		{`true or false and false`, `true`, "boolean"},
		{`1 == 1`, `true`, "boolean"},
		{`"a" != "b"`, `true`, "boolean"},
		{`"abc" < "abd"`, `true`, "boolean"},
		{`prop("Blank") == ""`, `true`, "boolean"},
		{`prop("Empty number") == 0`, `false`, "boolean"},
		{`prop("Empty number") > 5`, `false`, "boolean"},
		{`1 > 2 ? "big" : "small"`, `"small"`, "text"},
		{`true ? false ? 1 : 2 : 3`, `2`, "number"},
		{`and(true, true, false)`, `false`, "boolean"},
		{`or(false, true)`, `true`, "boolean"},
		{`not(false)`, `true`, "boolean"},
		{`true.not()`, `false`, "boolean"},
		{`if(prop("Flag"), "on", "off")`, `"on"`, "text"},
		{`if(prop("Score") > 10, 1, 0)`, `0`, "number"},
		{`ifs(prop("Score") > 10, "high", prop("Score") > 5, "mid", "low")`, `"mid"`, "text"},
		{`ifs(false, 1, false, 2, 3)`, `3`, "number"},
		{`empty(prop("Blank"))`, `true`, "boolean"},
		{`empty(0)`, `true`, "boolean"},
		{`empty([])`, `true`, "boolean"},
		{`empty(prop("Notes"))`, `false`, "boolean"},
		{`equal(1, 1)`, `true`, "boolean"},
		{`unequal("a", "a")`, `false`, "boolean"},
		{`larger(3, 2)`, `true`, "boolean"},
		{`smallerEq(2, 2)`, `true`, "boolean"},
		// let / lets
		{`let(x, 2, x * x)`, `4`, "number"},
		{`lets(a, 2, b, a + 3, a * b)`, `10`, "number"},
		{`let(x, 1, let(x, 2, x) + x)`, `3`, "number"},
		// properties
		{`prop("Budget")`, `125.5`, "number"},
		{`prop("budget")`, `125.5`, "number"},
		{`prop("BUDGET") * 2`, `251`, "number"},
		{`prop("Notes")`, `"Hello World"`, "text"},
		{`prop("Name")`, `"Launch"`, "text"},
		{`prop("Tags")`, `["Red","Blue"]`, "list:text"},
		{`prop("Stage")`, `"Doing"`, "text"},
		{`prop("Flag")`, `true`, "boolean"},
		{`prop("Files")`, `["a.pdf","x"]`, "list:text"},
		{`prop("Num")`, `12`, "number"},
		{`prop("Double")`, `null`, "number"},
		{`prop("Owner").map(current.name())`, `["Alice","Bob"]`, "list:text"},
		{`prop("Owner").first().email()`, `"alice@example.test"`, "text"},
		{`prop("Owner").last().email()`, `null`, "text"},
		{`prop("Lead").name()`, `"Alice"`, "text"},
		{`format(prop("Owner"))`, `"Alice, Bob"`, "text"},
		// text
		{`length("héllo")`, `5`, "number"},
		{`"héllo".length()`, `5`, "number"},
		{`substring("Hello", 1, 3)`, `"el"`, "text"},
		{`substring("Hello", 2)`, `"llo"`, "text"},
		{`substring("Hello", 4, 1)`, `""`, "text"},
		{`contains("Hello", "ell")`, `true`, "boolean"},
		{`prop("Tags").contains("Red")`, `true`, "boolean"},
		{`test("Order 123", "\\d+")`, `true`, "boolean"},
		{`match("a1b22c333", "\\d+")`, `["1","22","333"]`, "list:text"},
		{`replace("a-b-c", "-", "+")`, `"a+b-c"`, "text"},
		{`replaceAll("a-b-c", "-", "+")`, `"a+b+c"`, "text"},
		{`replaceAll("John Smith", "(\\w+) (\\w+)", "$2, $1")`, `"Smith, John"`, "text"},
		{`replace("x1", "\\d", "[$&]")`, `"x[1]"`, "text"},
		{`lower("ABC")`, `"abc"`, "text"},
		{`upper("abc")`, `"ABC"`, "text"},
		{`trim("  a  ")`, `"a"`, "text"},
		{`repeat("ab", 3)`, `"ababab"`, "text"},
		{`repeat("ab", 100000)`, `null`, "text"},
		{`padStart("7", 3, "0")`, `"007"`, "text"},
		{`padEnd("ab", 5, "xy")`, `"abxyx"`, "text"},
		{`split("a,b,c", ",")`, `["a","b","c"]`, "list:text"},
		{`join(["a", "b"], "-")`, `"a-b"`, "text"},
		{`[1, 2].join(", ")`, `"1, 2"`, "text"},
		{`concat("a", "b", "c")`, `"abc"`, "text"},
		{`concat([1], [2, 3])`, `[1,2,3]`, "list:number"},
		{`format(1.5)`, `"1.5"`, "text"},
		{`format(0.1 + 0.2)`, `"0.3"`, "text"},
		{`format(true)`, `"true"`, "text"},
		{`toNumber("42.5")`, `42.5`, "number"},
		{`toNumber("x")`, `null`, "number"},
		{`toNumber(true)`, `1`, "number"},
		{`link("Docs", "https://example.test")`, `"Docs"`, "text"},
		{`style("Hi", "b", "red")`, `"Hi"`, "text"},
		{`unstyle("Hi")`, `"Hi"`, "text"},
		{`slice("Hello", 1, -1)`, `"ell"`, "text"},
		// numbers
		{`abs(-3)`, `3`, "number"},
		{`ceil(1.2)`, `2`, "number"},
		{`floor(-1.2)`, `-2`, "number"},
		{`round(2.5)`, `3`, "number"},
		{`round(3.14159, 2)`, `3.14`, "number"},
		{`round(1234, -2)`, `1200`, "number"},
		{`sqrt(16)`, `4`, "number"},
		{`sqrt(-1)`, `null`, "number"},
		{`cbrt(27)`, `3`, "number"},
		{`ln(e())`, `1`, "number"},
		{`ln(0)`, `null`, "number"},
		{`log10(1000)`, `3`, "number"},
		{`log2(8)`, `3`, "number"},
		{`sign(-5)`, `-1`, "number"},
		{`pow(2, 10)`, `1024`, "number"},
		{`mod(10, 4)`, `2`, "number"},
		{`add(2, 3)`, `5`, "number"},
		{`add("a", "b")`, `"ab"`, "text"},
		{`subtract(5, 3)`, `2`, "number"},
		{`multiply(4, 2.5)`, `10`, "number"},
		{`divide(1, 4)`, `0.25`, "number"},
		{`divide(1, 0)`, `null`, "number"},
		{`min(3, 1, 2)`, `1`, "number"},
		{`max([3, 9], 4)`, `9`, "number"},
		{`sum(1, 2, 3)`, `6`, "number"},
		{`[1, 2, 3].sum()`, `6`, "number"},
		{`sum([])`, `0`, "number"},
		{`mean(1, 2, 3, 4)`, `2.5`, "number"},
		{`mean([])`, `null`, "number"},
		{`median([5, 1, 3])`, `3`, "number"},
		{`median(4, 1, 3, 2)`, `2.5`, "number"},
		{`round(pi(), 4)`, `3.1416`, "number"},
		// lists
		{`[1, 2, 3].at(0)`, `1`, "number"},
		{`[1, 2, 3].at(-1)`, `3`, "number"},
		{`[1, 2, 3].at(5)`, `null`, "number"},
		{`first([4, 5])`, `4`, "number"},
		{`last([4, 5])`, `5`, "number"},
		{`first([])`, `null`, "empty"},
		{`slice([1, 2, 3, 4], 1, 3)`, `[2,3]`, "list:number"},
		{`slice([1, 2, 3, 4], -2)`, `[3,4]`, "list:number"},
		{`sort([3, 1, 2])`, `[1,2,3]`, "list:number"},
		{`sort(["b", "A", "c"])`, `["A","b","c"]`, "list:text"},
		{`sort([3, 1, 2], 0 - current)`, `[3,2,1]`, "list:number"},
		{`reverse([1, 2, 3])`, `[3,2,1]`, "list:number"},
		{`includes([1, 2], 2)`, `true`, "boolean"},
		{`find([1, 5, 8], current > 4)`, `5`, "number"},
		{`find([1, 2], current > 4)`, `null`, "number"},
		{`findIndex([1, 5, 8], current > 4)`, `1`, "number"},
		{`findIndex([1], current > 4)`, `-1`, "number"},
		{`filter([1, 2, 3, 4], current % 2 == 0)`, `[2,4]`, "list:number"},
		{`map([1, 2, 3], current * 10 + index)`, `[10,21,32]`, "list:number"},
		{`[1, 2, 3].map(current * 2).filter(current > 2)`, `[4,6]`, "list:number"},
		{`some([1, 2], current > 1)`, `true`, "boolean"},
		{`every([1, 2], current > 1)`, `false`, "boolean"},
		{`every([], current > 1)`, `true`, "boolean"},
		{`flat([[1, 2], [3]])`, `[1,2,3]`, "list:number"},
		{`unique([1, 2, 1, 3, 2])`, `[1,2,3]`, "list:number"},
		{`length([1, 2, 3])`, `3`, "number"},
		{`[[1, 2], [3]].map(current.sum())`, `[3,3]`, "list:number"},
		{`map([1, 2], let(x, current, x + index))`, `[1,3]`, "list:number"},
		// relation traversal (the hidden task is never counted)
		{`prop("Tasks").length()`, `3`, "number"},
		{`prop("Tasks").map(current.prop("Hours")).sum()`, `10`, "number"},
		{`prop("Tasks").filter(current.prop("Done")).map(current.prop("Hours")).sum()`, `5`, "number"},
		{`prop("Tasks").map(current.prop("Task"))`, `["Design","Build","Ship"]`, "list:text"},
		{`prop("Tasks").some(current.prop("Hours") > 50)`, `false`, "boolean"},
		{`prop("Tasks").filter(not current.prop("Done")).first().prop("Task")`, `"Build"`, "text"},
		{`prop("Tasks").map(current.prop("Assignee")).flat().map(current.name())`, `["Alice","Bob"]`, "list:text"},
		{`format(prop("Tasks"))`, `"Design, Build, Ship"`, "text"},
		{`prop("Tasks").first().id()`, `"` + ftTask1.String() + `"`, "text"},
		{`id()`, `"` + ftProject.String() + `"`, "text"},
		{`length(prop("Tasks").map(current.prop("Hours")).filter(current >= 3))`, `2`, "number"},
		// dates (evaluation zone America/New_York; now is 2026-11-02 15:04:05)
		{`now()`, `{"start":"2026-11-02T15:04"}`, "date"},
		{`today()`, `{"start":"2026-11-02"}`, "date"},
		{`prop("Due")`, `{"start":"2026-11-02"}`, "date"},
		{`prop("Meeting")`, `{"start":"2026-11-02T17:30"}`, "date"},
		{`hour(prop("Meeting"))`, `17`, "number"},
		{`minute(prop("Meeting"))`, `30`, "number"},
		{`day(prop("Due"))`, `1`, "number"},
		{`day(dateAdd(prop("Due"), 6, "days"))`, `7`, "number"},
		{`date(prop("Due"))`, `2`, "number"},
		{`week(prop("Due"))`, `45`, "number"},
		{`month(prop("Due"))`, `11`, "number"},
		{`year(prop("Due"))`, `2026`, "number"},
		{`dateAdd(prop("Due"), 1, "months")`, `{"start":"2026-12-02"}`, "date"},
		{`dateAdd(parseDate("2026-01-31"), 1, "month")`, `{"start":"2026-02-28"}`, "date"},
		{`dateAdd(prop("Due"), 2, "weeks")`, `{"start":"2026-11-16"}`, "date"},
		{`dateAdd(prop("Due"), 1, "quarters")`, `{"start":"2027-02-02"}`, "date"},
		{`dateAdd(prop("Due"), 3, "hours")`, `{"start":"2026-11-02T03:00"}`, "date"},
		{`dateSubtract(prop("Due"), 1, "years")`, `{"start":"2025-11-02"}`, "date"},
		{`dateBetween(prop("Due"), parseDate("2026-10-01"), "days")`, `32`, "number"},
		{`dateBetween(parseDate("2026-10-01"), prop("Due"), "days")`, `-32`, "number"},
		{`dateBetween(prop("Due"), parseDate("2026-08-03"), "months")`, `2`, "number"},
		{`dateBetween(prop("Due"), parseDate("2025-11-03"), "years")`, `0`, "number"},
		{`dateBetween(prop("Due"), parseDate("2025-11-02"), "years")`, `1`, "number"},
		{`dateBetween(parseDate("2026-11-02T10:00"), parseDate("2026-11-02T07:30"), "minutes")`, `150`, "number"},
		{`dateBetween(parseDate("2026-11-02"), parseDate("2026-10-26"), "weeks")`, `1`, "number"},
		{`prop("Span")`, `{"end":"2026-02-10","start":"2026-01-31"}`, "date"},
		{`dateStart(prop("Span"))`, `{"start":"2026-01-31"}`, "date"},
		{`dateEnd(prop("Span"))`, `{"start":"2026-02-10"}`, "date"},
		{`dateRange(prop("Due"), dateAdd(prop("Due"), 2, "days"))`, `{"end":"2026-11-04","start":"2026-11-02"}`, "date"},
		{`dateRange(prop("Due"), dateSubtract(prop("Due"), 2, "days"))`, `null`, "date"},
		{`timestamp(fromTimestamp(1700000000000))`, `1700000000000`, "number"},
		{`fromTimestamp(0)`, `{"start":"1969-12-31T19:00"}`, "date"},
		{`formatDate(prop("Due"), "MMMM D, YYYY")`, `"November 2, 2026"`, "text"},
		{`formatDate(prop("Meeting"), "ddd, MMM Do YY h:mm A")`, `"Mon, Nov 2nd 26 5:30 PM"`, "text"},
		{`formatDate(prop("Meeting"), "dddd DD/MM/YYYY HH:mm:ss a")`, `"Monday 02/11/2026 17:30:00 pm"`, "text"},
		{`formatDate(prop("Due"), "[Week of] M/D")`, `"Week of 11/2"`, "text"},
		{`formatDate(prop("Due"))`, `"November 2, 2026"`, "text"},
		{`formatDate(prop("Meeting"), "hh:mm")`, `"05:30"`, "text"},
		{`parseDate("2026-03-04T05:06")`, `{"start":"2026-03-04T05:06"}`, "date"},
		{`parseDate("2026-03-04T10:00:00Z")`, `{"start":"2026-03-04T05:00"}`, "date"},
		{`parseDate("nope")`, `null`, "date"},
		{`prop("Due") < prop("Meeting")`, `true`, "boolean"},
		{`prop("Due") == today()`, `true`, "boolean"},
		{`toNumber(parseDate("1970-01-01T00:00:00Z"))`, `0`, "number"},
		{`prop("Created")`, `{"start":"2026-10-01T08:00"}`, "date"},
		{`format(prop("Meeting"))`, `"2026-11-02 17:30"`, "text"},
		{`format(prop("Span"))`, `"2026-01-31 → 2026-02-10"`, "text"},
	}
	for _, tc := range cases {
		v, f := ftEval(t, env, tc.expr)
		if got := ftJSON(v); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.expr, got, tc.want)
		}
		if got := f.Type.String(); got != tc.typ {
			t.Errorf("%s: type %s, want %s", tc.expr, got, tc.typ)
		}
	}
}

func TestFormulaErrors(t *testing.T) {
	env := newFormulaTestEnv(t)
	cases := []struct {
		expr string
		pos  int
		msg  string
	}{
		{`1 +`, 3, "The formula ends too soon."},
		{`(1 + 2`, 6, "Expected “)” but the formula ends."},
		{`"abc`, 0, "This text is missing its closing quote."},
		{`/* x`, 0, "This comment is missing its closing */."},
		{`1 # 2`, 2, "“#” isn't something formulas understand."},
		{`1 2`, 2, "Unexpected “2”. Is an operator or comma missing?"},
		{`1 + "a" * 2`, 4, "Expected a number, got text."},
		{`"a" - 1`, 0, "Expected a number, got text."},
		{`prop("Due") + 1`, 0, "Expected a number or text, got a date. Use dateAdd(), dateSubtract() or dateBetween() with dates."},
		{`1 == "a"`, 2, "Can't compare a number with text."},
		{`1 < "a"`, 2, "Can't compare a number with text."},
		{`if(1, 2, 3)`, 3, "Expected a true/false value, got a number."},
		{`if(true, 1)`, 0, "if() needs 3 arguments."},
		{`if(true, 1, "a")`, 12, "if() gives a number in one case and text in the other: both need the same kind."},
		{`true ? 1 : "a"`, 11, "Both sides of “? :” must give the same kind of value: a number and text."},
		{`ifs(true, 1, false, 2)`, 0, "ifs() needs pairs of conditions and values, then a value for when none match."},
		{`foo(1)`, 0, "Unknown function “foo”."},
		{`dateadd(now(), 1, "days")`, 0, "Unknown function “dateadd”. Did you mean “dateAdd”?"},
		{`prop("Nope")`, 5, "No property called “Nope”."},
		{`prop(1)`, 5, "prop() takes a property name in quotes, like prop(\"Status\")."},
		{`prop("Go")`, 5, "“Go” is a button: it has no value to use."},
		{`prop("Self")`, 5, "A formula can't use itself."},
		{`x + 1`, 0, "Unknown name “x”. Read a property with prop(\"x\")."},
		{`current + 1`, 0, "“current” works only inside map(), filter() and the other list functions."},
		{`sum`, 0, "sum is a function: write sum()."},
		{`substring("a")`, 0, "substring() needs 2 or 3 arguments."},
		{`sum()`, 0, "sum() needs at least 1 argument."},
		{`length(1)`, 7, "Expected text or a list, got a number."},
		{`sum(["a"])`, 4, "Expected a list of number, got a list of text."},
		{`[1, "a"]`, 4, "A list holds one kind of value: this is text, the others are number."},
		{`map(1, current)`, 4, "Expected a list, got a number."},
		{`filter([1], current + 1)`, 12, "Expected a true/false value, got a number."},
		{`prop("Tasks").prop("Hours")`, 14, "This is a list of pages: read each page's property with .map(current.prop(\"Hours\"))."},
		{`prop("Tasks").map(current.prop("Nope"))`, 31, "No property called “Nope”."},
		{`prop("Score").prop("Hours")`, 0, "Expected a page, got a number."},
		{`let(1, 2, 3)`, 4, "Expected a name for the value, like let(total, 1 + 2, total * 2)."},
		{`let(x, 2)`, 0, "let() needs 3 arguments: a name, its value and the result."},
		{`lets(a, 1, b, 2)`, 0, "lets() needs pairs of names and values, then the result."},
		{`let(sum, 1, sum)`, 4, "“sum” is a function name: choose another name."},
		{`test("a", "(")`, 10, "That pattern isn't valid: missing closing ): `(`."},
		{`dateAdd(now(), 1, "dayz")`, 18, "Unknown unit “dayz”. Use years, quarters, months, weeks, days, hours or minutes."},
		{`name(1)`, 5, "Expected a person, got a number."},
		{`x.`, 2, "Expected a function name after “.”."},
		{`[1].sum`, 7, "Expected “(” after .sum."},
		{`1 and`, 5, "The formula ends too soon."},
		{`concat("a", [1])`, 7, "Expected a list, got text."},
	}
	for _, tc := range cases {
		_, err := compileFormula(tc.expr, ftProjects, "self", env)
		fe, ok := err.(*FormulaError)
		if !ok {
			t.Errorf("%s: want a FormulaError, got %v", tc.expr, err)
			continue
		}
		if fe.Msg != tc.msg || fe.Pos != tc.pos {
			t.Errorf("%s: got %d %q, want %d %q", tc.expr, fe.Pos, fe.Msg, tc.pos, tc.msg)
		}
	}
}

func TestFormulaDepthAndBudget(t *testing.T) {
	env := newFormulaTestEnv(t)
	deep := strings.Repeat("(", 80) + "1" + strings.Repeat(")", 80)
	if _, err := compileFormula(deep, ftProjects, "", env); err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Fatalf("deep nesting: %v", err)
	}
	// Exponential work stops at the step budget instead of running away.
	expr := `lets(a, [1,2,3,4,5,6,7,8,9,10], b, a.map(a.map(a.map(a.map(a.map(a.map(current)))))), b.length())`
	f, err := compileFormula(expr, ftProjects, "", env)
	if err != nil {
		t.Fatal(err)
	}
	if v := f.Eval(env, ftProjects, ftProject); v.Kind != fEmpty {
		t.Fatalf("budget: got %v", ftJSON(v))
	}
	// Doubling text is capped.
	expr = `lets(t1, repeat("x", 9000), t2, t1 + t1, t3, t2 + t2, t4, t3 + t3, t5, t4 + t4, t6, t5 + t5, t7, t6 + t6, empty(t7))`
	if v, _ := ftEval(t, env, expr); !v.Bool {
		t.Fatalf("text cap: got %v", ftJSON(v))
	}
	var nilF *Formula
	if v := nilF.Eval(env, ftProjects, ftProject); v.Kind != fEmpty {
		t.Fatal("nil formula has a value")
	}
	empty, err := compileFormula("  ", ftProjects, "", env)
	if err != nil || empty.Type.String() != "empty" || empty.Eval(env, ftProjects, ftProject).Kind != fEmpty {
		t.Fatalf("empty formula: %v", err)
	}
}

func TestFormulaProps(t *testing.T) {
	env := newFormulaTestEnv(t)
	f, err := compileFormula(`prop("Score") + prop("score") + prop("Tasks").map(current.prop("Hours")).sum() + prop("Double")`, ftProjects, "self", env)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.Props, ",") != "score,tasks,dbl" {
		t.Fatalf("props: %v", f.Props)
	}
}

func TestFromStoredVisibility(t *testing.T) {
	p := Property{ID: "tasks", Name: "Tasks", Type: "relation", Relation: &RelationConfig{DatabaseID: ftTasks}}
	v := fromStored(p, []any{ftTask1.String(), ftSecret.String(), "not-an-id"}, time.UTC, func(id uuid.UUID) bool { return id != ftSecret })
	if v.Kind != fList || len(v.List) != 1 || v.List[0].ID != ftTask1 || v.List[0].DB != ftTasks {
		t.Fatalf("relation: %+v", v)
	}
	if v := fromStored(p, nil, nil, nil); v.Kind != fList || len(v.List) != 0 {
		t.Fatalf("empty relation: %+v", v)
	}
	if v := fromStored(Property{Type: "checkbox"}, nil, nil, nil); v.Kind != fBool || v.Bool {
		t.Fatalf("checkbox: %+v", v)
	}
	if v := fromStored(Property{Type: "text"}, "", nil, nil); v.Kind != fEmpty {
		t.Fatalf("blank text: %+v", v)
	}
	if v := fromStored(Property{Type: "select", Options: []Option{{ID: "a", Name: "A"}}}, "gone", nil, nil); v.Kind != fEmpty {
		t.Fatalf("missing option: %+v", v)
	}
	if v := fromStored(Property{Type: "created_by"}, ftAlice, nil, nil); v.Kind != fPerson || v.ID != ftAlice {
		t.Fatalf("created_by: %+v", v)
	}
	if v := fromStored(Property{Type: "number"}, "12", nil, nil); v.Kind != fEmpty {
		t.Fatalf("bad number: %+v", v)
	}
	if v := fromStored(Property{Type: "date"}, map[string]string{"start": "2026-11-02"}, nil, nil); v.Kind != fDate || v.Date.HasTime {
		t.Fatalf("date map[string]string: %+v", v)
	}
	if v := fromStored(Property{Type: "formula"}, vNum(3), nil, nil); v.Kind != fNumber || v.Num != 3 {
		t.Fatalf("passthrough: %+v", v)
	}
}

func TestFCompare(t *testing.T) {
	d1 := fValue{Kind: fDate, Date: fDateValue{Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}
	d2 := fValue{Kind: fDate, Date: fDateValue{Start: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}}
	cases := []struct {
		a, b fValue
		want int
	}{
		{vNum(1), vNum(2), -1},
		{vNum(2), vNum(2), 0},
		{vText("apple"), vText("Banana"), -1},
		{vText("ABC"), vText("abc"), 0},
		{vBool(false), vBool(true), -1},
		{vBool(true), vBool(true), 0},
		{d2, d1, 1},
		{vPerson(ftAlice), vPerson(ftBob), -1},
		{vList([]fValue{vNum(9)}), vList([]fValue{vNum(1), vNum(2)}), -1},
		{vList([]fValue{vNum(1), vNum(3)}), vList([]fValue{vNum(1), vNum(2)}), 1},
		{vEmpty, vNum(0), -1},
		{vNum(5), vText("a"), -1},
	}
	for i, tc := range cases {
		if got := fCompare(tc.a, tc.b); got != tc.want {
			t.Errorf("case %d: got %d, want %d", i, got, tc.want)
		}
		if got := fCompare(tc.b, tc.a); got != -tc.want {
			t.Errorf("case %d reversed: got %d, want %d", i, got, -tc.want)
		}
	}
}

func TestRenameFormulaProp(t *testing.T) {
	cases := []struct{ in, want string }{
		{`prop("Old") + 1`, `prop("New") + 1`},
		{`prop('old') * prop( "OLD" )`, `prop('New') * prop( "New" )`},
		{`prop("Older") + prop("Old")`, `prop("Older") + prop("New")`},
		{`current.prop("Old") + prop("Old")`, `current.prop("Old") + prop("New")`},
		{`"prop(\"Old\")" + prop("Old")`, `"prop(\"Old\")" + prop("New")`},
		{`/* prop("Old") */ 1`, `/* prop("Old") */ 1`},
		{`prop("Old"`, `prop("Old"`},
		{`"unterminated`, `"unterminated`},
	}
	for _, tc := range cases {
		if got := renameFormulaProp(tc.in, "Old", "New"); got != tc.want {
			t.Errorf("%s → %s, want %s", tc.in, got, tc.want)
		}
	}
	if got := renameFormulaProp(`prop("A")`, "A", `Say "hi"`); got != `prop("Say \"hi\"")` {
		t.Errorf("escaping: %s", got)
	}
	if got := renameFormulaProp(`prop('Score') + 1`, "Score", "It's"); got != `prop('It\'s') + 1` {
		t.Errorf("single quote escaping: %s", got)
	}
}

func TestFValueJSONAndText(t *testing.T) {
	env := newFormulaTestEnv(t)
	start := time.Date(2026, 11, 2, 9, 5, 0, 0, env.loc)
	end := start.Add(90 * time.Minute)
	cases := []struct {
		v          fValue
		json, text string
	}{
		{vNum(1.25), `1.25`, "1.25"},
		{vNum(3), `3`, "3"},
		{vText("hi"), `"hi"`, "hi"},
		{vBool(true), `true`, "true"},
		{vEmpty, `null`, ""},
		{fValue{Kind: fDate, Date: fDateValue{Start: start}}, `{"start":"2026-11-02"}`, "2026-11-02"},
		{fValue{Kind: fDate, Date: fDateValue{Start: start, End: &end, HasTime: true}}, `{"end":"2026-11-02T10:35","start":"2026-11-02T09:05"}`, "2026-11-02 09:05 → 2026-11-02 10:35"},
		{vPerson(ftAlice), `{"person":"` + ftAlice.String() + `"}`, "Alice"},
		{vPage(ftTasks, ftTask2), `{"page":"` + ftTask2.String() + `"}`, "Build"},
		{vPage(ftTasks, ftSecret), `{"page":"` + ftSecret.String() + `"}`, "Untitled"},
		{vList([]fValue{vNum(1), vText("a"), vBool(false)}), `[1,"a",false]`, "1, a, false"},
	}
	for i, tc := range cases {
		if got := ftJSON(tc.v); got != tc.json {
			t.Errorf("case %d json: %s, want %s", i, got, tc.json)
		}
		if got := fValueText(tc.v, env); got != tc.text {
			t.Errorf("case %d text: %q, want %q", i, got, tc.text)
		}
	}
	if fValueText(vPerson(ftAlice), nil) != ftAlice.String() {
		t.Error("person text without env")
	}
}

func TestFormulaNeverPanics(t *testing.T) {
	env := newFormulaTestEnv(t)
	exprs := []string{
		`[1, 2].at(1e300)`, `substring("abc", -5, 1e12)`, `slice([1], 1e18, -1e18)`, `padStart("a", 1e9, "b")`,
		`dateAdd(now(), 1e15, "days")`, `fromTimestamp(1e300)`, `round(1, 1e9)`, `repeat("a", -3)`, `pow(10, 400)`,
		`formatDate(now(), repeat("Y", 300))`, `split("", "")`, `[].first().length()`,
	}
	for _, e := range exprs {
		f, err := compileFormula(e, ftProjects, "", env)
		if err != nil {
			t.Errorf("%s: %v", e, err)
			continue
		}
		_ = f.Eval(env, ftProjects, ftProject)
	}
}
