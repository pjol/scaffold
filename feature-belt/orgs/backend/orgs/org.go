package orgs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// Org is an organization as one of its members sees it.
type Org struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	AvatarURL   *string   `json:"avatar_url"`
	Role        string    `json:"role"` // the caller's role
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
	JoinedAt    time.Time `json:"joined_at"`
}

// scanOrgs reads the columns orgSelect lists.
func (s *Service) scanOrgs(rows pgx.Rows) ([]Org, error) {
	defer rows.Close()
	out := []Org{}
	for rows.Next() {
		var (
			o   Org
			key *string
		)
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &key, &o.Role, &o.MemberCount, &o.CreatedAt, &o.JoinedAt); err != nil {
			return nil, err
		}
		o.AvatarURL = s.avatarURL(key)
		out = append(out, o)
	}
	return out, rows.Err()
}

const orgSelect = `
	SELECT o.id, o.name, o.slug, o.avatar_key, m.role,
	       (SELECT count(*) FROM org_members x WHERE x.org_id = o.id)::int, o.created_at, m.created_at
	FROM org_members m JOIN orgs o ON o.id = m.org_id`

func (s *Service) avatarURL(key *string) *string {
	if key == nil || *key == "" {
		return nil
	}
	u := s.d.Storage.PublicURL(*key)
	return &u
}

func (s *Service) loadOrg(ctx context.Context, orgID, userID uuid.UUID) (*Org, error) {
	rows, err := s.d.Pool.Query(ctx, orgSelect+` WHERE m.org_id = $1 AND m.user_id = $2`, orgID, userID)
	if err != nil {
		return nil, err
	}
	list, err := s.scanOrgs(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errOrgNotFound
	}
	return &list[0], nil
}

// listMine: GET /v1/orgs, the caller's orgs in the order they joined.
func (s *Service) listMine(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), orgSelect+`
		WHERE m.user_id = $1 AND ($2::timestamptz IS NULL OR (m.created_at, m.org_id) > ($2, $3))
		ORDER BY m.created_at, m.org_id LIMIT $4`, me, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := s.scanOrgs(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(o Org) any { return cursor{o.JoinedAt, o.ID} }))
	return nil
}

func (s *Service) getOrg(w http.ResponseWriter, r *http.Request) error {
	m := mustMembership(r)
	o, err := s.loadOrg(r.Context(), m.OrgID, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, o)
	return nil
}

type createOrgReq struct {
	Name      string  `json:"name"`
	Slug      *string `json:"slug"`
	AvatarKey *string `json:"avatar_key"`
}

// createOrg: POST /v1/orgs. The creator is the first owner. Without a slug,
// one is made from the name (with a short suffix if it's taken).
func (s *Service) createOrg(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req createOrgReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	explicit := req.Slug != nil && strings.TrimSpace(*req.Slug) != ""
	slug := slugify(name)
	if explicit {
		if slug, err = cleanSlug(*req.Slug); err != nil {
			return err
		}
	}
	if req.AvatarKey != nil && *req.AvatarKey != "" && !storage.KeyOwnedBy(*req.AvatarKey, AvatarPurpose, me) {
		return httpx.BadRequest("Upload the picture first (purpose org_avatar), then send its key.")
	}
	var orgID uuid.UUID
	for attempt := 0; ; attempt++ {
		candidate := slug
		if attempt > 0 {
			candidate = trimSlug(slug, 35) + "-" + randomSuffix()
		}
		err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO orgs (name, slug, avatar_key, created_by) VALUES ($1, $2, NULLIF($3, ''), $4)
				RETURNING id`, name, candidate, deref(req.AvatarKey), me).Scan(&orgID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, orgID, me)
			return err
		})
		if !db.IsUniqueViolation(err, "orgs_slug_key") {
			break
		}
		if explicit {
			return httpx.Conflict("That URL name is taken. Try another.")
		}
		if attempt >= 4 {
			return httpx.Conflict("Couldn't find a free URL name. Choose one.")
		}
	}
	if err != nil {
		return err
	}
	o, err := s.loadOrg(ctx, orgID, me)
	if err != nil {
		return err
	}
	s.publish(ctx, realtime.UserTopic(me), EventMembershipChanged, map[string]any{"org_id": orgID})
	httpx.JSON(w, http.StatusCreated, o)
	return nil
}

type updateOrgReq struct {
	Name      *string `json:"name"`
	Slug      *string `json:"slug"`
	AvatarKey *string `json:"avatar_key"` // "" removes the picture
}

// updateOrg: PATCH /v1/orgs/{orgID} (admin+).
func (s *Service) updateOrg(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m := mustMembership(r)
	var req updateOrgReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		name, err := cleanName(*req.Name)
		if err != nil {
			return err
		}
		req.Name = &name
	}
	if req.Slug != nil {
		slug, err := cleanSlug(*req.Slug)
		if err != nil {
			return err
		}
		req.Slug = &slug
	}
	if req.AvatarKey != nil && *req.AvatarKey != "" && !storage.KeyOwnedBy(*req.AvatarKey, AvatarPurpose, me) {
		return httpx.BadRequest("Upload the picture first (purpose org_avatar), then send its key.")
	}
	var oldKey *string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT avatar_key FROM orgs WHERE id = $1 FOR UPDATE`, m.OrgID).Scan(&oldKey); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE orgs SET name = COALESCE($2, name), slug = COALESCE($3, slug),
			  avatar_key = CASE WHEN $4::text IS NULL THEN avatar_key ELSE NULLIF($4, '') END,
			  updated_at = now()
			WHERE id = $1`, m.OrgID, req.Name, req.Slug, req.AvatarKey)
		return err
	})
	if db.IsUniqueViolation(err, "orgs_slug_key") {
		return httpx.Conflict("That URL name is taken. Try another.")
	}
	if err != nil {
		return err
	}
	if req.AvatarKey != nil && oldKey != nil && *oldKey != *req.AvatarKey {
		if err := s.d.Storage.Delete(context.WithoutCancel(ctx), *oldKey); err != nil {
			slog.WarnContext(ctx, "orgs: delete old avatar", "org_id", m.OrgID, "err", err)
		}
	}
	o, err := s.loadOrg(ctx, m.OrgID, me)
	if err != nil {
		return err
	}
	s.publish(ctx, orgTopic(m.OrgID), EventUpdated, map[string]any{"org_id": m.OrgID})
	httpx.OK(w, o)
	return nil
}

// deleteOrg: DELETE /v1/orgs/{orgID} (owner). Members, invites and links go
// with it.
func (s *Service) deleteOrg(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m := mustMembership(r)
	var (
		members []uuid.UUID
		key     *string
	)
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT avatar_key FROM orgs WHERE id = $1 FOR UPDATE`, m.OrgID).Scan(&key); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT user_id FROM org_members WHERE org_id = $1`, m.OrgID)
		if err != nil {
			return err
		}
		if members, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, m.OrgID)
		return err
	})
	if err != nil {
		return err
	}
	bg := context.WithoutCancel(ctx)
	if key != nil {
		if err := s.d.Storage.Delete(bg, *key); err != nil {
			slog.WarnContext(ctx, "orgs: delete avatar", "org_id", m.OrgID, "err", err)
		}
	}
	s.publish(bg, orgTopic(m.OrgID), EventDeleted, map[string]any{"org_id": m.OrgID})
	for _, u := range members {
		s.membershipChanged(bg, m.OrgID, u, EventMemberRemoved, map[string]any{})
	}
	fireMemberRemoved(bg, m.OrgID, members...)
	httpx.NoContent(w)
	return nil
}

// ---- validation --------------------------------------------------------------

func cleanName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(name); n == 0 || n > 80 {
		return "", httpx.BadRequest("Name must be 1 to 80 characters.")
	}
	return name, nil
}

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,38}[a-z0-9])$`)

// reservedSlugs would collide with app routes if orgs get URLs by slug.
var reservedSlugs = map[string]bool{
	"admin": true, "api": true, "app": true, "invites": true, "new": true, "settings": true,
	"orgs": true, "help": true, "support": true, "login": true, "signin": true, "signup": true,
}

func cleanSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !slugPattern.MatchString(slug) || strings.Contains(slug, "--") {
		return "", httpx.BadRequest("URL name must be 3 to 40 lowercase letters, numbers or single hyphens.")
	}
	if reservedSlugs[slug] {
		return "", httpx.Conflict("That URL name is taken. Try another.")
	}
	return slug, nil
}

// slugify turns a name into a valid slug: "Café Ünion!" → "cafe-union".
func slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(strings.ToLower(name)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := trimSlug(b.String(), 40)
	if len(slug) < 3 {
		slug = strings.Trim("org-"+slug+"-"+randomSuffix(), "-")
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if reservedSlugs[slug] {
		slug += "-" + randomSuffix()
	}
	return slug
}

func trimSlug(s string, n int) string {
	if len(s) > n {
		s = s[:n]
	}
	return strings.Trim(s, "-")
}

func randomSuffix() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
