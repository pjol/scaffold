package repos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/files"
	"scaffoldapp/backend/internal/httpx"
)

// "Turn into repository": a drive folder becomes a repository with the
// same name in the same place, its files the first commit, its sharing
// carried over; the folder itself moves to the trash (restorable). Needs
// manage on the folder (sharing moves with it) and the create permission.
// Refused when the folder holds anything the caller can't see (it would
// vanish into the trash unseen), another repository, or more than the
// limits below.

const (
	maxImportFiles = 5000
	importTimeout  = 5 * time.Minute
)

// ImportSkip is a drive entry the import leaves out, and why.
type ImportSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ImportPlan is what turning a folder into a repository would do.
type ImportPlan struct {
	FolderID     uuid.UUID    `json:"folder_id"`
	Name         string       `json:"name"`
	Files        int          `json:"files"`
	Bytes        int64        `json:"bytes"`
	EmptyFolders int          `json:"empty_folders"`
	Skipped      []ImportSkip `json:"skipped"`
	Blockers     []string     `json:"blockers"`
	CanConvert   bool         `json:"can_convert"`

	folder  files.Item
	sources []ImportSource
}

func (s *Service) planImport(ctx context.Context, me *auth.Identity, folderID uuid.UUID) (*ImportPlan, error) {
	folder, err := files.GetItem(ctx, s.d.Pool, me.UserID, folderID)
	if err != nil {
		return nil, err
	}
	if folder.Type != files.TypeFolder || folder.TrashedAt != nil {
		return nil, httpx.NotFound("Folder not found.")
	}
	plan := &ImportPlan{FolderID: folderID, Name: folder.Name, Skipped: []ImportSkip{}, Blockers: []string{}, folder: folder}
	block := func(msg string) { plan.Blockers = append(plan.Blockers, msg) }
	if lvl, err := access.LevelOf(ctx, s.d.Pool, me.UserID, itemKind, folderID); err != nil {
		return nil, err
	} else if lvl < access.Manage {
		block("Only people who manage this folder can turn it into a repository, since its sharing moves to the repository.")
	}
	if ok, err := access.Allowed(ctx, s.d.Pool, folder.OrgID, me.UserID, PermCreate); err != nil {
		return nil, err
	} else if !ok {
		block("Your workspace doesn't let you create repositories.")
	}
	if folder.ParentID == nil && (folder.Drive != files.DriveMy || folder.OwnerID == nil || *folder.OwnerID != me.UserID) {
		block("Folders at the top of a shared drive or someone else's drive can't become repositories. Turn a folder inside it into one instead.")
	}
	var walked int
	folders := map[string]bool{} // folder path → holds a file somewhere below
	err = files.WalkFolder(ctx, s.d.Pool, me.UserID, folderID, func(e files.Entry) error {
		walked++
		switch e.Type {
		case files.TypeFolder:
			if _, seen := folders[e.Path]; !seen {
				folders[e.Path] = false
			}
			return nil
		case files.TypeFile:
		default:
			what := e.Type
			if t, ok := files.ItemTypeOf(e.Type); ok {
				what = strings.ToLower(t.Title)
			}
			block(fmt.Sprintf("%s is a %s. Move it out of the folder first.", e.Path, what))
			return nil
		}
		p, perr := CleanPath(e.Path)
		switch {
		case perr != nil || p != e.Path:
			plan.Skipped = append(plan.Skipped, ImportSkip{Path: e.Path, Reason: "a name git can't store"})
			return nil
		case e.VersionID == nil:
			plan.Skipped = append(plan.Skipped, ImportSkip{Path: e.Path, Reason: "still uploading"})
			return nil
		case e.Size > s.maxPushBytes:
			plan.Skipped = append(plan.Skipped, ImportSkip{Path: e.Path, Reason: "larger than " + formatBytes(s.maxPushBytes)})
			return nil
		}
		for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
			folders[dir] = true
		}
		plan.Files++
		plan.Bytes += e.Size
		itemID, size := e.ID, e.Size
		plan.sources = append(plan.sources, ImportSource{Path: p, Size: size, Open: func(ctx context.Context) (io.ReadCloser, error) {
			rc, _, err := files.OpenFile(ctx, me.UserID, itemID)
			return rc, err
		}})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, holdsFiles := range folders {
		if !holdsFiles {
			plan.EmptyFolders++
		}
	}
	// Everything under the folder must be visible to the caller.
	var total int
	if err := s.d.Pool.QueryRow(ctx, `
		WITH RECURSIVE t AS (
			SELECT id, type, 1 AS depth FROM files_items WHERE parent_id = $1 AND trashed_at IS NULL
			UNION ALL SELECT c.id, c.type, t.depth + 1 FROM t JOIN files_items c ON c.parent_id = t.id
			WHERE t.type = 'folder' AND c.trashed_at IS NULL AND t.depth < 64
		) SELECT count(*) FROM t`, folderID).Scan(&total); err != nil {
		return nil, err
	}
	if total > walked {
		block(fmt.Sprintf("%d %s in this folder %s private to other people, so it can't move to the trash with the rest.", total-walked, plural(total-walked, "item", "items"), plural(total-walked, "is", "are")))
	}
	switch {
	case plan.Files > maxImportFiles:
		block(fmt.Sprintf("The folder has %d files; at most %d can be imported. Push it with git instead.", plan.Files, maxImportFiles))
	case plan.Bytes > s.maxPushBytes:
		block(fmt.Sprintf("The files add up to more than %s. Push them with git instead.", formatBytes(s.maxPushBytes)))
	case plan.Files == 0:
		block("The folder has no files to commit.")
	}
	plan.CanConvert = len(plan.Blockers) == 0
	return plan, nil
}

// importPreview is GET /v1/repos/folders/{folderID}/preview.
func (s *Service) importPreview(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "folderID")
	if err != nil {
		return err
	}
	plan, err := s.planImport(r.Context(), me, id)
	if err != nil {
		return err
	}
	if len(plan.Skipped) > 100 {
		plan.Skipped = plan.Skipped[:100]
	}
	httpx.OK(w, plan)
	return nil
}

type importReq struct {
	FolderID      uuid.UUID `json:"folder_id"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	Message       string    `json:"message"`
}

// importFolder is POST /v1/repos/import.
func (s *Service) importFolder(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	var req importReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if err := s.git.CheckRefFormat(ctx, "heads", branch); err != nil {
		return httpx.BadRequest("That isn't a valid branch name.")
	}
	desc := strings.TrimSpace(req.Description)
	if len([]rune(desc)) > 350 {
		return httpx.BadRequest("Keep the description under 350 characters.")
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Import files from the drive"
	}
	plan, err := s.planImport(ctx, me, req.FolderID)
	if err != nil {
		return err
	}
	if !plan.CanConvert {
		e := httpx.NewError(http.StatusConflict, "cannot_convert", strings.Join(plan.Blockers, " "))
		e.Details = map[string]any{"blockers": plan.Blockers}
		return e
	}
	folder := plan.folder
	var repo *Repo
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := files.TrashItem(ctx, tx, me.UserID, folder.ID); err != nil {
			return err
		}
		var err error
		if repo, err = s.newRepo(ctx, tx, me, folder.OrgID, folder.ParentID, folder.Name, desc, branch); err != nil {
			return err
		}
		// The folder's own sharing moves to the repository's drive item.
		if err := s.copySharing(ctx, tx, folder.ID, repo.ID); err != nil {
			return err
		}
		who := s.person(me)
		if _, err := s.git.FastImport(ctx, repo.ID, branch, plan.sources, message, who, who, importTimeout); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE repos_repos SET pushed_at = now() WHERE id = $1`, repo.ID)
		return err
	})
	if err != nil {
		if repo != nil {
			_ = s.git.Remove(repo.ID)
		}
		return err
	}
	access.Changed(context.WithoutCancel(ctx), access.Ref{Kind: itemKind, ID: folder.ID}, nil)
	s.updateSizeSoon(ctx, repo.ID)
	repo, err = s.loadRepo(ctx, s.d.Pool, repo.ID)
	if err != nil {
		return err
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, me.UserID, RepoKind, repo.ID)
	if err != nil {
		return err
	}
	v, err := s.view(ctx, repo, lvl)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, v)
	return nil
}

// copySharing gives the repository's new drive item the folder's own grants
// (through access.AddGrant, which applies the share dialog's checks) and its
// restricted flag, so the repository is shared exactly like the folder was.
// A grant to someone who can't be shared with any more (a suspended
// account) stays on the trashed folder only.
func (s *Service) copySharing(ctx context.Context, tx pgx.Tx, folderID, itemID uuid.UUID) error {
	type grant struct {
		Type  string
		ID    uuid.UUID
		Level int
		By    *uuid.UUID
	}
	rows, err := tx.Query(ctx, `SELECT subject_type, subject_id, level, granted_by FROM access_grants
		WHERE resource_kind = $1 AND resource_id = $2 ORDER BY created_at, id`, itemKind, folderID)
	if err != nil {
		return err
	}
	grants, err := pgx.CollectRows(rows, pgx.RowToStructByPos[grant])
	if err != nil {
		return err
	}
	ref := access.Ref{Kind: itemKind, ID: itemID}
	for _, g := range grants {
		by := uuid.Nil
		if g.By != nil {
			by = *g.By
		}
		err := access.AddGrant(ctx, tx, ref, access.SubjectRef{Type: g.Type, ID: g.ID}, access.Level(g.Level), by)
		var apiErr *httpx.Error
		if err != nil && g.Type == "user" && errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			continue
		}
		if err != nil {
			return err
		}
	}
	// Restricted folders stay restricted.
	folder, err := access.Lookup(ctx, tx, access.Ref{Kind: itemKind, ID: folderID})
	if err != nil {
		return err
	}
	return access.SetInherits(ctx, tx, ref, folder.Inherits)
}
