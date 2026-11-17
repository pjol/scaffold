package docs

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
)

// seedTasks adds a Tasks database related two-way to the seeded Projects
// (hours, done, a TASK- ID), and gives Projects a rollup (hours) and a
// formula (progress), so relations, rollups and formulas have data in
// development. Idempotent.
func (s *Service) seedTasks(ctx context.Context, orgID, sam, jordan, priya uuid.UUID) error {
	projects, tasks := seedID("projects"), seedID("tasks")
	taskSchema := []Property{
		{ID: "title", Name: "Task", Type: "title"},
		{ID: "project", Name: "Project", Type: "relation", Relation: &RelationConfig{DatabaseID: projects, TwoWay: true, MirrorProperty: "tasks", Limit: 1}},
		{ID: "hours", Name: "Hours", Type: "number", NumberFormat: "number"},
		{ID: "done", Name: "Done", Type: "checkbox"},
		{ID: "assignee", Name: "Assignee", Type: "person"},
		{ID: "tid", Name: "ID", Type: "unique_id", UniqueID: &UniqueIDConfig{Prefix: "TASK"}},
	}
	type task struct {
		key, title, project string
		hours               float64
		done                bool
		who                 uuid.UUID
	}
	list := []task{
		{"t-league-1", "Book the competition dates", "p-league", 2, true, sam},
		{"t-league-2", "Set the league routes", "p-league", 6, false, jordan},
		{"t-league-3", "Print score cards", "p-league", 1, false, priya},
		{"t-wall-1", "Order holds", "p-wall", 3, true, jordan},
		{"t-wall-2", "Bolt the new panels", "p-wall", 8, false, jordan},
		{"t-kit-1", "Collect sizes", "p-kit", 2, false, priya},
		{"t-fa-1", "Book the trainer", "p-firstaid", 1, true, sam},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		raw, _ := json.Marshal(taskSchema)
		tag, err := tx.Exec(ctx, `
			INSERT INTO docs_pages (id, org_id, kind, title, icon, position, schema, created_by, updated_by)
			VALUES ($1, $2, 'database', 'Tasks', '✅', $3, $4, $5, $5) ON CONFLICT (id) DO NOTHING`, tasks, orgID, keysAfter("", "", 13)[12], raw, jordan)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: tasks, OrgID: orgID, OwnerID: jordan, General: access.Edit}); err != nil {
			return err
		}
		views := []struct {
			key, name, typ string
			cfg            ViewConfig
		}{
			{"v-tasks", "All tasks", "table", ViewConfig{GroupBy: "project", Calculations: map[string]string{"hours": "sum", "done": "percent_checked"}}},
			{"v-tasks-chart", "Hours by project", "chart", ViewConfig{Chart: &ChartConfig{Type: "bar", X: "project", Y: "sum", YProperty: "hours"}}},
		}
		for i, v := range views {
			cfg, _ := json.Marshal(cleanConfigFor(taskSchema, tasks, v.typ, v.cfg))
			if _, err := tx.Exec(ctx, `INSERT INTO docs_views (id, database_id, name, type, position, config, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)
				ON CONFLICT (id) DO NOTHING`, seedID(v.key), tasks, v.name, v.typ, keysAfter("", "", len(views))[i], cfg, jordan); err != nil {
				return err
			}
		}
		byProject := map[string][]string{}
		keys := keysAfter("", "", len(list))
		for i, t := range list {
			id := seedID(t.key)
			pid := seedID(t.project).String()
			byProject[pid] = append(byProject[pid], id.String())
			props, _ := json.Marshal(map[string]any{"project": []string{pid}, "hours": t.hours, "done": t.done, "assignee": []string{t.who.String()}, "tid": i + 1})
			if _, err := tx.Exec(ctx, `
				INSERT INTO docs_pages (id, org_id, parent_id, kind, is_row, title, position, props, created_by, updated_by)
				VALUES ($1, $2, $3, 'page', true, $4, $5, $6, $7, $7) ON CONFLICT (id) DO NOTHING`, id, orgID, tasks, t.title, keys[i], props, jordan); err != nil {
				return err
			}
			if err := access.CreateResource(ctx, tx, access.NewResource{Kind: Kind, ID: id, OrgID: orgID, OwnerID: jordan, Parent: &access.Ref{Kind: Kind, ID: tasks}}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO docs_counters (database_id, property_id, last) VALUES ($1, 'tid', $2) ON CONFLICT DO NOTHING`, tasks, len(list)); err != nil {
			return err
		}
		// Projects: the other side of the relation, a rollup and a formula.
		var praw []byte
		if err := tx.QueryRow(ctx, `SELECT schema FROM docs_pages WHERE id = $1 FOR UPDATE`, projects).Scan(&praw); err != nil {
			return err
		}
		var pschema []Property
		_ = json.Unmarshal(praw, &pschema)
		if _, ok := findProp(pschema, "tasks"); !ok {
			pschema = append(pschema,
				Property{ID: "tasks", Name: "Tasks", Type: "relation", Relation: &RelationConfig{DatabaseID: tasks, TwoWay: true, MirrorProperty: "project"}},
				Property{ID: "hours", Name: "Hours", Type: "rollup", Rollup: &RollupConfig{Relation: "tasks", Property: "hours", Function: "sum", Type: "number"}},
				Property{ID: "progress", Name: "Progress", Type: "formula", NumberFormat: "percent", NumberDisplay: "bar", NumberMax: ptr(100.0),
					Formula: &FormulaConfig{Expression: `if(empty(prop("Tasks")), 0, round(prop("Tasks").filter(current.prop("Done")).length() / prop("Tasks").length() * 100))`, Type: "number"}},
			)
			b, _ := json.Marshal(pschema)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET schema = $2 WHERE id = $1`, projects, b); err != nil {
				return err
			}
		}
		for pid, ids := range byProject {
			b, _ := json.Marshal(ids)
			if _, err := tx.Exec(ctx, `UPDATE docs_pages SET props = jsonb_set(props, '{tasks}', $2::jsonb) WHERE id = $1`, uuid.MustParse(pid), b); err != nil {
				return err
			}
		}
		return nil
	})
}
