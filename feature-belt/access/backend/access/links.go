package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Public link tokens are "al_" + 32 random bytes (base64url); only their
// sha256 is stored, so the URL is shown once, when the link is made.
const linkPrefix = "al_"

func newLinkToken() (token string, hash []byte) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	token = linkPrefix + base64.RawURLEncoding.EncodeToString(b[:])
	return token, hashToken(token)
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// validToken rejects strings that can't be a token before touching the
// database.
func validToken(token string) bool {
	return strings.HasPrefix(token, linkPrefix) && len(token) == len(linkPrefix)+43
}

func randomSuffix() string {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// ErrLinkNotFound is what ResolveLink and LinkLevel callers answer for a bad,
// revoked or expired link, or one public sharing no longer allows.
var ErrLinkNotFound = httpx.NewError(http.StatusNotFound, "link_not_found", "This link doesn't work anymore. Ask for a new one.")

// Link is a resolved public link.
type Link struct {
	ID        uuid.UUID  `json:"id"`
	Ref       Ref        `json:"ref"`
	Level     Level      `json:"level"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// ResolveLink checks a public link token and returns what it opens, or
// ErrLinkNotFound. It applies the live checks (revoked, expired, the
// creator still holding access.share_public and the kind's LinkPermission)
// and records the use (hourly). Use it in the kind's public route; LinkLevel
// covers resources under a shared parent.
func ResolveLink(ctx context.Context, q db.Querier, token string) (*Link, error) {
	if !validToken(token) {
		return nil, ErrLinkNotFound
	}
	hash := hashToken(token)
	var l Link
	var lvl int
	var org, creator *uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT k.id, k.resource_kind, k.resource_id, k.expires_at, r.org_id, k.created_by,
		       access_link_level(k.token_hash, k.resource_kind, k.resource_id, $2)
		FROM access_links k JOIN access_resources r ON r.kind = k.resource_kind AND r.id = k.resource_id
		WHERE k.token_hash = $1`, hash, shareDefaults()).Scan(&l.ID, &l.Ref.Kind, &l.Ref.ID, &l.ExpiresAt, &org, &creator, &lvl)
	if db.IsNoRows(err) || (err == nil && lvl == 0) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	if ok, err := linkPermitted(ctx, q, l.Ref.Kind, org, creator); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrLinkNotFound
	}
	l.Level = Level(lvl)
	_, _ = q.Exec(ctx, `UPDATE access_links SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 hour')`, l.ID)
	return &l, nil
}

// LinkLevel returns the level a public link token grants on a resource: the
// link's own resource or one inheriting from it (a file in a shared folder).
// None when it grants nothing there, including while the link's creator
// lacks the link resource kind's LinkPermission.
func LinkLevel(ctx context.Context, q db.Querier, token, kind string, id uuid.UUID) (Level, error) {
	if !validToken(token) {
		return None, nil
	}
	var lvl int
	var linkKind string
	var org, creator *uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT access_link_level($1, $2, $3, $4), k.resource_kind, r.org_id, k.created_by
		FROM access_links k JOIN access_resources r ON r.kind = k.resource_kind AND r.id = k.resource_id
		WHERE k.token_hash = $1`, hashToken(token), kind, id, shareDefaults()).Scan(&lvl, &linkKind, &org, &creator)
	if db.IsNoRows(err) || (err == nil && lvl == 0) {
		return None, nil
	}
	if err != nil {
		return None, err
	}
	if ok, err := linkPermitted(ctx, q, linkKind, org, creator); err != nil || !ok {
		return None, err
	}
	return Level(lvl), nil
}

// linkPermitted applies the link resource kind's LinkPermission to the link's
// creator, live. Kinds without one and personal resources pass.
func linkPermitted(ctx context.Context, q db.Querier, kind string, org, creator *uuid.UUID) (bool, error) {
	k, _ := KindOf(kind)
	if k.LinkPermission == "" || org == nil {
		return true, nil
	}
	if creator == nil {
		return false, nil
	}
	return Allowed(ctx, q, *org, *creator, k.LinkPermission)
}

// missingLinkPermission is the org permission user lacks to make a public
// link on a resource of k in org ("" when they may): access.share_public,
// then the kind's LinkPermission. Personal resources need neither.
func missingLinkPermission(ctx context.Context, q db.Querier, k Kind, org *uuid.UUID, user uuid.UUID) (string, error) {
	if org == nil {
		return "", nil
	}
	for _, key := range []string{PermSharePublic, k.LinkPermission} {
		if key == "" {
			continue
		}
		ok, err := Allowed(ctx, q, *org, user, key)
		if err != nil {
			return "", err
		}
		if !ok {
			return key, nil
		}
	}
	return "", nil
}

func shareDefaults() []string {
	p, _ := PermissionOf(PermSharePublic)
	return p.DefaultRoles
}

// LinkInfo is a public link as the people managing the resource see it. URL
// and Token appear only in the response that creates it.
type LinkInfo struct {
	ID         uuid.UUID  `json:"id"`
	Level      Level      `json:"level"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedBy  *Person    `json:"created_by"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	// Working is false while the creator lacks access.share_public or the
	// kind's LinkPermission (the link resolves again if they get it back
	// before the link expires).
	Working bool    `json:"working"`
	URL     *string `json:"url,omitempty"`
	Token   *string `json:"token,omitempty"`
}

func (s *Service) activeLinks(ctx context.Context, ref Ref) ([]LinkInfo, error) {
	k, _ := KindOf(ref.Kind)
	var linkDefaults []string
	if p, ok := PermissionOf(k.LinkPermission); ok {
		linkDefaults = p.DefaultRoles
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT l.id, l.level, l.expires_at, l.created_by, u.display_name, u.avatar_url, l.last_used_at, l.created_at,
		       r.org_id IS NULL OR (l.created_by IS NOT NULL AND access_permission_allowed(r.org_id, l.created_by, $3, $4)
		         AND ($5 = '' OR access_permission_allowed(r.org_id, l.created_by, $5, $6)))
		FROM access_links l
		JOIN access_resources r ON r.kind = l.resource_kind AND r.id = l.resource_id
		LEFT JOIN users u ON u.id = l.created_by
		WHERE l.resource_kind = $1 AND l.resource_id = $2 AND l.revoked_at IS NULL
		  AND (l.expires_at IS NULL OR l.expires_at > now())
		ORDER BY l.created_at DESC, l.id DESC`, ref.Kind, ref.ID, PermSharePublic, shareDefaults(), k.LinkPermission, linkDefaults)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkInfo{}
	for rows.Next() {
		var li LinkInfo
		var lvl int16
		var by *uuid.UUID
		var name *string
		var avatar *string
		if err := rows.Scan(&li.ID, &lvl, &li.ExpiresAt, &by, &name, &avatar, &li.LastUsedAt, &li.CreatedAt, &li.Working); err != nil {
			return nil, err
		}
		li.Level = Level(lvl)
		if by != nil && name != nil {
			li.CreatedBy = &Person{ID: *by, Name: *name, AvatarURL: avatar}
		}
		out = append(out, li)
	}
	return out, rows.Err()
}

// CreateLink makes a public link to ref, in the feature's transaction, for a
// feature's own flows and seeds. It applies the route's rules: the kind has a
// PublicPath; level is View (None means View) or Comment where the kind
// allows public comments; expiresAt (nil: never) is between a minute and a
// year away; at most 20 working links per resource; and by holds
// access.share_public and the kind's LinkPermission in the resource's org
// (links stop working without them).
// Rule breaks answer the route's 400/403/409 errors. It doesn't check by's
// level on ref (Require(…, Manage) first when a person asked for the link)
// and doesn't audit or notify. The returned link carries URL and Token: only
// the token's sha256 is stored, so this is the one time they exist.
func CreateLink(ctx context.Context, q db.Querier, ref Ref, level Level, expiresAt *time.Time, by uuid.UUID) (*LinkInfo, error) {
	k, ok := KindOf(ref.Kind)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownKind, ref.Kind)
	}
	if by == uuid.Nil {
		return nil, errors.New("access: CreateLink needs the person making the link")
	}
	if _, err := q.Exec(ctx, `SELECT 1 FROM access_resources WHERE kind = $1 AND id = $2 FOR UPDATE`, ref.Kind, ref.ID); err != nil {
		return nil, err
	}
	res, err := Lookup(ctx, q, ref)
	if err != nil {
		return nil, err
	}
	return insertLink(ctx, q, k, ref, res.OrgID, level, expiresAt, by)
}

// insertLink checks the link rules and stores a new link's hash. The caller
// holds the resource row (FOR UPDATE), so the working-link count is exact.
func insertLink(ctx context.Context, q db.Querier, k Kind, ref Ref, org *uuid.UUID, level Level, expiresAt *time.Time, by uuid.UUID) (*LinkInfo, error) {
	if level == None {
		level = View
	}
	if k.PublicPath == nil {
		return nil, httpx.NewError(http.StatusBadRequest, "public_links_unsupported", "This can't be shared with a public link.")
	}
	if level != View && !(level == Comment && k.PublicComment) {
		return nil, httpx.BadRequest("Public links can only let people view this.")
	}
	if missing, err := missingLinkPermission(ctx, q, k, org, by); err != nil {
		return nil, err
	} else if missing != "" {
		return nil, permissionRequired(missing)
	}
	if expiresAt != nil {
		if !expiresAt.After(time.Now().Add(time.Minute)) {
			return nil, httpx.BadRequest("Choose an expiry in the future.")
		}
		if expiresAt.After(time.Now().AddDate(1, 0, 1)) {
			return nil, httpx.BadRequest("Links can last up to a year.")
		}
	}
	var active int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM access_links WHERE resource_kind = $1 AND resource_id = $2
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, ref.Kind, ref.ID).Scan(&active); err != nil {
		return nil, err
	}
	if active >= maxActiveLinks {
		return nil, httpx.Conflict("This item has 20 working links. Revoke one first.")
	}
	token, hash := newLinkToken()
	l := &LinkInfo{Level: level, ExpiresAt: expiresAt, Working: true}
	if err := q.QueryRow(ctx, `
		INSERT INTO access_links (resource_kind, resource_id, token_hash, level, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		ref.Kind, ref.ID, hash, int(level), expiresAt, by).Scan(&l.ID, &l.CreatedAt); err != nil {
		return nil, err
	}
	url := linkURL(k, token)
	l.URL, l.Token = &url, &token
	return l, nil
}

type createLinkReq struct {
	Level     Level      `json:"level"`
	ExpiresAt *time.Time `json:"expires_at"` // optional, within a year
}

// createLink: POST /v1/access/resources/{kind}/{id}/links {level, expires_at?}
// (manage + access.share_public). Returns the link with url and token once.
func (s *Service) createLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req createLinkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var created *LinkInfo
	t, err := s.manage(ctx, r, "link_create", func(tx pgx.Tx, t *target) (map[string]any, error) {
		var err error
		if created, err = insertLink(ctx, tx, t.kind, t.ref, t.res.OrgID, req.Level, req.ExpiresAt, t.me); err != nil {
			return nil, err
		}
		return map[string]any{"link_id": created.ID, "level": created.Level.String(), "expires_at": req.ExpiresAt}, nil
	})
	if err != nil {
		return err
	}
	var p Person
	if err := s.d.Pool.QueryRow(ctx, `SELECT id, display_name, avatar_url FROM users WHERE id = $1`, t.me).Scan(&p.ID, &p.Name, &p.AvatarURL); err == nil {
		created.CreatedBy = &p
	}
	resourceChanged(context.WithoutCancel(ctx), t.ref, t.res.OrgID, nil)
	httpx.JSON(w, http.StatusCreated, created)
	return nil
}

// revokeLink: DELETE /v1/access/resources/{kind}/{id}/links/{linkID} (manage).
func (s *Service) revokeLink(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	linkID, err := httpx.URLParamUUID(r, "linkID")
	if err != nil {
		return err
	}
	t, err := s.manage(ctx, r, "link_revoke", func(tx pgx.Tx, t *target) (map[string]any, error) {
		tag, err := tx.Exec(ctx, `UPDATE access_links SET revoked_at = now(), revoked_by = $4, updated_at = now()
			WHERE id = $1 AND resource_kind = $2 AND resource_id = $3 AND revoked_at IS NULL`, linkID, t.ref.Kind, t.ref.ID, t.me)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, httpx.NotFound("That link was already revoked.")
		}
		return map[string]any{"link_id": linkID}, nil
	})
	if err != nil {
		return err
	}
	resourceChanged(context.WithoutCancel(ctx), t.ref, t.res.OrgID, nil)
	httpx.NoContent(w)
	return nil
}

// webBase is where public links open: PUBLIC_WEB_URL, or the first CORS
// origin in development. New sets it.
var webBase atomic.Value // string

func setWebBase(cfg *config.Config) {
	base := cfg.PublicWebURL
	if base == "" && len(cfg.CORSOrigins) > 0 {
		base = cfg.CORSOrigins[0]
	}
	webBase.Store(strings.TrimRight(base, "/"))
}

// linkURL is the full public URL a link token opens.
func linkURL(k Kind, token string) string {
	base, _ := webBase.Load().(string)
	return base + k.PublicPath(token)
}

// LinkTarget is GET /v1/access/links/{token}: what a public link opens.
type LinkTarget struct {
	Kind      string     `json:"kind"`
	ID        uuid.UUID  `json:"id"`
	Label     string     `json:"label"`
	Title     string     `json:"title"`
	Level     Level      `json:"level"`
	Path      *string    `json:"path"` // the kind's public page for this token
	ExpiresAt *time.Time `json:"expires_at"`
}

// resolveLinkRoute: GET /v1/access/links/{token} (public, 30/min per IP).
func (s *Service) resolveLinkRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	token := chi.URLParam(r, "token")
	l, err := ResolveLink(ctx, s.d.Pool, token)
	if err != nil {
		return err
	}
	k, ok := KindOf(l.Ref.Kind)
	if !ok {
		return ErrLinkNotFound
	}
	title, err := k.Title(ctx, s.d.Pool, l.Ref.ID)
	if err != nil {
		return err
	}
	out := LinkTarget{Kind: k.Name, ID: l.Ref.ID, Label: k.Label, Title: title, Level: l.Level, ExpiresAt: l.ExpiresAt}
	if k.PublicPath != nil {
		p := k.PublicPath(token)
		out.Path = &p
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.OK(w, out)
	return nil
}
