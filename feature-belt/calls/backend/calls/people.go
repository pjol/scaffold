package calls

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Directory lists people the caller may call or invite, matching query (a
// name fragment or an exact email; "" = everyone, alphabetically). orgID
// narrows to one organization the caller belongs to.
type Directory func(ctx context.Context, q db.Querier, caller uuid.UUID, query string, orgID *uuid.UUID, limit int) ([]UserRef, error)

var directory Directory = orgDirectory

// SetDirectory replaces the people picker's source. Change it together with
// SetCallPolicy so the picker offers exactly who the policy allows.
func SetDirectory(d Directory) {
	if d != nil {
		directory = d
	}
}

// orgDirectory: active people who share an organization with the caller.
func orgDirectory(ctx context.Context, q db.Querier, caller uuid.UUID, query string, orgID *uuid.UUID, limit int) ([]UserRef, error) {
	query = strings.TrimSpace(query)
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	rows, err := q.Query(ctx, `
		SELECT DISTINCT u.id, u.display_name, u.avatar_url, u.status
		FROM org_members mine
		JOIN org_members theirs ON theirs.org_id = mine.org_id
		JOIN users u ON u.id = theirs.user_id
		WHERE mine.user_id = $1 AND u.id <> $1 AND u.status = 'active'
		  AND ($2::uuid IS NULL OR mine.org_id = $2)
		  AND ($3 = '' OR u.display_name ILIKE $4 OR lower(u.email) = lower($3))
		ORDER BY u.display_name, u.id LIMIT $5`, caller, orgID, query, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRef{}
	for rows.Next() {
		var id uuid.UUID
		var name, status string
		var avatar *string
		if err := rows.Scan(&id, &name, &avatar, &status); err != nil {
			return nil, err
		}
		out = append(out, userRef(id, name, avatar, status))
	}
	return out, rows.Err()
}

// people: GET /calls/people?q=&org_id= → Page[UserRef] (20 at most).
func (s *Service) people(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	var orgID *uuid.UUID
	if v := r.URL.Query().Get("org_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return httpx.BadRequest("Invalid org_id.")
		}
		role, err := orgs.RoleOf(ctx, s.d.Pool, id, me.UserID)
		if err != nil {
			return err
		}
		if role == "" {
			return httpx.NotFound(orgs.NounTitle() + " not found.")
		}
		orgID = &id
	}
	q := r.URL.Query().Get("q")
	if len(q) > 200 {
		return httpx.BadRequest("Search for at most 200 characters.")
	}
	list, err := directory(ctx, s.d.Pool, me.UserID, q, orgID, 20)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[UserRef]{Items: list})
	return nil
}
