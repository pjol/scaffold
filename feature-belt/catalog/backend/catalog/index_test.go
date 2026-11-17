package catalog

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Ported from MOX's universe tests (index_test.go, search_test.go) onto the
// development elements plus a small set of names with the cases the
// normaliser and the resolver must handle.

var (
	devOnce sync.Once
	devRecs []rec
	devErr  error
)

// devElements are devdata/elements.ndjson as validated rows.
func devElements(t testing.TB) []rec {
	t.Helper()
	devOnce.Do(func() {
		devErr = DecodeItems(bytes.NewReader(devItems), func(it Item) error {
			r, err := normalize(it, false)
			if err != nil {
				return err
			}
			devRecs = append(devRecs, r)
			return nil
		}, func(err error) error { return err })
	})
	if devErr != nil {
		t.Fatal(devErr)
	}
	return slices.Clone(devRecs)
}

// mk builds a valid row (normalize runs, so names and aliases are cleaned as a sync cleans them).
func mk(t testing.TB, key, name, category string, aliases ...string) rec {
	t.Helper()
	r, err := normalize(Item{Key: key, Name: name, Category: category, Aliases: aliases}, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// tricky are names the normaliser has to fold.
func tricky(t testing.TB) []rec {
	return []rec{
		mk(t, "torch", "Crème Brûlée Torch", "Kitchen"),
		mk(t, "anvil", "Smith's Anvil", "Workshop"),
		mk(t, "clamp", "Spring-Loaded Clamp", "Workshop"),
		mk(t, "lamp", "Æther Lamp", "Lighting"),
		mk(t, "mill", "Salt / Pepper Mill", "Kitchen", "Pepper grinder"),
		mk(t, "tram", "Straßenbahn Model", "Toys"),
		mk(t, "blocks", "ＡＢＣ Blocks", "Toys"),
		mk(t, "rope", "Jötun Rope", "Outdoor"),
		mk(t, "quote", "“Quoted” Mug", "Kitchen"),
	}
}

func testIndex(t testing.TB, recs ...rec) *index {
	t.Helper()
	x, err := buildIndex(recs, 1)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func fullIndex(t testing.TB) *index {
	return testIndex(t, append(devElements(t), tricky(t)...)...)
}

func names(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestDevElements(t *testing.T) {
	recs := devElements(t)
	if len(recs) != 50 {
		t.Fatalf("%d dev items, want 50", len(recs))
	}
	x := testIndex(t, recs...)
	priced := 0
	for _, it := range x.items(x.browse(Filter{}, "", "", 100)) {
		if it.Key == "" || it.Name == "" || it.Category == "" || it.Subtitle == "" || it.Attrs["symbol"] == "" || it.Attrs["atomic_number"] == "" {
			t.Errorf("%s: incomplete %+v", it.Key, it)
		}
		if len(it.Aliases) == 0 || it.Aliases[0] != it.Attrs["symbol"] {
			t.Errorf("%s: aliases %v don't start with the symbol", it.Name, it.Aliases)
		}
		if it.RefPrice != nil {
			priced++
			if it.Currency != "USD" || !it.RefPrice.IsPositive() || it.Attrs["price_unit"] == "" {
				t.Errorf("%s: price %v %s unit %q", it.Name, it.RefPrice, it.Currency, it.Attrs["price_unit"])
			}
		}
	}
	if priced != 5 {
		t.Errorf("%d priced items, want 5", priced)
	}
	if it, ok := x.byKey["element-10"]; !ok || x.item(it).Attrs["atomic_weight"] != "20.180" {
		t.Errorf("a number attribute should keep its digits as written: %+v", x.item(it).Attrs)
	}
}

func TestNameKey(t *testing.T) {
	for in, want := range map[string]string{
		"Crème Brûlée Torch":       "creme brulee torch",
		"Smith’s Anvil":            "smith's anvil",
		"Salt/Pepper":              "salt / pepper",
		"Salt//Pepper":             "salt / pepper",
		"Salt | Pepper":            "salt / pepper",
		"  Æther   Lamp ":          "aether lamp",
		"Spring‑Loaded — Clamp":    "spring-loaded - clamp",
		"Jötun Rope":               "jotun rope",
		"“Name”":                   `"name"`,
		"Straße":                   "strasse",
		"ＡＢＣ":                      "abc",
		"Iro\u200bn":               "iron",
		"Tab\tand\u00a0spaces\n ": "tab and spaces",
		"":                         "",
	} {
		if got := NameKey(in); got != want {
			t.Errorf("NameKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLookup(t *testing.T) {
	x := fullIndex(t)
	for _, tc := range []struct{ name, want, match string }{
		// exact, any case and spacing
		{"Iron", "element-26", MatchExact},
		{"  iRoN  ", "element-26", MatchExact},
		{"MERCURY", "element-80", MatchExact},
		// aliases: symbols, Latin names, other spellings
		{"Fe", "element-26", MatchAlias},
		{"Ferrum", "element-26", MatchAlias},
		{"Aluminum", "element-13", MatchAlias},
		{"Aluminium", "element-13", MatchExact},
		{"Quicksilver", "element-80", MatchAlias},
		{"Cesium", "element-55", MatchAlias},
		{"Pepper Grinder", "mill", MatchAlias},
		// accents, curly quotes, dashes, ligatures, full width, separators
		{"Creme Brulee Torch", "torch", MatchExact},
		{"Smith’s Anvil", "anvil", MatchExact},
		{"Spring‑Loaded Clamp", "clamp", MatchExact},
		{"Spring–Loaded Clamp", "clamp", MatchExact},
		{"aether lamp", "lamp", MatchExact},
		{"Salt/Pepper Mill", "mill", MatchExact},
		{"salt // pepper mill", "mill", MatchExact},
		{"Salt | Pepper Mill", "mill", MatchExact},
		{"Strassenbahn Model", "tram", MatchExact},
		{"ABC Blocks", "blocks", MatchExact},
		{"Jotun Rope", "rope", MatchExact},
		{`"Quoted" Mug`, "quote", MatchExact},
		// fuzzy: one closest item, ≤ 2 edits, names of ≥ 6 characters
		{"Magnesum", "element-12", MatchFuzzy},
		{"Potasium", "element-19", MatchFuzzy},
		{"Tungstne", "element-74", MatchFuzzy}, // a transposition is one edit
		{"Chromuim", "element-24", MatchFuzzy},
		{"Platnum", "element-78", MatchFuzzy},
		{"Quiksilvr", "element-80", MatchFuzzy}, // fuzzy reaches aliases too
	} {
		r := x.lookup(Query{Name: tc.name})
		if !r.OK || r.Item.Key != tc.want || r.Match != tc.match {
			t.Errorf("Lookup(%q) = %s %q ok=%v reason=%q, want %s %q", tc.name, r.Item.Key, r.Match, r.OK, r.Reason, tc.want, tc.match)
		}
	}
}

func TestLookupUnresolved(t *testing.T) {
	x := fullIndex(t)
	r := x.lookup(Query{Name: "Unobtainium"})
	if r.OK || r.Reason != ReasonUnknown {
		t.Errorf("unknown: %+v", r)
	}
	// a fragment: up to 5 candidates, names starting with it first
	r = x.lookup(Query{Name: "Chrom"})
	if r.OK || r.Reason != ReasonUnknown || len(r.Candidates) == 0 || len(r.Candidates) > 5 || r.Candidates[0].Name != "Chromium" {
		t.Errorf("candidates: %v", names(r.Candidates))
	}
	r = x.lookup(Query{Name: "ium"}) // many contain it: capped at 5
	if r.OK || len(r.Candidates) != 5 {
		t.Errorf("substring candidates: %v", names(r.Candidates))
	}
	// too short to be fuzzy
	if r := x.lookup(Query{Name: "Irn"}); r.OK {
		t.Errorf("Irn resolved to %s", r.Item.Name)
	}
	for _, blank := range []string{"", "  ", "\u200b"} {
		if r := x.lookup(Query{Name: blank}); r.OK || r.Reason != ReasonUnknown || r.Candidates == nil {
			t.Errorf("blank %q: %+v", blank, r)
		}
	}
}

// Fuzzy matching resolves only a unique closest item; a tie stays unresolved
// with the tied items as candidates, and 3 edits never resolve.
func TestLookupFuzzyUnique(t *testing.T) {
	x := testIndex(t, mk(t, "a", "Brightwater", "Maps"), mk(t, "b", "Brightwaker", "Maps"), mk(t, "c", "Thunderclap Valley", "Maps"))
	r := x.lookup(Query{Name: "Brightwaxer"}) // one edit from both
	if r.OK || r.Reason != ReasonUnknown || len(r.Candidates) != 2 {
		t.Errorf("tie: %+v", r)
	}
	r = x.lookup(Query{Name: "Brightwatr"}) // one edit from one, two from the other
	if !r.OK || r.Item.Key != "a" || r.Match != MatchFuzzy {
		t.Errorf("closest: %+v", r)
	}
	if r = x.lookup(Query{Name: "Brgtwatr"}); r.OK { // three edits
		t.Errorf("3 edits resolved: %+v", r)
	}
	if r = x.lookup(Query{Name: "Thundrclp Valley"}); !r.OK || r.Item.Key != "c" {
		t.Errorf("2 edits: %+v", r)
	}
}

// Several items with one name are ambiguous, unless exactly one of them is
// active: a retired item never makes a name ambiguous, and still resolves on
// its own.
func TestLookupAmbiguousAndRetired(t *testing.T) {
	a, b := mk(t, "hex-1", "Hex Key Set", "Tools"), mk(t, "hex-2", "Hex Key Set", "Bikes")
	x := testIndex(t, a, b)
	r := x.lookup(Query{Name: "hex key set"})
	if r.OK || r.Reason != ReasonAmbiguous || len(r.Candidates) != 2 {
		t.Fatalf("ambiguous: %+v", r)
	}
	b.retiredBy, b.retiredAt = RetiredBySource, time.Now()
	x = testIndex(t, a, b)
	if r = x.lookup(Query{Name: "Hex Key Set"}); !r.OK || r.Item.Key != "hex-1" || r.Item.Retired {
		t.Errorf("one active: %+v", r)
	}
	gone := mk(t, "old", "Discontinued Kettle", "Kitchen", "Kettle v1")
	gone.retiredBy, gone.retiredAt = RetiredByStaff, time.Now()
	x = testIndex(t, gone)
	if r = x.lookup(Query{Name: "Kettle v1"}); !r.OK || !r.Item.Retired || r.Item.RetiredBy != RetiredByStaff || r.Item.RetiredAt == nil {
		t.Errorf("a retired item should still resolve: %+v", r)
	}
}

func TestItemConversion(t *testing.T) {
	px := decimal.RequireFromString("12.50")
	r, err := normalize(Item{Key: "k", Name: "Kettle", Attrs: map[string]string{"b": "2", "a": "1"}, RefPrice: &px, Currency: "eur"}, false)
	if err != nil {
		t.Fatal(err)
	}
	it := recItem(r)
	if it.RefPrice == nil || it.RefPrice.String() != "12.5" || it.Currency != "EUR" || it.Attrs["a"] != "1" || it.Aliases == nil || it.Retired {
		t.Errorf("item: %+v", it)
	}
	if r.attrs != `{"a":"1","b":"2"}` {
		t.Errorf("attrs not canonical: %s", r.attrs)
	}
}

func TestDuplicateKeys(t *testing.T) {
	if _, err := buildIndex([]rec{mk(t, "a", "One", ""), mk(t, "a", "Two", "")}, 1); err == nil {
		t.Error("duplicate keys built")
	}
}

func BenchmarkLookupFuzzy(b *testing.B) {
	x := bigIndex(b, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.lookup(Query{Name: "Brass Hinge Modle 04217"})
	}
}

func BenchmarkSearch(b *testing.B) {
	x := bigIndex(b, 100_000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.search("hi", Filter{}, 20)
	}
}

// bigIndex is n generated product names, for benchmarks.
func bigIndex(t testing.TB, n int) *index {
	words := []string{"Brass", "Steel", "Oak", "Hinge", "Bracket", "Model", "Lamp", "Valve", "Cable", "Shelf"}
	recs := make([]rec, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s %s %s %05d", words[i%10], words[(i/10)%10], words[(i/100)%10], i)
		recs = append(recs, rec{key: fmt.Sprintf("p%d", i), name: name, aliases: []string{strings.ToUpper(name[:3]) + fmt.Sprint(i)}})
	}
	return testIndex(t, recs...)
}
