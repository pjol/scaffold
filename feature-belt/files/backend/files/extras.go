package files

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Locks (check out / check in), folder colours, named and kept versions,
// handing a My drive item to a colleague, and storage used per person.

// lockedForOthers answers 423 item_locked when someone other than me checked
// the file out.
func lockedForOthers(it *Item, me uuid.UUID) error {
	if it.Lock == nil || it.Lock.By != nil && it.Lock.By.ID == me {
		return nil
	}
	who := "Someone"
	if it.Lock.By != nil && it.Lock.By.Name != "" {
		who = it.Lock.By.Name
	}
	e := httpx.NewError(http.StatusLocked, "item_locked", who+" checked this file out. Ask them to check it in, or ask someone who manages it to break the lock.")
	e.Details = map[string]any{"locked_by": it.Lock.By, "locked_at": it.Lock.At}
	return e
}

type lockReq struct {
	Note string `json:"note"`
}

// lockRoute: POST /v1/files/items/{id}/lock {note?} (edit; files) — check out.
func (s *Service) lockRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req lockReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	note := strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(note) > 200 {
		return httpx.BadRequest("Keep the note under 200 characters.")
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.loadLive(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		if it.Type != TypeFile {
			return errNotFile
		}
		if _, err := tx.Exec(ctx, `UPDATE files_items SET locked_by = $2, locked_at = COALESCE(locked_at, now()), lock_note = $3 WHERE id = $1`, id, me, note); err != nil {
			return err
		}
		if it.Lock == nil {
			s.logActivity(ctx, tx, it.OrgID, id, it.ParentID, me, "locked", map[string]any{"note": note})
		}
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, out, "locked")
	httpx.OK(w, out)
	return nil
}

// unlockRoute: DELETE /v1/files/items/{id}/lock — check in (the person who
// checked it out) or break the lock (manage).
func (s *Service) unlockRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		lvl, err := access.Require(ctx, tx, me, Kind, id, access.View)
		if err != nil {
			return err
		}
		it, err := s.getItem(ctx, tx, me, id)
		if err != nil {
			return err
		}
		if it.Lock == nil {
			out = it
			return nil
		}
		mine := it.Lock.By != nil && it.Lock.By.ID == me
		if !mine && lvl < access.Manage {
			return lockedForOthers(it, me)
		}
		if _, err := tx.Exec(ctx, `UPDATE files_items SET locked_by = NULL, locked_at = NULL, lock_note = '' WHERE id = $1`, id); err != nil {
			return err
		}
		data := map[string]any{}
		if !mine {
			data["broken"] = true
			if it.Lock.By != nil {
				data["was_locked_by"] = it.Lock.By.Name
			}
		}
		s.logActivity(ctx, tx, it.OrgID, id, it.ParentID, me, "unlocked", data)
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, out, "unlocked")
	httpx.OK(w, out)
	return nil
}

var folderColors = map[string]bool{"": true, "primary": true, "accent": true, "success": true, "warning": true, "danger": true, "info": true}

type colorReq struct {
	Color string `json:"color"`
}

// colorRoute: PUT /v1/files/items/{id}/color {color} (edit; folders).
func (s *Service) colorRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req colorReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if !folderColors[req.Color] {
		return httpx.BadRequest("color must be one of primary, accent, success, warning, danger, info, or empty.")
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.loadLive(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		if !it.IsFolder() {
			return errNotFolder
		}
		if _, err := tx.Exec(ctx, `UPDATE files_items SET color = $2 WHERE id = $1`, id, req.Color); err != nil {
			return err
		}
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, out, "color")
	httpx.OK(w, out)
	return nil
}

type versionPatch struct {
	Name        *string `json:"name"`
	KeepForever *bool   `json:"keep_forever"`
}

// patchVersion: PATCH /v1/files/items/{id}/versions/{versionID} {name?, keep_forever?} (edit).
func (s *Service) patchVersion(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := idParam(r)
	if err != nil {
		return err
	}
	vid, err := uuid.Parse(chi.URLParam(r, "versionID"))
	if err != nil {
		return httpx.NotFound("Version not found.")
	}
	var req versionPatch
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if utf8.RuneCountInString(n) > 100 {
			return httpx.BadRequest("Keep the version name under 100 characters.")
		}
		req.Name = &n
	}
	var list []Version
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.loadLive(ctx, tx, me, id, access.Edit)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE files_versions SET name = COALESCE($3, name), keep_forever = COALESCE($4, keep_forever) WHERE id = $1 AND item_id = $2`,
			vid, id, req.Name, req.KeepForever)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("Version not found.")
		}
		if req.Name != nil && *req.Name != "" {
			s.logActivity(ctx, tx, it.OrgID, id, it.ParentID, me, "version_named", map[string]any{"name": *req.Name})
		}
		list, err = s.versions(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

type ownerReq struct {
	UserID uuid.UUID `json:"user_id"`
}

// transferOwner: POST /v1/files/items/{id}/owner {user_id} — the owner hands a
// top-level My drive item (and everything in it) to another member of the
// org; it moves to the top of their My drive and the previous owner keeps
// edit access. People only, never agents.
func (s *Service) transferOwner(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	if access.ViaAgent(ctx) {
		return httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't change who owns an item.")
	}
	id, err := idParam(r)
	if err != nil {
		return err
	}
	var req ownerReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var out *Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		it, err := s.loadLive(ctx, tx, me, id, access.Manage)
		if err != nil {
			return err
		}
		if it.Drive != DriveMy || it.OwnerID == nil || *it.OwnerID != me {
			return httpx.NewError(http.StatusForbidden, "owner_required", "Only the owner can hand over an item from their My drive.")
		}
		if req.UserID == me {
			return httpx.BadRequest("You already own it.")
		}
		role, err := orgs.RoleOf(ctx, tx, it.OrgID, req.UserID)
		if err != nil || role == "" {
			return httpx.BadRequest("Choose someone in this " + orgs.Noun() + ".")
		}
		pl := &placement{orgID: it.OrgID, drive: DriveMy, owner: &req.UserID}
		name, err := freeName(ctx, tx, pl, it.Name, it.Type == TypeFile, true, it.ID)
		if err != nil {
			return err
		}
		// The item itself changes owner, folder and name in one statement (the unique
		// name index sees only the final key); what's inside keys by its folder.
		if _, err := tx.Exec(ctx, `UPDATE files_items SET owner_id = $2, parent_id = NULL, name = $3, updated_at = now(), updated_by = $4 WHERE id = $1`,
			id, req.UserID, name, me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM files_items WHERE parent_id = $1
				UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id
			)
			UPDATE files_items SET owner_id = $2 WHERE id IN (SELECT id FROM sub)`, id, req.UserID); err != nil {
			return err
		}
		ref := access.Ref{Kind: Kind, ID: id}
		if err := access.MoveResource(ctx, tx, ref, nil); err != nil {
			return err
		}
		if err := access.SetOwnerTree(ctx, tx, ref, req.UserID); err != nil {
			return err
		}
		if err := access.AddGrant(ctx, tx, ref, access.SubjectRef{Type: "user", ID: me}, access.Edit, me); err != nil {
			return err
		}
		s.logActivity(ctx, tx, it.OrgID, id, nil, me, "owner_changed", map[string]any{"to": req.UserID})
		out, err = s.getItem(ctx, tx, me, id)
		return err
	})
	if err != nil {
		return err
	}
	s.changed(ctx, out, "moved")
	httpx.OK(w, out)
	return nil
}

// PersonUsage is one person's share of an org's storage: the versions they
// uploaded (every version and the trash count, like the quota).
type PersonUsage struct {
	Person *Person `json:"person"`
	Bytes  int64   `json:"bytes"`
	Files  int     `json:"files"`
}

// storageRoute: GET /v1/files/orgs/{orgID}/storage (member) → your usage, and
// everyone's for org owners and admins.
func (s *Service) storageRoute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	orgID := orgParam(r)
	role, err := orgs.RoleOf(ctx, s.d.Pool, orgID, me)
	if err != nil {
		return err
	}
	admin := role == orgs.RoleOwner || role == orgs.RoleAdmin
	rows, err := s.d.Pool.Query(ctx, `
		SELECT v.uploaded_by, u.display_name, u.avatar_url, sum(v.size)::bigint, count(DISTINCT v.item_id)::int
		FROM files_versions v LEFT JOIN users u ON u.id = v.uploaded_by
		WHERE v.org_id = $1 AND ($2 OR v.uploaded_by = $3)
		GROUP BY v.uploaded_by, u.display_name, u.avatar_url ORDER BY 4 DESC LIMIT 200`, orgID, admin, me)
	if err != nil {
		return err
	}
	defer rows.Close()
	people := []PersonUsage{}
	var mine PersonUsage
	for rows.Next() {
		var by *uuid.UUID
		var name, avatar *string
		var u PersonUsage
		if err := rows.Scan(&by, &name, &avatar, &u.Bytes, &u.Files); err != nil {
			return err
		}
		u.Person = person(by, name, avatar)
		if by != nil && *by == me {
			mine = u
		}
		if admin {
			people = append(people, u)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	used, err := s.usedBytes(ctx, s.d.Pool, orgID)
	if err != nil {
		return err
	}
	out := map[string]any{"used_bytes": used, "quota_bytes": s.cfg.quotaBytes, "mine": mine, "people": nil}
	if admin {
		out["people"] = people
	}
	httpx.OK(w, out)
	return nil
}
