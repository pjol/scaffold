package orgs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount runs inside the account-deletion transaction. The user leaves
// every org; where they were the only owner and others remain, the oldest
// admin (else the oldest member) becomes owner; orgs left empty are deleted.
// Invites and links they created keep working without their name, and org
// pictures they uploaded are cleared (their files are deleted with the
// account's storage). Their join requests and departure records are deleted;
// domains they added or verified stay with the org without their name.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT org_id, role FROM org_members WHERE user_id = $1 ORDER BY org_id`, userID)
	if err != nil {
		return err
	}
	type membership struct {
		OrgID uuid.UUID
		Role  string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[membership])
	if err != nil {
		return err
	}
	left := make([]uuid.UUID, 0, len(list))
	for _, m := range list {
		left = append(left, m.OrgID)
	}
	departedOnDeletion.Store(userID, left) // AccountDeleted fires OnMemberRemoved after the commit
	for _, m := range list {
		if _, err := lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		if m.Role == RoleOwner {
			n, err := ownerCount(ctx, tx, m.OrgID)
			if err != nil {
				return err
			}
			if n == 1 {
				if _, err := tx.Exec(ctx, `
					UPDATE org_members SET role = 'owner', updated_at = now()
					WHERE org_id = $1 AND user_id = (
					  SELECT user_id FROM org_members WHERE org_id = $1 AND user_id <> $2
					  ORDER BY (role = 'admin') DESC, created_at, user_id LIMIT 1)`, m.OrgID, userID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, m.OrgID, userID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM orgs WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM org_members WHERE org_id = $1)`,
			m.OrgID); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE orgs SET created_by = NULL WHERE created_by = $1`,
		`UPDATE orgs SET avatar_key = NULL, updated_at = now() WHERE avatar_key LIKE 'org_avatar/' || $1::text || '/%'`,
		`UPDATE org_invites SET invited_by = NULL WHERE invited_by = $1`,
		`UPDATE org_invites SET accepted_by = NULL WHERE accepted_by = $1`,
		`UPDATE org_invite_links SET created_by = NULL WHERE created_by = $1`,
		`DELETE FROM org_join_requests WHERE user_id = $1`,
		`UPDATE org_join_requests SET decided_by = NULL WHERE decided_by = $1`,
		`DELETE FROM org_departures WHERE user_id = $1`,
		`UPDATE org_domains SET added_by = NULL WHERE added_by = $1`,
		`UPDATE org_domains SET verified_by = NULL WHERE verified_by = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns the user's memberships, the pending invites they
// sent, the invite links they made, their join requests, the orgs they left
// or were removed from, and the email domains they added or verified.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type membership struct {
		OrgID    uuid.UUID `json:"org_id"`
		Name     string    `json:"name"`
		Slug     string    `json:"slug"`
		Role     string    `json:"role"`
		JoinedAt time.Time `json:"joined_at"`
	}
	type invite struct {
		OrgID     uuid.UUID `json:"org_id"`
		Email     string    `json:"email"`
		Role      string    `json:"role"`
		CreatedAt time.Time `json:"created_at"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	type link struct {
		OrgID     uuid.UUID  `json:"org_id"`
		Uses      int        `json:"uses"`
		MaxUses   *int       `json:"max_uses"`
		ExpiresAt *time.Time `json:"expires_at"`
		RevokedAt *time.Time `json:"revoked_at"`
		CreatedAt time.Time  `json:"created_at"`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT o.id, o.name, o.slug, m.role, m.created_at
		FROM org_members m JOIN orgs o ON o.id = m.org_id WHERE m.user_id = $1 ORDER BY m.created_at`, userID)
	if err != nil {
		return nil, err
	}
	memberships, err := pgx.CollectRows(rows, pgx.RowToStructByPos[membership])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT org_id, email, role, created_at, expires_at FROM org_invites
		WHERE invited_by = $1 AND status = 'pending' AND expires_at > now() ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	invites, err := pgx.CollectRows(rows, pgx.RowToStructByPos[invite])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT org_id, uses, max_uses, expires_at, revoked_at, created_at FROM org_invite_links
		WHERE created_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	links, err := pgx.CollectRows(rows, pgx.RowToStructByPos[link])
	if err != nil {
		return nil, err
	}
	type joinRequest struct {
		OrgID     uuid.UUID  `json:"org_id"`
		Domain    string     `json:"domain"`
		Status    string     `json:"status"`
		CreatedAt time.Time  `json:"created_at"`
		DecidedAt *time.Time `json:"decided_at"`
	}
	type departure struct {
		OrgID     uuid.UUID `json:"org_id"`
		Reason    string    `json:"reason"`
		CreatedAt time.Time `json:"created_at"`
	}
	type domain struct {
		OrgID        uuid.UUID  `json:"org_id"`
		Domain       string     `json:"domain"`
		AddedByYou   bool       `json:"added_by_you"`
		VerifyMethod *string    `json:"verify_method"`
		VerifiedAt   *time.Time `json:"verified_at"`
		CreatedAt    time.Time  `json:"created_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT org_id, domain, status, created_at, decided_at FROM org_join_requests WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	requests, err := pgx.CollectRows(rows, pgx.RowToStructByPos[joinRequest])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT org_id, reason, created_at FROM org_departures WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	departures, err := pgx.CollectRows(rows, pgx.RowToStructByPos[departure])
	if err != nil {
		return nil, err
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT org_id, domain, added_by IS NOT DISTINCT FROM $1, CASE WHEN verified_by = $1 THEN verify_method END,
		       CASE WHEN verified_by = $1 THEN verified_at END, created_at
		FROM org_domains WHERE added_by = $1 OR verified_by = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	domains, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain])
	if err != nil {
		return nil, err
	}
	if memberships == nil {
		memberships = []membership{}
	}
	if requests == nil {
		requests = []joinRequest{}
	}
	if departures == nil {
		departures = []departure{}
	}
	if domains == nil {
		domains = []domain{}
	}
	if invites == nil {
		invites = []invite{}
	}
	if links == nil {
		links = []link{}
	}
	return map[string]any{"memberships": memberships, "invites_sent": invites, "invite_links": links,
		"join_requests": requests, "departures": departures, "domains": domains}, nil
}
