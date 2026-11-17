package catalog

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func searchNames(x *index, q string, f Filter, limit int) []string {
	hits := x.search(q, f, limit)
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, x.recs[h.i].name)
	}
	return out
}

func TestSearchRanking(t *testing.T) {
	x := testIndex(t,
		mk(t, "1", "Subterranean Cavern", "Maps"),
		mk(t, "2", "Cavern of Echoes", "Maps"),
		mk(t, "3", "Uncavernly Box", "Boxes"),
		mk(t, "4", "Deep-Cavern Torch", "Lighting"),
		mk(t, "5", "Grotto Light", "Lighting", "Cavern light"),
		mk(t, "6", "Cavernous Pot", "Kitchen"),
		mk(t, "7", "Cavern Lamp", "Lighting"),
		mk(t, "8", "Hollow", "Maps", "Old cavern"),
	)
	// names starting with the query (name order), then later words, then
	// substrings, then aliases starting with it, then aliases containing it
	want := []string{"Cavern Lamp", "Cavern of Echoes", "Cavernous Pot", "Deep-Cavern Torch", "Subterranean Cavern", "Uncavernly Box", "Grotto Light", "Hollow"}
	if got := searchNames(x, "cavern", Filter{}, 50); !slices.Equal(got, want) {
		t.Fatalf("cavern: %v, want %v", got, want)
	}
	// an exact name comes first even when others start with it
	x = testIndex(t, mk(t, "a", "Lantern Hook", ""), mk(t, "b", "Lantern", ""), mk(t, "c", "Black Lantern", ""))
	if got := searchNames(x, "lantern", Filter{}, 10); !slices.Equal(got, []string{"Lantern", "Lantern Hook", "Black Lantern"}) {
		t.Errorf("exact first: %v", got)
	}
}

// Over the elements, every result list is ordered by rank.
func TestSearchRankOrder(t *testing.T) {
	x := fullIndex(t)
	rank := func(it Item, q string) int {
		k := NameKey(it.Name)
		switch {
		case k == q:
			return 0
		case strings.HasPrefix(k, q):
			return 1
		case wordStart(k, q):
			return 2
		case strings.Contains(k, q):
			return 3
		}
		for _, a := range it.Aliases {
			if strings.HasPrefix(NameKey(a), q) {
				return 4
			}
		}
		return 5
	}
	for _, q := range []string{"on", "ium", "ni", "co", "salt", "er", "mill"} {
		items := make([]Item, 0)
		for _, h := range x.search(q, Filter{}, 50) {
			items = append(items, x.item(h.i))
		}
		if len(items) == 0 {
			t.Errorf("%q: nothing", q)
			continue
		}
		prev := -1
		for _, it := range items {
			r := rank(it, NameKey(q))
			if r < prev {
				t.Errorf("%q: %s (rank %d) after a rank-%d item: %v", q, it.Name, r, prev, names(items))
				break
			}
			prev = r
		}
	}
	// aliases count, after names: only Iron's alias holds "ferr"
	if got := searchNames(x, "ferr", Filter{}, 10); !slices.Equal(got, []string{"Iron"}) {
		t.Errorf("ferr: %v", got)
	}
	// "co": Cobalt and Copper start with it, Silicon… contain it; Co is Cobalt's alias (already listed)
	got := searchNames(x, "co", Filter{}, 50)
	if len(got) < 3 || got[0] != "Cobalt" || got[1] != "Copper" {
		t.Errorf("co: %v", got)
	}
	// accents and full width fold
	if got := searchNames(x, "creme", Filter{}, 5); len(got) != 1 || got[0] != "Crème Brûlée Torch" {
		t.Errorf("creme: %v", got)
	}
	if got := searchNames(x, "abc", Filter{}, 5); len(got) != 1 || got[0] != "ＡＢＣ Blocks" {
		t.Errorf("abc: %v", got)
	}
}

func TestSearchFiltersAndLimits(t *testing.T) {
	recs := devElements(t)
	for i := range recs {
		if recs[i].name == "Uranium" {
			recs[i].retiredBy, recs[i].retiredAt = RetiredBySource, time.Now()
		}
	}
	x := testIndex(t, recs...)
	for _, n := range searchNames(x, "on", Filter{Category: "Noble gas"}, 50) {
		if !slices.Contains([]string{"Neon", "Argon", "Krypton", "Xenon"}, n) {
			t.Errorf("category filter let %s through", n)
		}
	}
	if got := searchNames(x, "uranium", Filter{}, 10); len(got) != 0 {
		t.Errorf("a retired item showed up: %v", got)
	}
	if got := searchNames(x, "uranium", Filter{Retired: true}, 10); !slices.Equal(got, []string{"Uranium"}) {
		t.Errorf("retired included: %v", got)
	}
	if got := x.search("s", Filter{}, 10); got != nil {
		t.Errorf("one-letter query = %v", got)
	}
	var many []rec
	for i := 0; i < 120; i++ {
		many = append(many, mk(t, fmt.Sprintf("w%d", i), fmt.Sprintf("Widget %03d", i), "Widgets"))
	}
	y := testIndex(t, many...)
	if got := y.search("widget", Filter{}, 0); len(got) != 20 {
		t.Errorf("default limit: %d", len(got))
	}
	if got := y.search("widget", Filter{}, 500); len(got) != 50 {
		t.Errorf("limit cap: %d", len(got))
	}
	if got := y.search("idget", Filter{}, 0); len(got) != 20 || y.recs[got[0].i].name != "Widget 000" {
		t.Errorf("substring hits in name order: %d", len(got))
	}
}

func TestBrowse(t *testing.T) {
	recs := devElements(t)
	recs[0].retiredBy, recs[0].retiredAt = RetiredByStaff, time.Now() // Hydrogen
	x := testIndex(t, recs...)
	var all []string
	n, k := "", ""
	for page := 0; page < 20; page++ {
		is := x.browse(Filter{}, n, k, 7)
		if len(is) == 0 {
			break
		}
		for _, i := range is {
			all = append(all, x.recs[i].name)
		}
		last := is[len(is)-1]
		n, k = x.keys[last], x.recs[last].key
	}
	if len(all) != 49 || slices.Contains(all, "Hydrogen") || !slices.IsSorted(all) {
		t.Errorf("browse: %d items %v", len(all), all)
	}
	gases := x.browse(Filter{Category: "Noble gas"}, "", "", 50)
	if len(gases) != 5 {
		t.Errorf("noble gases: %d", len(gases))
	}
	if got := x.browse(Filter{Category: "Noble gas"}, x.keys[gases[2]], x.recs[gases[2]].key, 50); len(got) != 2 {
		t.Errorf("category cursor: %d", len(got))
	}
	cats := x.categories()
	if len(cats) == 0 || cats[0].Name != "Transition metal" || cats[0].Count != 16 {
		t.Errorf("categories: %+v", cats)
	}
	for _, c := range cats {
		if c.Name == "Nonmetal" && c.Count != 6 { // 7 less the retired Hydrogen
			t.Errorf("nonmetals: %d", c.Count)
		}
	}
}
