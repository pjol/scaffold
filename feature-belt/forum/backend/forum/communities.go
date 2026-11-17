package forum

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// DefaultRules are shown for communities without their own.
var DefaultRules = []string{
	"Be civil: no harassment, slurs or threats.",
	"No scams, giveaways, or requests for passwords, codes or recovery phrases.",
	"No spam, vote rings or repeated self-promotion.",
	"Keep it on topic for the community.",
}

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

const communityCols = `c.id, c.slug, c.title, c.description, c.rules, c.locked, c.ref_kind, c.ref_id, c.created_at`

func scanCommunity(row pgx.Row, extra ...any) (Community, error) {
	var c Community
	dest := append([]any{&c.ID, &c.Slug, &c.Title, &c.Description, &c.Rules, &c.Locked, &c.RefKind, &c.RefID, &c.CreatedAt}, extra...)
	err := row.Scan(dest...)
	if len(c.Rules) == 0 {
		c.Rules = DefaultRules
	}
	return c, err
}

// listCommunities: every community, the most active first.
func (s *Service) listCommunities(ctx context.Context) ([]Community, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+communityCols+`,
		  (SELECT count(*) FROM forum_posts p WHERE p.community_id = c.id AND p.status = 'visible'
		     AND p.created_at > now() - interval '7 days')
		FROM forum_communities c
		ORDER BY 10 DESC, c.title, c.id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Community{}
	for rows.Next() {
		var n int
		c, err := scanCommunity(rows, &n)
		if err != nil {
			return nil, err
		}
		c.Posts7d = n
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Service) listCommunitiesHandler(w http.ResponseWriter, r *http.Request) error {
	list, err := s.listCommunities(r.Context())
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) communityBySlug(ctx context.Context, q db.Querier, slug string) (Community, error) {
	c, err := scanCommunity(q.QueryRow(ctx, `SELECT `+communityCols+` FROM forum_communities c WHERE c.slug = $1`, strings.ToLower(slug)))
	if db.IsNoRows(err) {
		return c, httpx.NotFound("That community doesn't exist.")
	}
	return c, err
}

func (s *Service) communityByID(ctx context.Context, q db.Querier, id uuid.UUID) (Community, error) {
	c, err := scanCommunity(q.QueryRow(ctx, `SELECT `+communityCols+` FROM forum_communities c WHERE c.id = $1`, id))
	if db.IsNoRows(err) {
		return c, httpx.NotFound("That community doesn't exist.")
	}
	return c, err
}

// canModerate: staff, or a moderator of the community.
func (s *Service) canModerate(ctx context.Context, v viewer, communityID uuid.UUID) (bool, error) {
	if v.staff {
		return true, nil
	}
	var ok bool
	err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM forum_moderators WHERE community_id = $1 AND user_id = $2)`,
		communityID, v.id).Scan(&ok)
	return ok, err
}

func (s *Service) moderators(ctx context.Context, communityID uuid.UUID) ([]Author, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url FROM forum_moderators m JOIN users u ON u.id = m.user_id
		WHERE m.community_id = $1 AND u.status <> 'deleted' ORDER BY m.created_at, u.id`, communityID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Author])
	if out == nil {
		out = []Author{}
	}
	return out, err
}

func (s *Service) getCommunityHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	v, err := s.viewerOf(ctx, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	c, err := s.communityBySlug(ctx, s.d.Pool, chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM forum_posts WHERE community_id = $1 AND status = 'visible'
		AND created_at > now() - interval '7 days'`, c.ID).Scan(&c.Posts7d); err != nil {
		return err
	}
	out := CommunityDetail{Community: c}
	if out.Moderators, err = s.moderators(ctx, c.ID); err != nil {
		return err
	}
	if out.CanModerate, err = s.canModerate(ctx, v, c.ID); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+postCols+postFrom+`
		WHERE p.community_id = $1 AND p.pinned AND p.status = 'visible' AND NOT (p.author_id = ANY($2))
		ORDER BY p.created_at DESC, p.id DESC LIMIT 5`, c.ID, v.blocked)
	if err != nil {
		return err
	}
	pinned, err := scanPosts(rows)
	if err != nil {
		return err
	}
	if out.Pinned, err = s.summaries(ctx, v, pinned); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- EnsureCommunity: communities for the app's own objects ----

// CommunitySpec describes a community the app creates for one of its own
// objects (a product, a market, an event). RefKind + RefID identify the
// object; Slug defaults to one made from the title.
type CommunitySpec struct {
	RefKind     string
	RefID       string
	Slug        string
	Title       string
	Description string
	Rules       []string
}

// EnsureCommunity creates the community for an app object, or updates its
// title and description, and returns it. Idempotent on (RefKind, RefID); call
// it inside your own transaction (q) when the object is created.
func EnsureCommunity(ctx context.Context, q db.Querier, spec CommunitySpec) (Community, error) {
	spec.RefKind, spec.RefID = strings.TrimSpace(spec.RefKind), strings.TrimSpace(spec.RefID)
	spec.Title, spec.Description = strings.TrimSpace(spec.Title), strings.TrimSpace(spec.Description)
	if spec.RefKind == "" || spec.RefID == "" || spec.Title == "" {
		return Community{}, fmt.Errorf("forum: EnsureCommunity needs RefKind, RefID and Title")
	}
	slug := strings.ToLower(strings.TrimSpace(spec.Slug))
	if slug == "" {
		slug = slugify(spec.Title)
	}
	if !slugRe.MatchString(slug) {
		return Community{}, fmt.Errorf("forum: EnsureCommunity slug %q must be 1-64 of a-z, 0-9 and hyphens", slug)
	}
	rules := spec.Rules
	if rules == nil {
		rules = []string{}
	}
	c, err := scanCommunity(q.QueryRow(ctx, `
		INSERT INTO forum_communities AS c (slug, title, description, rules, ref_kind, ref_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (ref_kind, ref_id) WHERE ref_kind IS NOT NULL
		DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description, updated_at = now()
		RETURNING `+communityCols, slug, clipRunes(spec.Title, 100), clipRunes(spec.Description, 500), rules, spec.RefKind, spec.RefID))
	if db.IsUniqueViolation(err) {
		return Community{}, fmt.Errorf("forum: EnsureCommunity: slug %q is taken by another community", slug)
	}
	return c, err
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// slugify makes "Bread & Baking!" into "bread-baking".
func slugify(title string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	if s == "" {
		s = "c-" + uuid.NewString()[:8]
	}
	return s
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---- admin: communities and their moderators ----

// AdminCommunity is a community as staff manage it.
type AdminCommunity struct {
	Community
	Moderators []Author `json:"moderators"`
	PostCount  int      `json:"post_count"`
}

func (s *Service) adminListCommunities(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+communityCols+`,
		  (SELECT count(*) FROM forum_posts p WHERE p.community_id = c.id AND p.status = 'visible' AND p.created_at > now() - interval '7 days'),
		  (SELECT count(*) FROM forum_posts p WHERE p.community_id = c.id AND p.status <> 'deleted')
		FROM forum_communities c ORDER BY c.title, c.id LIMIT 500`)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []AdminCommunity{}
	for rows.Next() {
		var a AdminCommunity
		var n7, total int
		c, err := scanCommunity(rows, &n7, &total)
		if err != nil {
			return err
		}
		c.Posts7d = n7
		a.Community, a.PostCount = c, total
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for i := range out {
		if out[i].Moderators, err = s.moderators(ctx, out[i].ID); err != nil {
			return err
		}
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

type communityReq struct {
	Slug        *string   `json:"slug"`
	Title       *string   `json:"title"`
	Description *string   `json:"description"`
	Rules       *[]string `json:"rules"`
	Locked      *bool     `json:"locked"`
}

func (req *communityReq) validate(create bool) error {
	if create && (req.Slug == nil || req.Title == nil) {
		return httpx.BadRequest("slug and title are required.")
	}
	if req.Slug != nil {
		v := strings.ToLower(strings.TrimSpace(*req.Slug))
		if !slugRe.MatchString(v) {
			return httpx.BadRequest("The address can use a-z, 0-9 and hyphens, up to 64 characters.")
		}
		req.Slug = &v
	}
	if req.Title != nil {
		v := strings.TrimSpace(*req.Title)
		if n := utf8.RuneCountInString(v); n < 2 || n > 100 {
			return httpx.BadRequest("The name needs 2 to 100 characters.")
		}
		req.Title = &v
	}
	if req.Description != nil {
		v := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(v) > 500 {
			return httpx.BadRequest("Keep the description under 500 characters.")
		}
		req.Description = &v
	}
	if req.Rules != nil {
		rules := []string{}
		for _, x := range *req.Rules {
			if x = strings.TrimSpace(x); x == "" {
				continue
			}
			if utf8.RuneCountInString(x) > 200 {
				return httpx.BadRequest("Keep each rule under 200 characters.")
			}
			rules = append(rules, x)
		}
		if len(rules) > 10 {
			return httpx.BadRequest("A community has 10 rules at most.")
		}
		req.Rules = &rules
	}
	return nil
}

// adminCreateCommunity: POST /v1/admin/forum/communities. Audited.
func (s *Service) adminCreateCommunity(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	var req communityReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(true); err != nil {
		return err
	}
	desc, rules, locked := "", []string{}, false
	if req.Description != nil {
		desc = *req.Description
	}
	if req.Rules != nil {
		rules = *req.Rules
	}
	if req.Locked != nil {
		locked = *req.Locked
	}
	var c Community
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		c, err = scanCommunity(tx.QueryRow(ctx, `
			INSERT INTO forum_communities AS c (slug, title, description, rules, locked, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+communityCols, *req.Slug, *req.Title, desc, rules, locked, staff.UserID))
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A community already uses that address.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "forum.community.create", "forum_community", c.ID.String(),
			map[string]any{"slug": c.Slug, "title": c.Title})
	})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

// adminUpdateCommunity: PATCH /v1/admin/forum/communities/{id}. Audited.
func (s *Service) adminUpdateCommunity(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req communityReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if err := req.validate(false); err != nil {
		return err
	}
	var c Community
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		before, err := s.communityByID(ctx, tx, id)
		if err != nil {
			return err
		}
		c, err = scanCommunity(tx.QueryRow(ctx, `
			UPDATE forum_communities c SET slug = COALESCE($2, slug), title = COALESCE($3, title),
			  description = COALESCE($4, description), rules = COALESCE($5, rules), locked = COALESCE($6, locked), updated_at = now()
			WHERE id = $1 RETURNING `+communityCols, id, req.Slug, req.Title, req.Description, req.Rules, req.Locked))
		if db.IsUniqueViolation(err) {
			return httpx.Conflict("A community already uses that address.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "forum.community.update", "forum_community", id.String(), map[string]any{
			"before": map[string]any{"slug": before.Slug, "title": before.Title, "locked": before.Locked},
			"after":  map[string]any{"slug": c.Slug, "title": c.Title, "locked": c.Locked},
		})
	})
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

// adminAddModerator: POST /v1/admin/forum/communities/{id}/moderators {email}. Audited.
func (s *Service) adminAddModerator(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return httpx.BadRequest("Enter the person's email address.")
	}
	var a Author
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := s.communityByID(ctx, tx, id); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE lower(email) = $1 AND status = 'active'`, email).
			Scan(&a.ID, &a.DisplayName, &a.AvatarURL)
		if db.IsNoRows(err) {
			return httpx.NotFound("No active account uses that email address.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO forum_moderators (community_id, user_id, added_by) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, id, a.ID, staff.UserID); err != nil {
			return err
		}
		return audit.Log(ctx, tx, staff.UserID, "forum.moderator.add", "forum_community", id.String(), map[string]any{"user_id": a.ID})
	})
	if err != nil {
		return err
	}
	httpx.OK(w, a)
	return nil
}

// adminRemoveModerator: DELETE /v1/admin/forum/communities/{id}/moderators/{user_id}. Audited.
func (s *Service) adminRemoveModerator(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	staff := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	userID, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM forum_moderators WHERE community_id = $1 AND user_id = $2`, id, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		return audit.Log(ctx, tx, staff.UserID, "forum.moderator.remove", "forum_community", id.String(), map[string]any{"user_id": userID})
	})
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// sinceFor is the start of a top period.
func sinceFor(t string) (time.Time, bool) {
	now := time.Now()
	switch t {
	case "day":
		return now.Add(-24 * time.Hour), true
	case "week":
		return now.Add(-7 * 24 * time.Hour), true
	case "month":
		return now.AddDate(0, -1, 0), true
	case "year":
		return now.AddDate(-1, 0, 0), true
	case "", "all":
		return time.Time{}, true
	}
	return time.Time{}, false
}
