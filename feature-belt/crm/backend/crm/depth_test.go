package crm

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"scaffoldapp/backend/internal/app"
)

// Line money in minor units: each step rounds half away from zero, and
// totals are sums of the rounded lines.
func TestLineTotals(t *testing.T) {
	cases := []struct {
		name string
		in   lineInput
		want LineTotals
	}{
		{"tax rounds up at .75", lineInput{UnitPrice: 1999, QuantityMilli: 3000, TaxBP: 825},
			LineTotals{Subtotal: 5997, Tax: 495, Total: 6492, ContractValue: 6492}},
		{"percent discount then tax", lineInput{UnitPrice: 1999, QuantityMilli: 1000, DiscountBP: 1000, TaxBP: 825},
			LineTotals{Subtotal: 1999, Discount: 200, Tax: 148, Total: 1947, ContractValue: 1947}},
		{"fractional quantity, half rounds up", lineInput{UnitPrice: 333, QuantityMilli: 1500},
			LineTotals{Subtotal: 500, Total: 500, ContractValue: 500}},
		{"exact half", lineInput{UnitPrice: 1, QuantityMilli: 500},
			LineTotals{Subtotal: 1, Total: 1, ContractValue: 1}},
		{"below half rounds down", lineInput{UnitPrice: 1, QuantityMilli: 499},
			LineTotals{Subtotal: 0, Total: 0, ContractValue: 0}},
		{"fixed discount is capped at the subtotal", lineInput{UnitPrice: 1000, QuantityMilli: 1000, DiscountAmount: 5000, TaxBP: 2000},
			LineTotals{Subtotal: 1000, Discount: 1000, Tax: 0, Total: 0, ContractValue: 0}},
		{"recurring: contract value is total × term", lineInput{UnitPrice: 5000, QuantityMilli: 2000, TaxBP: 1000, Billing: "recurring", BillingPeriod: "monthly", TermPeriods: 12},
			LineTotals{Subtotal: 10000, Tax: 1000, Total: 11000, ContractValue: 132000}},
		{"one-time ignores a term", lineInput{UnitPrice: 700, QuantityMilli: 1000, Billing: "one_time", TermPeriods: 12},
			LineTotals{Subtotal: 700, Total: 700, ContractValue: 700}},
		{"zero-decimal currency amounts are still minor units", lineInput{UnitPrice: 12345, QuantityMilli: 2500, DiscountBP: 333},
			LineTotals{Subtotal: 30863, Discount: 1028, Total: 29835, ContractValue: 29835}},
	}
	for _, c := range cases {
		got, ok := c.in.totals()
		if !ok || got != c.want {
			t.Errorf("%s: totals = %+v (ok %v), want %+v", c.name, got, ok, c.want)
		}
	}
	if _, ok := (lineInput{UnitPrice: maxMoney, QuantityMilli: maxQuantity}).totals(); ok {
		t.Error("an overflowing line was accepted")
	}
}

func TestSumLines(t *testing.T) {
	lines := []lineInput{
		{UnitPrice: 1999, QuantityMilli: 3000, TaxBP: 825},                                                       // 6492
		{UnitPrice: 5000, QuantityMilli: 1000, Billing: "recurring", BillingPeriod: "monthly", TermPeriods: 12}, // 5000/mo, 60000
		{UnitPrice: 120000, QuantityMilli: 1000, Billing: "recurring", BillingPeriod: "annual", TermPeriods: 2}, // 120000/yr, 240000
	}
	tot, each, ok := sumLines(lines)
	if !ok {
		t.Fatal("sumLines refused")
	}
	var sum int64
	for _, l := range each {
		sum += l.Total
	}
	if tot.Total != sum || tot.Total != 6492+5000+120000 {
		t.Errorf("total = %d, sum of lines %d", tot.Total, sum)
	}
	if tot.OneTime != 6492 || tot.ContractValue != 6492+60000+240000 || tot.Tax != 495 {
		t.Errorf("totals = %+v", tot)
	}
	if len(tot.Recurring) != 2 || tot.Recurring[0] != (RecurringTotal{"monthly", 5000}) || tot.Recurring[1] != (RecurringTotal{"annual", 120000}) {
		t.Errorf("recurring = %+v", tot.Recurring)
	}
}

func TestParseQuantity(t *testing.T) {
	ok := map[string]int64{"1": 1000, "1.5": 1500, "0.001": 1, ".5": 500, "2.25": 2250, "1000000": 1_000_000_000}
	for in, want := range ok {
		if got, good := parseQuantity(in); !good || got != want {
			t.Errorf("parseQuantity(%q) = %d, %v; want %d", in, got, good, want)
		}
	}
	for _, bad := range []string{"", "0", "-1", "+1", "1.2345", "abc", "1000001", "1e3", "0.000"} {
		if got, good := parseQuantity(bad); good {
			t.Errorf("parseQuantity(%q) = %d, accepted", bad, got)
		}
	}
	if formatQuantity(1500) != "1.5" || formatQuantity(2000) != "2" || formatQuantity(1025) != "1.025" {
		t.Errorf("formatQuantity: %s %s %s", formatQuantity(1500), formatQuantity(2000), formatQuantity(1025))
	}
	if formatPercent(825) != "8.25 %" || formatPercent(1000) != "10 %" || formatPercent(50) != "0.5 %" {
		t.Errorf("formatPercent: %s %s %s", formatPercent(825), formatPercent(1000), formatPercent(50))
	}
}

func TestRenderMerge(t *testing.T) {
	vals := map[string]string{"contact.first_name": "Jane", "company.name": "Acme", "sender.name": "Sam Lee", "deal.amount": ""}
	cases := []struct {
		in, want string
		missing  []string
	}{
		{"Hi {{contact.first_name}},", "Hi Jane,", nil},
		{"Hi {{ contact.first_name | there }},", "Hi Jane,", nil},
		{"Hi {{contact.last_name | there}},", "Hi there,", nil},
		{"About {{deal.amount}} for {{company.name}}", "About  for Acme", []string{"deal.amount"}},
		{"{{unknown.field}} and {{contact.first_name}}", " and Jane", []string{"unknown.field"}},
		{"{{nonsense}}", "", []string{"nonsense"}},
		{"No fields {here}", "No fields {here}", nil},
		{"— {{sender.name}}", "— Sam Lee", nil},
	}
	for _, c := range cases {
		got, missing := renderMerge(c.in, vals)
		if got != c.want || strings.Join(missing, ",") != strings.Join(c.missing, ",") {
			t.Errorf("renderMerge(%q) = %q %v; want %q %v", c.in, got, missing, c.want, c.missing)
		}
	}
	if bad := unknownMergeFields("{{contact.name}} {{deal.nope}} {{deal.nope}} {{booking_link}}"); strings.Join(bad, ",") != "deal.nope" {
		t.Errorf("unknownMergeFields = %v", bad)
	}
}

func TestWebhookSignature(t *testing.T) {
	body := []byte(`{"a":1}`)
	m := hmac.New(sha256.New, []byte("whsec_test"))
	m.Write([]byte("1700000000." + string(body)))
	if want := hex.EncodeToString(m.Sum(nil)); signWebhook("whsec_test", "1700000000", body) != want {
		t.Error("signature doesn't match HMAC-SHA256 of t.body")
	}
	if signWebhook("other", "1700000000", body) == signWebhook("whsec_test", "1700000000", body) {
		t.Error("the secret doesn't change the signature")
	}
}

func TestTriggerMatches(t *testing.T) {
	ev := recordEvent{kind: "updated", changes: []change{{Key: "amount"}}}
	if !(WorkflowTrigger{Type: "property_changed", Property: "amount"}).matches(ev) {
		t.Error("property_changed amount should match")
	}
	if (WorkflowTrigger{Type: "property_changed", Property: "name"}).matches(ev) {
		t.Error("property_changed name matched an amount change")
	}
	if (WorkflowTrigger{Type: "created"}).matches(ev) || !(WorkflowTrigger{Type: "created"}).matches(recordEvent{kind: "created"}) {
		t.Error("created trigger")
	}
	if (WorkflowTrigger{Type: "stage_changed"}).matches(ev) {
		t.Error("stage_changed matched without a stage move")
	}
}

func TestRFC822(t *testing.T) {
	raw, err := rfc822("sam@acme.test", outMail{To: []string{"jane@example.com"}, CC: []string{"b@example.com"},
		Subject: "Quote\r\nBcc: evil@x.test — Café", Body: "Hello\nThere"})
	if err != nil {
		t.Fatal(err)
	}
	msg := string(raw)
	head, body, _ := strings.Cut(msg, "\r\n\r\n")
	if strings.Contains(head, "\r\nBcc:") {
		t.Fatalf("header injection: %q", head)
	}
	if !strings.Contains(head, "From: sam@acme.test\r\n") || !strings.Contains(head, "Cc: b@example.com\r\n") || !strings.Contains(head, "=?utf-8?q?") {
		t.Errorf("headers: %q", head)
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil || string(dec) != "Hello\r\nThere" {
		t.Errorf("body = %q, %v", dec, err)
	}
	if raw, _ := rfc822("Acme team", outMail{To: []string{"j@x.test"}, Subject: "s", Body: "b"}); strings.Contains(string(raw), "From:") {
		t.Error("a non-address account became a From header")
	}
}

func TestMailHelpers(t *testing.T) {
	if got := htmlToText("<p>Hi&nbsp;Jane,</p><script>x()</script><div>Thanks<br>Sam</div>"); got != "Hi Jane,\nThanks\nSam" {
		t.Errorf("htmlToText = %q", got)
	}
	if got := addrOf(`"Jane Doe" <Jane@Example.com>`); got != "jane@example.com" {
		t.Errorf("addrOf = %q", got)
	}
	if got := addrList("a@x.test, B <b@y.test>"); strings.Join(got, ",") != "a@x.test,b@y.test" {
		t.Errorf("addrList = %v", got)
	}
	if _, err := validAddresses([]string{"ok@x.test", "not an address"}, "To"); err == nil {
		t.Error("validAddresses accepted a bad address")
	}
	prod := app.Connection{Endpoints: map[string]string{"drive": "https://www.googleapis.com/drive/v3"}}
	dev := app.Connection{Endpoints: map[string]string{"drive": "http://127.0.0.1:9000/g/www.googleapis.com/drive/v3"}}
	if gmailBase(prod) != "https://gmail.googleapis.com/gmail/v1" || gmailBase(dev) != "http://127.0.0.1:9000/g/gmail.googleapis.com/gmail/v1" {
		t.Errorf("gmailBase: %s %s", gmailBase(prod), gmailBase(dev))
	}
	if gmailBase(app.Connection{Endpoints: map[string]string{"gmail": "https://x.test/gmail/v1/"}}) != "https://x.test/gmail/v1" {
		t.Error("an explicit gmail endpoint should win")
	}
}

func TestBucketLabel(t *testing.T) {
	for in, want := range map[[2]string]string{{"2026-10-01", "quarter"}: "Q4 2026", {"2026-11-01", "month"}: "Nov 2026", {"2026-01-01", "year"}: "2026",
		{"2026-11-02", "week"}: "Week of 2 Nov 2026"} {
		if got := bucketLabel(in[0], in[1]); got != want {
			t.Errorf("bucketLabel(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactRun(t *testing.T) {
	run := &WorkflowRun{Record: RecordRef{Name: "Secret deal"}, Log: []WorkflowRunStep{
		{Type: "create_task", Status: "done", Message: `Created the task "Call about Secret deal".`},
		{Type: "webhook", Status: "failed", Message: "Webhook answered 500 for Secret deal."},
		{Type: "unknown", Status: "skipped", Message: "Secret deal"},
	}}
	redactRun(run)
	want := []string{"Create a task · done", "Send to a webhook · failed", "Step · skipped"}
	for i, l := range run.Log {
		if l.Message != want[i] {
			t.Errorf("step %d: %q, want %q", i, l.Message, want[i])
		}
	}
	if run.Record.Name != "" {
		t.Errorf("name kept: %q", run.Record.Name)
	}
}

// Every tool has a unique name the assistant accepts, an object schema, and
// every write names its target and previews itself.
func TestToolSchemas(t *testing.T) {
	seen := map[string]bool{}
	for _, tool := range (&Service{}).Tools() {
		if seen[tool.Name] {
			t.Errorf("duplicate tool %s", tool.Name)
		}
		seen[tool.Name] = true
		for _, w := range []string{"delete", "send", "email", "fetch", "browse"} {
			if strings.Contains(tool.Name, w) {
				t.Errorf("%s: the assistant refuses names containing %q", tool.Name, w)
			}
		}
		var schema struct {
			Type       string         `json:"type"`
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema.Type != "object" {
			t.Errorf("%s: schema is not a JSON object schema (%v)", tool.Name, err)
		}
		if !tool.ReadOnly && (tool.Target == nil || tool.Preview == nil) {
			t.Errorf("%s: a write tool needs Target and Preview", tool.Name)
		}
	}
	for _, name := range []string{"crm_get_ticket", "crm_create_ticket", "crm_add_line_item", "crm_draft_quote", "crm_draft_mail", "crm_run_report", "crm_forecast"} {
		if !seen[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}
