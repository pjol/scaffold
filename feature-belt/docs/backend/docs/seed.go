package docs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// seedNS makes seed ids stable, so re-running the seed finds its rows.
var seedNS = uuid.MustParse("6f1d2c1e-6a43-4b8e-9d0c-3c2f0b7d8a10")

func seedID(key string) uuid.UUID { return uuid.NewSHA1(seedNS, []byte(key)) }

type seedPage struct {
	key, org, title, icon string
	owner                 uuid.UUID
	parent                string       // key of the parent page
	general               access.Level // top level: everyone in the org
	restricted            bool         // don't inherit the parent's access
	team                  string       // handle of a team granted edit
	grants                map[uuid.UUID]access.Level
	markdown              string
}

// SeedDev builds a believable workspace in the orgs the orgs feature seeds
// (users: admin, sam, jordan, priya): a handbook shared with the club (with
// a restricted child only some can open), a wiki shared with the Coaching
// team, an onboarding page with every kind of block and comment threads, a
// Projects database with table, board and calendar views, and a private
// page for each member. Pages are only filled the first time.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	for _, m := range s.d.Modules {
		if seeder, ok := m.(app.DevSeeder); ok && m.Name() == "access" {
			if err := seeder.SeedDev(ctx, users); err != nil {
				return fmt.Errorf("access seed: %w", err)
			}
		}
	}
	sam, jordan, priya := users[1], users[2], users[3]
	handbook, safety := seedID("handbook"), seedID("safety")
	mention := func(id uuid.UUID, name string) string { return "@[" + name + "](user:" + id.String() + ")" }
	pages := []seedPage{
		{key: "handbook", org: "riverside-climbing", title: "Club handbook", icon: "📘", owner: sam, general: access.Edit, markdown: `
Everything members need to know about the club, kept up to date by the board. Ask in a comment if something is missing.

## How the club works
- Members climb any time the gym is open with their card.
- Coaching sessions run on **Tuesdays and Thursdays** from 6 pm.
- The board meets on the first Monday of each month.

## Contacts
| Role | Who | When |
| --- | --- | --- |
| Head coach | ` + mention(sam, "Sam Rivera") + ` | Tue, Thu |
| Route setting | ` + mention(jordan, "Jordan Lee") + ` | Weekends |`},
		{key: "safety", org: "riverside-climbing", title: "Safety rules", icon: "🧗", owner: sam, parent: "handbook", markdown: `
> ⚠️ Everyone belays with a partner check, every time. No exceptions.

- [x] Harness buckle doubled back
- [x] Knot dressed and finished with a stopper
- [ ] Belay device locked and loaded the right way
- [ ] Partner check done out loud

## Bouldering
1. Spot your partner on anything over head height.
2. Brush holds when you're done.
3. Walk, don't run, on the mats.`},
		{key: "board-notes", org: "riverside-climbing", title: "Board notes", icon: "🔒", owner: sam, parent: "handbook", restricted: true,
			grants: map[uuid.UUID]access.Level{jordan: access.Edit}, markdown: `
Only the board can open this page.

## October meeting
- Insurance renewal is due in December; quotes from two brokers.
- Membership fee stays the same next year.
- [ ] Sam: send the renewal paperwork
- [x] Jordan: count the new route holds`},
		{key: "coaching", org: "riverside-climbing", title: "Coaching wiki", icon: "🎯", owner: sam, team: "coaching", markdown: `
Plans, drills and notes for the coaching team.

## This term
- Footwork drills in week 1–3
- Lead climbing intro in week 4
- Video review night in week 6`},
		{key: "technique", org: "riverside-climbing", title: "Tuesday technique plan", icon: "🗓️", owner: sam, parent: "coaching", markdown: `
## Warm-up (15 min)
Easy traverses, then three silent-feet problems.

## Main set
` + "```" + `
4 × 4 on the 30° board
rest 3 min between sets
` + "```"},
		{key: "onboarding", org: "riverside-climbing", title: "Welcome to the club", icon: "👋", owner: jordan, general: access.Comment, markdown: `
Glad you're here! This page walks you through your first month. Start with the [Club handbook](page:` + handbook.String() + `) and the [Safety rules](page:` + safety.String() + `).

# Your first week
Your coach is ` + mention(sam, "Sam Rivera") + `. Say hi at the front desk on Tuesday.

- Pick up your member card
- Book a belay check
  - Top rope first
  - Lead later, after the course

1. Read the handbook
2. Sign the waiver
3. Join a Tuesday session

- [x] Create your account
- [ ] Watch the gym tour video
- [ ] Climb your first route

<details>
<summary>What should I bring?</summary>

Shoes and a chalk bag. Harnesses are free to borrow at the desk.
</details>

> The best climber in the gym is the one having the most fun.

> 💡 Tip: the quietest time to climb is weekday mornings before 10.

## Useful links
[Gym opening hours](https://example.com/hours)

` + "```text" + `
Front desk: 555-0100
Wi-Fi: RiversideGuest
` + "```" + `

---

_Questions? Leave a comment on this page._`},
		{key: "sam-notes", org: "riverside-climbing", title: "Sam's notes", icon: "📝", owner: sam, markdown: `
Private scratch space.

- [ ] Order new crash pads
- [ ] Draft the spring league rules`},
		{key: "jordan-notes", org: "riverside-climbing", title: "Route ideas", icon: "🪨", owner: jordan, markdown: `
Ideas for next month's set. Nobody else can see this page.

- A slab with only footholds on the left wall
- Pinch-heavy blue circuit`},
		{key: "priya-notes", org: "night-owls", title: "Books to suggest", icon: "📚", owner: priya, markdown: `
- _Piranesi_ by Susanna Clarke
- _The Dispossessed_ by Ursula K. Le Guin
- [ ] Check which ones the library has`},
		{key: "meetups", org: "night-owls", title: "Meetup notes", icon: "🦉", owner: priya, general: access.Edit, markdown: `
## November
We read _Station Eleven_. Next month: vote in the comments.`},
	}
	orgIDs := map[string]uuid.UUID{}
	for _, slug := range []string{"riverside-climbing", "night-owls"} {
		var id uuid.UUID
		err := s.d.Pool.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = $1`, slug).Scan(&id)
		if db.IsNoRows(err) {
			continue
		}
		if err != nil {
			return err
		}
		orgIDs[slug] = id
	}
	for i, sp := range pages {
		orgID, ok := orgIDs[sp.org]
		if !ok {
			continue
		}
		if err := s.seedPage(ctx, orgID, sp, i); err != nil {
			return fmt.Errorf("seed page %s: %w", sp.key, err)
		}
	}
	if orgID, ok := orgIDs["riverside-climbing"]; ok {
		if err := s.seedProjects(ctx, orgID, sam, jordan, priya); err != nil {
			return fmt.Errorf("seed projects: %w", err)
		}
		if err := s.seedComments(ctx, sam, jordan, priya); err != nil {
			return fmt.Errorf("seed comments: %w", err)
		}
		for _, f := range []struct {
			user uuid.UUID
			key  string
		}{{sam, "handbook"}, {jordan, "projects"}, {priya, "onboarding"}} {
			if _, err := s.d.Pool.Exec(ctx, `INSERT INTO docs_favorites (user_id, page_id) SELECT $1, $2 WHERE EXISTS (SELECT 1 FROM docs_pages WHERE id = $2)
				ON CONFLICT DO NOTHING`, f.user, seedID(f.key)); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedGrant shares a seeded page with access.AddGrant. A subject the share
// dialog would refuse (someone no longer in the org) is skipped, not fatal.
func seedGrant(ctx context.Context, tx pgx.Tx, ref access.Ref, subject access.SubjectRef, level access.Level, by uuid.UUID) error {
	err := access.AddGrant(ctx, tx, ref, subject, level, by)
	var refused *httpx.Error
	if errors.As(err, &refused) && refused.Status < 500 {
		slog.InfoContext(ctx, "docs: dev share skipped", "subject", subject.ID, "reason", refused.Message)
		return nil
	}
	return err
}

func (s *Service) seedPage(ctx context.Context, orgID uuid.UUID, sp seedPage, order int) error {
	id := seedID(sp.key)
	var parent *uuid.UUID
	if sp.parent != "" {
		pid := seedID(sp.parent)
		parent = &pid
	}
	fresh := false
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO docs_pages (id, org_id, parent_id, title, icon, position, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $7) ON CONFLICT (id) DO NOTHING`,
			id, orgID, parent, sp.title, sp.icon, keysAfter("", "", order+1)[order], sp.owner)
		if err != nil {
			return err
		}
		fresh = tag.RowsAffected() == 1
		res := access.NewResource{Kind: Kind, ID: id, OrgID: orgID, OwnerID: sp.owner, General: sp.general, Restricted: sp.restricted}
		if parent != nil {
			res.Parent = &access.Ref{Kind: Kind, ID: *parent}
			res.General = access.None
		}
		if err := access.CreateResource(ctx, tx, res); err != nil {
			return err
		}
		// Shares go through access (the share dialog's checks, nobody is
		// notified); a re-run never lowers a level.
		ref := access.Ref{Kind: Kind, ID: id}
		for u, lvl := range sp.grants {
			if err := seedGrant(ctx, tx, ref, access.SubjectRef{Type: "user", ID: u}, lvl, sp.owner); err != nil {
				return err
			}
		}
		if sp.team != "" {
			t, err := access.TeamByHandle(ctx, tx, orgID, sp.team)
			if err != nil {
				return err
			}
			if t != nil { // the access seed changed: share with nobody
				if err := seedGrant(ctx, tx, ref, access.SubjectRef{Type: "team", ID: t.ID}, access.Edit, sp.owner); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil || !fresh {
		return err
	}
	p, err := loadPage(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	ops := blockOps(markdownToBlocks(sp.markdown, 500), nil, "", "")
	if len(ops) == 0 {
		return nil
	}
	_, err = s.apply(ctx, sp.owner, p, ops, "seed")
	return err
}

func (s *Service) seedProjects(ctx context.Context, orgID, sam, jordan, priya uuid.UUID) error {
	dbID := seedID("projects")
	status := Property{ID: "status", Name: "Status", Type: "status", Options: []Option{
		{ID: "todo", Name: "Not started", Color: "neutral", Group: "todo"},
		{ID: "doing", Name: "In progress", Color: "info", Group: "doing"},
		{ID: "review", Name: "In review", Color: "warning", Group: "doing"},
		{ID: "done", Name: "Done", Color: "success", Group: "done"},
	}}
	schema := []Property{
		{ID: "title", Name: "Project", Type: "title"},
		status,
		{ID: "owner", Name: "Owner", Type: "person"},
		{ID: "due", Name: "Due", Type: "date"},
		{ID: "priority", Name: "Priority", Type: "select", Options: []Option{
			{ID: "low", Name: "Low", Color: "neutral"}, {ID: "medium", Name: "Medium", Color: "warning"}, {ID: "high", Name: "High", Color: "danger"}}},
		{ID: "tags", Name: "Tags", Type: "multi_select", Options: []Option{
			{ID: "youth", Name: "Youth", Color: "accent"}, {ID: "walls", Name: "Walls", Color: "primary"}, {ID: "events", Name: "Events", Color: "info"}, {ID: "admin", Name: "Admin", Color: "neutral"}}},
		{ID: "budget", Name: "Budget", Type: "number", NumberFormat: "dollar"},
		{ID: "approved", Name: "Approved", Type: "checkbox"},
	}
	type seedRow struct {
		key, title string
		values     map[string]any
	}
	rows := []seedRow{
		{"p-league", "Spring bouldering league", map[string]any{"status": "doing", "owner": []string{sam.String()}, "due": map[string]string{"start": "2026-11-20"}, "priority": "high", "tags": []string{"events"}, "budget": 1200, "approved": true}},
		{"p-wall", "New route-setting wall", map[string]any{"status": "review", "owner": []string{jordan.String()}, "due": map[string]string{"start": "2026-11-12"}, "priority": "medium", "tags": []string{"walls"}, "budget": 8400, "approved": true}},
		{"p-kit", "Youth team kit order", map[string]any{"status": "todo", "owner": []string{priya.String()}, "due": map[string]string{"start": "2026-11-28"}, "priority": "medium", "tags": []string{"youth", "admin"}, "budget": 650}},
		{"p-firstaid", "First-aid refresher", map[string]any{"status": "done", "owner": []string{sam.String(), jordan.String()}, "due": map[string]string{"start": "2026-10-15"}, "priority": "low", "tags": []string{"admin"}, "budget": 300, "approved": true}},
		{"p-website", "Website refresh", map[string]any{"status": "todo", "priority": "low", "tags": []string{"admin"}}},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(schema)
		tag, err := tx.Exec(ctx, `
			INSERT INTO docs_pages (id, org_id, kind, title, icon, position, schema, created_by, updated_by)
			VALUES ($1, $2, 'database', 'Projects', '🗂️', $3, $4, $5, $5) ON CONFLICT (id) DO NOTHING`, dbID, orgID, keysAfter("", "", 12)[11], raw, jordan)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: dbID, OrgID: orgID, OwnerID: jordan, General: access.Edit}); err != nil {
			return err
		}
		views := []struct {
			key, name, typ string
			cfg            ViewConfig
		}{
			{"v-table", "All projects", "table", ViewConfig{Sorts: []Sort{{Property: "due", Direction: "asc"}},
				Columns: []Column{{Property: "title", Width: 260}, {Property: "status"}, {Property: "owner"}, {Property: "due"}, {Property: "priority"}, {Property: "tags"}, {Property: "budget"}, {Property: "approved"}}}},
			{"v-board", "Board", "board", ViewConfig{GroupBy: "status", CardProperties: []string{"owner", "due", "priority"}}},
			{"v-calendar", "Calendar", "calendar", ViewConfig{DateProperty: "due"}},
			{"v-mine", "Open, high priority", "list", ViewConfig{Filters: []Filter{{Property: "priority", Op: "is", Value: json.RawMessage(`"high"`)}, {Property: "status", Op: "is_not", Value: json.RawMessage(`"done"`)}},
				CardProperties: []string{"status", "owner", "due"}}},
		}
		for i, v := range views {
			cfg, _ := json.Marshal(cleanConfig(schema, v.typ, v.cfg))
			if _, err := tx.Exec(ctx, `INSERT INTO docs_views (id, database_id, name, type, position, config, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (id) DO NOTHING`, seedID(v.key), dbID, v.name, v.typ, keysAfter("", "", len(views))[i], cfg, jordan); err != nil {
				return err
			}
		}
		keys := keysAfter("", "", len(rows))
		for i, r := range rows {
			id := seedID(r.key)
			props, _ := json.Marshal(r.values)
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
				VALUES ($1, $2, $3, 'page', true, $4, $5, $6, $7, $7) ON CONFLICT (id) DO NOTHING`, id, orgID, dbID, r.title, keys[i], props, jordan); err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: id, OrgID: orgID, OwnerID: jordan, Parent: &access.Ref{Kind: Kind, ID: dbID}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// seedComments adds a question with a reply on an onboarding block, and a
// resolved thread on the page.
func (s *Service) seedComments(ctx context.Context, sam, jordan, priya uuid.UUID) error {
	page := seedID("onboarding")
	var blockID *uuid.UUID
	var plain string
	err := s.d.Pool.QueryRow(ctx, `SELECT id, plain FROM docs_blocks WHERE page_id = $1 AND plain LIKE 'Your coach is%' LIMIT 1`, page).Scan(&blockID, &plain)
	if db.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	body := func(text string) []byte {
		b, _ := json.Marshal([]Span{{Text: text}})
		return b
	}
	mentionBody, _ := json.Marshal([]Span{{Text: "Yes! "}, {Text: "@Sam Rivera", Mention: &Mention{Type: "user", ID: sam.String()}}, {Text: " runs both. Tuesday is technique, Thursday is open coaching."}})
	threadA, threadB := seedID("comment-a"), seedID("comment-b")
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO docs_comments (id, page_id, block_id, quote, anchor_start, anchor_end, body, plain, author_id, created_at)
			VALUES ($1, $2, $3, $4, 0, 13, $5, $6, $7, now() - interval '2 days') ON CONFLICT (id) DO NOTHING`,
			[]any{threadA, page, blockID, "Your coach is", body("Is the coach the same for Tuesday and Thursday sessions?"), "Is the coach the same for Tuesday and Thursday sessions?", priya}},
		{`INSERT INTO docs_comments (id, page_id, thread_id, body, plain, author_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, now() - interval '1 day') ON CONFLICT (id) DO NOTHING`,
			[]any{seedID("comment-a-reply"), page, threadA, mentionBody, "Yes! @Sam Rivera runs both. Tuesday is technique, Thursday is open coaching.", jordan}},
		{`INSERT INTO docs_comments (id, page_id, body, plain, author_id, resolved_at, resolved_by, created_at)
			VALUES ($1, $2, $3, $4, $5, now() - interval '3 days', $6, now() - interval '5 days') ON CONFLICT (id) DO NOTHING`,
			[]any{threadB, page, body("The waiver link was broken, fixed now."), "The waiver link was broken, fixed now.", sam, jordan}},
	}
	for _, st := range stmts {
		if _, err := s.d.Pool.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}
