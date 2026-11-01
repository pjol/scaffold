package orgs

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
)

// SeedDev adds 2 orgs with every role and one pending email invite. users
// are admin, sam, jordan, priya (auth.DevIdentities order). The invite's
// accept URL is logged, since only its hash is stored.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	sam, jordan, priya := users[1], users[2], users[3]
	type seedOrg struct {
		name, slug string
		members    map[uuid.UUID]string
	}
	orgs := []seedOrg{
		{"Riverside Climbing Club", "riverside-climbing", map[uuid.UUID]string{sam: RoleOwner, jordan: RoleAdmin, priya: RoleMember}},
		{"Night Owls Book Club", "night-owls", map[uuid.UUID]string{priya: RoleOwner, sam: RoleMember}},
	}
	var inviteURL string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		for i, o := range orgs {
			var orgID uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO orgs (name, slug, created_by) VALUES ($1, $2, $3)
				ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name RETURNING id`,
				o.name, o.slug, firstOwner(o.members)).Scan(&orgID); err != nil {
				return err
			}
			for uid, role := range o.members {
				if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
					ON CONFLICT (org_id, user_id) DO NOTHING`, orgID, uid, role); err != nil {
					return err
				}
			}
			if i != 0 {
				continue
			}
			var pending bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_invites WHERE org_id = $1 AND email = 'casey@scaffoldapp.test')`,
				orgID).Scan(&pending); err != nil {
				return err
			}
			if pending {
				continue
			}
			token, hash := newToken(prefixInvite)
			if _, err := tx.Exec(ctx, `
				INSERT INTO org_invites (org_id, email, role, token_hash, invited_by, expires_at)
				VALUES ($1, 'casey@scaffoldapp.test', 'member', $2, $3, now() + make_interval(secs => $4))`,
				orgID, hash, sam, inviteTTL.Seconds()); err != nil {
				return err
			}
			inviteURL = s.inviteURL(token)
		}
		return nil
	})
	if err == nil && inviteURL != "" {
		slog.Info("orgs: dev invite for casey@scaffoldapp.test", "url", inviteURL)
	}
	return err
}

func firstOwner(members map[uuid.UUID]string) uuid.UUID {
	for uid, role := range members {
		if role == RoleOwner {
			return uid
		}
	}
	return uuid.Nil
}
