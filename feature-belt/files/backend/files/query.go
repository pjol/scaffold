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
	l.has_link, lk.provider, lk.product, lk.mime_type, lk.folder,
	i.locked_by, lku.display_name, lku.avatar_url, i.locked_at, i.lock_note, i.color, i.content_source`

// itemJoins add the people and the public-link flag.
const itemJoins = `
	LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users uu ON uu.id = i.updated_by
	LEFT JOIN users tu ON tu.id = i.trashed_by
	CROSS JOIN LATERAL (SELECT EXISTS (
		SELECT 1 FROM access_links k WHERE k.resource_kind = 'files.item' AND k.resource_id = i.id
		  AND k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())) AS has_link) l
	LEFT JOIN files_links lk ON lk.item_id = i.id
	LEFT JOIN users lku ON lku.id = i.locked_by`

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
		scProvider, scProduct, scMime               *string
		scFolder                                    *bool
		lockBy                                      *uuid.UUID
		lockName, lockAvatar                        *string
		lockAt                                      *time.Time
		lockNote                                    string
	)
	dest := []any{&it.ID, &it.OrgID, &it.ParentID, &it.Type, &it.Name, &it.Drive, &it.OwnerID, &it.MimeType, &it.Size, &it.TextLike,
		&it.VersionID, &it.VersionCount, &createdBy, &createdName, &createdAvatar, &updatedBy, &updatedName, &updatedAvatar,
		&it.CreatedAt, &it.UpdatedAt, &it.TrashedAt, &trashedBy, &trashedName, &trashedAvatar, &it.PublicDisabled, &lvl, &it.Starred, &it.Shared, &it.PublicLink,
		&scProvider, &scProduct, &scMime, &scFolder, &lockBy, &lockName, &lockAvatar, &lockAt, &lockNote, &it.Color, &it.Content}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	it.MyLevel = access.Level(lvl)
	it.CreatedBy = person(createdBy, createdName, createdAvatar)
	it.UpdatedBy = person(updatedBy, updatedName, updatedAvatar)
	it.TrashedBy = person(trashedBy, trashedName, trashedAvatar)
	if lockAt != nil {
		it.Lock = &LockInfo{By: person(lockBy, lockName, lockAvatar), At: *lockAt, Note: lockNote}
	}
	if scProvider != nil {
		it.Shortcut = &ShortcutInfo{Provider: *scProvider, Product: derefStr(scProduct), MimeType: derefStr(scMime), Folder: scFolder != nil && *scFolder}
	}
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
// Asking for edit or more on a file someone else checked out answers 423
// item_locked.
func (s *Service) load(ctx context.Context, q db.Querier, me, id uuid.UUID, min access.Level) (*Item, error) {
	if _, err := access.Require(ctx, q, me, Kind, id, min); err != nil {
		return nil, err
	}
	it, err := s.getItem(ctx, q, me, id)
	if err != nil {
		return nil, err
	}
	if min >= access.Edit {
		if err := lockedForOthers(it, me); err != nil {
			return nil, err
		}
	}
	return it, nil
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

// SearchFilters narrow a search like Drive's advanced search. Zero values
// don't filter.
type SearchFilters struct {
	Kind           string     // folder image video audio pdf document spreadsheet presentation text archive
	Owner          string     // me | others | a user id (whose My drive; shared folders count their creator)
	ModifiedAfter  *time.Time // updated at or after
	ModifiedBefore *time.Time // updated before
	Location       string     // my | shared | a folder id (everything inside it)
	Starred        bool
	SharedWithMe   bool // in other people's My drives
}

func (f SearchFilters) empty() bool {
	return f.Kind == "" && f.Owner == "" && f.ModifiedAfter == nil && f.ModifiedBefore == nil && f.Location == "" && !f.Starred && !f.SharedWithMe
}

// kindMimes are the stored types behind each document kind filter.
var kindMimes = map[string][]string{
	"document": {"application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.oasis.opendocument.text", "application/rtf", "application/x-iwork-pages-sffpages", "text/markdown"},
	"spreadsheet": {"application/vnd.ms-excel", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.oasis.opendocument.spreadsheet", "application/x-iwork-numbers-sffnumbers", "text/csv", "text/tab-separated-values"},
	"presentation": {"application/vnd.ms-powerpoint", "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.oasis.opendocument.presentation", "application/x-iwork-keynote-sffkey"},
	"archive": {"application/zip", "application/x-zip-compressed", "application/gzip", "application/x-gzip", "application/x-tar",
		"application/x-7z-compressed", "application/vnd.rar", "application/x-rar-compressed", "application/java-archive"},
}

// SearchKinds are the values SearchFilters.Kind takes.
var SearchKinds = []string{"folder", "image", "video", "audio", "pdf", "document", "spreadsheet", "presentation", "text", "archive"}

// where adds the filters to a query whose args already hold $1 (caller).
func (f SearchFilters) where(args *[]any) (string, error) {
	var parts []string
	arg := func(v any) string {
		*args = append(*args, v)
		return fmt.Sprintf("$%d", len(*args))
	}
	switch f.Kind {
	case "":
	case "folder":
		parts = append(parts, "i.type = 'folder'")
	case "image", "video", "audio":
		parts = append(parts, "i.type = 'file' AND i.mime_type LIKE "+arg(f.Kind+"/%"))
	case "pdf":
		parts = append(parts, "i.type = 'file' AND i.mime_type = 'application/pdf'")
	case "text":
		parts = append(parts, "i.type = 'file' AND i.text_like AND i.mime_type NOT IN ('text/csv', 'text/tab-separated-values', 'image/svg+xml')")
	case "document", "spreadsheet", "presentation", "archive":
		parts = append(parts, "i.type = 'file' AND i.mime_type = ANY("+arg(kindMimes[f.Kind])+")")
	default:
		return "", httpx.BadRequest("kind must be one of " + strings.Join(SearchKinds, ", ") + ".")
	}
	switch f.Owner {
	case "":
	case "me":
		parts = append(parts, "COALESCE(i.owner_id, i.created_by) = $1")
	case "others":
		parts = append(parts, "COALESCE(i.owner_id, i.created_by) IS DISTINCT FROM $1")
	default:
		id, err := uuid.Parse(f.Owner)
		if err != nil {
			return "", httpx.BadRequest("owner must be me, others or a person's id.")
		}
		parts = append(parts, "COALESCE(i.owner_id, i.created_by) = "+arg(id))
	}
	if f.ModifiedAfter != nil {
		parts = append(parts, "i.updated_at >= "+arg(*f.ModifiedAfter))
	}
	if f.ModifiedBefore != nil {
		parts = append(parts, "i.updated_at < "+arg(*f.ModifiedBefore))
	}
	switch f.Location {
	case "":
	case "my":
		parts = append(parts, "i.drive = 'my' AND i.owner_id = $1")
	case "shared":
		parts = append(parts, "i.drive = 'shared'")
	default:
		id, err := uuid.Parse(f.Location)
		if err != nil {
			return "", httpx.BadRequest("location must be my, shared or a folder id.")
		}
		parts = append(parts, `i.id IN (WITH RECURSIVE sub AS (
			SELECT id, 0 AS depth FROM files_items WHERE parent_id = `+arg(id)+`
			UNION ALL SELECT c.id, sub.depth + 1 FROM files_items c JOIN sub ON c.parent_id = sub.id WHERE sub.depth < 64
		) SELECT id FROM sub)`)
	}
	if f.Starred {
		parts = append(parts, "EXISTS (SELECT 1 FROM files_stars fs WHERE fs.user_id = $1 AND fs.item_id = i.id)")
	}
	if f.SharedWithMe {
		parts = append(parts, "i.drive = 'my' AND i.owner_id <> $1")
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " AND (" + strings.Join(parts, ") AND (") + ")", nil
}

// searchItems finds live items the caller can open by name or by their text
// (text files, and what was extracted from PDFs and office documents),
// narrowed by filters. An empty query with filters lists the matches newest
// first. orgID nil searches every org they belong to.
func (s *Service) searchItems(ctx context.Context, q db.Querier, me uuid.UUID, viaAgent bool, orgID *uuid.UUID, query string, f SearchFilters, cursor string, limit int) (httpx.Page[SearchHit], error) {
	query = strings.Join(strings.Fields(query), " ")
	if len([]rune(query)) > 200 {
		query = string([]rune(query)[:200])
	}
	if query == "" && f.empty() {
		return httpx.Page[SearchHit]{Items: []SearchHit{}}, nil
	}
	args := []any{me, viaAgent, query, likeEscape(query), orgID}
	match := `TRUE`
	if query != "" {
		match = `(i.name ILIKE '%' || $4 || '%' OR i.search_vector @@ websearch_to_tsquery('simple', $3))`
	}
	filter, err := f.where(&args)
	if err != nil {
		return httpx.Page[SearchHit]{}, err
	}
	sql := `SELECT ` + itemColumns + `, sc.score,
		CASE WHEN $3 <> '' AND sc.score < 0.6 AND i.content_text IS NOT NULL
		     THEN ts_headline('simple', left(i.content_text, 200000), websearch_to_tsquery('simple', $3),
		                      'StartSel="", StopSel="", MaxWords=28, MinWords=12, MaxFragments=1, FragmentDelimiter=" … "')
		     ELSE '' END
		FROM files_items i` + itemJoins + visibleJoin + `
		CROSS JOIN LATERAL (SELECT CASE
			WHEN $3 = '' THEN 0.5
			WHEN lower(i.name) = lower($3) THEN 1.0
			WHEN lower(i.name) LIKE lower($4) || '%' THEN 0.9
			WHEN i.name ILIKE '%' || $4 || '%' THEN 0.75
			ELSE 0.5 END::float8 AS score) sc
		WHERE ($5::uuid IS NULL OR i.org_id = $5) AND i.trashed_at IS NULL AND ` + match + filter
	if cursor != "" {
		var c searchCursor
		if err := httpx.DecodeCursor(cursor, &c); err != nil {
			return httpx.Page[SearchHit]{}, err
		}
		n := len(args)
		args = append(args, c.S, c.T, c.ID)
		sql += fmt.Sprintf(` AND (sc.score < $%d OR (sc.score = $%d AND (i.updated_at, i.id) < ($%d, $%d)))`, n+1, n+1, n+2, n+3)
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

// parseSearchFilters reads ?kind=&owner=&modified_after=&modified_before=&location=&starred=1&shared_with_me=1.
func parseSearchFilters(get func(string) string) (SearchFilters, error) {
	f := SearchFilters{Kind: get("kind"), Owner: get("owner"), Location: get("location"), Starred: get("starred") == "1" || get("starred") == "true",
		SharedWithMe: get("shared_with_me") == "1" || get("shared_with_me") == "true"}
	for _, p := range []struct {
		key string
		dst **time.Time
	}{{"modified_after", &f.ModifiedAfter}, {"modified_before", &f.ModifiedBefore}} {
		v := get(p.key)
		if v == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			d, derr := time.Parse("2006-01-02", v)
			if derr != nil {
				return f, httpx.BadRequest(p.key + " must be a date (2026-03-01) or a timestamp.")
			}
			t = d
		}
		*p.dst = &t
	}
	return f, nil
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

// addThumbs gives items a short-lived inline URL of their current version's
// thumbnail (made by the background pass, or a video poster from the
// browser); an image still waiting for one uses itself when it is small.
// Presigning is local: no storage round trip.
func (s *Service) addThumbs(ctx context.Context, q db.Querier, items []*Item) {
	var ids []uuid.UUID
	byVersion := map[uuid.UUID]*Item{}
	for _, it := range items {
		if it.Type == TypeFile && it.VersionID != nil {
			ids = append(ids, *it.VersionID)
			byVersion[*it.VersionID] = it
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := q.Query(ctx, `SELECT id, storage_key, thumb_key FROM files_versions WHERE id = ANY($1)`, ids)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var key string
		var thumb *string
		if rows.Scan(&id, &key, &thumb) != nil {
			return
		}
		it := byVersion[id]
		switch {
		case thumb != nil:
			if u, err := s.thumbFor(ctx, *thumb); err == nil {
				it.ThumbURL = &u
			}
		case it.Preview == "image" && isRasterSmall(it.Size):
			ct, cd := servePolicy(it.MimeType, it.Name, it.TextLike, true)
			if u, err := s.d.Storage.PresignGetWith(ctx, key, urlTTL, storage.GetOptions{ContentType: ct, Disposition: cd}); err == nil {
				it.ThumbURL = &u
			}
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
