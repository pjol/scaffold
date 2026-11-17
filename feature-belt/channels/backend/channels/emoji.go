package channels

// Custom emoji: an org's own images, used as :name: in messages and
// reactions. Who may add them is ActAddEmoji (any member by default); the
// person who added one, or an org admin, removes it. Aliases point at an
// emoji and share its image. Frequently used emoji come from the person's
// own reactions.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/storage"
)

const (
	// PurposeEmoji is the private upload purpose for custom emoji images.
	PurposeEmoji  = "channel_emoji"
	emojiMaxBytes = 256 << 10
	maxOrgEmoji   = 2000
	emojiURLTTL   = 6 * time.Hour
)

var (
	emojiNameRe   = regexp.MustCompile(`^[a-z0-9_+-]{2,40}$`)
	customEmojiRe = regexp.MustCompile(`^:[a-z0-9_+-]{2,40}:$`)
	emojiTypes    = []string{"image/png", "image/gif", "image/jpeg", "image/webp"}
	emojiExt      = map[string]string{"png": "image/png", "gif": "image/gif", "jpg": "image/jpeg", "webp": "image/webp"}
)

// CustomEmoji is one of an org's emoji. Aliases carry the image of the
// emoji they point at.
type CustomEmoji struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	AliasOf   *string   `json:"alias_of"` // the original's name, for aliases
	CreatedBy *UserRef  `json:"created_by"`
	CanDelete bool      `json:"can_delete"`
	CreatedAt time.Time `json:"created_at"`
}

// EmojiList is GET /channels/orgs/{orgID}/emoji.
type EmojiList struct {
	Items     []CustomEmoji `json:"items"`
	CanAdd    bool          `json:"can_add"`
	Frequent  []string      `json:"frequent"`   // the caller's most used: unicode or :name:
	ExpiresAt time.Time     `json:"expires_at"` // image links stop working; refetch before
}

// resolveEmoji returns the canonical name for name (an alias resolves to
// its original) when the org has it.
func (s *Service) resolveEmoji(ctx context.Context, orgID uuid.UUID, name string) (string, bool, error) {
	var canonical string
	err := s.d.Pool.QueryRow(ctx, `
		SELECT COALESCE(o.name, e.name) FROM channel_emoji e LEFT JOIN channel_emoji o ON o.id = e.alias_of
		WHERE e.org_id = $1 AND e.name = $2`, orgID, name).Scan(&canonical)
	if db.IsNoRows(err) {
		return "", false, nil
	}
	return canonical, err == nil, err
}

// GET /channels/orgs/{orgID}/emoji
func (s *Service) listEmoji(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	canAdd, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActAddEmoji, OrgID: m.OrgID, UserID: me, OrgRole: m.Role})
	if err != nil {
		return err
	}
	orgAdmin := orgs.RoleAtLeast(m.Role, orgs.RoleAdmin)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT e.id, e.name, COALESCE(o.storage_key, e.storage_key), COALESCE(o.content_type, e.content_type), o.name,
		       e.created_by, u.display_name, u.avatar_url, u.status, e.created_at
		FROM channel_emoji e
		LEFT JOIN channel_emoji o ON o.id = e.alias_of
		LEFT JOIN users u ON u.id = e.created_by
		WHERE e.org_id = $1 ORDER BY e.name`, m.OrgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := EmojiList{Items: []CustomEmoji{}, CanAdd: canAdd, ExpiresAt: time.Now().Add(emojiURLTTL).UTC()}
	for rows.Next() {
		var e CustomEmoji
		var key, ct string
		var by *uuid.UUID
		var name, status, avatar *string
		if err := rows.Scan(&e.ID, &e.Name, &key, &ct, &e.AliasOf, &by, &name, &avatar, &status, &e.CreatedAt); err != nil {
			return err
		}
		if e.URL, err = s.d.Storage.PresignGetWith(ctx, key, emojiURLTTL, storage.GetOptions{ContentType: ct, Disposition: "inline"}); err != nil {
			return err
		}
		if by != nil {
			u := userRef(*by, deref(name), avatar, deref(status))
			e.CreatedBy = &u
		}
		e.CanDelete = orgAdmin || (by != nil && *by == me)
		out.Items = append(out.Items, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if out.Frequent, err = s.frequentEmoji(ctx, me, m.OrgID); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// frequentEmoji is the person's 16 most used reactions in the org over the
// last 90 days, most used first.
func (s *Service) frequentEmoji(ctx context.Context, me, orgID uuid.UUID) ([]string, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.emoji FROM channel_reactions r
		JOIN channel_messages m ON m.id = r.message_id JOIN channels c ON c.id = m.channel_id
		WHERE r.user_id = $1 AND c.org_id = $2 AND r.created_at > now() - interval '90 days'
		GROUP BY r.emoji ORDER BY count(*) DESC, max(r.created_at) DESC LIMIT 16`, me, orgID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if list == nil {
		list = []string{}
	}
	return list, err
}

type emojiReq struct {
	Name    string `json:"name"`
	Key     string `json:"key"`      // an upload with purpose channel_emoji
	AliasOf string `json:"alias_of"` // or the name of an emoji this one aliases
}

// POST /channels/orgs/{orgID}/emoji {name, key | alias_of} — 201.
func (s *Service) createEmoji(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req emojiReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.ToLower(strings.Trim(strings.TrimSpace(req.Name), ":"))
	if !emojiNameRe.MatchString(name) {
		return httpx.BadRequest("Emoji names are 2 to 40 lowercase letters, numbers, -, _ or +.")
	}
	if _, builtin := builtinEmojiNames[name]; builtin {
		return httpx.Conflict(":" + name + ": is a built-in emoji. Choose another name.")
	}
	if err := checkText(ctx, name); err != nil {
		return err
	}
	ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActAddEmoji, OrgID: m.OrgID, UserID: me, OrgRole: m.Role})
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("You can't add emoji in this " + orgs.Noun() + ".")
	}
	key := strings.TrimSpace(req.Key)
	alias := strings.ToLower(strings.Trim(strings.TrimSpace(req.AliasOf), ":"))
	var ct string
	switch {
	case (key == "") == (alias == ""):
		return httpx.BadRequest("Upload an image, or name the emoji this one is an alias of.")
	case key != "":
		if !storage.KeyOwnedBy(key, PurposeEmoji, me) {
			return httpx.BadRequest("Upload the image first, then send its key.")
		}
		ext := key[strings.LastIndex(key, ".")+1:]
		if ct = emojiExt[ext]; ct == "" {
			return httpx.BadRequest("Emoji are PNG, GIF, JPEG or WebP images.")
		}
		info, err := s.d.Storage.Stat(ctx, key)
		if err != nil {
			return httpx.BadRequest("Upload the image first, then send its key.")
		}
		if info.Size > emojiMaxBytes {
			return httpx.BadRequest("Emoji images can be up to 256 KB.")
		}
	}
	var e CustomEmoji
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_emoji WHERE org_id = $1`, m.OrgID).Scan(&n); err != nil {
			return err
		}
		if n >= maxOrgEmoji {
			return httpx.BadRequest("This " + orgs.Noun() + " has 2000 emoji, the most it can have.")
		}
		var aliasID *uuid.UUID
		if alias != "" {
			var id, origin uuid.UUID
			var parent *uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id, alias_of FROM channel_emoji WHERE org_id = $1 AND name = $2`, m.OrgID, alias).Scan(&id, &parent)
			if db.IsNoRows(err) {
				return httpx.NotFound(":" + alias + ": isn't an emoji in this " + orgs.Noun() + ".")
			}
			if err != nil {
				return err
			}
			origin = id
			if parent != nil {
				origin = *parent // an alias of an alias points at the original
			}
			aliasID = &origin
		}
		var keyArg, ctArg *string
		if key != "" {
			keyArg, ctArg = &key, &ct
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO channel_emoji (org_id, name, alias_of, storage_key, content_type, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`, m.OrgID, name, aliasID, keyArg, ctArg, me).Scan(&e.ID, &e.CreatedAt)
		switch {
		case db.IsUniqueViolation(err, "channel_emoji_name_uidx"):
			return httpx.Conflict(":" + name + ": is taken. Choose another name.")
		case db.IsUniqueViolation(err):
			return httpx.Conflict("That image is already an emoji.")
		}
		return err
	})
	if err != nil {
		return err
	}
	e.Name, e.CanDelete = name, true
	if alias != "" {
		e.AliasOf = &alias
	}
	s.publishEmoji(ctx, m.OrgID)
	httpx.JSON(w, http.StatusCreated, e)
	return nil
}

// DELETE /channels/emoji/{id} — whoever added it, or an org admin. Its
// aliases go with it; reactions that used it show its :name:.
func (s *Service) deleteEmoji(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID uuid.UUID
	var by *uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		SELECT e.org_id, e.created_by FROM channel_emoji e
		JOIN org_members om ON om.org_id = e.org_id AND om.user_id = $2 WHERE e.id = $1`, id, me).Scan(&orgID, &by)
	if db.IsNoRows(err) {
		return httpx.NotFound("Emoji not found.")
	}
	if err != nil {
		return err
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, orgID, me)
	if err != nil {
		return err
	}
	if !orgs.RoleAtLeast(role, orgs.RoleAdmin) && (by == nil || *by != me) {
		return httpx.Forbidden("Only the person who added it and admins can remove an emoji.")
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM channel_emoji WHERE id = $1`, id); err != nil {
		return err
	}
	s.kickObjectCleanup(ctx)
	s.publishEmoji(ctx, orgID)
	httpx.NoContent(w)
	return nil
}

// publishEmoji tells the org's members their emoji list changed (through the
// sidebar event on each member's user topic, newest 2000 members).
func (s *Service) publishEmoji(ctx context.Context, orgID uuid.UUID) {
	rows, err := s.d.Pool.Query(ctx, `SELECT user_id FROM org_members WHERE org_id = $1 ORDER BY created_at DESC LIMIT 2000`, orgID)
	if err != nil {
		return
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return
	}
	for _, id := range ids {
		s.toUser(ctx, id, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "emoji"})
	}
}
