package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// Database tests: a throwaway database on TEST_DATABASE_URL (a role that may
// CREATE DATABASE) with every migration applied, shared by the tests below.

type sentNotes struct {
	mu   sync.Mutex
	list []notify.Notification
}

func (n *sentNotes) Send(_ context.Context, x notify.Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.list = append(n.list, x)
	return nil
}

var (
	dbOnce sync.Once
	dbPool *pgxpool.Pool
	dbSvc  *Service
	dbErr  error
)

func testService(t *testing.T) *Service {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set: a Postgres URL whose role can create databases")
	}
	dbOnce.Do(func() {
		ctx := context.Background()
		admin, err := pgx.Connect(ctx, base)
		if err != nil {
			dbErr = err
			return
		}
		defer admin.Close(ctx)
		name := fmt.Sprintf("crm_test_%d", time.Now().UnixNano())
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
			dbErr = err
			return
		}
		cfg, err := pgxpool.ParseConfig(base)
		if err != nil {
			dbErr = err
			return
		}
		cfg.ConnConfig.Database = name
		if dbPool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
			dbErr = err
			return
		}
		if err := db.Migrate(ctx, dbPool, false); err != nil {
			dbErr = err
			return
		}
		deps := &app.Deps{Pool: dbPool, Cfg: &config.Config{Env: "development", PublicWebURL: "http://web.test"}, Broker: realtime.NewMemory(), Notify: &sentNotes{}}
		access.New(deps)
		dbSvc = New(deps)
		dbSvc.inline = true
	})
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	return dbSvc
}

type world struct {
	t        *testing.T
	s        *Service
	ctx      context.Context
	org      uuid.UUID
	ann, bob uuid.UUID // ann owns the org; bob is a member
}

// newWorld makes an org whose records only their owner (and admins) open:
// bob can't see ann's records.
func newWorld(t *testing.T) *world {
	s := testService(t)
	ctx := context.Background()
	w := &world{t: t, s: s, ctx: ctx}
	user := func(name string) uuid.UUID {
		var id uuid.UUID
		sub := fmt.Sprintf("test_%s_%d", name, time.Now().UnixNano())
		w.must(s.d.Pool.QueryRow(ctx, `INSERT INTO users (auth_subject, email, display_name, status) VALUES ($1, $1 || '@example.test', $2, 'active') RETURNING id`,
			sub, strings.ToUpper(name[:1])+name[1:]).Scan(&id))
		return id
	}
	w.ann, w.bob = user("ann"), user("bob")
	slug := fmt.Sprintf("crm-test-%d", time.Now().UnixNano())
	w.must(s.d.Pool.QueryRow(ctx, `INSERT INTO orgs (name, slug) VALUES ('Northwind', $1) RETURNING id`, slug).Scan(&w.org))
	_, err := s.d.Pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner'), ($1, $3, 'member')`, w.org, w.ann, w.bob)
	w.must(err)
	w.must(s.bootstrap(ctx, w.org))
	w.must(db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := applyRecordAccess(ctx, tx, w.org, AccessOwnersTeams); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE crm_settings SET record_access = 'owners_teams' WHERE org_id = $1`, w.org)
		return err
	}))
	return w
}

func (w *world) must(err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func (w *world) deal(owner uuid.UUID, name string, amount int64, close string) uuid.UUID {
	w.t.Helper()
	rec, err := w.s.create(w.ctx, owner, w.org, objDeal, map[string]json.RawMessage{"name": raw(name), "amount": raw(amount), "close_date": raw(close)}, false)
	w.must(err)
	return rec.base().ID
}

// Reports, the forecast and goals count only what the viewer can open.
func TestReportsFollowAccess(t *testing.T) {
	w := newWorld(t)
	month := time.Now().UTC().Format("2006-01")
	close := month + "-15"
	w.deal(w.ann, "Alpha", 100000, close)
	w.deal(w.ann, "Beta", 250050, close)
	w.deal(w.ann, "Gamma", 50000, close)

	spec := ReportSpec{Object: "deal", Measure: "sum", Property: "amount", GroupBy: "stage"}
	mine, err := w.s.runReport(w.ctx, w.ann, w.org, spec, "UTC")
	w.must(err)
	if mine.Count != 3 || mine.Total != 400050 || mine.ValueType != "money" || len(mine.Rows) != 1 || mine.Rows[0].Label != "Qualified" {
		t.Fatalf("ann's report = %+v", mine)
	}
	theirs, err := w.s.runReport(w.ctx, w.bob, w.org, spec, "UTC")
	w.must(err)
	if theirs.Count != 0 || theirs.Total != 0 || len(theirs.Rows) != 0 {
		t.Fatalf("bob sees deals he can't open: %+v", theirs)
	}
	count, err := w.s.runReport(w.ctx, w.ann, w.org, ReportSpec{Object: "deal", GroupBy: "close_date", Bucket: "month"}, "UTC")
	w.must(err)
	if len(count.Rows) != 1 || count.Rows[0].Value != 3 || count.Rows[0].Key != month+"-01" {
		t.Errorf("count by month = %+v", count.Rows)
	}
	if _, err := w.s.runReport(w.ctx, w.ann, w.org, ReportSpec{Object: "deal", Measure: "sum", Property: "name"}, "UTC"); err == nil {
		t.Error("summing a text property was accepted")
	}

	from, _ := monthStart(month)
	f, err := w.s.forecast(w.ctx, w.ann, w.org, from, 3, nil, "", nil)
	w.must(err)
	if f.Total.Count != 3 || f.Total.Pipeline != 400050 || f.Total.Weighted != 40005 || len(f.Rows) != 1 || f.Rows[0].Owner == nil {
		t.Fatalf("ann's forecast = %+v", f.Total)
	}
	fb, err := w.s.forecast(w.ctx, w.bob, w.org, from, 3, nil, "", nil)
	w.must(err)
	if fb.Total.Count != 0 || len(fb.Rows) != 0 {
		t.Fatalf("bob's forecast shows hidden deals: %+v", fb)
	}

	g := &Goal{Metric: "deals_created", User: &UserRef{ID: w.ann}, Period: "month", PeriodStart: month + "-01"}
	w.must(w.s.goalProgress(w.ctx, w.bob, w.org, g))
	if g.Progress != 0 {
		t.Errorf("bob's view of ann's goal counts %d hidden deals", g.Progress)
	}
	w.must(w.s.goalProgress(w.ctx, w.ann, w.org, g))
	if g.Progress != 3 {
		t.Errorf("ann's goal progress = %d, want 3", g.Progress)
	}
}

// Line items, quote totals, publishing and the public page: accept once,
// and an expired link answers 410.
func TestQuoteFlow(t *testing.T) {
	w := newWorld(t)
	dealID := w.deal(w.ann, "Renewal", 0, time.Now().UTC().Format("2006-01-02"))
	_, err := w.s.d.Pool.Exec(w.ctx, `UPDATE crm_deals SET amount_from_items = true WHERE id = $1`, dealID)
	w.must(err)
	var productID uuid.UUID
	w.must(w.s.d.Pool.QueryRow(w.ctx, `INSERT INTO crm_products (org_id, name, unit_price, currency, tax_bp) VALUES ($1, 'Seat', 1999, 'USD', 825) RETURNING id`, w.org).Scan(&productID))
	if _, err := w.s.createLineItem(w.ctx, w.ann, dealID, lineItemReq{ProductID: &productID, Quantity: raw(3)}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.s.createLineItem(w.ctx, w.bob, dealID, lineItemReq{ProductID: &productID}); err == nil {
		t.Fatal("bob added a line to a deal he can't open")
	}
	var amount int64
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT amount FROM crm_deals WHERE id = $1`, dealID).Scan(&amount))
	if amount != 6492 {
		t.Fatalf("deal amount = %d, want 6492 (3 × 19.99 + 8.25 %% tax)", amount)
	}
	if _, err := w.s.update(w.ctx, w.ann, objDeal, dealID, map[string]json.RawMessage{"amount": raw(1)}); err == nil {
		t.Error("typing an amount was accepted while it follows the line items")
	}

	qt, err := w.s.draftQuote(w.ctx, w.ann, dealID, quoteReq{})
	w.must(err)
	if qt.Status != QuoteDraft || qt.Totals.Total != 6492 || len(qt.Items) != 1 || qt.Number == "" || qt.URL != nil {
		t.Fatalf("draft = %+v", qt)
	}
	if lvl, _ := access.LevelOf(w.ctx, w.s.d.Pool, w.bob, KindQuote, qt.ID); lvl != access.None {
		t.Fatalf("bob can open the quote (%s) though he can't open its deal", lvl)
	}

	call := func(handler func(http.ResponseWriter, *http.Request) error, method, path string, params map[string]string, body string, as *uuid.UUID) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		rc := chi.NewRouteContext()
		for k, v := range params {
			rc.URLParams.Add(k, v)
		}
		ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rc)
		if as != nil {
			ctx = auth.WithIdentity(ctx, &auth.Identity{UserID: *as, Status: "active"})
		}
		rec := httptest.NewRecorder()
		if err := handler(rec, r.WithContext(ctx)); err != nil {
			http.Error(rec, err.Error(), statusOf(err))
		}
		return rec
	}
	if rec := call(w.s.publishQuote, "POST", "/", map[string]string{"quoteID": qt.ID.String()}, "{}", &w.bob); rec.Code != http.StatusNotFound {
		t.Fatalf("bob publishing: %d %s", rec.Code, rec.Body)
	}
	rec := call(w.s.publishQuote, "POST", "/", map[string]string{"quoteID": qt.ID.String()}, "{}", &w.ann)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", rec.Code, rec.Body)
	}
	var pub Quote
	w.must(json.Unmarshal(rec.Body.Bytes(), &pub))
	if pub.Status != QuotePublished || pub.URL == nil || !strings.HasPrefix(*pub.URL, "http://web.test/q/al_") {
		t.Fatalf("published = %+v", pub)
	}
	token := strings.TrimPrefix(*pub.URL, "http://web.test/q/")

	view := call(w.s.publicQuote, "GET", "/", map[string]string{"token": token}, "", nil)
	if view.Code != http.StatusOK || !strings.Contains(view.Body.String(), `"total":6492`) || strings.Contains(view.Body.String(), w.ann.String()) {
		t.Fatalf("public view: %d %s", view.Code, view.Body)
	}
	if bad := call(w.s.decideQuote(true), "POST", "/", map[string]string{"token": token}, `{"name":"Jo"}`, nil); bad.Code != http.StatusBadRequest {
		t.Fatalf("accept without the tick: %d", bad.Code)
	}
	ok := call(w.s.decideQuote(true), "POST", "/", map[string]string{"token": token}, `{"name":"Jo Buyer","email":"Jo@Buyer.test","accept":true}`, nil)
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"status":"accepted"`) {
		t.Fatalf("accept: %d %s", ok.Code, ok.Body)
	}
	var signer, email, ip string
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT signer_name, signer_email, signer_ip FROM crm_quotes WHERE id = $1`, qt.ID).Scan(&signer, &email, &ip))
	if signer != "Jo Buyer" || email != "jo@buyer.test" || ip == "" {
		t.Errorf("signature = %q %q %q", signer, email, ip)
	}
	if again := call(w.s.decideQuote(false), "POST", "/", map[string]string{"token": token}, `{"name":"Jo Buyer"}`, nil); again.Code != http.StatusConflict {
		t.Errorf("deciding twice: %d", again.Code)
	}
	if n := len(w.s.d.Notify.(*sentNotes).list); n == 0 {
		t.Error("the sender wasn't told")
	}

	// A second quote, published then expired: 410 with who to ask.
	q2, err := w.s.draftQuote(w.ctx, w.ann, dealID, quoteReq{})
	w.must(err)
	rec = call(w.s.publishQuote, "POST", "/", map[string]string{"quoteID": q2.ID.String()}, "{}", &w.ann)
	w.must(json.Unmarshal(rec.Body.Bytes(), &pub))
	token2 := strings.TrimPrefix(*pub.URL, "http://web.test/q/")
	_, err = w.s.d.Pool.Exec(w.ctx, `UPDATE crm_quotes SET expires_at = now() - interval '1 day' WHERE id = $1`, q2.ID)
	w.must(err)
	exp := call(w.s.publicQuote, "GET", "/", map[string]string{"token": token2}, "", nil)
	if exp.Code != http.StatusGone || !strings.Contains(exp.Body.String(), "expired") {
		t.Fatalf("expired link: %d %s", exp.Code, exp.Body)
	}
	if acc := call(w.s.decideQuote(true), "POST", "/", map[string]string{"token": token2}, `{"name":"Jo Buyer","accept":true}`, nil); acc.Code != http.StatusGone {
		t.Errorf("accepting an expired quote: %d", acc.Code)
	}
	// The first (accepted) quote stays readable after its expiry.
	_, err = w.s.d.Pool.Exec(w.ctx, `UPDATE crm_quotes SET expires_at = now() - interval '1 day' WHERE id = $1`, qt.ID)
	w.must(err)
	if v := call(w.s.publicQuote, "GET", "/", map[string]string{"token": token}, "", nil); v.Code != http.StatusOK {
		t.Errorf("accepted quote after expiry: %d", v.Code)
	}
	if bad := call(w.s.publicQuote, "GET", "/", map[string]string{"token": "al_nope"}, "", nil); bad.Code != http.StatusNotFound {
		t.Errorf("bad token: %d", bad.Code)
	}
}

func statusOf(err error) int {
	var e *httpx.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return http.StatusInternalServerError
}

// Form submissions find their contact by email (any case), update it, and
// create it once.
func TestFormDedupe(t *testing.T) {
	w := newWorld(t)
	f := &Form{OrgID: w.org, Name: "Contact us", ownerID: &w.ann, NotifyIDs: []uuid.UUID{w.ann, w.bob}, TagIDs: []uuid.UUID{},
		Fields: []FormField{{Key: "first_name", Object: "contact", Label: "First name"}, {Key: "email", Object: "contact", Label: "Email", Required: true},
			{Key: "name", Object: "company", Label: "Company"}}}
	w.must(w.s.d.Pool.QueryRow(w.ctx, `INSERT INTO crm_forms (org_id, name, fields, owner_id) VALUES ($1, $2, $3, $4) RETURNING id`, w.org, f.Name, fieldsJSON(f.Fields), w.ann).Scan(&f.ID))
	w.must(w.s.fillFieldTypes(w.ctx, w.org, f.Fields))
	first, err := w.s.applySubmission(w.ctx, f, submitReq{Values: map[string]json.RawMessage{"contact.first_name": raw("Lee"), "contact.email": raw("Lee@Example.COM"), "company.name": raw("Lee Labs")}})
	w.must(err)
	if !first.created {
		t.Fatal("first submission didn't create a contact")
	}
	second, err := w.s.applySubmission(w.ctx, f, submitReq{Values: map[string]json.RawMessage{"contact.first_name": raw("Leona"), "contact.email": raw("lee@example.com")}})
	w.must(err)
	if second.created || second.contactID != first.contactID {
		t.Fatalf("second submission made another contact: %+v vs %+v", second, first)
	}
	var n, subs int
	var name string
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT count(*), max(first_name) FROM crm_contacts WHERE org_id = $1 AND 'lee@example.com' = ANY (emails)`, w.org).Scan(&n, &name))
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT count(*) FROM crm_form_submissions WHERE form_id = $1`, f.ID).Scan(&subs))
	if n != 1 || name != "Leona" || subs != 2 {
		t.Errorf("contacts %d (%q), submissions %d", n, name, subs)
	}
	var company string
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT co.name FROM crm_contact_companies cc JOIN crm_companies co ON co.id = cc.company_id WHERE cc.contact_id = $1`, first.contactID).Scan(&company))
	if company != "Lee Labs" {
		t.Errorf("company = %q", company)
	}
	if _, err := w.s.applySubmission(w.ctx, f, submitReq{Values: map[string]json.RawMessage{"contact.first_name": raw("X")}}); err == nil {
		t.Error("a submission without the required email was accepted")
	}
	// Bob can't open ann's contact, so only ann hears about it.
	for _, x := range w.s.d.Notify.(*sentNotes).list {
		if x.Kind == NotifyForm && x.UserID == w.bob {
			t.Error("bob was notified about a contact he can't open")
		}
	}
}

// Workflows run as their owner, stop direct loops (a workflow never enrolls
// from its own chain) and stop chains at depth 3.
func TestWorkflowLoopProtection(t *testing.T) {
	w := newWorld(t)
	mk := func(name, triggerProp, setProp string, value any) uuid.UUID {
		var id uuid.UUID
		trig, _ := json.Marshal(WorkflowTrigger{Type: "property_changed", Property: triggerProp})
		acts, _ := json.Marshal([]WorkflowAction{{Type: "set_property", Property: setProp, Value: raw(value)}})
		w.must(w.s.d.Pool.QueryRow(w.ctx, `INSERT INTO crm_workflows (org_id, name, object_type, enabled, trigger, actions, owner_id, secret)
			VALUES ($1, $2, 'deal', true, $3, $4, $5, 'whsec_x') RETURNING id`, w.org, name, trig, acts, w.ann).Scan(&id))
		return id
	}
	runs := func(id uuid.UUID) (n int, status string) {
		w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT count(*), COALESCE(string_agg(status, ','), '') FROM crm_workflow_runs WHERE workflow_id = $1`, id).Scan(&n, &status))
		return
	}
	// ann can run workflows: owners hold crm.manage_workflows by default.
	a := mk("close → amount", "close_date", "amount", 1000)
	b := mk("amount → close", "amount", "close_date", "2031-01-01")
	dealID := w.deal(w.ann, "Loop", 0, "2030-01-01")
	if _, err := w.s.update(w.ctx, w.ann, objDeal, dealID, map[string]json.RawMessage{"close_date": raw("2030-06-01")}); err != nil {
		t.Fatal(err)
	}
	if n, st := runs(a); n != 1 || st != "done" {
		t.Errorf("A runs %d (%s), want 1 done", n, st)
	}
	if n, st := runs(b); n != 1 || st != "done" {
		t.Errorf("B runs %d (%s), want 1 done", n, st)
	}
	var amount int64
	var close string
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT amount, to_char(close_date, 'YYYY-MM-DD') FROM crm_deals WHERE id = $1`, dealID).Scan(&amount, &close))
	if amount != 1000 || close != "2031-01-01" {
		t.Errorf("deal after the chain: %d %s", amount, close)
	}
	var agent string
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT agent FROM crm_events WHERE record_id = $1 AND kind = 'updated' ORDER BY created_at DESC LIMIT 1`, dealID).Scan(&agent))
	if !strings.Contains(agent, "amount → close") {
		t.Errorf("timeline attribution = %q", agent)
	}
	// Enrolled once: changing it again doesn't re-run A (no re-enrollment).
	if _, err := w.s.update(w.ctx, w.ann, objDeal, dealID, map[string]json.RawMessage{"close_date": raw("2032-01-01")}); err != nil {
		t.Fatal(err)
	}
	if n, _ := runs(a); n != 1 {
		t.Errorf("A re-enrolled: %d runs", n)
	}

	// A chain over four workflows stops at depth 3.
	c1 := mk("c1 currency → name", "currency", "name", "Chained")
	c2 := mk("c2 name → amount", "name", "amount", 7)
	c3 := mk("c3 amount → close", "amount", "close_date", "2033-03-03")
	c4 := mk("c4 close → currency", "close_date", "currency", "EUR")
	for _, id := range []uuid.UUID{a, b} { // keep the first pair out of this chain
		_, err := w.s.d.Pool.Exec(w.ctx, `UPDATE crm_workflows SET enabled = false WHERE id = $1`, id)
		w.must(err)
	}
	other := w.deal(w.ann, "Chain", 0, "2030-01-01")
	if _, err := w.s.update(w.ctx, w.ann, objDeal, other, map[string]json.RawMessage{"currency": raw("GBP")}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []uuid.UUID{c1, c2, c3} {
		if n, st := runs(id); n != 1 || st != "done" {
			t.Errorf("c%d runs %d (%s), want 1 done", i+1, n, st)
		}
	}
	if n, _ := runs(c4); n != 0 {
		t.Errorf("c4 ran %d times past the depth limit", n)
	}

	// The owner loses crm.manage_workflows' basis (leaves the org): runs fail, nothing changes.
	_, err := w.s.d.Pool.Exec(w.ctx, `UPDATE crm_workflows SET enabled = true WHERE id = $1`, a)
	w.must(err)
	third := w.deal(w.ann, "Orphan", 0, "2030-01-01")
	_, err = w.s.d.Pool.Exec(w.ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, w.org, w.ann)
	w.must(err)
	_, _ = w.s.d.Pool.Exec(w.ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner') ON CONFLICT DO NOTHING`, w.org, w.bob)
	w.must(w.s.enroll(w.ctx, recordEvent{orgID: w.org, o: objDeal, id: third, kind: "updated", changes: []change{{Key: "close_date"}}}))
	w.must(w.s.runDueWorkflows(w.ctx))
	var amt *int64
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT amount FROM crm_deals WHERE id = $1`, third).Scan(&amt))
	if amt != nil && *amt == 1000 {
		t.Error("a workflow ran for an owner who left the org")
	}
}

// Tickets: the first logged reply sets the first response; closing sets closed_at.
func TestTicketTimes(t *testing.T) {
	w := newWorld(t)
	rec, err := w.s.create(w.ctx, w.ann, w.org, objTicket, map[string]json.RawMessage{"name": raw("Can't log in"), "priority": raw("high")}, false)
	w.must(err)
	tk := rec.(*Ticket)
	if tk.Stage.Name != "New" || tk.Priority != "high" || tk.FirstResponseAt != nil {
		t.Fatalf("new ticket = %+v", tk)
	}
	body := "Called back, reset the password"
	out := "outbound"
	if _, err := w.s.logActivity(w.ctx, w.ann, objTicket, tk.ID, activityReq{Type: "call", Body: &body, Direction: &out}); err != nil {
		t.Fatal(err)
	}
	var closed uuid.UUID
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT st.id FROM crm_stages st WHERE st.pipeline_id = $1 AND st.outcome = 'closed'`, tk.PipelineID).Scan(&closed))
	got, err := w.s.update(w.ctx, w.ann, objTicket, tk.ID, map[string]json.RawMessage{"stage": raw(closed.String())})
	w.must(err)
	tk = got.(*Ticket)
	if tk.FirstResponseSeconds == nil || tk.ResolutionSeconds == nil || tk.Stage.Outcome != "closed" {
		t.Fatalf("after reply and close: %+v", tk)
	}
	if _, err := w.s.update(w.ctx, w.ann, objTicket, tk.ID, map[string]json.RawMessage{"stage": raw(uuid.New().String())}); err == nil {
		t.Error("an unknown status was accepted")
	}
	var dealStage uuid.UUID
	w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT st.id FROM crm_stages st JOIN crm_pipelines p ON p.id = st.pipeline_id WHERE p.org_id = $1 AND p.object_type = 'deal' LIMIT 1`, w.org).Scan(&dealStage))
	if _, err := w.s.update(w.ctx, w.ann, objTicket, tk.ID, map[string]json.RawMessage{"stage": raw(dealStage.String())}); err == nil {
		t.Error("a deal stage was accepted for a ticket")
	}
	if _, err := w.s.getOne(w.ctx, w.s.d.Pool, w.bob, objTicket, tk.ID); err == nil {
		t.Error("bob opened ann's ticket")
	}
}

// Lead scores: the sum of the points of every rule a contact matches, kept
// current when the contact changes.
func TestLeadScoring(t *testing.T) {
	w := newWorld(t)
	add := func(label string, points int, filters string) {
		_, err := w.s.d.Pool.Exec(w.ctx, `INSERT INTO crm_score_rules (org_id, label, filters, points) VALUES ($1, $2, $3, $4)`, w.org, label, filters, points)
		w.must(err)
	}
	add("Customer", 20, `[{"key":"lifecycle_stage","op":"is_any","value":["customer"]}]`)
	add("Director", 15, `[{"key":"job_title","op":"contains","value":"director"}]`)
	add("Webmail", -5, `[{"key":"emails","op":"contains","value":"gmail.com"}]`)
	rec, err := w.s.create(w.ctx, w.ann, w.org, objContact, map[string]json.RawMessage{"first_name": raw("Ivy"), "emails": raw([]string{"ivy@gmail.com"}),
		"job_title": raw("Marketing Director"), "lifecycle_stage": raw("lead")}, false)
	w.must(err)
	id := rec.base().ID
	score := func() (n int) {
		w.must(w.s.d.Pool.QueryRow(w.ctx, `SELECT score FROM crm_contacts WHERE id = $1`, id).Scan(&n))
		return
	}
	if got := score(); got != 10 {
		t.Fatalf("score after create = %d, want 15 - 5 = 10", got)
	}
	if _, err := w.s.update(w.ctx, w.ann, objContact, id, map[string]json.RawMessage{"lifecycle_stage": raw("customer")}); err != nil {
		t.Fatal(err)
	}
	if got := score(); got != 30 {
		t.Fatalf("score after becoming a customer = %d, want 30", got)
	}
	_, err = w.s.d.Pool.Exec(w.ctx, `DELETE FROM crm_score_rules WHERE org_id = $1 AND label = 'Director'`, w.org)
	w.must(err)
	w.must(w.s.rescoreOrg(w.ctx, w.org))
	if got := score(); got != 15 {
		t.Fatalf("score after a rule went = %d, want 15", got)
	}
}
