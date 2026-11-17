package docs

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Formula is a compiled formula: parsed, type-checked against its
// database's properties, ready to evaluate for any row.
type Formula struct {
	Type  fType    // static result type
	Props []string // ids of its own database's properties it reads through prop()

	root *fnode
}

const (
	maxFormulaDepth = 64
	maxFormulaSteps = 200_000
	maxFormulaList  = 10_000
	maxFormulaText  = 10_000
)

// ---- lexer ---------------------------------------------------------------------------

type fTokKind uint8

const (
	tkEOF fTokKind = iota
	tkNum
	tkStr
	tkIdent
	tkOp
)

type fToken struct {
	kind fTokKind
	pos  int    // byte offset
	end  int    // byte offset after the token
	text string // identifier, operator, or decoded string
	num  float64
}

func lexFormula(src string) ([]fToken, error) {
	var out []fToken
	i := 0
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		switch {
		case unicode.IsSpace(r):
			i += size
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, &FormulaError{Pos: i, Msg: "This comment is missing its closing */."}
			}
			i += end + 4
		case r >= '0' && r <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			if j+1 < len(src) && src[j] == '.' && src[j+1] >= '0' && src[j+1] <= '9' {
				j++
				for j < len(src) && src[j] >= '0' && src[j] <= '9' {
					j++
				}
			}
			if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
				k := j + 1
				if k < len(src) && (src[k] == '+' || src[k] == '-') {
					k++
				}
				if k < len(src) && src[k] >= '0' && src[k] <= '9' {
					for k < len(src) && src[k] >= '0' && src[k] <= '9' {
						k++
					}
					j = k
				}
			}
			n, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil {
				return nil, &FormulaError{Pos: i, Msg: "That number is too large."}
			}
			out = append(out, fToken{kind: tkNum, pos: i, end: j, text: src[i:j], num: n})
			i = j
		case r == '"' || r == '\'':
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(src) {
				c := src[j]
				if c == byte(r) {
					closed = true
					j++
					break
				}
				if c == '\\' && j+1 < len(src) {
					switch src[j+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
						b.WriteByte(src[j+1])
					}
					j += 2
					continue
				}
				b.WriteByte(c)
				j++
			}
			if !closed {
				return nil, &FormulaError{Pos: i, Msg: "This text is missing its closing quote."}
			}
			out = append(out, fToken{kind: tkStr, pos: i, end: j, text: b.String()})
			i = j
		case r == '_' || unicode.IsLetter(r):
			j := i
			for j < len(src) {
				r2, s2 := utf8.DecodeRuneInString(src[j:])
				if r2 != '_' && !unicode.IsLetter(r2) && !unicode.IsDigit(r2) {
					break
				}
				j += s2
			}
			out = append(out, fToken{kind: tkIdent, pos: i, end: j, text: src[i:j]})
			i = j
		default:
			op := ""
			for _, cand := range []string{"==", "!=", "<=", ">=", "&&", "||"} {
				if strings.HasPrefix(src[i:], cand) {
					op = cand
					break
				}
			}
			if op == "" && strings.ContainsRune("+-*/%^<>!?:()[],.", r) {
				op = string(r)
			}
			if op == "" {
				return nil, &FormulaError{Pos: i, Msg: fmt.Sprintf("“%c” isn't something formulas understand.", r)}
			}
			out = append(out, fToken{kind: tkOp, pos: i, end: i + len(op), text: op})
			i += len(op)
		}
	}
	return append(out, fToken{kind: tkEOF, pos: len(src), end: len(src)}), nil
}

// ---- parser ---------------------------------------------------------------------------

type fNodeKind uint8

const (
	fnLit fNodeKind = iota
	fnList
	fnVar
	fnUnary
	fnBinary
	fnTernary
	fnCall
)

// fnode is an expression node. Calls keep a method's receiver as args[0].
type fnode struct {
	kind   fNodeKind
	pos    int    // the operator, function name or literal
	start  int    // where the whole expression starts
	op     string // unary / binary operator (normalised: and, or, not, ==, …)
	name   string // variable or function name
	method bool
	val    fValue
	args   []*fnode

	// Set by the checker.
	fn   *fBuiltin
	prop *Property // prop() and page.prop()
}

type fParser struct {
	toks  []fToken
	i     int
	depth int
}

func (p *fParser) peek() fToken { return p.toks[p.i] }
func (p *fParser) next() fToken {
	t := p.toks[p.i]
	if t.kind != tkEOF {
		p.i++
	}
	return t
}

func (p *fParser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tkOp && t.text == s
}

func (p *fParser) expect(s string) (fToken, error) {
	t := p.peek()
	if t.kind == tkOp && t.text == s {
		return p.next(), nil
	}
	return t, &FormulaError{Pos: t.pos, Msg: fmt.Sprintf("Expected “%s”%s.", s, fFoundText(t))}
}

func fFoundText(t fToken) string {
	switch t.kind {
	case tkEOF:
		return " but the formula ends"
	case tkStr:
		return " before the text " + strconv.Quote(t.text)
	default:
		return " before “" + t.text + "”"
	}
}

func (p *fParser) enter(pos int) error {
	p.depth++
	if p.depth > maxFormulaDepth {
		return &FormulaError{Pos: pos, Msg: "This formula is nested too deeply."}
	}
	return nil
}

func (p *fParser) leave() { p.depth-- }

func (p *fParser) expr() (*fnode, error) {
	if err := p.enter(p.peek().pos); err != nil {
		return nil, err
	}
	defer p.leave()
	cond, err := p.or()
	if err != nil {
		return nil, err
	}
	if !p.isOp("?") {
		return cond, nil
	}
	q := p.next()
	a, err := p.expr()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(":"); err != nil {
		return nil, err
	}
	b, err := p.expr()
	if err != nil {
		return nil, err
	}
	return &fnode{kind: fnTernary, pos: q.pos, start: cond.start, args: []*fnode{cond, a, b}}, nil
}

func (p *fParser) binaryLevel(sub func() (*fnode, error), ops map[string]string) (*fnode, error) {
	left, err := sub()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op, ok := "", false
		if t.kind == tkOp || t.kind == tkIdent {
			op, ok = ops[t.text]
		}
		if !ok {
			return left, nil
		}
		p.next()
		right, err := sub()
		if err != nil {
			return nil, err
		}
		left = &fnode{kind: fnBinary, pos: t.pos, start: left.start, op: op, args: []*fnode{left, right}}
	}
}

func (p *fParser) or() (*fnode, error) {
	return p.binaryLevel(p.and, map[string]string{"or": "or", "||": "or"})
}

func (p *fParser) and() (*fnode, error) {
	return p.binaryLevel(p.equality, map[string]string{"and": "and", "&&": "and"})
}

func (p *fParser) equality() (*fnode, error) {
	return p.binaryLevel(p.comparison, map[string]string{"==": "==", "!=": "!="})
}

func (p *fParser) comparison() (*fnode, error) {
	return p.binaryLevel(p.additive, map[string]string{"<": "<", "<=": "<=", ">": ">", ">=": ">="})
}

func (p *fParser) additive() (*fnode, error) {
	return p.binaryLevel(p.multiplicative, map[string]string{"+": "+", "-": "-"})
}

func (p *fParser) multiplicative() (*fnode, error) {
	return p.binaryLevel(p.unary, map[string]string{"*": "*", "/": "/", "%": "%"})
}

// unary binds looser than ^, so -2^2 is -(2^2).
func (p *fParser) unary() (*fnode, error) {
	t := p.peek()
	if (t.kind == tkOp && (t.text == "-" || t.text == "!")) || (t.kind == tkIdent && t.text == "not") {
		p.next()
		if err := p.enter(t.pos); err != nil {
			return nil, err
		}
		defer p.leave()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		op := "-"
		if t.text != "-" {
			op = "not"
		}
		return &fnode{kind: fnUnary, pos: t.pos, start: t.pos, op: op, args: []*fnode{x}}, nil
	}
	return p.power()
}

func (p *fParser) power() (*fnode, error) {
	base, err := p.postfix()
	if err != nil {
		return nil, err
	}
	if !p.isOp("^") {
		return base, nil
	}
	t := p.next()
	if err := p.enter(t.pos); err != nil {
		return nil, err
	}
	defer p.leave()
	exp, err := p.unary()
	if err != nil {
		return nil, err
	}
	return &fnode{kind: fnBinary, pos: t.pos, start: base.start, op: "^", args: []*fnode{base, exp}}, nil
}

func (p *fParser) postfix() (*fnode, error) {
	x, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.isOp(".") {
		p.next()
		name := p.next()
		if name.kind != tkIdent {
			return nil, &FormulaError{Pos: name.pos, Msg: "Expected a function name after “.”."}
		}
		if _, err := p.expect("("); err != nil {
			return nil, &FormulaError{Pos: name.end, Msg: "Expected “(” after ." + name.text + "."}
		}
		args, err := p.callArgs()
		if err != nil {
			return nil, err
		}
		x = &fnode{kind: fnCall, pos: name.pos, start: x.start, name: name.text, method: true, args: append([]*fnode{x}, args...)}
	}
	return x, nil
}

// callArgs reads arguments after "(" up to and including ")".
func (p *fParser) callArgs() ([]*fnode, error) {
	var args []*fnode
	if p.isOp(")") {
		p.next()
		return args, nil
	}
	for {
		a, err := p.expr()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
		if p.isOp(",") {
			p.next()
			continue
		}
		if _, err := p.expect(")"); err != nil {
			return nil, err
		}
		return args, nil
	}
}

func (p *fParser) primary() (*fnode, error) {
	t := p.next()
	switch t.kind {
	case tkNum:
		return &fnode{kind: fnLit, pos: t.pos, start: t.pos, val: vNum(t.num)}, nil
	case tkStr:
		return &fnode{kind: fnLit, pos: t.pos, start: t.pos, val: vText(t.text)}, nil
	case tkIdent:
		switch t.text {
		case "true", "false":
			return &fnode{kind: fnLit, pos: t.pos, start: t.pos, val: vBool(t.text == "true")}, nil
		}
		if p.isOp("(") {
			p.next()
			args, err := p.callArgs()
			if err != nil {
				return nil, err
			}
			return &fnode{kind: fnCall, pos: t.pos, start: t.pos, name: t.text, args: args}, nil
		}
		if t.text == "and" || t.text == "or" {
			return nil, &FormulaError{Pos: t.pos, Msg: "“" + t.text + "” needs a value on each side."}
		}
		return &fnode{kind: fnVar, pos: t.pos, start: t.pos, name: t.text}, nil
	case tkOp:
		switch t.text {
		case "(":
			x, err := p.expr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(")"); err != nil {
				return nil, err
			}
			return x, nil
		case "[":
			n := &fnode{kind: fnList, pos: t.pos, start: t.pos}
			if p.isOp("]") {
				p.next()
				return n, nil
			}
			for {
				item, err := p.expr()
				if err != nil {
					return nil, err
				}
				n.args = append(n.args, item)
				if p.isOp(",") {
					p.next()
					continue
				}
				if _, err := p.expect("]"); err != nil {
					return nil, err
				}
				return n, nil
			}
		}
		return nil, &FormulaError{Pos: t.pos, Msg: "Unexpected “" + t.text + "”."}
	}
	return nil, &FormulaError{Pos: t.pos, Msg: "The formula ends too soon."}
}

// ---- static types --------------------------------------------------------------------

// sType is the checker's type: fType plus the database of page values, so
// `.prop()` on a related page resolves against the right schema.
type sType struct {
	Kind fKind
	Elem *sType
	DB   uuid.UUID
}

var (
	sAny    = sType{Kind: fEmpty}
	sNum    = sType{Kind: fNumber}
	sText   = sType{Kind: fText}
	sBool   = sType{Kind: fBool}
	sDate   = sType{Kind: fDate}
	sPerson = sType{Kind: fPerson}
)

func sList(e sType) sType { return sType{Kind: fList, Elem: &e} }

func (t sType) elem() sType {
	if t.Kind == fList && t.Elem != nil {
		return *t.Elem
	}
	return sAny
}

func (t sType) f() fType {
	if t.Kind == fList {
		return tList(t.elem().f())
	}
	return fType{Kind: t.Kind}
}

func sFrom(t fType) sType {
	if t.Kind == fList {
		if t.Elem == nil {
			return sList(sAny)
		}
		return sList(sFrom(*t.Elem))
	}
	return sType{Kind: t.Kind}
}

var fKindLabels = map[fKind]string{fEmpty: "nothing", fNumber: "a number", fText: "text", fBool: "a true/false value", fDate: "a date",
	fPerson: "a person", fPage: "a page", fList: "a list"}

func (t sType) label() string {
	if t.Kind == fList && t.Elem != nil && t.Elem.Kind != fEmpty {
		return "a list of " + strings.TrimPrefix(strings.TrimPrefix(t.Elem.label(), "a "), "an ")
	}
	return fKindLabels[t.Kind]
}

func fKindsLabel(kinds []fKind) string {
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fKindLabels[k]
	}
	if len(parts) <= 1 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1]
}

// fUnify merges two branch types; ok is false when they can't agree.
func fUnify(a, b sType) (sType, bool) {
	switch {
	case a.Kind == fEmpty:
		return b, true
	case b.Kind == fEmpty:
		return a, true
	case a.Kind != b.Kind:
		return sAny, false
	case a.Kind == fList:
		e, ok := fUnify(a.elem(), b.elem())
		return sList(e), ok
	case a.Kind == fPage && a.DB != b.DB:
		return sType{Kind: fPage}, true
	}
	return a, true
}

// ---- checker --------------------------------------------------------------------------

type fScopeVar struct {
	name string
	typ  sType
}

type fChecker struct {
	dbID    uuid.UUID
	selfID  string
	schemas fSchemas
	props   []string
	scope   []fScopeVar
	lambdas int
	depth   int
}

func fErr(n *fnode, format string, a ...any) error {
	return fErrAt(n.pos, format, a...)
}

func fErrAt(pos int, format string, a ...any) error {
	return &FormulaError{Pos: pos, Msg: fmt.Sprintf(format, a...)}
}

// need checks that t is one of kinds (or unknown).
func (c *fChecker) need(n *fnode, t sType, kinds ...fKind) error {
	if t.Kind == fEmpty {
		return nil
	}
	for _, k := range kinds {
		if t.Kind == k {
			return nil
		}
	}
	return fErrAt(n.start, "Expected %s, got %s.", fKindsLabel(kinds), t.label())
}

// needList checks for a list whose elements are one of kinds (none: any).
func (c *fChecker) needList(n *fnode, t sType, kinds ...fKind) error {
	if t.Kind == fEmpty {
		return nil
	}
	if t.Kind != fList {
		if len(kinds) > 0 {
			return fErrAt(n.start, "Expected a list of %s, got %s.", strings.TrimPrefix(fKindsLabel(kinds), "a "), t.label())
		}
		return fErrAt(n.start, "Expected a list, got %s.", t.label())
	}
	e := t.elem()
	if len(kinds) == 0 || e.Kind == fEmpty {
		return nil
	}
	for _, k := range kinds {
		if e.Kind == k {
			return nil
		}
	}
	return fErrAt(n.start, "Expected a list of %s, got %s.", strings.TrimPrefix(fKindsLabel(kinds), "a "), t.label())
}

func (c *fChecker) lookup(name string) (sType, bool) {
	for i := len(c.scope) - 1; i >= 0; i-- {
		if c.scope[i].name == name {
			return c.scope[i].typ, true
		}
	}
	return sAny, false
}

func (c *fChecker) check(n *fnode) (sType, error) {
	c.depth++
	defer func() { c.depth-- }()
	if c.depth > maxFormulaDepth*2 {
		return sAny, fErr(n, "This formula is nested too deeply.")
	}
	switch n.kind {
	case fnLit:
		return sType{Kind: n.val.Kind}, nil
	case fnList:
		el := sAny
		for _, item := range n.args {
			t, err := c.check(item)
			if err != nil {
				return sAny, err
			}
			u, ok := fUnify(el, t)
			if !ok {
				return sAny, fErr(item, "A list holds one kind of value: this is %s, the others are %s.", t.label(), strings.TrimPrefix(el.label(), "a "))
			}
			el = u
		}
		return sList(el), nil
	case fnVar:
		if t, ok := c.lookup(n.name); ok {
			return t, nil
		}
		if n.name == "current" || n.name == "index" {
			return sAny, fErr(n, "“%s” works only inside map(), filter() and the other list functions.", n.name)
		}
		if _, ok := fBuiltins[n.name]; ok {
			return sAny, fErr(n, "%s is a function: write %s().", n.name, n.name)
		}
		return sAny, fErr(n, "Unknown name “%s”. Read a property with prop(\"%s\").", n.name, n.name)
	case fnUnary:
		t, err := c.check(n.args[0])
		if err != nil {
			return sAny, err
		}
		if n.op == "-" {
			return sNum, c.need(n.args[0], t, fNumber)
		}
		return sBool, c.need(n.args[0], t, fBool)
	case fnBinary:
		return c.checkBinary(n)
	case fnTernary:
		ct, err := c.check(n.args[0])
		if err != nil {
			return sAny, err
		}
		if err := c.need(n.args[0], ct, fBool); err != nil {
			return sAny, err
		}
		a, err := c.check(n.args[1])
		if err != nil {
			return sAny, err
		}
		b, err := c.check(n.args[2])
		if err != nil {
			return sAny, err
		}
		u, ok := fUnify(a, b)
		if !ok {
			return sAny, fErr(n.args[2], "Both sides of “? :” must give the same kind of value: %s and %s.", a.label(), b.label())
		}
		return u, nil
	case fnCall:
		return c.checkCall(n)
	}
	return sAny, fErr(n, "This formula can't be read.")
}

func (c *fChecker) checkBinary(n *fnode) (sType, error) {
	a, err := c.check(n.args[0])
	if err != nil {
		return sAny, err
	}
	b, err := c.check(n.args[1])
	if err != nil {
		return sAny, err
	}
	switch n.op {
	case "+":
		if a.Kind == fText || b.Kind == fText {
			return sText, nil
		}
		if err := c.need(n.args[0], a, fNumber, fText); err != nil {
			return sAny, fAddHint(err, a)
		}
		if err := c.need(n.args[1], b, fNumber, fText); err != nil {
			return sAny, fAddHint(err, b)
		}
		if a.Kind == fEmpty && b.Kind == fEmpty {
			return sAny, nil
		}
		return sNum, nil
	case "-", "*", "/", "%", "^":
		if err := c.need(n.args[0], a, fNumber); err != nil {
			return sAny, fAddHint(err, a)
		}
		if err := c.need(n.args[1], b, fNumber); err != nil {
			return sAny, fAddHint(err, b)
		}
		return sNum, nil
	case "<", "<=", ">", ">=":
		if err := c.need(n.args[0], a, fNumber, fText, fDate); err != nil {
			return sAny, err
		}
		if err := c.need(n.args[1], b, fNumber, fText, fDate); err != nil {
			return sAny, err
		}
		if a.Kind != fEmpty && b.Kind != fEmpty && a.Kind != b.Kind {
			return sAny, fErr(n, "Can't compare %s with %s.", a.label(), b.label())
		}
		return sBool, nil
	case "==", "!=":
		if a.Kind != fEmpty && b.Kind != fEmpty && a.Kind != b.Kind {
			return sAny, fErr(n, "Can't compare %s with %s.", a.label(), b.label())
		}
		return sBool, nil
	case "and", "or":
		if err := c.need(n.args[0], a, fBool); err != nil {
			return sAny, err
		}
		if err := c.need(n.args[1], b, fBool); err != nil {
			return sAny, err
		}
		return sBool, nil
	}
	return sAny, fErr(n, "Unknown operator “%s”.", n.op)
}

// fAddHint points date arithmetic at dateAdd / dateBetween.
func fAddHint(err error, t sType) error {
	if fe, ok := err.(*FormulaError); ok && t.Kind == fDate {
		fe.Msg += " Use dateAdd(), dateSubtract() or dateBetween() with dates."
	}
	return err
}

// resolveProp finds a property of database db by name (any case) or id.
func (c *fChecker) resolveProp(n *fnode, db uuid.UUID, nameNode *fnode) (*Property, sType, error) {
	if nameNode == nil || nameNode.kind != fnLit || nameNode.val.Kind != fText {
		at := n
		if nameNode != nil {
			at = nameNode
		}
		return nil, sAny, fErr(at, "prop() takes a property name in quotes, like prop(\"Status\").")
	}
	name := nameNode.val.Str
	schema, ok := c.schemas.Schema(db)
	if !ok {
		return nil, sAny, fErr(nameNode, "The database for “%s” isn't available.", name)
	}
	var found *Property
	for i := range schema {
		if schema[i].ID == name {
			found = &schema[i]
			break
		}
	}
	if found == nil {
		for i := range schema {
			if strings.EqualFold(schema[i].Name, strings.TrimSpace(name)) {
				found = &schema[i]
				break
			}
		}
	}
	if found == nil {
		return nil, sAny, fErr(nameNode, "No property called “%s”.", name)
	}
	p := *found
	if p.Type == "button" {
		return nil, sAny, fErr(nameNode, "“%s” is a button: it has no value to use.", p.Name)
	}
	ft, _ := propFType(p)
	t := sFrom(ft)
	if p.Type == "relation" && p.Relation != nil {
		t = sList(sType{Kind: fPage, DB: p.Relation.DatabaseID})
	}
	return &p, t, nil
}

func (c *fChecker) checkCall(n *fnode) (sType, error) {
	switch {
	case n.name == "prop" && !n.method:
		if len(n.args) != 1 {
			return sAny, fErr(n, "prop() needs 1 argument: the property name.")
		}
		p, t, err := c.resolveProp(n, c.dbID, n.args[0])
		if err != nil {
			return sAny, err
		}
		if p.ID == c.selfID {
			return sAny, fErr(n.args[0], "A formula can't use itself.")
		}
		n.prop = p
		if !fContainsStr(c.props, p.ID) {
			c.props = append(c.props, p.ID)
		}
		return t, nil
	case n.name == "prop" && n.method:
		if len(n.args) != 2 {
			return sAny, fErr(n, "prop() needs 1 argument: the property name.")
		}
		rt, err := c.check(n.args[0])
		if err != nil {
			return sAny, err
		}
		if rt.Kind == fList && rt.elem().Kind == fPage {
			return sAny, fErr(n, "This is a list of pages: read each page's property with .map(current.prop(\"%s\")).", fNameOf(n.args[1]))
		}
		if rt.Kind != fPage {
			if rt.Kind == fEmpty {
				return sAny, fErr(n.args[0], "Use .prop() on a page from a relation, like current.prop(\"Name\") inside map().")
			}
			return sAny, fErr(n.args[0], "Expected a page, got %s.", rt.label())
		}
		if rt.DB == uuid.Nil {
			return sAny, fErr(n.args[0], "The database of these pages isn't known here, so their properties can't be read.")
		}
		p, t, err := c.resolveProp(n, rt.DB, n.args[1])
		if err != nil {
			return sAny, err
		}
		n.prop = p
		return t, nil
	case (n.name == "let" || n.name == "lets") && !n.method:
		return c.checkLet(n)
	}
	b, ok := fBuiltins[n.name]
	if !ok {
		for name := range fBuiltins {
			if strings.EqualFold(name, n.name) {
				return sAny, fErr(n, "Unknown function “%s”. Did you mean “%s”?", n.name, name)
			}
		}
		return sAny, fErr(n, "Unknown function “%s”.", n.name)
	}
	n.fn = b
	if len(n.args) < b.min || (b.max >= 0 && len(n.args) > b.max) {
		return sAny, fErr(n, "%s() needs %s.", n.name, fArityText(b.min, b.max))
	}
	types := make([]sType, len(n.args))
	for i, a := range n.args {
		if b.lambda && i == 1 {
			el := types[0].elem()
			if err := c.needList(n.args[0], types[0]); err != nil {
				return sAny, err
			}
			c.scope = append(c.scope, fScopeVar{"current", el}, fScopeVar{"index", sNum})
			c.lambdas++
			t, err := c.check(a)
			c.scope = c.scope[:len(c.scope)-2]
			c.lambdas--
			if err != nil {
				return sAny, err
			}
			types[i] = t
			continue
		}
		t, err := c.check(a)
		if err != nil {
			return sAny, err
		}
		types[i] = t
	}
	return b.check(c, n, types)
}

func (c *fChecker) checkLet(n *fnode) (sType, error) {
	if len(n.args) < 3 || len(n.args)%2 == 0 {
		if n.name == "let" {
			return sAny, fErr(n, "let() needs 3 arguments: a name, its value and the result.")
		}
		return sAny, fErr(n, "lets() needs pairs of names and values, then the result.")
	}
	if n.name == "let" && len(n.args) != 3 {
		return sAny, fErr(n, "let() needs 3 arguments: a name, its value and the result.")
	}
	base := len(c.scope)
	defer func() { c.scope = c.scope[:base] }()
	for i := 0; i+1 < len(n.args); i += 2 {
		nameNode := n.args[i]
		if nameNode.kind != fnVar {
			return sAny, fErr(nameNode, "Expected a name for the value, like let(total, 1 + 2, total * 2).")
		}
		if _, ok := fBuiltins[nameNode.name]; ok || nameNode.name == "prop" {
			return sAny, fErr(nameNode, "“%s” is a function name: choose another name.", nameNode.name)
		}
		t, err := c.check(n.args[i+1])
		if err != nil {
			return sAny, err
		}
		c.scope = append(c.scope, fScopeVar{nameNode.name, t})
	}
	return c.check(n.args[len(n.args)-1])
}

func fNameOf(n *fnode) string {
	if n != nil && n.kind == fnLit && n.val.Kind == fText {
		return n.val.Str
	}
	return "Name"
}

func fArityText(min, max int) string {
	plural := func(k int) string {
		if k == 1 {
			return "1 argument"
		}
		return strconv.Itoa(k) + " arguments"
	}
	switch {
	case max == min:
		return plural(min)
	case max < 0:
		return "at least " + plural(min)
	case max == min+1:
		return strconv.Itoa(min) + " or " + plural(max)
	}
	return strconv.Itoa(min) + " to " + plural(max)
}

func fContainsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// compileFormula parses and type-checks expr for database dbID. selfID is
// the property being compiled. An empty expression compiles to a formula
// with no value.
func compileFormula(expr string, dbID uuid.UUID, selfID string, schemas fSchemas) (*Formula, error) {
	if strings.TrimSpace(expr) == "" {
		return &Formula{Type: tEmpty}, nil
	}
	toks, err := lexFormula(expr)
	if err != nil {
		return nil, err
	}
	p := &fParser{toks: toks}
	root, err := p.expr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tkEOF {
		label := t.text
		if t.kind == tkStr {
			label = strconv.Quote(t.text)
		}
		return nil, &FormulaError{Pos: t.pos, Msg: "Unexpected “" + label + "”. Is an operator or comma missing?"}
	}
	c := &fChecker{dbID: dbID, selfID: selfID, schemas: schemas}
	t, err := c.check(root)
	if err != nil {
		return nil, err
	}
	return &Formula{Type: t.f(), Props: c.props, root: root}, nil
}

// renameFormulaProp rewrites prop("Old") references (either quote style,
// any letter case) to prop("New"). Only prop() on the formula's own row is
// rewritten; page.prop() reads another database. Everything else stays
// byte-identical; an expression that doesn't lex is returned unchanged.
func renameFormulaProp(expr, oldName, newName string) string {
	toks, err := lexFormula(expr)
	if err != nil {
		return expr
	}
	var b strings.Builder
	last := 0
	for i := 0; i+3 < len(toks); i++ {
		t := toks[i]
		if t.kind != tkIdent || t.text != "prop" {
			continue
		}
		if i > 0 && toks[i-1].kind == tkOp && toks[i-1].text == "." {
			continue
		}
		open, str, cls := toks[i+1], toks[i+2], toks[i+3]
		if open.kind != tkOp || open.text != "(" || str.kind != tkStr || cls.kind != tkOp || cls.text != ")" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(str.text), strings.TrimSpace(oldName)) {
			continue
		}
		quote := expr[str.pos]
		b.WriteString(expr[last:str.pos])
		b.WriteString(fQuote(newName, quote))
		last = str.end
	}
	if last == 0 {
		return expr
	}
	b.WriteString(expr[last:])
	return b.String()
}

func fQuote(s string, quote byte) string {
	var b strings.Builder
	b.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case quote, '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// literalRegexp compiles a pattern given as a literal so mistakes show when
// the formula is saved.
func (c *fChecker) literalRegexp(n *fnode) error {
	if n.kind != fnLit || n.val.Kind != fText {
		return nil
	}
	if len(n.val.Str) > 1000 {
		return fErr(n, "That pattern is too long.")
	}
	if _, err := regexp.Compile(n.val.Str); err != nil {
		return fErr(n, "That pattern isn't valid: %s.", strings.TrimPrefix(err.Error(), "error parsing regexp: "))
	}
	return nil
}
