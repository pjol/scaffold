package docs

import "testing"

// The Go filter path (used when a filter touches a computed property) must
// agree with SQL: currency amounts compare in whole minor units.
func TestValueConditionCurrencyMinorUnits(t *testing.T) {
	usd := Property{Type: "number", NumberFormat: "usd"}
	plain := Property{Type: "number"}
	cases := []struct {
		p    Property
		v    float64
		op   string
		arg  float64
		want bool
	}{
		{usd, 12.34, "eq", 12.339, true},
		{usd, 12.34, "eq", 12.346, false},
		{usd, 12.34, "neq", 12.339, false},
		{usd, 12.34, "lte", 12.336, true},
		{usd, 0.3, "eq", 0.1 + 0.2, true},
		{plain, 12.34, "eq", 12.339, false},
		{plain, 0.3, "eq", 0.3, true},
	}
	for _, c := range cases {
		got, ok := valueCondition(fNumC(c.v), c.p, Filter{Op: c.op}, filterArg{num: c.arg, hasNum: true}, nil)
		if !ok || got != c.want {
			t.Errorf("%s %v %s %v = %v (ok %v), want %v", c.p.NumberFormat, c.v, c.op, c.arg, got, ok, c.want)
		}
	}
}
