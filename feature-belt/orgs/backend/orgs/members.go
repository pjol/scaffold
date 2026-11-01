package orgs

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

// Member is one person in an org. Email is shown to admins and owners only.
type Member struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
	Email       *string   `json:"email"`
	AvatarURL   *string   `json:"avatar_url"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
}

const memberSelect = `
	SELECT m.user_id, u.display_name, u.email, u.avatar_url, m.role, m.created_at
	FROM org_members m JOIN users u ON u.id = m.user_id`

func scanMembers(rows pgx.Rows, showEmail bool) ([]Member, error) {
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Member])
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []Member{}
	}
	if !showEmail {
		for i := range list {
			list[i].Email = nil
		}
	}
	return list, nil
}

// listMembers: GET /v1/orgs/{orgID}/members, owners first, then by join date.
func (s *Service) listMembers(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	p := httpx.ParsePage(r)
	var c struct {
		R  int       `json:"r"`
		T  time.Time `json:"t"`
		ID uuid.UUID `json:"id"`
	}
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	const rankSQL = `CASE m.role WHEN 'owner' THEN 1 WHEN 'admin' THEN 2 ELSE 3 END`
	rows, err := s.d.Pool.Query(r.Context(), memberSelect+`
		WHERE m.org_id = $1 AND ($2::timestamptz IS NULL OR (`+rankSQL+`, m.created_at, m.user_id) > ($3, $2, $4))
		ORDER BY `+rankSQL+`, m.created_at, m.user_id LIMIT $5`,
		m.OrgID, nullTime(c.T), c.R, c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := scanMembers(rows, RoleAtLeast(m.Role, RoleAdmin))
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(x Member) any {
		return map[string]any{"r": 4 - rank[x.Role], "t": x.JoinedAt, "id": x.UserID}
	}))
	return nil
}

func (s *Service) loadMember(ctx context.Context, q db.Querier, orgID, userID uuid.UUID, showEmail bool) (*Member, error) {
	rows, err := q.Query(ctx, memberSelect+` WHERE m.org_id = $1 AND m.user_id = $2`, orgID, userID)
	if err != nil {
		return nil, err
	}
	list, err := scanMembers(rows, showEmail)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, httpx.NotFound("Member not found.")
	}
	return &list[0], nil
}

// lockOrg serialises membership changes in one org so the owner count can't
// race to zero.
func lockOrg(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (name string, err error) {
	err = tx.QueryRow(ctx, `SELECT name FROM orgs WHERE id = $1 FOR UPDATE`, orgID).Scan(&name)
	if db.IsNoRows(err) {
		return "", errOrgNotFound
	}
	return name, err
}

func ownerCount(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1 AND role = 'owner'`, orgID).Scan(&n)
	return n, err
}

type updateMemberReq struct {
	Role string `json:"role"`
}

// updateMember: PATCH /v1/orgs/{orgID}/members/{userID} {role}. Admins move
// people between member and admin; only owners grant or take away owner.
// The last owner can't be demoted.
func (s *Service) updateMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m := mustMembership(r)
	target, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	var req updateMemberReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if rank[req.Role] == 0 {
		return httpx.BadRequest("role must be owner, admin or member.")
	}
	var (
		orgName string
		changed bool
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		current, err := RoleOf(ctx, tx, m.OrgID, target)
		if err != nil {
			return err
		}
		if current == "" {
			return httpx.NotFound("Member not found.")
		}
		if current == req.Role {
			return nil
		}
		if (current == RoleOwner || req.Role == RoleOwner) && m.Role != RoleOwner {
			return httpx.Forbidden("Only owners can add or remove owners.")
		}
		if current == RoleOwner {
			n, err := ownerCount(ctx, tx, m.OrgID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return httpx.Conflict("An organization needs at least 1 owner. Make someone else an owner first.")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE org_members SET role = $3, updated_at = now() WHERE org_id = $1 AND user_id = $2`,
			m.OrgID, target, req.Role); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return err
	}
	me := auth.MustFromContext(ctx).UserID
	if changed {
		bg := context.WithoutCancel(ctx)
		s.membershipChanged(bg, m.OrgID, target, EventMemberUpdated, map[string]any{"role": req.Role})
		if target != me {
			s.notifyRole(bg, target, m.OrgID, orgName, req.Role)
		}
	}
	mem, err := s.loadMember(ctx, s.d.Pool, m.OrgID, target, true)
	if err != nil {
		return err
	}
	httpx.OK(w, mem)
	return nil
}

// removeMember: DELETE /v1/orgs/{orgID}/members/{userID}. Your own id leaves
// the org. Removing someone else needs a higher role (owners remove anyone).
// The last owner can't leave while others remain.
func (s *Service) removeMember(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	target, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	self := target == me
	var orgName string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		role, err := RoleOf(ctx, tx, m.OrgID, target)
		if err != nil {
			return err
		}
		if role == "" {
			return httpx.NotFound("Member not found.")
		}
		if !self && m.Role != RoleOwner && rank[m.Role] <= rank[role] {
			return httpx.Forbidden("You can only remove people with a lower role than yours.")
		}
		if role == RoleOwner {
			n, err := ownerCount(ctx, tx, m.OrgID)
			if err != nil {
				return err
			}
			if n <= 1 {
				var members int
				if err := tx.QueryRow(ctx, `SELECT count(*) FROM org_members WHERE org_id = $1`, m.OrgID).Scan(&members); err != nil {
					return err
				}
				if members <= 1 {
					return httpx.Conflict("You're the only member. Delete the organization instead.")
				}
				return httpx.Conflict("Make someone else an owner before you leave.")
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, m.OrgID, target)
		return err
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.membershipChanged(bg, m.OrgID, target, EventMemberRemoved, map[string]any{})
	if !self {
		s.send(bg, notify.Notification{UserID: target, Kind: KindRole, Title: "You were removed from " + orgName, Link: "/settings/orgs",
			Data: map[string]any{"org_id": m.OrgID}})
	}
	httpx.NoContent(w)
	return nil
}

type transferReq struct {
	UserID uuid.UUID `json:"user_id"`
}

// transferOwnership: POST /v1/orgs/{orgID}/transfer {user_id} (owner). The
// member becomes an owner and the caller steps down to admin.
func (s *Service) transferOwnership(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	var req transferReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.UserID == uuid.Nil || req.UserID == me {
		return httpx.BadRequest("Choose another member to make owner.")
	}
	var orgName string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if orgName, err = lockOrg(ctx, tx, m.OrgID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE org_members SET role = 'owner', updated_at = now() WHERE org_id = $1 AND user_id = $2`,
			m.OrgID, req.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("Member not found.")
		}
		_, err = tx.Exec(ctx, `UPDATE org_members SET role = 'admin', updated_at = now() WHERE org_id = $1 AND user_id = $2`,
			m.OrgID, me)
		return err
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	s.membershipChanged(bg, m.OrgID, req.UserID, EventMemberUpdated, map[string]any{"role": RoleOwner})
	s.membershipChanged(bg, m.OrgID, me, EventMemberUpdated, map[string]any{"role": RoleAdmin})
	s.notifyRole(bg, req.UserID, m.OrgID, orgName, RoleOwner)
	o, err := s.loadOrg(ctx, m.OrgID, me)
	if err != nil {
		return err
	}
	httpx.OK(w, o)
	return nil
}

func (s *Service) notifyRole(ctx context.Context, userID, orgID uuid.UUID, orgName, role string) {
	title := "You're now a member of " + orgName
	switch role {
	case RoleOwner:
		title = "You're now an owner of " + orgName
	case RoleAdmin:
		title = "You're now an admin of " + orgName
	}
	s.send(ctx, notify.Notification{UserID: userID, Kind: KindRole, Title: title, Link: "/settings/orgs/" + orgID.String(),
		Data: map[string]any{"org_id": orgID, "role": role}})
}
