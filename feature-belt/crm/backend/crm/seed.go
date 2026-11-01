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

// seedNS makes seed ids stable, so re-running the seed changes nothing.
var seedNS = uuid.MustParse("3f0d5c1e-9a57-4f6b-8c3d-2b1e6f7a9c40")

func seedID(key string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte(key)) }

type seedCompany struct {
	key, name, domain, industry, city string
	employees                         int
	owner                             int // index into users
	tier                              string
	tags                              []string
}

type seedContact struct {
	key, first, last, email, title, lifecycle, company string
	owner                                              int
	phone                                              string
	climbs                                             bool
}

type seedDeal struct {
	key, name, company, stage string
	amount                    int64 // cents
	closeIn                   int   // days from today
	owner                     int
	contacts                  []string
	seats                     int
	tags                      []string
}

// SeedDev gives the first seeded org (Riverside Climbing Club) a book of
// business: 12 companies, 25 contacts, 15 deals across the default pipeline,
// activities and tasks due today and overdue, owned by Sam, Jordan and Priya
// (users are admin, sam, jordan, priya). The orgs and access seeds run first
// (feature files sort before crm, and access runs the orgs seed).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	var orgID uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'riverside-climbing'`).Scan(&orgID)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.bootstrap(ctx, orgID); err != nil {
		return err
	}
	owners := []uuid.UUID{users[1], users[2], users[3]} // sam, jordan, priya
	companies := []seedCompany{
		{"northwind", "Northwind Outfitters", "northwindoutfitters.com", "Outdoor retail", "Portland", 120, 0, "gold", []string{"Sponsor"}},
		{"brightline", "Brightline Software", "brightline.io", "Software", "Portland", 340, 1, "silver", []string{"Corporate", "Hot"}},
		{"cedar", "Cedar Valley School District", "cedarvalleyschools.org", "Education", "Beaverton", 2100, 2, "", []string{"Schools"}},
		{"summit", "Summit Physical Therapy", "summitpt.com", "Healthcare", "Portland", 18, 2, "bronze", nil},
		{"harbor", "Harbor Credit Union", "harborcu.org", "Financial services", "Vancouver", 260, 0, "gold", []string{"Sponsor", "Renewal"}},
		{"alder", "Alder & Pine Architects", "alderpine.com", "Architecture", "Portland", 45, 1, "", []string{"Corporate"}},
		{"kestrel", "Kestrel Robotics", "kestrelrobotics.com", "Hardware", "Hillsboro", 85, 0, "", []string{"Corporate"}},
		{"parks", "Riverside Parks Department", "riversideparks.gov", "Government", "Riverside", 140, 2, "", []string{"Hot"}},
		{"bluefern", "Blue Fern Coffee", "bluefern.coffee", "Food and drink", "Portland", 32, 1, "", nil},
		{"granite", "Granite Peak Insurance", "granitepeak.com", "Insurance", "Lake Oswego", 510, 0, "silver", []string{"Corporate", "Hot"}},
		{"lumen", "Lumen Health Clinics", "lumenhealth.org", "Healthcare", "Portland", 900, 1, "", []string{"Corporate"}},
		{"tidewater", "Tidewater Events", "tidewaterevents.com", "Events", "Astoria", 12, 2, "bronze", nil},
	}
	contacts := []seedContact{
		{"maya", "Maya", "Chen", "maya@northwindoutfitters.com", "Marketing director", "opportunity", "northwind", 0, "+1 503 555 0101", true},
		{"luis", "Luis", "Ortega", "luis.ortega@northwindoutfitters.com", "Store manager", "customer", "northwind", 0, "+1 503 555 0102", false},
		{"dana", "Dana", "Kim", "dana@brightline.io", "Head of people", "opportunity", "brightline", 1, "+1 503 555 0103", true},
		{"owen", "Owen", "Price", "owen@brightline.io", "Office manager", "sql", "brightline", 1, "+1 503 555 0104", false},
		{"ruth", "Ruth", "Adebayo", "ruth@brightline.io", "CFO", "sql", "brightline", 1, "", false},
		{"grace", "Grace", "Holloway", "gholloway@cedarvalleyschools.org", "Athletics coordinator", "opportunity", "cedar", 2, "+1 503 555 0106", false},
		{"tom", "Tom", "Becker", "tbecker@cedarvalleyschools.org", "Principal", "customer", "cedar", 2, "+1 503 555 0107", false},
		{"nina", "Nina", "Patel", "nina@summitpt.com", "Clinic owner", "lead", "summit", 2, "+1 503 555 0108", true},
		{"marcus", "Marcus", "Reed", "mreed@harborcu.org", "Community relations lead", "customer", "harbor", 0, "+1 360 555 0109", true},
		{"alicia", "Alicia", "Wong", "awong@harborcu.org", "Marketing coordinator", "customer", "harbor", 0, "", false},
		{"ben", "Ben", "Foster", "ben@alderpine.com", "Partner", "customer", "alder", 1, "+1 503 555 0111", true},
		{"sofia", "Sofia", "Márquez", "sofia@kestrelrobotics.com", "People operations", "opportunity", "kestrel", 0, "+1 503 555 0112", false},
		{"jamal", "Jamal", "Turner", "jamal@kestrelrobotics.com", "CEO", "sql", "kestrel", 0, "", false},
		{"helen", "Helen", "Park", "hpark@riversideparks.gov", "Recreation manager", "opportunity", "parks", 2, "+1 503 555 0114", false},
		{"victor", "Victor", "Alvarez", "valvarez@riversideparks.gov", "Capital projects lead", "sql", "parks", 2, "+1 503 555 0115", false},
		{"chloe", "Chloe", "Martin", "chloe@bluefern.coffee", "Owner", "other", "bluefern", 1, "+1 503 555 0116", true},
		{"andre", "André", "Silva", "asilva@granitepeak.com", "Benefits manager", "opportunity", "granite", 0, "+1 503 555 0117", false},
		{"karen", "Karen", "Liu", "kliu@granitepeak.com", "VP people", "sql", "granite", 0, "", false},
		{"peter", "Peter", "Novak", "pnovak@granitepeak.com", "Events lead", "lead", "granite", 0, "+1 503 555 0119", true},
		{"fatima", "Fatima", "Hassan", "fhassan@lumenhealth.org", "Wellness program lead", "mql", "lumen", 1, "+1 503 555 0120", false},
		{"david", "David", "Cho", "dcho@lumenhealth.org", "HR director", "lead", "lumen", 1, "", false},
		{"emma", "Emma", "Schultz", "emma@tidewaterevents.com", "Producer", "opportunity", "tidewater", 2, "+1 503 555 0122", true},
		{"lena", "Lena", "Morris", "lena@tidewaterevents.com", "Founder", "sql", "tidewater", 2, "+1 503 555 0123", false},
		{"rafael", "Rafael", "Costa", "rafa.costa@gmail.com", "Freelance route setter", "lead", "", 2, "+1 503 555 0124", true},
		{"ivy", "Ivy", "Nguyen", "ivy.nguyen@outlook.com", "Climbing creator", "subscriber", "", 1, "", true},
	}
	deals := []seedDeal{
		{"brightline-members", "Brightline: corporate memberships (40 seats)", "brightline", "Negotiation", 2880000, 12, 1, []string{"dana", "ruth"}, 40, []string{"Hot"}},
		{"northwind-demo", "Northwind: gear demo days", "northwind", "Proposal sent", 650000, 21, 0, []string{"maya"}, 0, []string{"Sponsor"}},
		{"cedar-afterschool", "Cedar Valley: after-school climbing", "cedar", "Meeting booked", 1420000, 45, 2, []string{"grace"}, 60, []string{"Schools"}},
		{"summit-referrals", "Summit PT: referral partnership", "summit", "Qualified", 240000, 60, 2, []string{"nina"}, 0, nil},
		{"harbor-youth", "Harbor CU: youth team sponsorship", "harbor", "Won", 1000000, -3, 0, []string{"marcus", "alicia"}, 0, []string{"Sponsor", "Renewal"}},
		{"alder-offsite", "Alder & Pine: team offsite", "alder", "Won", 320000, -40, 1, []string{"ben"}, 45, nil},
		{"kestrel-teambuilding", "Kestrel: monthly team-building", "kestrel", "Proposal sent", 840000, 18, 0, []string{"sofia", "jamal"}, 30, nil},
		{"parks-wall", "Riverside Parks: outdoor wall at Pier Park", "parks", "Negotiation", 4600000, 30, 2, []string{"helen", "victor"}, 0, []string{"Hot"}},
		{"bluefern-cafe", "Blue Fern: café corner", "bluefern", "Lost", 480000, -5, 1, []string{"chloe"}, 0, nil},
		{"granite-wellness", "Granite Peak: wellness memberships (120 seats)", "granite", "Meeting booked", 5400000, 50, 0, []string{"andre", "karen"}, 120, []string{"Hot"}},
		{"lumen-pilot", "Lumen Health: staff wellness pilot", "lumen", "Qualified", 1200000, 75, 1, []string{"fatima"}, 25, nil},
		{"tidewater-comp", "Tidewater: bouldering comp production", "tidewater", "Proposal sent", 750000, 25, 2, []string{"emma", "lena"}, 0, nil},
		{"brightline-interns", "Brightline: summer intern socials", "brightline", "Qualified", 180000, 120, 1, []string{"owen"}, 15, nil},
		{"cedar-pd", "Cedar Valley: teacher PD day", "cedar", "Won", 260000, -12, 2, []string{"tom"}, 30, []string{"Schools"}},
		{"granite-climbathon", "Granite Peak: climb-a-thon sponsorship", "granite", "Lost", 1500000, -20, 0, []string{"peter"}, 0, nil},
	}

	now := time.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// Custom properties and tags.
		props := []struct {
			o             objectType
			key, label, t string
			opts          []Option
		}{
			{objCompany, "partner_tier", "Partner tier", "select", []Option{{"bronze", "Bronze"}, {"silver", "Silver"}, {"gold", "Gold"}}},
			{objContact, "climbs_with_us", "Climbs with us", "checkbox", nil},
			{objDeal, "seats", "Seats", "number", nil},
			{objDeal, "contract_term", "Contract term", "select", []Option{{"6m", "6 months"}, {"12m", "12 months"}, {"24m", "24 months"}}},
		}
		for i, p := range props {
			opts := p.opts
			if opts == nil {
				opts = []Option{}
			}
			b, _ := json.Marshal(opts)
			if _, err := tx.Exec(ctx, `INSERT INTO crm_properties (org_id, object_type, key, label, type, options, position) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (org_id, object_type, key) DO NOTHING`, orgID, string(p.o), p.key, p.label, p.t, b, i); err != nil {
				return err
			}
		}
		tagIDs := map[string]uuid.UUID{}
		for _, t := range []struct{ name, tone string }{{"Corporate", "primary"}, {"Schools", "info"}, {"Sponsor", "accent"}, {"Renewal", "warning"}, {"Hot", "danger"}} {
			var id uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO crm_tags (org_id, name, tone) VALUES ($1, $2, $3)
				ON CONFLICT (org_id, lower(name)) DO UPDATE SET updated_at = crm_tags.updated_at RETURNING id`, orgID, t.name, t.tone).Scan(&id); err != nil {
				return err
			}
			tagIDs[t.name] = id
		}
		tagList := func(names []string) []uuid.UUID {
			out := []uuid.UUID{}
			for _, n := range names {
				out = append(out, tagIDs[n])
			}
			return out
		}
		register := func(o objectType, id uuid.UUID, owner uuid.UUID, created time.Time) (*uuid.UUID, error) {
			team, err := firstTeam(ctx, tx, orgID, owner)
			if err != nil {
				return nil, err
			}
			parent, err := parentFor(ctx, tx, orgID, team)
			if err != nil {
				return nil, err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: o.kind(), ID: id, OrgID: orgID, OwnerID: owner, Parent: &parent}); err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO crm_events (id, org_id, record_type, record_id, kind, actor_id, created_at)
				VALUES ($1, $2, $3, $4, 'created', $5, $6) ON CONFLICT (id) DO NOTHING`, seedID("event:created:"+id.String()), orgID, string(o), id, owner, created)
			return team, err
		}

		companyIDs := map[string]uuid.UUID{}
		for i, c := range companies {
			id := seedID("company:" + c.key)
			companyIDs[c.key] = id
			owner := owners[c.owner]
			created := day.AddDate(0, 0, -70+i*3).Add(10 * time.Hour)
			team, err := firstTeam(ctx, tx, orgID, owner)
			if err != nil {
				return err
			}
			propsJSON := map[string]any{}
			if c.tier != "" {
				propsJSON["partner_tier"] = c.tier
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crm_companies (id, org_id, name, domain, industry, employees, city, country, team_id, tag_ids, props, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'United States', $8, $9, $10, $11, $12, $12) ON CONFLICT (id) DO NOTHING`,
				id, orgID, c.name, c.domain, c.industry, c.employees, c.city, team, tagList(c.tags), propsJSON, owner, created)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue // seeded before
			}
			if _, err := register(objCompany, id, owner, created); err != nil {
				return err
			}
		}

		contactIDs := map[string]uuid.UUID{}
		for i, c := range contacts {
			id := seedID("contact:" + c.key)
			contactIDs[c.key] = id
			owner := owners[c.owner]
			created := day.AddDate(0, 0, -60+i*2).Add(11 * time.Hour)
			team, err := firstTeam(ctx, tx, orgID, owner)
			if err != nil {
				return err
			}
			phones := []string{}
			if c.phone != "" {
				phones = append(phones, c.phone)
			}
			props := map[string]any{}
			if c.climbs {
				props["climbs_with_us"] = true
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crm_contacts (id, org_id, first_name, last_name, emails, phones, job_title, lifecycle_stage, city, country,
				team_id, props, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'Portland', 'United States', $9, $10, $11, $12, $12) ON CONFLICT (id) DO NOTHING`,
				id, orgID, c.first, c.last, []string{c.email}, phones, c.title, c.lifecycle, team, props, owner, created)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			if _, err := register(objContact, id, owner, created); err != nil {
				return err
			}
			if c.company != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO crm_contact_companies (contact_id, company_id, org_id, is_primary, created_by, created_at)
					VALUES ($1, $2, $3, true, $4, $5) ON CONFLICT DO NOTHING`, id, companyIDs[c.company], orgID, owner, created); err != nil {
					return err
				}
			}
		}

		pipelines, err := s.loadPipelines(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if len(pipelines) == 0 {
			return nil
		}
		pl := pipelines[0]
		stageIndex := map[string]int{}
		for i, st := range pl.Stages {
			stageIndex[st.Name] = i
		}
		dealIDs := map[string]uuid.UUID{}
		for i, d := range deals {
			id := seedID("deal:" + d.key)
			dealIDs[d.key] = id
			idx, ok := stageIndex[d.stage]
			if !ok {
				continue // the pipeline was changed; skip deals whose stage is gone
			}
			stage := pl.Stages[idx]
			owner := owners[d.owner]
			created := day.AddDate(0, 0, -50+i*2).Add(14 * time.Hour)
			// The path through the pipeline: open stages up to the current one,
			// or up to Negotiation (won) / an earlier stage (lost).
			path := []Stage{}
			switch stage.Outcome {
			case "open":
				path = append(path, pl.Stages[:idx+1]...)
			case "won":
				for _, st := range pl.Stages {
					if st.Outcome == "open" {
						path = append(path, st)
					}
				}
				path = append(path, stage)
			default:
				path = append(path, pl.Stages[:2]...)
				path = append(path, stage)
			}
			closeDate := day.AddDate(0, 0, d.closeIn)
			// Spread the path between creation and now (closed deals close on their close date).
			end := now.Add(-time.Duration(i+1) * 3 * time.Hour)
			if stage.Outcome != "open" {
				end = closeDate.Add(15 * time.Hour)
				if d.closeIn > -10 && end.Before(monthStart) {
					end = monthStart.Add(2 * time.Hour)
				}
				if end.After(now) {
					end = now.Add(-time.Hour)
				}
			}
			step := end.Sub(created) / time.Duration(len(path))
			var closedAt *time.Time
			entered := created.Add(step * time.Duration(len(path)-1))
			if stage.Outcome != "open" {
				closedAt = &entered
			}
			team, err := firstTeam(ctx, tx, orgID, owner)
			if err != nil {
				return err
			}
			props := map[string]any{"contract_term": "12m"}
			if d.seats > 0 {
				props["seats"] = d.seats
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crm_deals (id, org_id, name, amount, currency, pipeline_id, stage_id, close_date, company_id,
				stage_entered_at, closed_at, team_id, tag_ids, props, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, $4, 'USD', $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $9) ON CONFLICT (id) DO NOTHING`,
				id, orgID, d.name, d.amount, pl.ID, stage.ID, closeDate, companyIDs[d.company], entered, closedAt, team, tagList(d.tags), props, owner, created)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			if _, err := register(objDeal, id, owner, created); err != nil {
				return err
			}
			for j, st := range path {
				in := created.Add(step * time.Duration(j))
				var out *time.Time
				if j < len(path)-1 {
					o := created.Add(step * time.Duration(j+1))
					out = &o
				}
				if _, err := tx.Exec(ctx, `INSERT INTO crm_deal_stages (id, deal_id, org_id, pipeline_id, stage_id, stage_name, entered_at, exited_at, entered_by)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (id) DO NOTHING`,
					seedID("stage:"+d.key+":"+st.Name), id, orgID, pl.ID, st.ID, st.Name, in, out, owner); err != nil {
					return err
				}
				if j > 0 {
					data, _ := json.Marshal(map[string]any{"from_stage": path[j-1].Name, "to_stage": st.Name, "to_outcome": st.Outcome,
						"seconds_in_stage": int64(step.Seconds())})
					if _, err := tx.Exec(ctx, `INSERT INTO crm_events (id, org_id, record_type, record_id, kind, actor_id, data, created_at)
						VALUES ($1, $2, 'deal', $3, 'stage_changed', $4, $5, $6) ON CONFLICT (id) DO NOTHING`,
						seedID("event:stage:"+d.key+":"+st.Name), orgID, id, owner, data, in); err != nil {
						return err
					}
				}
			}
			for _, ck := range d.contacts {
				if _, err := tx.Exec(ctx, `INSERT INTO crm_deal_contacts (deal_id, contact_id, org_id, created_by, created_at) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT DO NOTHING`, id, contactIDs[ck], orgID, owner, created); err != nil {
					return err
				}
			}
		}

		// Activities: logged on a record and shown on its company and deal too.
		type seedActivity struct {
			key, typ, subject, body string
			on                      []refKey
			by                      int
			at                      time.Time
			outcome, direction      string
			minutes                 int
			due                     *time.Time
			assignee                int
			done                    bool
			reminder                *time.Time
		}
		at := func(days int, hour int) time.Time {
			return day.AddDate(0, 0, days).Add(time.Duration(hour) * time.Hour)
		}
		pt := func(t time.Time) *time.Time { return &t }
		c, co, dl := func(k string) refKey { return refKey{objContact, contactIDs[k]} },
			func(k string) refKey { return refKey{objCompany, companyIDs[k]} },
			func(k string) refKey { return refKey{objDeal, dealIDs[k]} }
		jordanMention := "<@" + owners[1].String() + ">"
		activities := []seedActivity{
			{key: "n1", typ: "note", body: "Dana wants pricing for 40 seats with a 12-month term and a quarterly invoice. " + jordanMention + " can you join the pricing call Thursday?",
				on: []refKey{dl("brightline-members"), c("dana"), co("brightline")}, by: 0, at: at(-6, 15)},
			{key: "c1", typ: "call", body: "Walked Ruth through the invoice schedule. She needs the final number by Friday.", outcome: "connected", direction: "outbound", minutes: 18,
				on: []refKey{c("ruth"), dl("brightline-members"), co("brightline")}, by: 1, at: at(-2, 17)},
			{key: "m1", typ: "meeting", subject: "Site visit at Pier Park", body: "Measured the wall footprint with Helen and Victor. Permit review takes about 6 weeks.",
				outcome: "completed", minutes: 60, on: []refKey{dl("parks-wall"), c("helen"), c("victor"), co("parks")}, by: 2, at: at(-9, 16)},
			{key: "e1", typ: "email", subject: "Revised proposal", body: "Sent the revised proposal with the shade structure as an option.", direction: "outbound",
				on: []refKey{dl("parks-wall"), c("victor"), co("parks")}, by: 2, at: at(-1, 19)},
			{key: "n2", typ: "note", body: "Maya prefers two demo weekends in spring over one in March. Their budget resets in January.",
				on: []refKey{dl("northwind-demo"), c("maya"), co("northwind")}, by: 0, at: at(-4, 18)},
			{key: "c2", typ: "call", body: "Left a voicemail about the benefits fair booth.", outcome: "left_voicemail", direction: "outbound", minutes: 2,
				on: []refKey{c("andre"), dl("granite-wellness"), co("granite")}, by: 0, at: at(-1, 16)},
			{key: "m2", typ: "meeting", subject: "Kickoff with Granite Peak HR", body: "Karen and André want a pilot for one department before the full 120 seats.",
				outcome: "completed", minutes: 45, on: []refKey{dl("granite-wellness"), c("karen"), c("andre"), co("granite")}, by: 0, at: at(-8, 17)},
			{key: "n3", typ: "note", body: "Grace confirmed Tuesday and Thursday afternoons work for buses.",
				on: []refKey{dl("cedar-afterschool"), c("grace"), co("cedar")}, by: 2, at: at(-3, 21)},
			{key: "c3", typ: "call", body: "Chloe decided to keep the space for seating. Revisit next year.", outcome: "connected", direction: "inbound", minutes: 9,
				on: []refKey{dl("bluefern-cafe"), c("chloe"), co("bluefern")}, by: 1, at: at(-5, 15)},
			{key: "n4", typ: "note", body: "Harbor CU signed. Jerseys go to print on the 15th; logo files are in the shared drive.",
				on: []refKey{dl("harbor-youth"), c("marcus"), co("harbor")}, by: 0, at: at(-3, 20)},
			{key: "e2", typ: "email", subject: "Comp production quote", body: "Emma asked for a line item for isolation-zone staffing.", direction: "inbound",
				on: []refKey{dl("tidewater-comp"), c("emma"), co("tidewater")}, by: 2, at: at(-2, 14)},
			{key: "t1", typ: "task", subject: "Send revised membership pricing to Dana", on: []refKey{dl("brightline-members"), c("dana")}, by: 0, at: at(-2, 9),
				due: pt(at(0, 23)), assignee: 1, reminder: pt(at(0, 22))},
			{key: "t2", typ: "task", subject: "Call Helen about the permit timeline", on: []refKey{dl("parks-wall"), c("helen")}, by: 2, at: at(-5, 9),
				due: pt(at(-2, 17)), assignee: 2},
			{key: "t3", typ: "task", subject: "Book a second site visit with Victor", on: []refKey{dl("parks-wall"), c("victor")}, by: 2, at: at(-1, 9),
				due: pt(at(3, 17)), assignee: 2},
			{key: "t4", typ: "task", subject: "Follow up with André after the benefits fair", on: []refKey{dl("granite-wellness"), c("andre")}, by: 0, at: at(-1, 10),
				due: pt(at(0, 21)), assignee: 0},
			{key: "t5", typ: "task", subject: "Draft the demo-days sponsorship deck", on: []refKey{dl("northwind-demo"), co("northwind")}, by: 0, at: at(-4, 10),
				due: pt(at(-1, 17)), assignee: 0},
			{key: "t6", typ: "task", subject: "Check in with Nina on referral forms", on: []refKey{dl("summit-referrals"), c("nina")}, by: 2, at: at(-1, 11),
				due: pt(at(6, 16)), assignee: 2},
			{key: "t7", typ: "task", subject: "Renewal check-in with Marcus", on: []refKey{co("harbor"), c("marcus")}, by: 0, at: at(-1, 12),
				due: pt(at(20, 16)), assignee: 0},
			{key: "t8", typ: "task", subject: "Send youth team photos to Harbor CU", on: []refKey{dl("harbor-youth")}, by: 0, at: at(-3, 12),
				due: pt(at(-1, 16)), assignee: 0, done: true},
		}
		for _, a := range activities {
			id := seedID("activity:" + a.key)
			primary := a.on[0]
			var outcome, direction *string
			if a.outcome != "" {
				outcome = &a.outcome
			}
			if a.direction != "" {
				direction = &a.direction
			}
			var minutes *int
			if a.minutes > 0 {
				minutes = &a.minutes
			}
			var assignee *uuid.UUID
			var done, reminded *time.Time
			if a.typ == "task" {
				assignee = &owners[a.assignee]
				if a.done {
					done = pt(a.due.Add(-2 * time.Hour))
				}
				if a.reminder != nil && a.reminder.Before(now) {
					reminded = a.reminder
				}
			}
			mentions := []uuid.UUID{}
			if a.key == "n1" {
				mentions = append(mentions, owners[1])
			}
			tag, err := tx.Exec(ctx, `INSERT INTO crm_activities (id, org_id, type, record_type, record_id, subject, body, occurred_at, duration_minutes, outcome,
				direction, due_at, assignee_id, reminder_at, reminded_at, done_at, mentions, created_by, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $8, $8) ON CONFLICT (id) DO NOTHING`,
				id, orgID, a.typ, string(primary.Type), primary.ID, a.subject, a.body, a.at, minutes, outcome, direction, a.due, assignee, a.reminder,
				reminded, done, mentions, owners[a.by])
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				continue
			}
			for _, k := range a.on {
				if _, err := tx.Exec(ctx, `INSERT INTO crm_activity_records (activity_id, record_type, record_id, org_id) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
					id, string(k.Type), k.ID, orgID); err != nil {
					return err
				}
				if a.typ != "task" {
					if _, err := tx.Exec(ctx, `UPDATE `+k.Type.table()+` SET last_activity_at = GREATEST(COALESCE(last_activity_at, '-infinity'), $2) WHERE id = $1`,
						k.ID, a.at); err != nil {
						return err
					}
				}
			}
		}

		// A shared view and a personal one.
		closing, _ := json.Marshal([]Filter{{Key: "close_date", Op: "next_days", Value: json.RawMessage("30")}})
		mine, _ := json.Marshal([]Filter{{Key: "owner", Op: "is_any", Value: json.RawMessage(`["me"]`)}, {Key: "lifecycle_stage", Op: "is_any", Value: json.RawMessage(`["customer"]`)}})
		if _, err := tx.Exec(ctx, `INSERT INTO crm_views (id, org_id, object_type, name, owner_id, shared, columns, sort_key, sort_dir, filters)
			VALUES ($1, $2, 'deal', 'Closing in 30 days', $3, true, $4, 'close_date', 'asc', $5),
			       ($6, $2, 'contact', 'My customers', $3, false, $7, 'name', 'asc', $8)
			ON CONFLICT (id) DO NOTHING`,
			seedID("view:closing"), orgID, owners[0], []string{"name", "amount", "stage", "close_date", "owner", "company"}, closing,
			seedID("view:my-customers"), []string{"name", "emails", "company", "job_title", "last_activity_at"}, mine); err != nil {
			return err
		}
		return nil
	})
}
