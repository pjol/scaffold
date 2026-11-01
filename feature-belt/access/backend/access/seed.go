package access

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
)

// SeedDev adds teams to the orgs the orgs feature seeds. users are admin,
// sam, jordan, priya (auth.DevIdentities order). Seeders run in feature file
// order and access sorts before orgs, so this runs the orgs seed first (it is
// idempotent).
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	for _, m := range s.d.Modules {
		if seeder, ok := m.(app.DevSeeder); ok && m.Name() == "orgs" {
			if err := seeder.SeedDev(ctx, users); err != nil {
				return fmt.Errorf("orgs seed: %w", err)
			}
		}
	}
	sam, jordan, priya := users[1], users[2], users[3]
	type seedTeam struct {
		org, name, handle, desc string
		members                 map[uuid.UUID]string
	}
	teams := []seedTeam{
		{"riverside-climbing", "Route setters", "route-setters", "Plan, set and strip the routes on the walls each month.",
			map[uuid.UUID]string{jordan: TeamLead, priya: TeamMember}},
		{"riverside-climbing", "Coaching", "coaching", "Runs the youth team and the Tuesday technique sessions.",
			map[uuid.UUID]string{sam: TeamLead, jordan: TeamMember}},
		{"night-owls", "Organizers", "organizers", "Picks the books and hosts the monthly meetups.",
			map[uuid.UUID]string{priya: TeamLead}},
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for _, t := range teams {
			var orgID uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = $1`, t.org).Scan(&orgID)
			if db.IsNoRows(err) {
				continue // the orgs seed changed; nothing to attach to
			}
			if err != nil {
				return err
			}
			var teamID uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO access_teams (org_id, name, handle, description) VALUES ($1, $2, $3, $4)
				ON CONFLICT (org_id, handle) DO UPDATE SET name = EXCLUDED.name RETURNING id`,
				orgID, t.name, t.handle, t.desc).Scan(&teamID); err != nil {
				return err
			}
			for uid, role := range t.members {
				if _, err := tx.Exec(ctx, `
					INSERT INTO access_team_members (team_id, user_id, role)
					SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM org_members WHERE org_id = $4 AND user_id = $2)
					ON CONFLICT (team_id, user_id) DO NOTHING`, teamID, uid, role, orgID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
