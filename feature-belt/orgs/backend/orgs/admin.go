package orgs

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
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

// AdminOrgDetail adds the owners and admins, and the claimed email domains.
type AdminOrgDetail struct {
	AdminOrg
	Leaders []Member      `json:"leaders"`
	Domains []AdminDomain `json:"domains"`
}

// AdminDomain is a claimed email domain as staff see it.
type AdminDomain struct {
	ID           uuid.UUID  `json:"id"`
	Domain       string     `json:"domain"`
	Policy       string     `json:"policy"`
	VerifyMethod *string    `json:"verify_method"`
	VerifiedAt   *time.Time `json:"verified_at"`
	CreatedAt    time.Time  `json:"created_at"`
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
// name, slug, id or a claimed email domain; member_id lists one person's orgs.
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
		WHERE ($1 = '' OR o.name ILIKE $2 OR o.slug ILIKE $2 OR o.id::text = $1
		       OR EXISTS (SELECT 1 FROM org_domains d WHERE d.org_id = o.id AND d.domain = lower($1)))
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
	rows, err = s.d.Pool.Query(r.Context(), `
		SELECT id, domain, policy, verify_method, verified_at, created_at FROM org_domains
		WHERE org_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return err
	}
	domains, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdminDomain])
	if err != nil {
		return err
	}
	if domains == nil {
		domains = []AdminDomain{}
	}
	httpx.OK(w, AdminOrgDetail{AdminOrg: list[0], Leaders: leaders, Domains: domains})
	return nil
}

// adminReleaseDomain: DELETE /v1/admin/orgs/{orgID}/domains/{domainID}
// (admin role, audited). Settles disputes: the domain is free to claim again.
func (s *Service) adminReleaseDomain(w http.ResponseWriter, r *http.Request) error {
	staff := auth.MustFromContext(r.Context())
	orgID, err := httpx.URLParamUUID(r, "orgID")
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "domainID")
	if err != nil {
		return err
	}
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		return deleteDomain(r.Context(), tx, staff.UserID, orgID, id, "orgs.domain.release")
	})
	if err != nil {
		return err
	}
	s.publish(context.WithoutCancel(r.Context()), orgTopic(orgID), EventDomainsChanged, map[string]any{"org_id": orgID})
	httpx.NoContent(w)
	return nil
}
