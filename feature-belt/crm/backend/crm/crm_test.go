package crm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in, cur string
		want    int64
		ok      bool
	}{
		{"1,234.56", "USD", 123456, true},
		{"$4,250.50", "USD", 425050, true},
		{"1800", "USD", 180000, true},
		{"1.234,56", "EUR", 123456, true},
		{"1 234,5", "EUR", 123450, true},
		{"12000", "JPY", 12000, true},
		{"1.5", "JPY", 0, false},
		{"2.345", "KWD", 2345, true},
		{"abc", "USD", 0, false},
		{"-5", "USD", 0, false},
		{"", "USD", 0, false},
	}
	for _, c := range cases {
		got, err := parseMoney(c.in, c.cur)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("parseMoney(%q, %s) = %d, %v; want %d ok=%v", c.in, c.cur, got, err, c.want, c.ok)
		}
	}
}

func TestMoneyDecimal(t *testing.T) {
	for in, want := range map[int64]string{0: "0.00", 5: "0.05", 123456: "1234.56", -250: "-2.50"} {
		if got := moneyDecimal(in, "USD"); got != want {
			t.Errorf("moneyDecimal(%d) = %s, want %s", in, got, want)
		}
	}
	if got := moneyDecimal(1500, "JPY"); got != "1500" {
		t.Errorf("JPY = %s", got)
	}
	if got := formatMoney(2880000, "USD"); got != "28800.00 USD" {
		t.Errorf("formatMoney = %s", got)
	}
}

func TestCleanDomain(t *testing.T) {
	ok := map[string]string{
		"https://www.Example.com/about": "example.com",
		"example.co.uk":                 "example.co.uk",
		"sam@brightline.io":             "brightline.io",
		"sub.example.com:8080":          "sub.example.com",
		"":                              "",
	}
	for in, want := range ok {
		got, err := cleanDomain(in)
		if err != nil || got != want {
			t.Errorf("cleanDomain(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"not a domain", "localhost", "exa mple.com"} {
		if _, err := cleanDomain(bad); err == nil {
			t.Errorf("cleanDomain(%q) accepted", bad)
		}
	}
}

func TestCleanEmail(t *testing.T) {
	if e, err := cleanEmail("  Dana@Brightline.IO "); err != nil || e != "dana@brightline.io" {
		t.Errorf("cleanEmail = %q, %v", e, err)
	}
	for _, bad := range []string{"not-an-email", "a@b", "Dana <dana@x.com>"} {
		if _, err := cleanEmail(bad); err == nil {
			t.Errorf("cleanEmail(%q) accepted", bad)
		}
	}
}

func TestCSVSafe(t *testing.T) {
	for in, want := range map[string]string{"=SUM(A1)": "'=SUM(A1)", "+1 503": "'+1 503", "@cmd": "'@cmd", "-2": "'-2", "Acme": "Acme", "": ""} {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseCustomValue(t *testing.T) {
	sel := Property{Key: "tier", Label: "Tier", Type: "select", Options: []Option{{"gold", "Gold"}}}
	if v, err := parseCustomValue(sel, json.RawMessage(`"gold"`), nil); err != nil || v != "gold" {
		t.Errorf("select = %v, %v", v, err)
	}
	if _, err := parseCustomValue(sel, json.RawMessage(`"platinum"`), nil); err == nil {
		t.Error("select accepted an unknown option")
	}
	member := uuid.New()
	user := Property{Key: "csm", Label: "CSM", Type: "user"}
	isMember := func(id uuid.UUID) bool { return id == member }
	if _, err := parseCustomValue(user, json.RawMessage(`"`+uuid.NewString()+`"`), isMember); err == nil {
		t.Error("user accepted a non-member")
	}
	if v, _ := parseCustomValue(user, json.RawMessage("null"), isMember); v != nil {
		t.Error("null didn't clear")
	}
	date := Property{Key: "d", Label: "D", Type: "date"}
	if _, err := parseCustomValue(date, json.RawMessage(`"2026-02-30"`), nil); err == nil {
		t.Error("date accepted Feb 30")
	}
	url := Property{Key: "u", Label: "U", Type: "url"}
	if _, err := parseCustomValue(url, json.RawMessage(`"javascript:alert(1)"`), nil); err == nil {
		t.Error("url accepted javascript:")
	}
}

// Filters are always parameters; an unknown op or a value of the wrong shape is refused.
func TestFilterSQL(t *testing.T) {
	props := builtinProps(objDeal)
	amount, _ := findProp(props, "amount")
	b := &sqlb{args: []any{uuid.Nil, false}}
	sql, err := filterSQL(b, objDeal, amount, Filter{Key: "amount", Op: "gt", Value: json.RawMessage(`100000`)}, uuid.Nil, "UTC")
	if err != nil || !strings.Contains(sql, "$3::numeric") || len(b.args) != 3 {
		t.Errorf("amount gt = %q, %v, args %v", sql, err, b.args)
	}
	if _, err := filterSQL(b, objDeal, amount, Filter{Key: "amount", Op: "contains", Value: json.RawMessage(`"x"`)}, uuid.Nil, "UTC"); err == nil {
		t.Error("contains accepted on a number")
	}
	name, _ := findProp(props, "name")
	sql, err = filterSQL(b, objDeal, name, Filter{Key: "name", Op: "contains", Value: json.RawMessage(`"50%'; DROP"`)}, uuid.Nil, "UTC")
	if err != nil || strings.Contains(sql, "DROP") {
		t.Errorf("name contains leaked input into SQL: %q", sql)
	}
	custom := Property{Key: "seats", Label: "Seats", Type: "number"}
	custom.expr, custom.sortExpr, custom.sortType = customExpr("number")
	sql, err = filterSQL(b, objDeal, custom, Filter{Key: "seats", Op: "gte", Value: json.RawMessage(`40`)}, uuid.Nil, "UTC")
	if err != nil || strings.Contains(sql, "seats") {
		t.Errorf("custom key must be a parameter: %q, %v", sql, err)
	}
}

func TestKeyFromLabel(t *testing.T) {
	for in, want := range map[string]string{"Renewal date": "renewal_date", "2026 budget": "p_2026_budget", "  Seats!! ": "seats"} {
		if got := keyFromLabel(in); got != want || !propKeyRe.MatchString(got) {
			t.Errorf("keyFromLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// crm_update_field never changes who can open a record: owner and team are
// refused before anything is read or written (preview, run and accept).
func TestUpdateFieldRefusesAccessFields(t *testing.T) {
	id := uuid.New().String()
	for _, field := range []string{"owner", "team"} {
		raw := json.RawMessage(`{"record_type":"deal","record_id":"` + id + `","field":"` + field + `","value":"` + uuid.New().String() + `"}`)
		if _, _, err := decodeFieldInput(raw); err != errAccessField {
			t.Errorf("%s: err = %v, want errAccessField", field, err)
		}
	}
	raw := json.RawMessage(`{"record_type":"deal","record_id":"` + id + `","field":"amount","value":125000}`)
	if in, o, err := decodeFieldInput(raw); err != nil || o != objDeal || in.Field != "amount" {
		t.Errorf("amount: %v %v %v", in, o, err)
	}
}
