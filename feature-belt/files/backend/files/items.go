package files

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// ---- placement ------------------------------------------------------------------

// placement is where a new or moved item goes: a folder, or the top of a
// My drive or of the org's shared folders.
type placement struct {
	orgID  uuid.UUID
	parent *Item
	drive  string
	owner  *uuid.UUID
}

func (p *placement) parentID() *uuid.UUID {
	if p.parent == nil {
		return nil
	}
	return &p.parent.ID
}

// siblingKey matches the unique name index: COALESCE(parent, owner, org).
func (p *placement) siblingKey() uuid.UUID {
	switch {
	case p.parent != nil:
		return p.parent.ID
	case p.owner != nil:
		return *p.owner
	}
	return p.orgID
}

// placeFor resolves a folder the caller can edit (nil: the top of their My
// drive in orgID).
func (s *Service) placeFor(ctx context.Context, q db.Querier, me, orgID uuid.UUID, parentID *uuid.UUID) (*placement, error) {
	if parentID != nil {
		parent, err := s.loadLive(ctx, q, me, *parentID, access.Edit)
		if err != nil {
			return nil, err
		}
		if !parent.IsFolder() {
			return nil, errNotFolder
		}
		if orgID != uuid.Nil && parent.OrgID != orgID {
			return nil, errItemNotFound
		}
		return &placement{orgID: parent.OrgID, parent: parent, drive: parent.Drive, owner: parent.OwnerID}, nil
	}
	role, err := orgs.RoleOf(ctx, q, orgID, me)
	if err != nil {
		return nil, err
	}
	if role == "" {
		return nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return &placement{orgID: orgID, drive: DriveMy, owner: &me}, nil
}

func nameTaken(ctx context.Context, q db.Querier, pl *placement, name string, except uuid.UUID) (bool, error) {
	var taken bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM files_items WHERE org_id = $1 AND drive = $2 AND COALESCE(parent_id, owner_id, org_id) = $3
		  AND lower(name) = lower($4) AND trashed_at IS NULL AND id <> $5)`,
		pl.orgID, pl.drive, pl.siblingKey(), name, except).Scan(&taken)
	return taken, err
}

// freeName returns name, or "name (n)" when it's taken; rename false
// answers 409 instead.
func freeName(ctx context.Context, q db.Querier, pl *placement, name string, isFile, rename bool, except uuid.UUID) (string, error) {
	taken, err := nameTaken(ctx, q, pl, name, except)
	if err != nil || !taken {
		return name, err
	}
	if !rename {
		return "", errNameTaken
	}
	for n := 1; n < 1000; n++ {
		candidate := numberedName(name, n, isFile)
		if taken, err = nameTaken(ctx, q, pl, candidate, except); err != nil || !taken {
			return candidate, err
		}
	}
	return "", errNameTaken
}

// insertItem writes a new item and its access resource. It returns the id.
func (s *Service) insertItem(ctx context.Context, tx pgx.Tx, pl *placement, typ, rawName string, by uuid.UUID, rename bool) (uuid.UUID, error) {
	return s.insertItemWith(ctx, tx, pl, typ, rawName, by, rename, access.None, false)
}

// insertItemWith also sets general access (shared folders) or restricts the
// item from its folder's access.
func (s *Service) insertItemWith(ctx context.Context, tx pgx.Tx, pl *placement, typ, rawName string, by uuid.UUID, rename bool, general access.Level, restricted bool) (uuid.UUID, error) {
	name, err := cleanName(rawName)
	if err != nil {
		return uuid.Nil, err
	}
	if name, err = freeName(ctx, tx, pl, name, typ != TypeFolder, rename, uuid.Nil); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO files_items (org_id, parent_id, type, name, drive, owner_id, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7) RETURNING id`,
		pl.orgID, pl.parentID(), typ, name, pl.drive, pl.owner, nullID(by)).Scan(&id)
	if db.IsUniqueViolation(err, "files_items_name_key") {
		return uuid.Nil, errNameTaken
	}
	if err != nil {
		return uuid.Nil, err
	}
	res := access.NewResource{Kind: Kind, ID: id, OrgID: pl.orgID, General: general, Restricted: restricted}
	if pl.owner != nil {
		res.OwnerID = *pl.owner
	}
	if pl.parent != nil {
		res.Parent = &access.Ref{Kind: Kind, ID: pl.parent.ID}
	}
	return id, access.CreateResource(ctx, tx, res)
}

func nullID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func eqID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ---- activity and recents ---------------------------------------------------------

// logActivity records an event in the caller's transaction. Agent calls are
// marked with the agent's name.
func (s *Service) logActivity(ctx context.Context, q db.Querier, orgID, itemID uuid.UUID, folderID *uuid.UUID, actor uuid.UUID, action string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	if a, ok := auth.AgentFrom(ctx); ok {
		data["agent"] = a.Client
	}
	if _, err := q.Exec(ctx, `INSERT INTO files_activity (org_id, item_id, folder_id, actor_id, action, data) VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID, itemID, folderID, nullID(actor), action, data); err != nil {
		slog.WarnContext(ctx, "files: activity", "action", action, "err", err)
	}
}

// touchRecent puts an item at the top of the caller's Recent.
func (s *Service) touchRecent(ctx context.Context, me uuid.UUID, it *Item, action string) {
	if it == nil || it.IsFolder() {
		return
	}
	if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `
		INSERT INTO files_recents (user_id, item_id, org_id, action, at) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id, item_id) DO UPDATE SET at = now(), action = EXCLUDED.action`, me, it.ID, it.OrgID, action); err != nil {
		slog.WarnContext(ctx, "files: recent", "err", err)
	}
}

// ---- operations -------------------------------------------------------------------

// rename gives an item a new name (409 when a sibling has it).
func (s *Service) rename(ctx context.Context, tx pgx.Tx, me uuid.UUID, it *Item, raw string) (*Item, error) {
	if it.TrashedAt != nil {
		return nil, errTrashed
	}
	name, err := cleanName(raw)
	if err != nil {
		return nil, err
	}
	if name == it.Name {
		return it, nil
	}
	pl := &placement{orgID: it.OrgID, drive: it.Drive, owner: it.OwnerID}
	if it.ParentID != nil {
		pl.parent = &Item{ID: *it.ParentID}
	}
	if _, err := freeName(ctx, tx, pl, name, false, false, it.ID); err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE files_items SET name = $2, updated_at = now(), updated_by = $3 WHERE id = $1`, it.ID, name, me)
	if db.IsUniqueViolation(err, "files_items_name_key") {
		return nil, errNameTaken
	}
	if err != nil {
		return nil, err
	}
	s.logActivity(ctx, tx, it.OrgID, it.ID, it.ParentID, me, "renamed", map[string]any{"from": it.Name, "to": name})
	return s.getItem(ctx, tx, me, it.ID)
}

// move puts an item in another folder (nil: the top of the caller's My
// drive). Moving into another drive (someone else's My drive, a shared
// folder, or out of one) needs manage and hands the subtree to that drive.
// A taken name gets a number.
func (s *Service) move(ctx context.Context, tx pgx.Tx, me uuid.UUID, it *Item, dest *uuid.UUID) (*Item, error) {
	if it.TrashedAt != nil {
		return nil, errTrashed
	}
	var pl *placement
	if dest != nil {
		if *dest == it.ID {
			return nil, httpx.BadRequest("A folder can't go inside itself.")
		}
		var err error
		if pl, err = s.placeFor(ctx, tx, me, it.OrgID, dest); err != nil {
			return nil, err
		}
	} else {
		pl = &placement{orgID: it.OrgID, drive: DriveMy, owner: &me}
	}
	if eqID(pl.parentID(), it.ParentID) && pl.drive == it.Drive && eqID(pl.owner, it.OwnerID) {
		return it, nil
	}
	if err := s.agentMove(ctx, tx, me, it, pl.parent); err != nil {
		return nil, err
	}
	crossing := pl.drive != it.Drive || !eqID(pl.owner, it.OwnerID)
	if crossing && it.MyLevel < access.Manage {
		return nil, errCrossDrive
	}
	name, err := freeName(ctx, tx, pl, it.Name, it.Type != TypeFolder, true, it.ID)
	if err != nil {
		return nil, err
	}
	var parentRef *access.Ref
	if pl.parent != nil {
		parentRef = &access.Ref{Kind: Kind, ID: pl.parent.ID}
	}
	if err := access.MoveResource(ctx, tx, access.Ref{Kind: Kind, ID: it.ID}, parentRef); err != nil {
		if errors.Is(err, access.ErrCycle) {
			return nil, httpx.BadRequest("A folder can't go inside itself.")
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE files_items SET parent_id = $2, name = $3, updated_at = now(), updated_by = $4 WHERE id = $1`,
		it.ID, pl.parentID(), name, me); err != nil {
		if db.IsUniqueViolation(err, "files_items_name_key") {
			return nil, errNameTaken
		}
		return nil, err
	}
	if crossing {
		// The subtree now belongs to the destination drive: its person for a
		// My drive, nobody (org admins manage it) for shared folders.
		if _, err := tx.Exec(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM files_items WHERE id = $1
				UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id
			)
			UPDATE files_items SET drive = $2, owner_id = $3 WHERE id IN (SELECT id FROM sub)`, it.ID, pl.drive, pl.owner); err != nil {
			return nil, err
		}
		owner := uuid.Nil
		if pl.owner != nil {
			owner = *pl.owner
		}
		if err := access.SetOwnerTree(ctx, tx, access.Ref{Kind: Kind, ID: it.ID}, owner); err != nil {
			return nil, err
		}
	}
	data := map[string]any{"from_id": it.ParentID, "to_id": pl.parentID()}
	if pl.parent != nil {
		data["to_name"] = pl.parent.Name
	}
	if name != it.Name {
		data["renamed_to"] = name
	}
	s.logActivity(ctx, tx, it.OrgID, it.ID, pl.parentID(), me, "moved", data)
	return s.getItem(ctx, tx, me, it.ID)
}

// errAgentReshare refuses an agent's move that would change who can open
// the item.
var errAgentReshare = httpx.NewError(http.StatusForbidden, "agent_forbidden",
	"Moving this item there changes who can open it, and AI agents can't change sharing. Ask the person to move it.")

// agentMove refuses, for an AI agent's call (and its suggestion's preview),
// a move that changes who can open the item: into another drive (which
// hands the subtree over), or between folders whose inherited sharing
// differs (which shares or unshares it). An item that inherits gets the
// grants, owners and public links of every folder up its inheriting chain,
// so the old and new folders' chains must carry the same ones; a restricted
// item keeps its access wherever it goes. dest nil is the top of the
// caller's My drive. People make these moves themselves.
func (s *Service) agentMove(ctx context.Context, q db.Querier, me uuid.UUID, it *Item, dest *Item) error {
	if !access.ViaAgent(ctx) {
		return nil
	}
	drive, owner, to := DriveMy, &me, (*uuid.UUID)(nil)
	if dest != nil {
		drive, owner, to = dest.Drive, dest.OwnerID, &dest.ID
	}
	if drive != it.Drive || !eqID(owner, it.OwnerID) {
		return errAgentReshare
	}
	var inherits bool
	var itemOwner *uuid.UUID
	err := q.QueryRow(ctx, `SELECT inherits, owner_id FROM access_resources WHERE kind = $1 AND id = $2`, Kind, it.ID).Scan(&inherits, &itemOwner)
	if db.IsNoRows(err) {
		return errItemNotFound
	}
	if err != nil || !inherits {
		return err
	}
	from, err := inheritedSharing(ctx, q, it.ParentID, itemOwner)
	if err != nil {
		return err
	}
	next, err := inheritedSharing(ctx, q, to, itemOwner)
	if err != nil {
		return err
	}
	if !slices.Equal(from, next) {
		return errAgentReshare
	}
	return nil
}

// inheritedSharing lists what an item inheriting from folder gets from it:
// owners (other than the item's own), each subject's highest grant and the
// working public links on the folder and its inheriting ancestors, sorted.
// It walks like access_level and access_link_level (32 rows with the item).
// A nil folder (the top of a drive) passes nothing down.
func inheritedSharing(ctx context.Context, q db.Querier, folder, itemOwner *uuid.UUID) ([]string, error) {
	if folder == nil {
		return []string{}, nil
	}
	rows, err := q.Query(ctx, `
		WITH RECURSIVE chain AS (
			SELECT kind, id, owner_id, parent_kind, parent_id, inherits, 0 AS depth
			FROM access_resources WHERE kind = $1 AND id = $2
			UNION ALL
			SELECT r.kind, r.id, r.owner_id, r.parent_kind, r.parent_id, r.inherits, c.depth + 1
			FROM chain c JOIN access_resources r ON r.kind = c.parent_kind AND r.id = c.parent_id
			WHERE c.inherits AND c.depth < 30
		)
		SELECT x FROM (
			SELECT 'owner:' || owner_id::text AS x FROM chain WHERE owner_id IS NOT NULL AND owner_id IS DISTINCT FROM $3::uuid
			UNION
			SELECT g.subject_type || ':' || g.subject_id::text || ':' || max(g.level)::text
			FROM chain c JOIN access_grants g ON g.resource_kind = c.kind AND g.resource_id = c.id
			GROUP BY g.subject_type, g.subject_id
			UNION
			SELECT 'link:' || k.id::text || ':' || k.level::text
			FROM chain c JOIN access_links k ON k.resource_kind = c.kind AND k.resource_id = c.id
			WHERE k.revoked_at IS NULL AND (k.expires_at IS NULL OR k.expires_at > now())
		) s ORDER BY x`, Kind, *folder, itemOwner)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if out == nil {
		out = []string{}
	}
	return out, err
}

// afterMove publishes the change to both folders and lets access revoke
// subscriptions people lost (after the commit).
func (s *Service) afterMove(ctx context.Context, before, after *Item) {
	access.Changed(ctx, access.Ref{Kind: Kind, ID: after.ID}, nil)
	s.changed(ctx, before, "moved")
	s.changed(ctx, after, "moved")
}

// trash marks an item and its live subtree trashed, with it as the root.
func (s *Service) trash(ctx context.Context, tx pgx.Tx, me uuid.UUID, it *Item) error {
	if it.TrashedAt != nil {
		return nil
	}
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM files_items WHERE id = $1
			UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id WHERE c.trashed_at IS NULL
		)
		UPDATE files_items SET trashed_at = now(), trashed_by = $2, trash_root = $1,
		       purge_after = now() + make_interval(days => $3), updated_at = now()
		WHERE id IN (SELECT id FROM sub) RETURNING id, type`, it.ID, nullID(me), s.cfg.trashDays)
	if err != nil {
		return err
	}
	if err := runTypeHooks(ctx, tx, rows, func(t ItemType) func(context.Context, pgx.Tx, uuid.UUID) error { return t.OnTrash }); err != nil {
		return err
	}
	s.logActivity(ctx, tx, it.OrgID, it.ID, it.ParentID, me, "trashed", nil)
	return nil
}

// runTypeHooks calls a registered type's hook for each (id, type) row.
func runTypeHooks(ctx context.Context, tx pgx.Tx, rows pgx.Rows, pick func(ItemType) func(context.Context, pgx.Tx, uuid.UUID) error) error {
	type row struct {
		ID   uuid.UUID
		Type string
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[row])
	if err != nil {
		return err
	}
	for _, r := range list {
		if t, ok := ItemTypeOf(r.Type); ok {
			if fn := pick(t); fn != nil {
				if err := fn(ctx, tx, r.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// restore brings a trashed item (and what was trashed with it) back where
// it was, numbering its name if it's taken.
func (s *Service) restore(ctx context.Context, tx pgx.Tx, me uuid.UUID, it *Item) (*Item, error) {
	if it.TrashedAt == nil {
		return nil, errNotTrashed
	}
	var root uuid.UUID
	var parentTrashed bool
	var parentName *string
	if err := tx.QueryRow(ctx, `
		SELECT i.trash_root, p.trashed_at IS NOT NULL, p.name FROM files_items i LEFT JOIN files_items p ON p.id = i.parent_id
		WHERE i.id = $1 FOR UPDATE OF i`, it.ID).Scan(&root, &parentTrashed, &parentName); err != nil {
		return nil, err
	}
	if root != it.ID {
		return nil, httpx.NewError(http.StatusConflict, "restore_parent", "It was trashed with its folder. Restore the folder instead.")
	}
	if parentTrashed {
		return nil, httpx.NewError(http.StatusConflict, "restore_parent", "Its folder “"+derefStr(parentName)+"” is in the trash. Restore that folder first.")
	}
	pl := &placement{orgID: it.OrgID, drive: it.Drive, owner: it.OwnerID}
	if it.ParentID != nil {
		pl.parent = &Item{ID: *it.ParentID}
	}
	name, err := freeName(ctx, tx, pl, it.Name, it.Type != TypeFolder, true, it.ID)
	if err != nil {
		return nil, err
	}
	if name != it.Name {
		if _, err := tx.Exec(ctx, `UPDATE files_items SET name = $2 WHERE id = $1`, it.ID, name); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, `
		UPDATE files_items SET trashed_at = NULL, trashed_by = NULL, trash_root = NULL, purge_after = NULL, updated_at = now()
		WHERE trash_root = $1 RETURNING id, type`, it.ID)
	if err != nil {
		return nil, err
	}
	if err := runTypeHooks(ctx, tx, rows, func(t ItemType) func(context.Context, pgx.Tx, uuid.UUID) error { return t.OnRestore }); err != nil {
		if db.IsUniqueViolation(err, "files_items_name_key") {
			return nil, errNameTaken
		}
		return nil, err
	}
	s.logActivity(ctx, tx, it.OrgID, it.ID, it.ParentID, me, "restored", nil)
	return s.getItem(ctx, tx, me, it.ID)
}

// purge deletes an item and everything under it for good: registered types
// clean up first, versions' objects are queued by trigger, access rows go
// with the root's.
func (s *Service) purge(ctx context.Context, tx pgx.Tx, rootID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id, type FROM files_items WHERE id = $1
			UNION ALL SELECT c.id, c.type FROM files_items c JOIN sub ON c.parent_id = sub.id
		)
		SELECT id, type FROM sub WHERE type NOT IN ('folder', 'file')`, rootID)
	if err != nil {
		return err
	}
	if err := runTypeHooks(ctx, tx, rows, func(t ItemType) func(context.Context, pgx.Tx, uuid.UUID) error { return t.OnDelete }); err != nil {
		return err
	}
	// Children first, so version rows (and their trigger) go before folders.
	if _, err := tx.Exec(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id, 0 AS depth FROM files_items WHERE id = $1
			UNION ALL SELECT c.id, sub.depth + 1 FROM files_items c JOIN sub ON c.parent_id = sub.id
		)
		DELETE FROM files_items WHERE id IN (SELECT id FROM sub)`, rootID); err != nil {
		return err
	}
	return access.DeleteResource(ctx, tx, access.Ref{Kind: Kind, ID: rootID})
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- reading routes -----------------------------------------------------------------

func idParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		return uuid.Nil, errItemNotFound
	}
	return id, nil
}

func orgParam(r *http.Request) uuid.UUID {
	m, _ := orgs.FromContext(r.Context())
	return m.OrgID
}

func parseListOpts(r *http.Request) listOpts {
	q := r.URL.Query()
	p := httpx.ParsePage(r)
	o := listOpts{Sort: q.Get("sort"), Desc: q.Get("dir") == "desc", Cursor: p.Cursor, Limit: p.Limit, Thumbs: q.Get("thumbs") == "1"}
	if q.Get("limit") == "" {
		o.Limit = 50
	}
	switch o.Sort {
	case "name", "updated", "size":
	default:
		o.Sort = "name"
	}
	return o
}

// listTypes: GET /v1/files/types.
func (s *Service) listTypes(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, map[string]any{"items": typeSummaries()})
	return nil
}

// driveInfo: GET /v1/files/orgs/{orgID}/drive (member).
func (s *Service) driveInfo(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	used, err := s.usedBytes(ctx, s.d.Pool, orgID)
	if err != nil {
		return err
	}
	can, err := access.Allowed(ctx, s.d.Pool, orgID, me, PermCreateShared)
	if err != nil {
		return err
	}
	httpx.OK(w, DriveInfo{OrgID: orgID, UsedBytes: used, QuotaBytes: s.cfg.quotaBytes, MaxUploadBytes: s.cfg.maxUpload,
		TrashDays: s.cfg.trashDays, CanCreateShared: can, Types: typeSummaries()})
	return nil
}

func (s *Service) usedBytes(ctx context.Context, q db.Querier, orgID uuid.UUID) (int64, error) {
	var used int64
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(size), 0)::bigint FROM files_versions WHERE org_id = $1`, orgID).Scan(&used)
	return used, err
}

// listSection: GET /v1/files/orgs/{orgID}/items?section=&sort=&dir=&cursor=&limit=&thumbs=1 (member).
func (s *Service) listSection(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	section := r.URL.Query().Get("section")
	if section == "" {
		section = SectionMy
	}
	page, err := s.section(ctx, s.d.Pool, me, false, orgParam(r), section, parseListOpts(r))
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// searchRoute: GET /v1/files/orgs/{orgID}/search?q=&cursor= (member).
func (s *Service) searchRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	org := orgParam(r)
	p := httpx.ParsePage(r)
	page, err := s.searchItems(ctx, s.d.Pool, me, false, &org, r.URL.Query().Get("q"), p.Cursor, p.Limit)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// getItemRoute: GET /v1/files/items/{id} (view) → {item, path, root}.
func (s *Service) getItemRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	it, err := s.load(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	path, root, err := s.path(ctx, s.d.Pool, me, false, it)
	if err != nil {
		return err
	}
	if it.TrashedAt == nil {
		s.touchRecent(ctx, me, it, "viewed")
	}
	httpx.OK(w, ItemDetail{Item: it, Path: path, Root: root})
	return nil
}

// childrenRoute: GET /v1/files/items/{id}/children?sort=&dir=&cursor=&thumbs=1 (view).
func (s *Service) childrenRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	folder, err := s.loadLive(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if !folder.IsFolder() {
		return errNotFolder
	}
	page, err := s.children(ctx, s.d.Pool, me, false, id, parseListOpts(r))
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// ---- writing routes -----------------------------------------------------------------

type createFolderReq struct {
	ParentID   *uuid.UUID    `json:"parent_id"`
	Name       string        `json:"name"`
	Shared     bool          `json:"shared"`      // a top-level shared folder (files.create_shared_folders)
	General    *access.Level `json:"general"`     // shared folders: everyone in the org at view, comment or edit
	OnConflict string        `json:"on_conflict"` // rename (default) | fail
}

// createFolder: POST /v1/files/orgs/{orgID}/folders.
func (s *Service) createFolder(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	var req createFolderReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var it *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var pl *placement
		general := access.None
		if req.Shared {
			if req.ParentID != nil {
				return httpx.BadRequest("Shared folders sit at the top level. Leave parent_id out.")
			}
			ok, err := access.Allowed(ctx, tx, orgID, me, PermCreateShared)
			if err != nil {
				return err
			}
			if !ok {
				return permissionRequired(PermCreateShared, "Create shared folders")
			}
			if req.General != nil {
				if *req.General < access.View || *req.General > access.Edit {
					return httpx.BadRequest("Everyone in the " + orgs.Noun() + " can view, comment or edit.")
				}
				general = *req.General
			}
			pl = &placement{orgID: orgID, drive: DriveShared}
		} else {
			var err error
			if pl, err = s.placeFor(ctx, tx, me, orgID, req.ParentID); err != nil {
				return err
			}
		}
		id, err := s.insertItemWith(ctx, tx, pl, TypeFolder, req.Name, me, req.OnConflict != "fail", general, false)
		if err != nil {
			return err
		}
		if req.Shared {
			// The org owns it; its creator manages it.
			if err := access.AddGrant(ctx, tx, access.Ref{Kind: Kind, ID: id}, access.SubjectRef{Type: "user", ID: me}, access.Manage, me); err != nil {
				return err
			}
		}
		s.logActivity(ctx, tx, orgID, id, pl.parentID(), me, "created", map[string]any{"type": TypeFolder})
		it, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, it, "created")
	httpx.JSON(w, http.StatusCreated, it)
	return nil
}

func permissionRequired(key, title string) *httpx.Error {
	e := httpx.NewError(http.StatusForbidden, "permission_required",
		"You don't have the \""+title+"\" permission in this "+orgs.Noun()+". Ask an admin.")
	e.Details = map[string]any{"permission": key}
	return e
}

type renameReq struct {
	Name string `json:"name"`
}

// renameRoute: PATCH /v1/files/items/{id} {name} (edit).
func (s *Service) renameRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req renameReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.load(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		out, err = s.rename(ctx, tx, me, it, req.Name)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, out, "renamed")
	httpx.OK(w, out)
	return nil
}

type moveReq struct {
	ParentID *uuid.UUID `json:"parent_id"` // null: the top of your My drive
}

// moveRoute: POST /v1/files/items/{id}/move {parent_id} (edit on both; manage across drives).
func (s *Service) moveRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req moveReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	before, after, err := s.moveOne(ctx, me, id, req.ParentID)
	if err != nil {
		return err
	}
	s.afterMove(ctx, before, after)
	httpx.OK(w, after)
	return nil
}

func (s *Service) moveOne(ctx context.Context, me, id uuid.UUID, dest *uuid.UUID) (before, after *Item, err error) {
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if before, err = s.load(ctx, tx, me, id, access.Edit); err != nil {
			return err
		}
		after, err = s.move(ctx, tx, me, before, dest)
		return err
	})
	return before, after, err
}

type copyReq struct {
	ParentID *uuid.UUID `json:"parent_id"` // null: the top of your My drive
	Name     string     `json:"name"`      // default: the same name, numbered if taken
}

// copyRoute: POST /v1/files/items/{id}/copy (view on it, edit on the folder).
func (s *Service) copyRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req copyReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	src, err := s.loadLive(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	out, err := s.copyItem(ctx, me, src, req.ParentID, req.Name)
	if err != nil {
		return err
	}
	s.changed(ctx, out, "created")
	httpx.JSON(w, http.StatusCreated, out)
	return nil
}

// trashRoute: POST /v1/files/items/{id}/trash (edit).
func (s *Service) trashRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	it, err := s.trashOne(ctx, me, id)
	if err != nil {
		return err
	}
	s.changed(ctx, it, "trashed")
	httpx.NoContent(w)
	return nil
}

func (s *Service) trashOne(ctx context.Context, me, id uuid.UUID) (*Item, error) {
	var it *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if it, err = s.load(ctx, tx, me, id, access.Edit); err != nil {
			return err
		}
		return s.trash(ctx, tx, me, it)
	})
	return it, err
}

// restoreRoute: POST /v1/files/items/{id}/restore (edit).
func (s *Service) restoreRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	it, err := s.restoreOne(ctx, me, id)
	if err != nil {
		return err
	}
	s.changed(ctx, it, "restored")
	httpx.OK(w, it)
	return nil
}

func (s *Service) restoreOne(ctx context.Context, me, id uuid.UUID) (*Item, error) {
	var out *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.load(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		out, err = s.restore(ctx, tx, me, it)
		return err
	})
	return out, err
}

// deleteRoute: DELETE /v1/files/items/{id} (manage; trashed items only).
func (s *Service) deleteRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	if _, err := s.deleteOne(ctx, me, id); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) deleteOne(ctx context.Context, me, id uuid.UUID) (*Item, error) {
	var it *Item
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if it, err = s.load(ctx, tx, me, id, access.Manage); err != nil {
			return err
		}
		if it.TrashedAt == nil {
			return errNotTrashed
		}
		var root uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT trash_root FROM files_items WHERE id = $1 FOR UPDATE`, id).Scan(&root); err != nil {
			return err
		}
		if root != id {
			return httpx.NewError(http.StatusConflict, "restore_parent", "It was trashed with its folder. Delete the folder instead.")
		}
		return s.purge(ctx, tx, id)
	})
	return it, err
}

// emptyTrash: DELETE /v1/files/orgs/{orgID}/trash — deletes for good every
// trashed item in the org the caller manages → {deleted}.
func (s *Service) emptyTrash(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id FROM files_items WHERE org_id = $1 AND trash_root = id AND access_level($2, 'files.item', id) >= 4
		ORDER BY trashed_at LIMIT 1000`, orgParam(r), me)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	deleted := 0
	for _, id := range ids {
		if _, err := s.deleteOne(ctx, me, id); err == nil {
			deleted++
		} else if !isClientError(err) {
			return err
		}
	}
	httpx.OK(w, map[string]int{"deleted": deleted})
	return nil
}

func isClientError(err error) bool {
	var e *httpx.Error
	return errors.As(err, &e) && e.Status < 500
}

// star / unstar: PUT / DELETE /v1/files/items/{id}/star (view).
func (s *Service) star(w http.ResponseWriter, r *http.Request) error {
	return s.setStar(w, r, true)
}

func (s *Service) unstar(w http.ResponseWriter, r *http.Request) error {
	return s.setStar(w, r, false)
}

func (s *Service) setStar(w http.ResponseWriter, r *http.Request, on bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	if err := s.starOne(ctx, me, id, on); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) starOne(ctx context.Context, me, id uuid.UUID, on bool) error {
	if !on {
		_, err := s.d.Pool.Exec(ctx, `DELETE FROM files_stars WHERE user_id = $1 AND item_id = $2`, me, id)
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	_, err := s.d.Pool.Exec(ctx, `INSERT INTO files_stars (user_id, item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, me, id)
	return err
}

// ---- batch ------------------------------------------------------------------------

type batchReq struct {
	Action   string      `json:"action"` // move | trash | restore | delete | star | unstar
	IDs      []uuid.UUID `json:"ids"`
	ParentID *uuid.UUID  `json:"parent_id"` // move: the folder (null: the top of your My drive)
}

// BatchResult is one item's outcome in POST /v1/files/batch.
type BatchResult struct {
	ID    uuid.UUID    `json:"id"`
	OK    bool         `json:"ok"`
	Error *httpx.Error `json:"error,omitempty"`
}

// batch: POST /v1/files/batch {action, ids, parent_id?} → {results}. Each
// item runs on its own, with the same checks as its single-item route.
func (s *Service) batch(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var req batchReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxBatch {
		return httpx.BadRequest("Choose between 1 and " + strconv.Itoa(maxBatch) + " items.")
	}
	results := make([]BatchResult, 0, len(req.IDs))
	for _, id := range req.IDs {
		var err error
		switch req.Action {
		case "move":
			var before, after *Item
			if before, after, err = s.moveOne(ctx, me, id, req.ParentID); err == nil {
				s.afterMove(ctx, before, after)
			}
		case "trash":
			var it *Item
			if it, err = s.trashOne(ctx, me, id); err == nil {
				s.changed(ctx, it, "trashed")
			}
		case "restore":
			var it *Item
			if it, err = s.restoreOne(ctx, me, id); err == nil {
				s.changed(ctx, it, "restored")
			}
		case "delete":
			_, err = s.deleteOne(ctx, me, id)
		case "star", "unstar":
			err = s.starOne(ctx, me, id, req.Action == "star")
		default:
			return httpx.BadRequest("action must be move, trash, restore, delete, star or unstar.")
		}
		res := BatchResult{ID: id, OK: err == nil}
		if err != nil {
			var e *httpx.Error
			if !errors.As(err, &e) {
				return err
			}
			res.Error = e
		}
		results = append(results, res)
	}
	httpx.OK(w, map[string]any{"results": results})
	return nil
}
