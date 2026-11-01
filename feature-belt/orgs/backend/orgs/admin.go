package orgs

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// AdminOrg is an org as staff see it.
type AdminOrg struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	AvatarURL      *string   `json:"avatar_url"`
	MemberCount    int       `json:"member_count"`
	PendingInvites int       `json:"pending_invites"`
	CreatedAt      time.Time `json:"created_at"`
}

// AdminOrgDetail adds the owners and admins.
type AdminOrgDetail struct {
	AdminOrg
	Leaders []Member `json:"leaders"`
}

const adminOrgSelect = `
	SELECT o.id, o.name, o.slug, o.avatar_key,
	       (SELECT count(*) FROM org_members m WHERE m.org_id = o.id)::int,
	       (SELECT count(*) FROM org_invites i WHERE i.org_id = o.id AND i.status = 'pending' AND i.expires_at > now())::int,
	       o.created_at
	FROM orgs o`

func (s *Service) scanAdminOrgs(rows pgx.Rows) ([]AdminOrg, error) {
	defer rows.Close()
	out := []AdminOrg{}
	for rows.Next() {
		var (
			o   AdminOrg
			key *string
		)
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &key, &o.MemberCount, &o.PendingInvites, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.AvatarURL = s.avatarURL(key)
		out = append(out, o)
	}
	return out, rows.Err()
}

// adminListOrgs: GET /v1/admin/orgs?q=&member_id=, newest first. q matches
// name, slug or id; member_id lists one person's orgs.
func (s *Service) adminListOrgs(w http.ResponseWriter, r *http.Request) error {
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var memberID *uuid.UUID
	if v := r.URL.Query().Get("member_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid member_id.")
		}
		memberID = &id
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := s.d.Pool.Query(r.Context(), adminOrgSelect+`
		WHERE ($1 = '' OR o.name ILIKE $2 OR o.slug ILIKE $2 OR o.id::text = $1)
		  AND ($3::uuid IS NULL OR EXISTS (SELECT 1 FROM org_members m WHERE m.org_id = o.id AND m.user_id = $3))
		  AND ($4::timestamptz IS NULL OR (o.created_at, o.id) < ($4, $5))
		ORDER BY o.created_at DESC, o.id DESC LIMIT $6`, q, like, memberID, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := s.scanAdminOrgs(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(o AdminOrg) any { return cursor{o.CreatedAt, o.ID} }))
	return nil
}

// adminGetOrg: GET /v1/admin/orgs/{orgID} with owners and admins.
func (s *Service) adminGetOrg(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.URLParamUUID(r, "orgID")
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), adminOrgSelect+` WHERE o.id = $1`, id)
	if err != nil {
		return err
	}
	list, err := s.scanAdminOrgs(rows)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errOrgNotFound
	}
	rows, err = s.d.Pool.Query(r.Context(), memberSelect+`
		WHERE m.org_id = $1 AND m.role IN ('owner', 'admin')
		ORDER BY CASE m.role WHEN 'owner' THEN 1 ELSE 2 END, m.created_at LIMIT 100`, id)
	if err != nil {
		return err
	}
	leaders, err := scanMembers(rows, true)
	if err != nil && !db.IsNoRows(err) {
		return err
	}
	httpx.OK(w, AdminOrgDetail{AdminOrg: list[0], Leaders: leaders})
	return nil
}
