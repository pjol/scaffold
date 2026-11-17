package repos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// Releases: a tag (made in the app when it doesn't exist) with Markdown
// notes and uploaded files. Drafts are seen by people who can edit; a
// draft's tag is made when it's published. Assets are uploaded through
// storage (purpose repos.release_asset), then copied under the workspace's
// key so they outlive the uploader's account; downloads are short-lived
// signed links served as attachments.

// AssetPurpose is the upload purpose for release files.
const AssetPurpose = "repos.release_asset"

const maxAssetsPerRelease = 50

// Release is a release with its assets.
type Release struct {
	ID          uuid.UUID      `json:"id"`
	Tag         string         `json:"tag"`
	Name        string         `json:"name"`
	Body        string         `json:"body"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	TargetSHA   string         `json:"target_sha"`
	Author      *UserRef       `json:"author"`
	PublishedAt *time.Time     `json:"published_at"`
	CreatedAt   time.Time      `json:"created_at"`
	Latest      bool           `json:"latest"`
	Assets      []ReleaseAsset `json:"assets"`
	Mentions    []MentionRef   `json:"mentions"`
}

// ReleaseAsset is an uploaded file.
type ReleaseAsset struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	ContentType   string    `json:"content_type"`
	SizeBytes     int64     `json:"size_bytes"`
	DownloadCount int       `json:"download_count"`
	CreatedAt     time.Time `json:"created_at"`
}

const releaseSelect = `
	SELECT r.id, r.tag, r.name, r.body, r.draft, r.prerelease, r.target_sha, r.author_id, a.display_name, a.avatar_url, a.status,
	       r.published_at, r.created_at
	FROM repos_releases r LEFT JOIN users a ON a.id = r.author_id`

func scanRelease(row pgx.Row) (*Release, error) {
	var rl Release
	var aID *uuid.UUID
	var aName, aAv, aSt *string
	if err := row.Scan(&rl.ID, &rl.Tag, &rl.Name, &rl.Body, &rl.Draft, &rl.Prerelease, &rl.TargetSHA, &aID, &aName, &aAv, &aSt,
		&rl.PublishedAt, &rl.CreatedAt); err != nil {
		return nil, err
	}
	rl.Author = userRef(aID, aName, aAv, aSt)
	rl.Assets, rl.Mentions = []ReleaseAsset{}, []MentionRef{}
	return &rl, nil
}

func (s *Service) fillAssets(ctx context.Context, list []*Release) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(list))
	by := map[uuid.UUID]*Release{}
	for i, r := range list {
		ids[i], by[r.ID] = r.ID, r
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT release_id, id, name, content_type, size_bytes, download_count, created_at
		FROM repos_release_assets WHERE release_id = ANY($1) ORDER BY lower(name), id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid uuid.UUID
		var a ReleaseAsset
		if err := rows.Scan(&rid, &a.ID, &a.Name, &a.ContentType, &a.SizeBytes, &a.DownloadCount, &a.CreatedAt); err != nil {
			return err
		}
		by[rid].Assets = append(by[rid].Assets, a)
	}
	return rows.Err()
}

// latestReleaseID is the newest published, non-prerelease release.
func (s *Service) latestReleaseID(ctx context.Context, repoID uuid.UUID) uuid.UUID {
	var id uuid.UUID
	_ = s.d.Pool.QueryRow(ctx, `SELECT id FROM repos_releases WHERE repo_id = $1 AND NOT draft AND NOT prerelease
		ORDER BY published_at DESC NULLS LAST, id DESC LIMIT 1`, repoID).Scan(&id)
	return id
}

func (s *Service) listReleases(w http.ResponseWriter, r *http.Request) error {
	repo, lvl, err := s.repoLevelFromRoute(r, levelView)
	if err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), releaseSelect+` WHERE r.repo_id = $1 AND (NOT r.draft OR $2)
		AND ($3::timestamptz IS NULL OR (r.created_at, r.id) < ($3, $4)) ORDER BY r.created_at DESC, r.id DESC LIMIT $5`,
		repo.ID, lvl >= access.Edit, nullTime(c.T), c.ID, page.Limit+1)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (*Release, error) { return scanRelease(r) })
	if err != nil {
		return err
	}
	if err := s.fillAssets(r.Context(), list); err != nil {
		return err
	}
	latest := s.latestReleaseID(r.Context(), repo.ID)
	out := make([]Release, len(list))
	for i, rl := range list {
		rl.Latest = rl.ID == latest
		out[i] = *rl
	}
	httpx.OK(w, httpx.NewPage(out, page.Limit, func(rl Release) any { return cursor{T: rl.CreatedAt, ID: rl.ID} }))
	return nil
}

func (s *Service) releaseFromRoute(r *http.Request, min access.Level) (*Repo, *Release, access.Level, error) {
	repo, lvl, err := s.repoLevelFromRoute(r, min)
	if err != nil {
		return nil, nil, lvl, err
	}
	id, err := httpx.URLParamUUID(r, "releaseID")
	if err != nil {
		return nil, nil, lvl, httpx.NotFound("Release not found.")
	}
	rl, err := scanRelease(s.d.Pool.QueryRow(r.Context(), releaseSelect+` WHERE r.id = $1 AND r.repo_id = $2`, id, repo.ID))
	if db.IsNoRows(err) || (err == nil && rl.Draft && lvl < access.Edit) {
		return nil, nil, lvl, httpx.NotFound("Release not found.")
	}
	if err != nil {
		return nil, nil, lvl, err
	}
	if err := s.fillAssets(r.Context(), []*Release{rl}); err != nil {
		return nil, nil, lvl, err
	}
	rl.Latest = rl.ID == s.latestReleaseID(r.Context(), repo.ID)
	if rl.Mentions, err = s.mentionRefs(r.Context(), rl.Body); err != nil {
		return nil, nil, lvl, err
	}
	return repo, rl, lvl, nil
}

func (s *Service) getRelease(w http.ResponseWriter, r *http.Request) error {
	_, rl, _, err := s.releaseFromRoute(r, levelView)
	if err != nil {
		return err
	}
	httpx.OK(w, rl)
	return nil
}

type releaseReq struct {
	Tag        *string `json:"tag"`
	Target     *string `json:"target"` // branch or commit for a new tag (default: the default branch)
	Name       *string `json:"name"`
	Body       *string `json:"body"`
	Draft      *bool   `json:"draft"`
	Prerelease *bool   `json:"prerelease"`
}

// createRelease is POST /repos/{repoID}/releases: edit level. An existing
// tag is used as it is; otherwise the tag is made at target when the
// release is published.
func (s *Service) createRelease(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req releaseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if req.Tag == nil || strings.TrimSpace(*req.Tag) == "" {
		return httpx.BadRequest("Choose a tag for the release (v1.0.0).")
	}
	tag := strings.TrimSpace(*req.Tag)
	if err := s.git.CheckRefFormat(ctx, "tags", tag); err != nil || len(tag) > 200 {
		return httpx.BadRequest("That isn't a valid tag name.")
	}
	name, body := tag, ""
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		name = strings.Join(strings.Fields(*req.Name), " ")
	}
	if utf8.RuneCountInString(name) > 200 {
		return httpx.BadRequest("Keep the title under 200 characters.")
	}
	if req.Body != nil {
		if body, err = cleanLongText(*req.Body, "release notes"); err != nil {
			return err
		}
	}
	draft := req.Draft != nil && *req.Draft
	pre := req.Prerelease != nil && *req.Prerelease
	target, err := s.git.RefValue(ctx, repo.ID, "refs/tags/"+tag)
	if err != nil {
		return err
	}
	if target != "" {
		if target, err = s.git.ResolveCommit(ctx, repo.ID, target); err != nil {
			return httpx.BadRequest("The tag " + tag + " doesn't point at a commit.")
		}
	} else {
		ref := repo.DefaultBranch
		if req.Target != nil && strings.TrimSpace(*req.Target) != "" {
			ref = strings.TrimSpace(*req.Target)
		}
		at, err := s.resolve(ctx, repo, ref)
		if err != nil {
			return err
		}
		target = at.SHA
	}
	var id uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var published *time.Time
		if !draft {
			now := time.Now()
			published = &now
		}
		err := tx.QueryRow(ctx, `INSERT INTO repos_releases (repo_id, tag, name, body, draft, prerelease, target_sha, author_id, published_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, repo.ID, tag, name, body, draft, pre, target, me.UserID, published).Scan(&id)
		if db.IsUniqueViolation(err, "repos_releases_tag_key") {
			return httpx.Conflict("There's a release for " + tag + " already.")
		}
		if err != nil {
			return err
		}
		if !draft {
			return s.ensureTag(ctx, repo, tag, target)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !draft {
		s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: ZeroOID, New: target, Ref: "refs/tags/" + tag}}, "release")
	}
	rl, err := scanRelease(s.d.Pool.QueryRow(ctx, releaseSelect+` WHERE r.id = $1`, id))
	if err != nil {
		return err
	}
	if !draft {
		s.emitWebhook(ctx, repo, me, "release", "published", map[string]any{"release": s.releaseHookView(repo, rl)})
	}
	httpx.JSON(w, http.StatusCreated, rl)
	return nil
}

// ensureTag makes a lightweight tag at sha unless it exists (at any commit).
func (s *Service) ensureTag(ctx context.Context, repo *Repo, tag, sha string) error {
	cur, err := s.git.RefValue(ctx, repo.ID, "refs/tags/"+tag)
	if err != nil || cur != "" {
		return err
	}
	if err := s.git.UpdateRef(ctx, repo.ID, "refs/tags/"+tag, sha, ZeroOID, "release"); err != nil {
		if errors.Is(err, ErrRefMoved) {
			return httpx.Conflict("The tag " + tag + " was just made by someone else.")
		}
		return err
	}
	return nil
}

func (s *Service) updateRelease(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, rl, _, err := s.releaseFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req releaseReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Tag != nil && *req.Tag != rl.Tag {
		return httpx.BadRequest("A release keeps its tag. Delete it and make a new one instead.")
	}
	ctx := r.Context()
	name, body, draft, pre := rl.Name, rl.Body, rl.Draft, rl.Prerelease
	if req.Name != nil {
		name = strings.Join(strings.Fields(*req.Name), " ")
		if name == "" {
			name = rl.Tag
		}
		if utf8.RuneCountInString(name) > 200 {
			return httpx.BadRequest("Keep the title under 200 characters.")
		}
	}
	if req.Body != nil {
		if body, err = cleanLongText(*req.Body, "release notes"); err != nil {
			return err
		}
	}
	if req.Prerelease != nil {
		pre = *req.Prerelease
	}
	publishing := false
	if req.Draft != nil {
		if !*req.Draft && rl.Draft {
			publishing = true
		} else if *req.Draft && !rl.Draft {
			return httpx.BadRequest("A published release can't go back to draft.")
		}
		draft = *req.Draft
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE repos_releases SET name = $2, body = $3, draft = $4, prerelease = $5,
			published_at = CASE WHEN $4 THEN NULL ELSE COALESCE(published_at, now()) END, updated_at = now() WHERE id = $1`,
			rl.ID, name, body, draft, pre); err != nil {
			return err
		}
		if publishing {
			return s.ensureTag(ctx, repo, rl.Tag, rl.TargetSHA)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if publishing {
		s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: ZeroOID, New: rl.TargetSHA, Ref: "refs/tags/" + rl.Tag}}, "release")
	}
	out, err := scanRelease(s.d.Pool.QueryRow(ctx, releaseSelect+` WHERE r.id = $1`, rl.ID))
	if err != nil {
		return err
	}
	if err := s.fillAssets(ctx, []*Release{out}); err != nil {
		return err
	}
	if publishing {
		s.emitWebhook(ctx, repo, me, "release", "published", map[string]any{"release": s.releaseHookView(repo, out)})
	} else if !draft {
		s.emitWebhook(ctx, repo, me, "release", "edited", map[string]any{"release": s.releaseHookView(repo, out)})
	}
	httpx.OK(w, out)
	return nil
}

// deleteRelease is DELETE /releases/{id}?delete_tag=1: edit level; the
// tag stays unless asked (and it isn't protected by anything else).
func (s *Service) deleteRelease(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, rl, _, err := s.releaseFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	ctx := r.Context()
	var keys []string
	rows, err := s.d.Pool.Query(ctx, `SELECT storage_key FROM repos_release_assets WHERE release_id = $1`, rl.ID)
	if err != nil {
		return err
	}
	if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM repos_releases WHERE id = $1`, rl.ID); err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	if r.URL.Query().Get("delete_tag") == "1" {
		if cur, err := s.git.RefValue(ctx, repo.ID, "refs/tags/"+rl.Tag); err == nil && cur != "" {
			if err := s.git.DeleteRef(ctx, repo.ID, "refs/tags/"+rl.Tag, cur, "delete release"); err == nil {
				s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: cur, New: ZeroOID, Ref: "refs/tags/" + rl.Tag}}, "release")
			}
		}
	}
	if !rl.Draft {
		s.emitWebhook(ctx, repo, me, "release", "deleted", map[string]any{"release": s.releaseHookView(repo, rl)})
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	if len(keys) == 0 || s.d.Storage == nil {
		return
	}
	bg.Go(ctx, "repos.delete_assets", func(ctx context.Context) error {
		for _, k := range keys {
			if err := s.d.Storage.Delete(ctx, k); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
				slog.WarnContext(ctx, "repos: deleting an asset failed", "err", err)
			}
		}
		return nil
	})
}

// ---- assets --------------------------------------------------------------------

type assetReq struct {
	Key  string `json:"key"`  // from POST /v1/uploads with purpose repos.release_asset
	Name string `json:"name"` // the file name people download
}

func (s *Service) addAsset(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, rl, _, err := s.releaseFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	if s.d.Storage == nil {
		return httpx.NewError(http.StatusServiceUnavailable, "storage_unavailable", "File storage isn't configured.")
	}
	var req assetReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	if !storage.KeyOwnedBy(req.Key, AssetPurpose, me.UserID) {
		return httpx.BadRequest("Upload the file first (purpose repos.release_asset).")
	}
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(req.Name), "\\", "/"))
	if name == "" || name == "." || name == "/" || utf8.RuneCountInString(name) > 255 {
		return httpx.BadRequest("Name the file (up to 255 characters).")
	}
	if len(rl.Assets) >= maxAssetsPerRelease {
		return httpx.Conflict(fmt.Sprintf("A release holds up to %d files.", maxAssetsPerRelease))
	}
	info, err := s.d.Storage.Stat(ctx, req.Key)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return httpx.BadRequest("The upload didn't finish. Try again.")
	}
	if err != nil {
		return err
	}
	ct := info.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	key, err := storage.NewKey(AssetPurpose, repo.OrgID, ct)
	if err != nil {
		return httpx.BadRequest("That file type can't be stored.")
	}
	if err := s.d.Storage.Copy(ctx, req.Key, key); err != nil {
		return err
	}
	var a ReleaseAsset
	err = s.d.Pool.QueryRow(ctx, `INSERT INTO repos_release_assets (release_id, name, storage_key, content_type, size_bytes, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, name, content_type, size_bytes, download_count, created_at`,
		rl.ID, name, key, ct, info.Size, me.UserID).Scan(&a.ID, &a.Name, &a.ContentType, &a.SizeBytes, &a.DownloadCount, &a.CreatedAt)
	if db.IsUniqueViolation(err, "repos_release_assets_name_key") {
		_ = s.d.Storage.Delete(ctx, key)
		return httpx.Conflict("This release has a file named " + name + " already.")
	}
	if err != nil {
		_ = s.d.Storage.Delete(ctx, key)
		return err
	}
	_ = s.d.Storage.Delete(ctx, req.Key)
	httpx.JSON(w, http.StatusCreated, a)
	return nil
}

func (s *Service) assetFromRoute(r *http.Request, min access.Level) (*Repo, *Release, uuid.UUID, string, string, error) {
	repo, rl, _, err := s.releaseFromRoute(r, min)
	if err != nil {
		return nil, nil, uuid.Nil, "", "", err
	}
	aid, err := httpx.URLParamUUID(r, "assetID")
	if err != nil {
		return nil, nil, uuid.Nil, "", "", httpx.NotFound("File not found.")
	}
	var key, name string
	err = s.d.Pool.QueryRow(r.Context(), `SELECT storage_key, name FROM repos_release_assets WHERE id = $1 AND release_id = $2`, aid, rl.ID).Scan(&key, &name)
	if db.IsNoRows(err) {
		return nil, nil, uuid.Nil, "", "", httpx.NotFound("File not found.")
	}
	return repo, rl, aid, key, name, err
}

// downloadAsset is GET …/assets/{assetID}: a signed link (5 minutes), as an attachment.
func (s *Service) downloadAsset(w http.ResponseWriter, r *http.Request) error {
	_, _, aid, key, name, err := s.assetFromRoute(r, levelView)
	if err != nil {
		return err
	}
	if s.d.Storage == nil {
		return httpx.NewError(http.StatusServiceUnavailable, "storage_unavailable", "File storage isn't configured.")
	}
	u, err := s.d.Storage.PresignGetWith(r.Context(), key, 5*time.Minute, storage.GetOptions{
		ContentType: "application/octet-stream",
		Disposition: `attachment; filename="` + strings.NewReplacer(`"`, "", "\n", "", "\r", "").Replace(name) + `"`,
	})
	if err != nil {
		return err
	}
	_, _ = s.d.Pool.Exec(r.Context(), `UPDATE repos_release_assets SET download_count = download_count + 1 WHERE id = $1`, aid)
	httpx.OK(w, map[string]any{"url": u, "name": name})
	return nil
}

func (s *Service) deleteAsset(w http.ResponseWriter, r *http.Request) error {
	_, _, aid, key, _, err := s.assetFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `DELETE FROM repos_release_assets WHERE id = $1`, aid); err != nil {
		return err
	}
	s.deleteObjects(r.Context(), []string{key})
	httpx.NoContent(w)
	return nil
}

// ---- tags ----------------------------------------------------------------------

// createTag is POST /repos/{repoID}/tags {name, target}: edit level, a lightweight tag.
func (s *Service) createTag(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	var req struct {
		Name   string `json:"name"`
		Target string `json:"target"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	name := strings.TrimSpace(req.Name)
	if err := s.git.CheckRefFormat(ctx, "tags", name); err != nil || name == "" || len(name) > 200 {
		return httpx.BadRequest("That isn't a valid tag name.")
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		target = repo.DefaultBranch
	}
	at, err := s.resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	if cur, err := s.git.RefValue(ctx, repo.ID, "refs/tags/"+name); err != nil {
		return err
	} else if cur != "" {
		return httpx.Conflict("The tag " + name + " exists already.")
	}
	if err := s.ensureTag(ctx, repo, name, at.SHA); err != nil {
		return err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: ZeroOID, New: at.SHA, Ref: "refs/tags/" + name}}, "tag")
	httpx.JSON(w, http.StatusCreated, map[string]any{"name": name, "sha": at.SHA})
	return nil
}

// deleteTag is DELETE /repos/{repoID}/tags?name=: edit level; refused while a release uses it.
func (s *Service) deleteTag(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	ctx := r.Context()
	name := r.URL.Query().Get("name")
	cur, err := s.git.RefValue(ctx, repo.ID, "refs/tags/"+name)
	if err != nil || name == "" {
		return httpx.NotFound("Tag not found.")
	}
	if cur == "" {
		return httpx.NotFound("Tag not found.")
	}
	var used bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_releases WHERE repo_id = $1 AND tag = $2 AND NOT draft)`, repo.ID, name).Scan(&used); err != nil {
		return err
	}
	if used {
		return httpx.Conflict("A release uses " + name + ". Delete the release with its tag instead.")
	}
	if err := s.git.DeleteRef(ctx, repo.ID, "refs/tags/"+name, cur, "delete tag"); err != nil {
		return err
	}
	s.afterRefUpdates(context.WithoutCancel(ctx), repo, me, []refUpdate{{Old: cur, New: ZeroOID, Ref: "refs/tags/" + name}}, "tag")
	httpx.NoContent(w)
	return nil
}

// releaseNotes is GET /releases/notes?target=&previous=: the pull requests
// merged between the previous tag and target, as Markdown to start from.
func (s *Service) releaseNotes(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelEdit)
	if err != nil {
		return err
	}
	ctx := r.Context()
	q := r.URL.Query()
	target := q.Get("target")
	if target == "" {
		target = repo.DefaultBranch
	}
	to, err := s.resolve(ctx, repo, target)
	if err != nil {
		return err
	}
	from := ""
	if prev := q.Get("previous"); prev != "" {
		at, err := s.resolve(ctx, repo, prev)
		if err != nil {
			return err
		}
		from = at.SHA
	}
	commits, err := s.git.RangeCommits(ctx, repo.ID, from, to.SHA, 1000)
	if err != nil {
		return err
	}
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT p.number, p.title, COALESCE(u.display_name, 'Someone') FROM repos_pulls p LEFT JOIN users u ON u.id = p.author_id
		WHERE p.repo_id = $1 AND p.state = 'merged' AND p.merge_commit_sha = ANY($2) ORDER BY p.merged_at`, repo.ID, shas)
	if err != nil {
		return err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var num int
		var title, who string
		if err := rows.Scan(&num, &title, &who); err != nil {
			return err
		}
		if n == 0 {
			b.WriteString("## What's changed\n\n")
		}
		fmt.Fprintf(&b, "* %s by %s in #%d\n", title, who, num)
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n == 0 && len(commits) > 0 {
		fmt.Fprintf(&b, "%d %s since the previous release.\n", len(commits), plural(len(commits), "commit", "commits"))
	}
	httpx.OK(w, map[string]any{"body": b.String(), "pulls": n, "commits": len(commits)})
	return nil
}

func (s *Service) releaseHookView(repo *Repo, rl *Release) map[string]any {
	return map[string]any{"id": rl.ID, "tag": rl.Tag, "name": rl.Name, "body": rl.Body, "draft": rl.Draft, "prerelease": rl.Prerelease,
		"target_commitish": rl.TargetSHA, "published_at": rl.PublishedAt, "html_url": s.webURL(fmt.Sprintf("/repos/%s/releases/%s", repo.ID, rl.ID))}
}
