package access

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// DirectoryEntry is a person or team a picker can offer.
type DirectoryEntry struct {
	Type        string    `json:"type"` // user | team
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Handle      *string   `json:"handle"`
	AvatarURL   *string   `json:"avatar_url"`
	Email       *string   `json:"email"`        // org admins only
	MemberCount *int      `json:"member_count"` // teams
}

// directory: GET /v1/access/orgs/{orgID}/directory?q=&types=user,team&limit=
// (members): matching teams, then people, each by name. "@des" matches team
// handles only. Org admins also match and see email addresses.
func (s *Service) directory(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 80 {
		q = string([]rune(q)[:80])
	}
	limit := 8
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 25)
	}
	types := r.URL.Query().Get("types")
	wantUsers := types == "" || strings.Contains(types, "user")
	wantTeams := types == "" || strings.Contains(types, "team")
	handleOnly := strings.HasPrefix(q, "@")
	if handleOnly {
		q, wantUsers = strings.TrimPrefix(q, "@"), false
	}
	admin := orgs.RoleAtLeast(m.Role, orgs.RoleAdmin)
	pattern := likeEscape(q)

	out := []DirectoryEntry{}
	if wantTeams {
		rows, err := s.d.Pool.Query(ctx, `
			SELECT t.id, t.name, t.handle, t.avatar_key, (SELECT count(*) FROM access_team_members x WHERE x.team_id = t.id)::int
			FROM access_teams t
			WHERE t.org_id = $1 AND ($2 = '' OR t.handle ILIKE $2 || '%' OR (NOT $3 AND t.name ILIKE '%' || $2 || '%'))
			ORDER BY (t.handle ILIKE $2 || '%') DESC, lower(t.name), t.id LIMIT $4`, m.OrgID, pattern, handleOnly, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			e := DirectoryEntry{Type: "team"}
			var handle string
			var key *string
			var n int
			if err := rows.Scan(&e.ID, &e.Name, &handle, &key, &n); err != nil {
				rows.Close()
				return err
			}
			e.Handle, e.AvatarURL, e.MemberCount = &handle, s.publicURL(key), &n
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	if wantUsers {
		rows, err := s.d.Pool.Query(ctx, `
			SELECT u.id, u.display_name, u.avatar_url, u.email
			FROM org_members m JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1 AND u.status = 'active'
			  AND ($2 = '' OR u.display_name ILIKE '%' || $2 || '%' OR ($3 AND u.email ILIKE $2 || '%'))
			ORDER BY (u.id = $5) DESC, lower(u.display_name), u.id LIMIT $4`, m.OrgID, pattern, admin, limit, me)
		if err != nil {
			return err
		}
		for rows.Next() {
			e := DirectoryEntry{Type: "user"}
			var email string
			if err := rows.Scan(&e.ID, &e.Name, &e.AvatarURL, &email); err != nil {
				rows.Close()
				return err
			}
			if admin {
				e.Email = &email
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}
