package access

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount runs inside the account-deletion transaction. The user's team
// memberships, grants to them and permission overrides naming them are
// deleted; public links they made are revoked. Resources stay with the
// features that own them: their owner becomes empty, so org owners and admins
// manage them (personal resources become unreachable; their features delete
// them). Attribution columns (granted_by, created_by, …) are cleared.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM access_team_members WHERE user_id = $1`,
		`DELETE FROM access_grants WHERE subject_type = 'user' AND subject_id = $1`,
		`DELETE FROM access_permission_subjects WHERE subject_type = 'user' AND subject_id = $1`,
		`UPDATE access_links SET revoked_at = now(), revoked_by = NULL, updated_at = now() WHERE created_by = $1 AND revoked_at IS NULL`,
		`UPDATE access_links SET created_by = NULL WHERE created_by = $1`,
		`UPDATE access_links SET revoked_by = NULL WHERE revoked_by = $1`,
		`UPDATE access_resources SET owner_id = NULL, updated_at = now() WHERE owner_id = $1`,
		`UPDATE access_grants SET granted_by = NULL WHERE granted_by = $1`,
		`UPDATE access_team_members SET added_by = NULL WHERE added_by = $1`,
		`UPDATE access_teams SET created_by = NULL WHERE created_by = $1`,
		`UPDATE access_teams SET avatar_key = NULL, updated_at = now() WHERE avatar_key LIKE 'access_team_avatar/' || $1::text || '/%'`,
		`UPDATE access_permission_policies SET updated_by = NULL WHERE updated_by = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns the user's teams, what was shared with them, what
// they own, the public links they made (never tokens) and permissions that
// name them.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type team struct {
		TeamID   uuid.UUID `json:"team_id"`
		OrgID    uuid.UUID `json:"org_id"`
		Name     string    `json:"name"`
		Handle   string    `json:"handle"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	type grant struct {
		Kind      string    `json:"kind"`
		ID        uuid.UUID `json:"id"`
		Level     int16     `json:"level"`
		CreatedAt time.Time `json:"created_at"`
	}
	type owned struct {
		Kind      string     `json:"kind"`
		ID        uuid.UUID  `json:"id"`
		OrgID     *uuid.UUID `json:"org_id"`
		CreatedAt time.Time  `json:"created_at"`
	}
	type link struct {
		Kind      string     `json:"kind"`
		ID        uuid.UUID  `json:"id"`
		Level     int16      `json:"level"`
		ExpiresAt *time.Time `json:"expires_at"`
		RevokedAt *time.Time `json:"revoked_at"`
		CreatedAt time.Time  `json:"created_at"`
	}
	type perm struct {
		OrgID uuid.UUID `json:"org_id"`
		Key   string    `json:"key"`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT t.id, t.org_id, t.name, t.handle, tm.role, tm.created_at
		FROM access_team_members tm JOIN access_teams t ON t.id = tm.team_id WHERE tm.user_id = $1 ORDER BY tm.created_at`, userID)
	if err != nil {
		return nil, err
	}
	teams, err := pgx.CollectRows(rows, pgx.RowToStructByPos[team])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT resource_kind, resource_id, level, created_at FROM access_grants
		WHERE subject_type = 'user' AND subject_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	grants, err := pgx.CollectRows(rows, pgx.RowToStructByPos[grant])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT kind, id, org_id, created_at FROM access_resources WHERE owner_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	ownedList, err := pgx.CollectRows(rows, pgx.RowToStructByPos[owned])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT resource_kind, resource_id, level, expires_at, revoked_at, created_at FROM access_links
		WHERE created_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[link])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT org_id, key FROM access_permission_subjects WHERE subject_type = 'user' AND subject_id = $1 ORDER BY org_id, key`, userID)
	if err != nil {
		return nil, err
	}
	permsList, err := pgx.CollectRows(rows, pgx.RowToStructByPos[perm])
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"teams":                orEmpty(teams),
		"shared_with_me":       orEmpty(grants),
		"owned_resources":      orEmpty(ownedList),
		"public_links_created": orEmpty(links),
		"permissions_granted":  orEmpty(permsList),
		"levels":               map[int]string{1: "view", 2: "comment", 3: "edit", 4: "manage"},
	}, nil
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
