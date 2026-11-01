package files

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
)

// DeleteAccount runs inside the account-deletion transaction.
//
//   - Shared folders and items in other people's drives are org content:
//     they stay; the person's name is cleared from them.
//   - Each top-level item of the person's My drive whose subtree anyone else
//     can open (a grant to a person, a team or the org) becomes an org-owned
//     shared folder item, so that work survives; org owners and admins
//     manage it.
//   - Every other top-level item (private to them) goes to the org's trash,
//     where org owners and admins can restore it for
//     FILES_DELETED_ACCOUNT_KEEP_DAYS; then it is purged. With 0 days it is
//     deleted now. Objects are deleted by the files.delete_objects job.
//   - Stars, recents, pending uploads and zip links are deleted.
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.org_id, r.name, r.trashed_at IS NOT NULL, EXISTS (
			WITH RECURSIVE sub AS (
				SELECT r.id AS id
				UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id
			)
			SELECT 1 FROM access_grants g JOIN sub ON g.resource_kind = 'files.item' AND g.resource_id = sub.id
			WHERE NOT (g.subject_type = 'user' AND g.subject_id = $1))
		FROM files_items r
		WHERE r.owner_id = $1 AND r.parent_id IS NULL`, userID)
	if err != nil {
		return err
	}
	type root struct {
		ID      uuid.UUID
		OrgID   uuid.UUID
		Name    string
		Trashed bool
		Shared  bool
	}
	roots, err := pgx.CollectRows(rows, pgx.RowToStructByPos[root])
	if err != nil {
		return err
	}
	for _, r := range roots {
		if !r.Shared && !r.Trashed && s.cfg.keepDays == 0 {
			if err := s.purge(ctx, tx, r.ID); err != nil {
				return err
			}
			continue
		}
		if !r.Shared && !r.Trashed {
			if _, err := tx.Exec(ctx, `
				WITH RECURSIVE sub AS (
					SELECT id FROM files_items WHERE id = $1
					UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id WHERE c.trashed_at IS NULL
				)
				UPDATE files_items SET trashed_at = now(), trashed_by = NULL, trash_root = $1,
				       purge_after = now() + make_interval(days => $2)
				WHERE id IN (SELECT id FROM sub)`, r.ID, s.cfg.keepDays); err != nil {
				return err
			}
		}
		if !r.Trashed && r.Shared {
			// Keep the name unless another shared folder has it.
			pl := &placement{orgID: r.OrgID, drive: DriveShared}
			name, err := freeName(ctx, tx, pl, r.Name, true, true, r.ID)
			if err != nil {
				return err
			}
			if name != r.Name {
				if _, err := tx.Exec(ctx, `UPDATE files_items SET name = $2 WHERE id = $1`, r.ID, name); err != nil {
					return err
				}
			}
		}
		if _, err := tx.Exec(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM files_items WHERE id = $1
				UNION ALL SELECT c.id FROM files_items c JOIN sub ON c.parent_id = sub.id
			)
			UPDATE files_items SET drive = 'shared', owner_id = NULL, updated_at = now() WHERE id IN (SELECT id FROM sub)`, r.ID); err != nil {
			return err
		}
		// Org owners and admins manage it now. An item missing from access's
		// registry (unreachable anyway) mustn't block the deletion.
		if err := access.SetOwnerTree(ctx, tx, access.Ref{Kind: Kind, ID: r.ID}, uuid.Nil); err != nil && !errors.Is(err, access.ErrNotRegistered) {
			return err
		}
	}
	for _, q := range []string{
		`UPDATE files_items SET created_by = NULL WHERE created_by = $1`,
		`UPDATE files_items SET updated_by = NULL WHERE updated_by = $1`,
		`UPDATE files_items SET trashed_by = NULL WHERE trashed_by = $1`,
		`UPDATE files_items SET public_disabled_by = NULL WHERE public_disabled_by = $1`,
		`UPDATE files_versions SET uploaded_by = NULL WHERE uploaded_by = $1`,
		`UPDATE files_activity SET actor_id = NULL WHERE actor_id = $1`,
		`DELETE FROM files_stars WHERE user_id = $1`,
		`DELETE FROM files_recents WHERE user_id = $1`,
		`DELETE FROM files_uploads WHERE user_id = $1`,
		`DELETE FROM files_archives WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns what the person made in drives (metadata; file
// contents stay downloadable in the app while they can open them), their
// uploads, stars and activity.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type item struct {
		ID        uuid.UUID  `json:"id"`
		OrgID     uuid.UUID  `json:"org_id"`
		Type      string     `json:"type"`
		Name      string     `json:"name"`
		MimeType  string     `json:"mime_type"`
		Size      int64      `json:"size"`
		InMyDrive bool       `json:"in_my_drive"`
		CreatedAt time.Time  `json:"created_at"`
		TrashedAt *time.Time `json:"trashed_at"`
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, org_id, type, name, mime_type, size, owner_id IS NOT DISTINCT FROM $1, created_at, trashed_at
		FROM files_items WHERE created_by = $1 OR owner_id = $1 ORDER BY created_at LIMIT 10000`, userID)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[item])
	if err != nil {
		return nil, err
	}
	type version struct {
		ItemID    uuid.UUID `json:"item_id"`
		Number    int       `json:"number"`
		Size      int64     `json:"size"`
		CreatedAt time.Time `json:"created_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT item_id, number, size, created_at FROM files_versions WHERE uploaded_by = $1 ORDER BY created_at LIMIT 10000`, userID)
	if err != nil {
		return nil, err
	}
	versions, err := pgx.CollectRows(rows, pgx.RowToStructByPos[version])
	if err != nil {
		return nil, err
	}
	type star struct {
		ItemID    uuid.UUID `json:"item_id"`
		CreatedAt time.Time `json:"created_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `SELECT item_id, created_at FROM files_stars WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	stars, err := pgx.CollectRows(rows, pgx.RowToStructByPos[star])
	if err != nil {
		return nil, err
	}
	type event struct {
		ItemID    uuid.UUID `json:"item_id"`
		Action    string    `json:"action"`
		CreatedAt time.Time `json:"created_at"`
	}
	rows, err = s.d.Pool.Query(ctx, `
		SELECT item_id, action, created_at FROM files_activity WHERE actor_id = $1 ORDER BY created_at LIMIT 10000`, userID)
	if err != nil {
		return nil, err
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByPos[event])
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"items":    orEmpty(items),
		"versions": orEmpty(versions),
		"starred":  orEmpty(stars),
		"activity": orEmpty(events),
	}, nil
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
