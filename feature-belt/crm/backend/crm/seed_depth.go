package crm

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
)

// seedDepth adds the 1.3 surfaces to the seeded org: a product library, line
// items and a draft quote on the biggest open deal, support tickets, lead
// scoring rules, a shared email template, a shared dashboard, a sales goal, a
// workflow (off) and a lead form (unpublished). Idempotent like SeedDev.
func (s *Service) seedDepth(ctx context.Context, orgID uuid.UUID, owners []uuid.UUID) error {
	sam, jordan, priya := owners[0], owners[1], owners[2]
	now := time.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		exec := func(sql string, args ...any) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		}
		// Products.
		type product struct {
			key, name, sku, desc, billing string
			period                        *string
			term                          *int
			price                         int64
			tax                           int
		}
		monthly, annual := "monthly", "annual"
		twelve, one := 12, 1
		products := []product{
			{"day-pass", "Day pass", "DP-1", "One visit, gear included", "one_time", nil, nil, 2500, 0},
			{"membership", "Monthly membership", "MEM-M", "Unlimited climbing", "recurring", &monthly, &twelve, 8900, 0},
			{"corporate-seat", "Corporate seat", "CORP-Y", "A named membership paid by the employer", "recurring", &annual, &one, 65000, 0},
			{"team-session", "Team-building session", "TB-2H", "Two hours with an instructor for up to 20 people", "one_time", nil, nil, 180000, 825},
		}
		for _, p := range products {
			if err := exec(`INSERT INTO crm_products (id, org_id, name, sku, description, unit_price, currency, billing, billing_period, term_periods, tax_bp, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, 'USD', $7, $8, $9, $10, $11) ON CONFLICT (id) DO NOTHING`,
				seedID("product:"+p.key), orgID, p.name, p.sku, p.desc, p.price, p.billing, p.period, p.term, p.tax, sam); err != nil {
				return err
			}
		}
		// Line items and a draft quote on Brightline's memberships deal.
		deal := seedID("deal:brightline-members")
		var dealExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm_deals WHERE id = $1)`, deal).Scan(&dealExists); err != nil || !dealExists {
			return err
		}
		if err := exec(`INSERT INTO crm_line_items (id, org_id, deal_id, product_id, position, name, sku, description, unit_price, quantity_milli,
			discount_bp, billing, billing_period, term_periods, created_by)
			VALUES ($1, $2, $3, $4, 0, 'Corporate seat', 'CORP-Y', 'A named membership paid by the employer', 65000, 40000, 1000, 'recurring', 'annual', 1, $5),
			       ($6, $2, $3, $7, 1, 'Team-building session', 'TB-2H', 'Two hours with an instructor for up to 20 people', 180000, 2000, 0, 'one_time', NULL, 1, $5)
			ON CONFLICT (id) DO NOTHING`, seedID("line:brightline:seats"), orgID, deal, seedID("product:corporate-seat"), jordan,
			seedID("line:brightline:session"), seedID("product:team-session")); err != nil {
			return err
		}
		quote := seedID("quote:brightline")
		tag, err := tx.Exec(ctx, `INSERT INTO crm_quotes (id, org_id, deal_id, number, title, currency, expires_at, terms, comments, contact_id, sender_id, created_by)
			VALUES ($1, $2, $3, 'Q-0001', 'Brightline corporate memberships', 'USD', $4, 'Net 30. Seats can be reassigned at any time.',
			        'Thanks for the visit last week — here is the 40-seat plan we discussed.', $5, $6, $6) ON CONFLICT (id) DO NOTHING`,
			quote, orgID, deal, day.AddDate(0, 0, 21), seedID("contact:dana"), jordan)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindQuote, ID: quote, OrgID: orgID, OwnerID: jordan, Parent: &access.Ref{Kind: KindDeal, ID: deal}}); err != nil {
				return err
			}
			if err := exec(`INSERT INTO crm_quote_items (quote_id, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp,
				discount_amount, tax_bp, billing, billing_period, term_periods)
				SELECT $1, product_id, position, name, sku, description, unit_price, quantity_milli, discount_bp, discount_amount, tax_bp, billing, billing_period, term_periods
				FROM crm_line_items WHERE deal_id = $2`, quote, deal); err != nil {
				return err
			}
			if err := exec(`UPDATE crm_settings SET quote_seq = GREATEST(quote_seq, 1) WHERE org_id = $1`, orgID); err != nil {
				return err
			}
		}

		// Support tickets in the service pipeline.
		stages := map[string]uuid.UUID{}
		var pipeline uuid.UUID
		rows, err := tx.Query(ctx, `SELECT p.id, s.id, s.name FROM crm_pipelines p JOIN crm_stages s ON s.pipeline_id = p.id
			WHERE p.org_id = $1 AND p.object_type = 'ticket' ORDER BY p.position, s.position`, orgID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sid uuid.UUID
			var name string
			if err := rows.Scan(&pipeline, &sid, &name); err != nil {
				rows.Close()
				return err
			}
			if _, ok := stages[name]; !ok {
				stages[name] = sid
			}
		}
		rows.Close()
		type ticket struct {
			key, name, desc, priority, category, channel, stage, company, contact string
			owner                                                                 uuid.UUID
			ageHours, replyHours                                                  int
		}
		tickets := []ticket{
			{"harbor-badges", "Youth team badges won't scan at the front desk", "Three kids from the Harbor CU team were turned away on Saturday.", "high", "Access", "email", "Waiting on us", "harbor", "marcus", sam, 30, 2},
			{"brightline-invoice", "Invoice shows 38 seats instead of 40", "Finance needs a corrected invoice before the 15th.", "medium", "Billing", "email", "Waiting on contact", "brightline", "ruth", jordan, 52, 5},
			{"cedar-waiver", "Bulk waiver upload for the after-school group", "", "low", "Onboarding", "phone", "New", "cedar", "grace", priya, 6, 0},
			{"summit-referral", "Referral link goes to the wrong landing page", "", "urgent", "Website", "chat", "New", "summit", "nina", priya, 3, 0},
			{"alder-refund", "Refund for the cancelled offsite add-on", "Approved by Jordan; refunded to the card on file.", "medium", "Billing", "email", "Closed", "alder", "ben", jordan, 200, 4},
		}
		for _, t := range tickets {
			id := seedID("ticket:" + t.key)
			created := now.Add(-time.Duration(t.ageHours) * time.Hour)
			var reply, closed *time.Time
			if t.replyHours > 0 {
				r := created.Add(time.Duration(t.replyHours) * time.Hour)
				reply = &r
			}
			if t.stage == "Closed" {
				c := created.Add(30 * time.Hour)
				closed = &c
			}
			team, err := firstTeam(ctx, tx, orgID, t.owner)
			if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crm_tickets (id, org_id, name, description, priority, category, channel, pipeline_id, stage_id, company_id,
				stage_entered_at, first_response_at, closed_at, team_id, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $11, $11) ON CONFLICT (id) DO NOTHING`,
				id, orgID, t.name, t.desc, t.priority, t.category, t.channel, pipeline, stages[t.stage], seedID("company:"+t.company),
				created, reply, closed, team, t.owner)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			parent, err := parentFor(ctx, tx, orgID, team)
			if err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindTicket, ID: id, OrgID: orgID, OwnerID: t.owner, Parent: &parent}); err != nil {
				return err
			}
			if err := exec(`INSERT INTO crm_ticket_contacts (ticket_id, contact_id, org_id, created_by, created_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
				id, seedID("contact:"+t.contact), orgID, t.owner, created); err != nil {
				return err
			}
			if err := exec(`INSERT INTO crm_events (id, org_id, record_type, record_id, kind, actor_id, created_at) VALUES ($1, $2, 'ticket', $3, 'created', $4, $5)
				ON CONFLICT (id) DO NOTHING`, seedID("event:created:"+id.String()), orgID, id, t.owner, created); err != nil {
				return err
			}
		}

		// Lead scoring rules.
		rule := func(key, label string, points int, filters []Filter, pos int) error {
			f, _ := json.Marshal(filters)
			return exec(`INSERT INTO crm_score_rules (id, org_id, label, filters, points, position, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (id) DO NOTHING`, seedID("score:"+key), orgID, label, f, points, pos, sam)
		}
		if err := rule("customer", "Already a customer", 20, []Filter{{Key: "lifecycle_stage", Op: "is_any", Value: json.RawMessage(`["customer","opportunity"]`)}}, 0); err != nil {
			return err
		}
		if err := rule("decider", "Decision maker", 15, []Filter{{Key: "job_title", Op: "contains", Value: json.RawMessage(`"director"`)}}, 1); err != nil {
			return err
		}
		if err := rule("recent", "Active in the last 30 days", 10, []Filter{{Key: "last_activity_at", Op: "last_days", Value: json.RawMessage(`30`)}}, 2); err != nil {
			return err
		}
		if err := rule("webmail", "Personal email address", -5, []Filter{{Key: "emails", Op: "contains", Value: json.RawMessage(`"gmail.com"`)}}, 3); err != nil {
			return err
		}

		// A shared email template.
		if err := exec(`INSERT INTO crm_email_templates (id, org_id, owner_id, shared, name, subject, body) VALUES ($1, $2, $3, true, 'Follow-up after a visit',
			'Great to see you at Riverside, {{contact.first_name | there}}',
			$4) ON CONFLICT (id) DO NOTHING`, seedID("template:follow-up"), orgID, sam,
			"Hi {{contact.first_name | there}},\n\nThanks for coming by with the {{company.name | team}}. "+
				"If you'd like to plan the next session, pick a time here: {{booking_link}}\n\n{{sender.first_name}}"); err != nil {
			return err
		}

		// A dashboard everyone in the org can open (each sees their own numbers).
		dash := seedID("dashboard:sales")
		tag, err = tx.Exec(ctx, `INSERT INTO crm_dashboards (id, org_id, name, created_by) VALUES ($1, $2, 'Sales overview', $3) ON CONFLICT (id) DO NOTHING`, dash, orgID, sam)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: KindDashboard, ID: dash, OrgID: orgID, OwnerID: sam, General: access.View}); err != nil {
				return err
			}
		}
		reports := []struct {
			key, name string
			spec      ReportSpec
		}{
			{"pipeline", "Open pipeline by stage", ReportSpec{Object: "deal", Measure: "sum", Property: "amount", GroupBy: "stage", Chart: "bar",
				Filters: []Filter{{Key: "closed_at", Op: "empty"}}}},
			{"created", "Deals created by month", ReportSpec{Object: "deal", Measure: "count", GroupBy: "created_at", Bucket: "month", Chart: "line"}},
			{"lifecycle", "Contacts by lifecycle stage", ReportSpec{Object: "contact", Measure: "count", GroupBy: "lifecycle_stage", Chart: "donut"}},
			{"tickets", "Tickets by priority", ReportSpec{Object: "ticket", Measure: "count", GroupBy: "priority", Chart: "table"}},
		}
		for i, r := range reports {
			spec, _ := json.Marshal(r.spec)
			if err := exec(`INSERT INTO crm_reports (id, org_id, dashboard_id, position, name, spec, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (id) DO NOTHING`, seedID("report:"+r.key), orgID, dash, i, r.name, spec, sam); err != nil {
				return err
			}
		}

		// A quarterly revenue goal for Sam.
		q := time.Date(now.Year(), time.Month((int(now.Month())-1)/3*3+1), 1, 0, 0, 0, 0, time.UTC)
		if err := exec(`INSERT INTO crm_goals (id, org_id, metric, user_id, period, period_start, target, currency, created_by)
			VALUES ($1, $2, 'revenue_won', $3, 'quarter', $4, 5000000, 'USD', $3) ON CONFLICT (id) DO NOTHING`, seedID("goal:sam:"+q.Format("2006-01")), orgID, sam, q); err != nil {
			return err
		}

		// A workflow, off until someone turns it on (and it then runs as them).
		trig, _ := json.Marshal(WorkflowTrigger{Type: "created"})
		conds, _ := json.Marshal([]Filter{{Key: "lifecycle_stage", Op: "is_any", Value: json.RawMessage(`["lead"]`)}})
		acts, _ := json.Marshal([]WorkflowAction{
			{Type: "create_task", Title: "Call {{record.name}}", Body: "New lead: reach out within a day.", DueDays: 1, Priority: "high"},
			{Type: "delay", Minutes: 3 * 24 * 60},
			{Type: "set_property", Property: "lead_status", Value: json.RawMessage(`"attempted_to_contact"`)},
		})
		if err := exec(`INSERT INTO crm_workflows (id, org_id, name, object_type, enabled, trigger, conditions, actions, secret, created_by)
			VALUES ($1, $2, 'New lead follow-up', 'contact', false, $3, $4, $5, $6, $7) ON CONFLICT (id) DO NOTHING`,
			seedID("workflow:new-lead"), orgID, trig, conds, acts, newSecret(), sam); err != nil {
			return err
		}

		// A lead form, unpublished.
		form := seedID("form:group-visit")
		fields := fieldsJSON([]FormField{{Key: "first_name", Object: "contact", Label: "First name", Required: true}, {Key: "last_name", Object: "contact", Label: "Last name"},
			{Key: "email", Object: "contact", Label: "Work email", Required: true}, {Key: "name", Object: "company", Label: "Company"},
			{Key: "phones", Object: "contact", Label: "Phone"}})
		tag, err = tx.Exec(ctx, `INSERT INTO crm_forms (id, org_id, name, fields, submit_label, success_message, consent_text, owner_id, notify_ids, lifecycle_stage, created_by)
			VALUES ($1, $2, 'Book a group visit', $3, 'Send request', 'Thanks — we''ll reply within a working day.',
			        'I agree that Riverside Climbing Club may contact me about my request.', $4, $5, 'lead', $4) ON CONFLICT (id) DO NOTHING`,
			form, orgID, fields, sam, []uuid.UUID{sam})
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			return access.CreateResource(ctx, tx, access.NewResource{Kind: KindForm, ID: form, OrgID: orgID, OwnerID: sam})
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.rescoreOrg(ctx, orgID)
}
