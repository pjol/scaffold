package catalog

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// The sync's pure part (ported from MOX's sync_test.go): what a pull changes.

func byKey(p plan) map[string]rec {
	out := map[string]rec{}
	for _, r := range p.update {
		out[r.key] = r
	}
	return out
}

func TestDiff(t *testing.T) {
	now := time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)
	before := now.Add(-48 * time.Hour)
	old := func(key, name string, aliases ...string) rec {
		r := mk(t, key, name, "Tools", aliases...)
		r.firstSeen, r.updatedAt = before, before
		return r
	}
	gone := old("gone", "Retired By Source")
	gone.retiredBy, gone.retiredAt = RetiredBySource, before
	held := old("held", "Retired By Staff")
	held.retiredBy, held.retiredAt = RetiredByStaff, before
	cur := testIndex(t,
		old("same", "Claw Hammer"),
		old("edit", "Tape Measure"),
		old("rename", "Spirit Level", "Bubble level"),
		old("drop", "Hand Saw"),
		gone, held,
	)
	pull := []rec{
		mk(t, "same", "Claw Hammer", "Tools"),
		mk(t, "edit", "Tape Measure", "Measuring"),            // category changed
		mk(t, "rename", "Torpedo Level", "Tools", "Box level"), // renamed, new alias
		mk(t, "gone", "Retired By Source", "Tools"),            // back in the source
		mk(t, "held", "Retired By Staff (2026)", "Tools"),      // staff retired: stays retired, content follows
		mk(t, "new", "Pipe Wrench", "Tools"),
	}
	p := diff(cur, pull, now)
	if len(p.insert) != 1 || p.insert[0].key != "new" || !p.insert[0].firstSeen.Equal(now) {
		t.Errorf("insert: %+v", p.insert)
	}
	if !slices.Equal(p.retire, []string{"drop"}) {
		t.Errorf("retire: %v", p.retire)
	}
	if !slices.Equal(p.restore, []string{"gone"}) {
		t.Errorf("restore: %v", p.restore)
	}
	if p.updated != 3 || p.unchanged != 1 || p.active != 4 {
		t.Errorf("updated %d unchanged %d active %d", p.updated, p.unchanged, p.active)
	}
	up := byKey(p)
	if r := up["rename"]; r.name != "Torpedo Level" || !slices.Equal(r.aliases, []string{"Bubble level", "Spirit Level", "Box level"}) || !r.firstSeen.Equal(before) || !r.updatedAt.Equal(now) {
		t.Errorf("rename keeps the old name and aliases: %+v", r)
	}
	if r := up["edit"]; r.category != "Measuring" {
		t.Errorf("edit: %+v", r)
	}
	if r, ok := up["gone"]; !ok || !r.active() {
		t.Errorf("restored row: %+v", r)
	}
	if r := up["held"]; r.retiredBy != RetiredByStaff || r.name != "Retired By Staff (2026)" {
		t.Errorf("staff retired: %+v", r)
	}
	if _, ok := up["same"]; ok {
		t.Error("an unchanged item was rewritten")
	}
	// aliases only grow: a pull without them keeps them
	p = diff(testIndex(t, old("k", "Spirit Level", "Bubble level")), []rec{mk(t, "k", "Spirit Level", "Tools")}, now)
	if p.unchanged != 1 || len(p.update) != 0 {
		t.Errorf("dropping an alias changed the item: %+v", p)
	}
	// an alias that becomes the name stops being an alias
	p = diff(testIndex(t, old("k", "Spirit Level", "Bubble level")), []rec{mk(t, "k", "Bubble Level", "Tools")}, now)
	if r := byKey(p)["k"]; !slices.Equal(r.aliases, []string{"Spirit Level"}) {
		t.Errorf("aliases after renaming to an alias: %v", r.aliases)
	}
	// a staff-retired item the source drops stays as it is; a source-retired one too
	p = diff(testIndex(t, held, gone), nil, now)
	if len(p.retire) != 0 || len(p.update) != 0 {
		t.Errorf("already retired items changed: %+v", p)
	}
}

func TestSafetyValve(t *testing.T) {
	var recs []rec
	for i := 0; i < 200; i++ {
		recs = append(recs, mk(t, fmt.Sprintf("k%d", i), fmt.Sprintf("Item %d", i), ""))
	}
	cur := testIndex(t, recs...)
	keep := func(n int) []rec { return slices.Clone(recs[:n]) }
	if why := diff(cur, keep(120), time.Now()).refusal(120, 40); why != "" { // 80 of 200 = 40%: allowed
		t.Errorf("40%% refused: %s", why)
	}
	why := diff(cur, keep(119), time.Now()).refusal(119, 40) // 81 of 200
	if !strings.Contains(why, "81 of 200 active items (40.5%)") || !strings.Contains(why, "Nothing changed") {
		t.Errorf("41%% allowed: %q", why)
	}
	if why := diff(cur, nil, time.Now()).refusal(0, 100); !strings.Contains(why, "no usable items") {
		t.Errorf("an empty pull must always be refused: %q", why)
	}
	// small catalogs aren't guarded by the share, but an empty pull still is
	small := testIndex(t, recs[:10]...)
	if why := diff(small, keep(1), time.Now()).refusal(1, 40); why != "" {
		t.Errorf("small catalog refused: %s", why)
	}
	if why := diff(small, nil, time.Now()).refusal(0, 40); why == "" {
		t.Error("small catalog emptied")
	}
	if why := diff(cur, keep(1), time.Now()).refusal(1, 100); why != "" {
		t.Errorf("a 100%% valve should let 199 of 200 go: %s", why)
	}
}

func TestNormalize(t *testing.T) {
	px := decimal.RequireFromString("3")
	neg := decimal.RequireFromString("-1")
	ok := Item{Key: "sku-1", Name: "  Pipe   Wrench ", Category: " Tools ", Aliases: []string{"pipe wrench", " Stillson  ", "STILLSON", ""}}
	r, err := normalize(ok, false)
	if err != nil || r.name != "Pipe Wrench" || r.category != "Tools" || !slices.Equal(r.aliases, []string{"Stillson"}) {
		t.Fatalf("normalize: %+v %v", r, err)
	}
	for _, tc := range []struct {
		it   Item
		want string
	}{
		{Item{Name: "x"}, "key is required"},
		{Item{Key: "a b", Name: "x"}, "key"},
		{Item{Key: "a/b", Name: "x"}, "key"},
		{Item{Key: "-a", Name: "x"}, "key"},
		{Item{Key: "a", Name: "  "}, "name is required"},
		{Item{Key: "a", Name: "\u200b"}, "name is required"},
		{Item{Key: "a", Name: strings.Repeat("n", 301)}, "name is over"},
		{Item{Key: "a", Name: "x", ImageURL: "http://img.example.com/a.png"}, "https"},
		{Item{Key: "a", Name: "x", ImageURL: "javascript:alert(1)"}, "https"},
		{Item{Key: "a", Name: "x", RefPrice: &px}, "currency"},
		{Item{Key: "a", Name: "x", RefPrice: &px, Currency: "dollars"}, "currency"},
		{Item{Key: "a", Name: "x", RefPrice: &neg, Currency: "USD"}, "negative"},
		{Item{Key: "a", Name: "x", Attrs: map[string]string{" ": "v"}}, "attribute"},
	} {
		if _, err := normalize(tc.it, false); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("normalize(%+v) = %v, want an error about %q", tc.it, err, tc.want)
		}
	}
	if _, err := normalize(Item{Key: "a", Name: "x", ImageURL: "http://localhost:8080/a.png"}, true); err != nil {
		t.Errorf("http images in development: %v", err)
	}
	if r, err := normalize(Item{Key: "a", Name: "x", Currency: "USD"}, false); err != nil || r.currency != "" {
		t.Errorf("a currency without a price is dropped: %+v %v", r, err)
	}
}

func decodeAll(t *testing.T, body []byte) (items []Item, bad []string, err error) {
	t.Helper()
	err = DecodeItems(bytes.NewReader(body), func(it Item) error { items = append(items, it); return nil },
		func(e error) error { bad = append(bad, e.Error()); return nil })
	return
}

func TestDecodeItems(t *testing.T) {
	arr := []byte(` [ {"key":"a","name":"A","ref_price":12.5,"currency":"USD","attrs":{"n":7,"ok":true,"s":"x","none":null}},
	  {"key":"b","name":"B","ref_price":"0.10","currency":"EUR"} ]`)
	items, bad, err := decodeAll(t, arr)
	if err != nil || len(items) != 2 || len(bad) != 0 {
		t.Fatalf("array: %v %v %v", items, bad, err)
	}
	if a := items[0]; a.RefPrice.String() != "12.5" || a.Attrs["n"] != "7" || a.Attrs["ok"] != "true" || a.Attrs["s"] != "x" || len(a.Attrs) != 3 {
		t.Errorf("array item: %+v", a)
	}
	nd := []byte("\ufeff{\"key\":\"a\",\"name\":\"A\"}\n{\"key\":\"b\",\"name\":7}\n\n{\"key\":\"c\",\"name\":\"C\",\"attrs\":{\"deep\":{\"x\":1}}}\n{\"key\":\"d\",\"name\":\"D\"}\n")
	items, bad, err = decodeAll(t, nd)
	if err != nil || len(items) != 2 || len(bad) != 2 || items[1].Key != "d" {
		t.Fatalf("ndjson: %v %v %v", items, bad, err)
	}
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	_, _ = zw.Write(nd)
	_ = zw.Close()
	if items, _, err = decodeAll(t, z.Bytes()); err != nil || len(items) != 2 {
		t.Errorf("gzip: %v %v", items, err)
	}
	if items, _, err = decodeAll(t, []byte("  \n")); err != nil || len(items) != 0 {
		t.Errorf("empty: %v %v", items, err)
	}
	if _, _, err = decodeAll(t, []byte(`{"key":"a","name":"A"}{"key":`)); err == nil {
		t.Error("truncated NDJSON decoded")
	}
	if _, _, err = decodeAll(t, []byte(`"not items"`)); err == nil {
		t.Error("a JSON string decoded")
	}
	stop := errors.New("stop")
	if err := DecodeItems(bytes.NewReader(arr), func(Item) error { return stop }, nil); !errors.Is(err, stop) {
		t.Errorf("emit's error should stop decoding: %v", err)
	}
}
