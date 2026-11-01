package files

// The queries every surface shares. REST handlers, agent tools and global
// search call these functions with access.ViaAgent(ctx), so they can't
// disagree about what someone may see. Every row is filtered with
// access_level (bounded lists) or access_visible (search).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// Sections of an org's drive.
const (
	SectionMy           = "my"             // the caller's My drive, top level
	SectionShared       = "shared"         // the org's shared folders they can open
	SectionSharedWithMe = "shared_with_me" // items shared with them or their teams
	SectionRecent       = "recent"         // what they opened or changed lately
	SectionStarred      = "starred"
	SectionTrash        = "trash" // trashed items they can restore (edit)
)

// itemColumns are scanned by scanItem; a.lvl is the caller's level, l.has_link
// a working public link on the item itself. $1 is the caller, $2 via agent.
const itemColumns = `
	i.id, i.org_id, i.parent_id, i.type, i.name, i.drive, i.owner_id, i.mime_type, i.size, i.text_like,
	i.version_id, i.version_count, i.created_by, cu.display_name, cu.avatar_url, i.updated_by, uu.display_name, uu.avatar_url,
	i.created_at, i.updated_at, i.trashed_at, i.trashed_by, tu.display_name, tu.avatar_url, i.public_disabled_at IS NOT NULL, a.lvl,
	EXISTS (SELECT 1 FROM files_stars st WHERE st.user_id = $1 AND st.item_id = i.id),
	l.has_link OR EXISTS (SELECT 1 FROM access_grants g WHERE g.resource_kind = 'files.item' AND g.resource_id = i.id),
	l.has_link`

// itemJoins add the people and the public-link flag.
const itemJoins = `
	LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users uu ON uu.id = i.updated_by
	LEFT JOIN users tu ON tu.id = i.trashed_by
	CROSS JOIN LATERAL (SELECT EXISTS (
		SELECT 1 FROM access_links k WHERE k.resource_kind = 'files.item' AND k.resource_id = i.id
		  AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())) AS has_link) l`

// levelJoin computes the caller's level per row (bounded lists).
const levelJoin = ` CROSS JOIN LATERAL (SELECT access_level($1, 'files.item', i.id, $2) AS lvl) a`

// visibleJoin is everything the caller can open (search).
const visibleJoin = ` JOIN (SELECT id, level AS lvl FROM access_visible($1, 'files.item', $2)) a ON a.id = i.id`

// scanItem reads itemColumns plus any extra trailing columns.
func scanItem(row pgx.Row, extra ...any) (*Item, error) {
	var it Item
	var (
		createdBy, updatedBy, trashedBy             *uuid.UUID
		createdName, updatedName, trashedName       *string
		createdAvatar, updatedAvatar, trashedAvatar *string
		lvl                                         int
	)
	dest := []any{&it.ID, &it.OrgID, &it.ParentID, &it.Type, &it.Name, &it.Drive, &it.OwnerID, &it.MimeType, &it.Size, &it.TextLike,
		&it.VersionID, &it.VersionCount, &createdBy, &createdName, &createdAvatar, &updatedBy, &updatedName, &updatedAvatar,
		&it.CreatedAt, &it.UpdatedAt, &it.TrashedAt, &trashedBy, &trashedName, &trashedAvatar, &it.PublicDisabled, &lvl, &it.Starred, &it.Shared, &it.PublicLink}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	it.MyLevel = access.Level(lvl)
	it.CreatedBy = person(createdBy, createdName, createdAvatar)
	it.UpdatedBy = person(updatedBy, updatedName, updatedAvatar)
	it.TrashedBy = person(trashedBy, trashedName, trashedAvatar)
	finishItem(&it)
	return &it, nil
}

func person(id *uuid.UUID, name, avatar *string) *Person {
	if id == nil {
		return nil
	}
	p := &Person{ID: *id, AvatarURL: avatar}
	if name != nil {
		p.Name = *name
	}
	return p
}

// finishItem fills the computed fields: preview kind, link, type info.
func finishItem(it *Item) {
	it.Preview = previewKind(it.Type, it.MimeType, it.Name, it.TextLike)
	it.Link = itemLink(it.ID)
	if t, ok := ItemTypeOf(it.Type); ok {
		it.TypeInfo = &TypeInfo{Type: t.Type, Title: t.Title, Icon: t.Icon}
		it.Link = t.Link(it.ID)
	}
}

func collectItems(rows pgx.Rows, extra func() []any) ([]*Item, error) {
	defer rows.Close()
	out := []*Item{}
	for rows.Next() {
		var ex []any
		if extra != nil {
			ex = extra()
		}
		it, err := scanItem(rows, ex...)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// getItem reads one item with the caller's level (no check: callers ran
// access.Require first, or read it for a public link or staff).
func (s *Service) getItem(ctx context.Context, q db.Querier, me, id uuid.UUID) (*Item, error) {
	row := q.QueryRow(ctx, `SELECT `+itemColumns+` FROM files_items i`+itemJoins+levelJoin+` WHERE i.id = $3`,
		me, access.ViaAgent(ctx), id)
	it, err := scanItem(row)
	if db.IsNoRows(err) {
		return nil, errItemNotFound
	}
	return it, err
}

// load checks the caller's level on an item (404 without view, 403 below
// min; agent-capped) and reads it.
func (s *Service) load(ctx context.Context, q db.Querier, me, id uuid.UUID, min access.Level) (*Item, error) {
	if _, err := access.Require(ctx, q, me, Kind, id, min); err != nil {
		return nil, err
	}
	return s.getItem(ctx, q, me, id)
}

// loadLive is load for items that must not be in the trash.
func (s *Service) loadLive(ctx context.Context, q db.Querier, me, id uuid.UUID, min access.Level) (*Item, error) {
	it, err := s.load(ctx, q, me, id, min)
	if err != nil {
		return nil, err
	}
	if it.TrashedAt != nil {
		return nil, errTrashed
	}
	return it, nil
}

// ---- sorted listings (folders, My drive, shared folders) ---------------------------

// listOpts are the paging and sort options of a listing.
type listOpts struct {
	Sort   string // name | updated | size
	Desc   bool
	Cursor string
	Limit  int
	Thumbs bool
}

type sortCursor struct {
	F  bool       `json:"f"` // false: folders, which come first
	S  string     `json:"s,omitempty"`
	T  *time.Time `json:"t,omitempty"`
	N  *int64     `json:"n,omitempty"`
	ID uuid.UUID  `json:"id"`
}

// sortedPage runs a listing ordered folders first, then by the sort key,
// keyset-paginated. where uses $1 (caller), $2 (via agent) and $3.
func (s *Service) sortedPage(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, where string, arg3 any, o listOpts) (httpx.Page[*Item], error) {
	key, dir, cmp := "lower(i.name)", "ASC", ">"
	switch o.Sort {
	case "updated":
		key = "i.updated_at"
	case "size":
		key = "i.size"
	}
	if o.Desc {
		dir, cmp = "DESC", "<"
	}
	args := []any{me, viaAgent, arg3}
	sql := `SELECT ` + itemColumns + `, (i.type <> 'folder'), lower(i.name), i.updated_at, i.size FROM files_items i` + itemJoins + levelJoin +
		` WHERE ` + where
	if o.Cursor != "" {
		var c sortCursor
		if err := httpx.DecodeCursor(o.Cursor, &c); err != nil {
			return httpx.Page[*Item]{}, err
		}
		var kv any
		switch o.Sort {
		case "updated":
			if c.T == nil {
				return httpx.Page[*Item]{}, httpx.BadRequest("Invalid cursor.")
			}
			kv = *c.T
		case "size":
			if c.N == nil {
				return httpx.Page[*Item]{}, httpx.BadRequest("Invalid cursor.")
			}
			kv = *c.N
		default:
			kv = c.S
		}
		args = append(args, c.F, kv, c.ID)
		sql += fmt.Sprintf(` AND ((i.type <> 'folder') > $4 OR ((i.type <> 'folder') = $4 AND (%s, i.id) %s ($5, $6)))`, key, cmp)
	}
	args = append(args, o.Limit+1)
	sql += fmt.Sprintf(` ORDER BY (i.type <> 'folder') ASC, %s %s, i.id %s LIMIT $%d`, key, dir, dir, len(args))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return httpx.Page[*Item]{}, err
	}
	type keys struct {
		f    bool
		name string
		t    time.Time
		n    int64
	}
	var ks []*keys
	items, err := collectItems(rows, func() []any {
		k := &keys{}
		ks = append(ks, k)
		return []any{&k.f, &k.name, &k.t, &k.n}
	})
	if err != nil {
		return httpx.Page[*Item]{}, err
	}
	idx := map[uuid.UUID]*keys{}
	for i, it := range items {
		idx[it.ID] = ks[i]
	}
	page := httpx.NewPage(items, o.Limit, func(it *Item) any {
		k := idx[it.ID]
		c := sortCursor{F: k.f, ID: it.ID}
		switch o.Sort {
		case "updated":
			c.T = &k.t
		case "size":
			c.N = &k.n
		default:
			c.S = k.name
		}
		return c
	})
	if o.Thumbs {
		s.addThumbs(ctx, q, page.Items)
	}
	return page, nil
}

// children lists a folder's live items the caller can open. The caller has
// checked view on the folder.
func (s *Service) children(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, folderID uuid.UUID, o listOpts) (httpx.Page[*Item], error) {
	return s.sortedPage(ctx, q, me, viaAgent, `i.parent_id = $3 AND i.trashed_at IS NULL AND a.lvl >= 1`, folderID, o)
}

// ---- sections -------------------------------------------------------------------

type timeCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// section lists one of an org's drive sections for the caller.
func (s *Service) section(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, orgID uuid.UUID, section string, o listOpts) (httpx.Page[*Item], error) {
	switch section {
	case SectionMy:
		return s.sortedPage(ctx, q, me, viaAgent,
			`i.org_id = $3 AND i.parent_id IS NULL AND i.drive = 'my' AND i.owner_id = $1 AND i.trashed_at IS NULL AND a.lvl >= 1`, orgID, o)
	case SectionShared:
		return s.sortedPage(ctx, q, me, viaAgent,
			`i.org_id = $3 AND i.parent_id IS NULL AND i.drive = 'shared' AND i.trashed_at IS NULL AND a.lvl >= 1`, orgID, o)
	}
	var from, at, where string
	switch section {
	case SectionSharedWithMe:
		// Items with a grant to the caller or one of their teams, newest
		// share first; not their own and not top-level shared folders
		// (those are under Shared folders).
		from = ` JOIN (SELECT g.resource_id AS id, max(g.created_at) AS at FROM access_grants g
			WHERE g.resource_kind = 'files.item' AND ((g.subject_type = 'user' AND g.subject_id = $1)
			   OR (g.subject_type = 'team' AND g.subject_id IN (SELECT team_id FROM access_team_members WHERE user_id = $1)))
			GROUP BY g.resource_id) x ON x.id = i.id`
		at = "x.at"
		where = `i.org_id = $3 AND i.trashed_at IS NULL AND a.lvl >= 1 AND NOT (i.drive = 'my' AND i.owner_id = $1)
			AND NOT (i.parent_id IS NULL AND i.drive = 'shared')`
	case SectionRecent:
		from = ` JOIN files_recents x ON x.item_id = i.id AND x.user_id = $1`
		at = "x.at"
		where = `x.org_id = $3 AND i.trashed_at IS NULL AND a.lvl >= 1`
	case SectionStarred:
		from = ` JOIN files_stars x ON x.item_id = i.id AND x.user_id = $1`
		at = "x.created_at"
		where = `i.org_id = $3 AND i.trashed_at IS NULL AND a.lvl >= 1`
	case SectionTrash:
		at = "i.trashed_at"
		where = `i.org_id = $3 AND i.trash_root = i.id AND a.lvl >= 3`
	default:
		return httpx.Page[*Item]{}, httpx.BadRequest("section must be my, shared, shared_with_me, recent, starred or trash.")
	}
	args := []any{me, viaAgent, orgID}
	sql := `SELECT ` + itemColumns + `, ` + at + ` FROM files_items i` + from + itemJoins + levelJoin + ` WHERE ` + where
	if o.Cursor != "" {
		var c timeCursor
		if err := httpx.DecodeCursor(o.Cursor, &c); err != nil {
			return httpx.Page[*Item]{}, err
		}
		args = append(args, c.T, c.ID)
		sql += ` AND (` + at + `, i.id) < ($4, $5)`
	}
	args = append(args, o.Limit+1)
	sql += fmt.Sprintf(` ORDER BY %s DESC, i.id DESC LIMIT $%d`, at, len(args))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return httpx.Page[*Item]{}, err
	}
	ats := map[*Item]*time.Time{}
	var pending []*time.Time
	items, err := collectItems(rows, func() []any {
		t := new(time.Time)
		pending = append(pending, t)
		return []any{t}
	})
	if err != nil {
		return httpx.Page[*Item]{}, err
	}
	for i, it := range items {
		ats[it] = pending[i]
	}
	page := httpx.NewPage(items, o.Limit, func(it *Item) any { return timeCursor{T: *ats[it], ID: it.ID} })
	if o.Thumbs {
		s.addThumbs(ctx, q, page.Items)
	}
	return page, nil
}

// ---- search ---------------------------------------------------------------------

// SearchHit is a search result: the item and, for a match in the text, a
// short excerpt.
type SearchHit struct {
	Item    *Item   `json:"item"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

type searchCursor struct {
	S  float64   `json:"s"`
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// searchItems finds live items the caller can open by name, or by the text
// of small text files. orgID nil searches every org they belong to.
func (s *Service) searchItems(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, orgID *uuid.UUID, query, cursor string, limit int) (httpx.Page[SearchHit], error) {
	query = strings.Join(strings.Fields(query), " ")
	if len([]rune(query)) > 200 {
		query = string([]rune(query)[:200])
	}
	if query == "" {
		return httpx.Page[SearchHit]{Items: []SearchHit{}}, nil
	}
	args := []any{me, viaAgent, query, likeEscape(query), orgID}
	sql := `SELECT ` + itemColumns + `, sc.score,
		CASE WHEN sc.score < 0.6 THEN substr(i.content_text, greatest(1, strpos(lower(i.content_text), lower($3)) - 60), 180) ELSE '' END
		FROM files_items i` + itemJoins + visibleJoin + `
		CROSS JOIN LATERAL (SELECT CASE
			WHEN lower(i.name) = lower($3) THEN 1.0
			WHEN lower(i.name) LIKE lower($4) || '%' THEN 0.9
			WHEN i.name ILIKE '%' || $4 || '%' THEN 0.75
			ELSE 0.5 END::float8 AS score) sc
		WHERE ($5::uuid IS NULL OR i.org_id = $5) AND i.trashed_at IS NULL
		  AND (i.name ILIKE '%' || $4 || '%'
		       OR (i.content_text IS NOT NULL AND to_tsvector('simple', COALESCE(i.content_text, '')) @@ websearch_to_tsquery('simple', $3)))`
	if cursor != "" {
		var c searchCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil {
			return httpx.Page[SearchHit]{}, err
		}
		args = append(args, c.S, c.T, c.ID)
		sql += ` AND (sc.score < $6 OR (sc.score = $6 AND (i.updated_at, i.id) < ($7, $8)))`
	}
	args = append(args, limit+1)
	sql += fmt.Sprintf(` ORDER BY sc.score DESC, i.updated_at DESC, i.id DESC LIMIT $%d`, len(args))
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return httpx.Page[SearchHit]{}, err
	}
	var hits []SearchHit
	type extra struct {
		score   float64
		snippet *string
	}
	var exs []*extra
	items, err := collectItems(rows, func() []any {
		e := &extra{}
		exs = append(exs, e)
		return []any{&e.score, &e.snippet}
	})
	if err != nil {
		return httpx.Page[SearchHit]{}, err
	}
	for i, it := range items {
		h := SearchHit{Item: it, Score: exs[i].score}
		if exs[i].snippet != nil {
			h.Snippet = strings.Join(strings.Fields(*exs[i].snippet), " ")
		}
		hits = append(hits, h)
	}
	return httpx.NewPage(hits, limit, func(h SearchHit) any { return searchCursor{S: h.Score, T: h.Item.UpdatedAt, ID: h.Item.ID} }), nil
}

// ---- path ------------------------------------------------------------------------

// path returns the ancestors of an item the caller can open, top first,
// stopping at the first one they can't, and where the path starts.
func (s *Service) path(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, it *Item) ([]Crumb, string, error) {
	crumbs := []Crumb{}
	root := rootOf(it, me)
	if it.ParentID == nil {
		return crumbs, root, nil
	}
	rows, err := q.Query(ctx, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id, name, type, drive, owner_id, 0 AS depth FROM files_items WHERE id = $3
			UNION ALL
			SELECT p.id, p.parent_id, p.name, p.type, p.drive, p.owner_id, up.depth + 1
			FROM up JOIN files_items p ON p.id = up.parent_id WHERE up.depth < 64
		)
		SELECT id, parent_id, name, type, drive, owner_id, access_level($1, 'files.item', id, $2) FROM up ORDER BY depth`,
		me, viaAgent, *it.ParentID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var chain []Crumb
	cut := false
	var top struct {
		parent *uuid.UUID
		drive  string
		owner  *uuid.UUID
	}
	for rows.Next() {
		var c Crumb
		var parent, owner *uuid.UUID
		var drive string
		var lvl int
		if err := rows.Scan(&c.ID, &parent, &c.Name, &c.Type, &drive, &owner, &lvl); err != nil {
			return nil, "", err
		}
		if cut {
			continue
		}
		if lvl < 1 {
			cut = true
			continue
		}
		chain = append(chain, c)
		top.parent, top.drive, top.owner = parent, drive, owner
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	for i := len(chain) - 1; i >= 0; i-- {
		crumbs = append(crumbs, chain[i])
	}
	switch {
	case cut || len(chain) == 0 && it.ParentID != nil:
		root = SectionSharedWithMe
	case top.parent == nil:
		root = rootOf(&Item{Drive: top.drive, OwnerID: top.owner}, me)
	}
	return crumbs, root, nil
}

func rootOf(it *Item, me uuid.UUID) string {
	switch {
	case it.Drive == DriveShared:
		return SectionShared
	case it.OwnerID != nil && *it.OwnerID == me:
		return SectionMy
	}
	return SectionSharedWithMe
}

// ---- thumbnails and versions -------------------------------------------------------

// addThumbs gives image items a short-lived inline URL of their current
// version (presigning is local: no storage round trip).
func (s *Service) addThumbs(ctx context.Context, q db.Querier, items []*Item) {
	var ids []uuid.UUID
	byVersion := map[uuid.UUID]*Item{}
	for _, it := range items {
		if it.Preview == "image" && it.VersionID != nil {
			ids = append(ids, *it.VersionID)
			byVersion[*it.VersionID] = it
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := q.Query(ctx, `SELECT id, storage_key FROM files_versions WHERE id = ANY($1)`, ids)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var key string
		if rows.Scan(&id, &key) != nil {
			return
		}
		it := byVersion[id]
		ct, cd := servePolicy(it.MimeType, it.Name, it.TextLike, true)
		if u, err := s.d.Storage.PresignGetWith(ctx, key, urlTTL, storage.GetOptions{ContentType: ct, Disposition: cd}); err == nil {
			it.ThumbURL = &u
		}
	}
}

// versionKey is the storage key of one of an item's versions.
func (s *Service) versionKey(ctx context.Context, q db.Querier, itemID, versionID uuid.UUID) (string, error) {
	var key string
	err := q.QueryRow(ctx, `SELECT storage_key FROM files_versions WHERE id = $1 AND item_id = $2`, versionID, itemID).Scan(&key)
	if db.IsNoRows(err) {
		return "", httpx.NotFound("Version not found.")
	}
	return key, err
}
