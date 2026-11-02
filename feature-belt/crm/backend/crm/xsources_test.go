package crm

import (
	"encoding/json"
	"testing"
	"time"
)

func TestXRef(t *testing.T) {
	id := xref("11111111-1111-4111-8111-000000000001", "1003")
	c, p, ok := parseXRef(id)
	if !ok || c != "11111111-1111-4111-8111-000000000001" || p != "1003" {
		t.Fatalf("parseXRef(%q) = %q, %q, %v", id, c, p, ok)
	}
	for _, bad := range []string{"", "~1", "abc~", "no-tilde", "a~b/c", "a~b?c", "a~b%2F"} {
		if _, _, ok := parseXRef(bad); ok {
			t.Errorf("parseXRef(%q) accepted", bad)
		}
	}
	if got := agentTargetID("c1", objDeal, "3003"); got != "c1:deal:3003" {
		t.Errorf("agentTargetID = %q", got)
	}
}

func TestXTargetEmptyInput(t *testing.T) {
	kind, id := xTarget(json.RawMessage(`{}`))
	if kind != KindExternal || id != "" {
		t.Fatalf("xTarget({}) = %q, %q", kind, id)
	}
	kind, id = xTarget(json.RawMessage(`{"deal_id":"c1~3003","stage_id":"x"}`))
	if kind != KindExternal || id != "c1:deal:3003" {
		t.Fatalf("xTarget(deal) = %q, %q", kind, id)
	}
	kind, id = xTarget(json.RawMessage(`{"record_type":"company","record_id":"c1~1003"}`))
	if kind != KindExternal || id != "c1:company:1003" {
		t.Fatalf("xTarget(company) = %q, %q", kind, id)
	}
}

func TestAccessFieldsRefused(t *testing.T) {
	for _, c := range []struct{ provider, key string }{
		{"hubspot", "owner"}, {"hubspot", "hubspot_owner_id"}, {"hubspot", "hs_all_accessible_team_ids"}, {"hubspot", "team"},
		{"salesforce", "OwnerId"}, {"salesforce", "owner"},
	} {
		if !xAccessField(c.provider, c.key) {
			t.Errorf("%s %s should be refused to agents", c.provider, c.key)
		}
	}
	if xAccessField("hubspot", "amount") {
		t.Error("amount refused")
	}
}

func TestMergeKeyOrder(t *testing.T) {
	cheap, dear := int64(100), int64(-50)
	a := &XDeal{XRecord: XRecord{Title: "b", CreatedAt: time.Unix(10, 0)}, Amount: &cheap}
	b := &Deal{Record: Record{Title: "A", CreatedAt: time.Unix(20, 0)}, Amount: &dear}
	if ka, _ := mergeKey(a, "name"); ka != "b" {
		t.Errorf("name key = %q", ka)
	}
	ka, _ := mergeKey(a, "amount")
	kb, _ := mergeKey(b, "amount")
	if !(kb < ka) {
		t.Errorf("negative amount should order first: %q vs %q", kb, ka)
	}
	ka, _ = mergeKey(a, "created_at")
	kb, _ = mergeKey(b, "created_at")
	if !(ka < kb) {
		t.Errorf("created_at order: %q vs %q", ka, kb)
	}
	if _, null := mergeKey(&XDeal{}, "close_date"); !null {
		t.Error("missing close date should sort as null")
	}
}

func TestSOQLQuoting(t *testing.T) {
	if got := soqlString(`O'Brien \ "x"`); got != `'O\'Brien \\ \"x\"'` {
		t.Errorf("soqlString = %s", got)
	}
	if got := soqlLike(`50%_off`); got != `'%50\%\_off%'` {
		t.Errorf("soqlLike = %s", got)
	}
	for _, f := range []string{"Name", "Account.Name", "Segment__c"} {
		if !sfFieldRe.MatchString(f) {
			t.Errorf("%s rejected", f)
		}
	}
	for _, f := range []string{"Name, Id", "Name;", "a.b.c", "1x"} {
		if sfFieldRe.MatchString(f) {
			t.Errorf("%q accepted as a field", f)
		}
	}
}

func TestPlatformConversions(t *testing.T) {
	if got := moneyMinor(1234.565, "USD"); got != 123457 {
		t.Errorf("moneyMinor = %d", got)
	}
	if got := moneyMinor(500, "JPY"); got != 500 {
		t.Errorf("moneyMinor JPY = %d", got)
	}
	if got := htmlText("<p>Kickoff <b>went</b> well &amp; fast</p><p>Next</p>"); got != "Kickoff went well & fast\nNext" {
		t.Errorf("htmlText = %q", got)
	}
	if hsLifecycle("marketingqualifiedlead") != "mql" || hsLifecycleOut("sql") != "salesqualifiedlead" || hsLifecycle("customer") != "customer" {
		t.Error("lifecycle mapping")
	}
	if tm, ok := parseTime("1767225600000"); !ok || tm.Year() != 2026 {
		t.Errorf("epoch ms parse = %v %v", tm, ok)
	}
	if d := dateOnly("2026-12-15T00:00:00Z"); d == nil || *d != "2026-12-15" {
		t.Errorf("dateOnly = %v", d)
	}
}

func TestTaskBucket(t *testing.T) {
	loc := time.UTC
	now := time.Now().UTC()
	past, soon, later := now.Add(-48*time.Hour), time.Date(now.Year(), now.Month(), now.Day(), 23, 0, 0, 0, loc), now.Add(72*time.Hour)
	if b := taskBucket(&XActivity{DueAt: &past}, loc); b != "overdue" {
		t.Errorf("past = %s", b)
	}
	if b := taskBucket(&XActivity{DueAt: &soon}, loc); b != "today" {
		t.Errorf("today = %s", b)
	}
	if b := taskBucket(&XActivity{DueAt: &later}, loc); b != "upcoming" {
		t.Errorf("later = %s", b)
	}
	if b := taskBucket(&XActivity{DueAt: &later, DoneAt: &past}, loc); b != "done" {
		t.Errorf("done = %s", b)
	}
}
