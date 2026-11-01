package repos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/files"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Repo is a repository: its drive item's name and folder, and its settings.
type Repo struct {
	ID                  uuid.UUID
	OrgID               uuid.UUID
	OrgSlug             string
	Name                string
	Slug                string
	FolderID            *uuid.UUID
	DefaultBranch       string
	Description         string
	AllowMergeCommit    bool
	AllowSquash         bool
	DeleteBranchOnMerge bool
	SizeBytes           int64
	CreatedBy           *uuid.UUID
	PushedAt            *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Trashed             bool
}

var errRepoNotFound = httpx.NotFound("Repository not found.")

const repoSelect = `
	SELECT r.id, r.org_id, o.slug, i.name, r.slug, i.parent_id, r.default_branch, r.description,
	       r.allow_merge_commit, r.allow_squash, r.delete_branch_on_merge, r.size_bytes, r.created_by,
	       r.pushed_at, r.created_at, GREATEST(r.updated_at, i.updated_at), i.trashed_at IS NOT NULL
	FROM repos_repos r
	JOIN files_items i ON i.id = r.id
	JOIN orgs o ON o.id = r.org_id`

func scanRepo(row pgx.Row) (*Repo, error) {
	var r Repo
	err := row.Scan(&r.ID, &r.OrgID, &r.OrgSlug, &r.Name, &r.Slug, &r.FolderID, &r.DefaultBranch, &r.Description,
		&r.AllowMergeCommit, &r.AllowSquash, &r.DeleteBranchOnMerge, &r.SizeBytes, &r.CreatedBy,
		&r.PushedAt, &r.CreatedAt, &r.UpdatedAt, &r.Trashed)
	return &r, err
}

// loadRepo reads a repository; trashed ones read as not found.
func (s *Service) loadRepo(ctx context.Context, q db.Querier, id uuid.UUID) (*Repo, error) {
	r, err := scanRepo(q.QueryRow(ctx, repoSelect+` WHERE r.id = $1`, id))
	if db.IsNoRows(err) {
		return nil, errRepoNotFound
	}
	if err != nil {
		return nil, err
	}
	if r.Trashed {
		return nil, httpx.NotFound("This repository is in the trash. Restore it from the drive to use it.")
	}
	return r, nil
}

// repoFor checks the caller's level on a repository (404 without view, 403
// below min; AI agent calls are capped by their scope) and loads it.
func (s *Service) repoFor(ctx context.Context, userID, id uuid.UUID, min access.Level) (*Repo, access.Level, error) {
	lvl, err := access.Require(ctx, s.d.Pool, userID, RepoKind, id, min)
	if err != nil {
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
			return nil, access.None, errRepoNotFound
		}
		return nil, lvl, err
	}
	r, err := s.loadRepo(ctx, s.d.Pool, id)
	if err != nil {
		return nil, access.None, err
	}
	return r, lvl, nil
}

// repoFromRoute is repoFor for {repoID} routes.
func (s *Service) repoFromRoute(r *http.Request, min access.Level) (*Repo, error) {
	repo, _, err := s.repoLevelFromRoute(r, min)
	return repo, err
}

func (s *Service) repoLevelFromRoute(r *http.Request, min access.Level) (*Repo, access.Level, error) {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "repoID")
	if err != nil {
		return nil, access.None, errRepoNotFound
	}
	return s.repoFor(r.Context(), me.UserID, id, min)
}

// ---- views ---------------------------------------------------------------------

// RepoView is a repository as clients see it.
type RepoView struct {
	ID                  uuid.UUID  `json:"id"`
	OrgID               uuid.UUID  `json:"org_id"`
	Name                string     `json:"name"`
	Slug                string     `json:"slug"`
	Description         string     `json:"description"`
	FolderID            *uuid.UUID `json:"folder_id"`
	DefaultBranch       string     `json:"default_branch"`
	CloneURL            string     `json:"clone_url"`
	Link                string     `json:"link"`
	Empty               bool       `json:"empty"`
	MyLevel             string     `json:"my_level"`
	CanPush             bool       `json:"can_push"`
	CanManage           bool       `json:"can_manage"`
	AllowMergeCommit    bool       `json:"allow_merge_commit"`
	AllowSquash         bool       `json:"allow_squash"`
	DeleteBranchOnMerge bool       `json:"delete_branch_on_merge"`
	OpenPulls           int        `json:"open_pulls"`
	SizeBytes           int64      `json:"size_bytes"`
	PushedAt            *time.Time `json:"pushed_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (s *Service) cloneURL(r *Repo) string {
	return s.publicAPI + "/git/" + r.OrgSlug + "/" + r.Slug + ".git"
}

func (s *Service) view(ctx context.Context, r *Repo, lvl access.Level) (RepoView, error) {
	v := RepoView{
		ID: r.ID, OrgID: r.OrgID, Name: r.Name, Slug: r.Slug, Description: r.Description, FolderID: r.FolderID,
		DefaultBranch: r.DefaultBranch, CloneURL: s.cloneURL(r), Link: "/repos/" + r.ID.String(),
		MyLevel: lvl.String(), CanPush: lvl >= access.Edit, CanManage: lvl >= access.Manage,
		AllowMergeCommit: r.AllowMergeCommit, AllowSquash: r.AllowSquash, DeleteBranchOnMerge: r.DeleteBranchOnMerge,
		SizeBytes: r.SizeBytes, PushedAt: r.PushedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	head, err := s.git.RefValue(ctx, r.ID, "refs/heads/"+r.DefaultBranch)
	if err != nil {
		return v, err
	}
	if head == "" {
		refs, err := s.git.Refs(ctx, r.ID)
		if err != nil {
			return v, err
		}
		v.Empty = len(refs) == 0
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM repos_pulls WHERE repo_id = $1 AND state = 'open'`, r.ID).Scan(&v.OpenPulls); err != nil {
		return v, err
	}
	return v, nil
}

func (s *Service) getRepo(w http.ResponseWriter, r *http.Request) error {
	repo, lvl, err := s.repoLevelFromRoute(r, levelView)
	if err != nil {
		return err
	}
	v, err := s.view(r.Context(), repo, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// RepoSummary is a row of the repository list.
type RepoSummary struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	Description   string     `json:"description"`
	FolderID      *uuid.UUID `json:"folder_id"`
	DefaultBranch string     `json:"default_branch"`
	OpenPulls     int        `json:"open_pulls"`
	MyLevel       string     `json:"my_level"`
	PushedAt      *time.Time `json:"pushed_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type listCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// visibleRepos is the one permission-filtered list query: REST, agent
// tools and search all use it. ViaAgent applies the agent scope cap.
func (s *Service) visibleRepos(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, q string, after *listCursor, limit int) ([]RepoSummary, error) {
	var at *time.Time
	var afterID uuid.UUID
	if after != nil {
		at, afterID = &after.T, after.ID
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT r.id, i.name, r.slug, r.description, i.parent_id, r.default_branch,
		       (SELECT count(*) FROM repos_pulls p WHERE p.repo_id = r.id AND p.state = 'open')::int,
		       v.level, r.pushed_at, COALESCE(r.pushed_at, r.created_at) AS active_at
		FROM repos_repos r
		JOIN access_visible($1, $2, $3) v ON v.id = r.id
		JOIN files_items i ON i.id = r.id AND i.trashed_at IS NULL
		WHERE ($4::uuid IS NULL OR r.org_id = $4)
		  AND ($5 = '' OR lower(i.name) LIKE '%' || $5 || '%' OR r.slug LIKE '%' || $5 || '%' OR lower(r.description) LIKE '%' || $5 || '%')
		  AND ($6::timestamptz IS NULL OR (COALESCE(r.pushed_at, r.created_at), r.id) < ($6, $7))
		ORDER BY active_at DESC, r.id DESC LIMIT $8`,
		userID, RepoKind, access.ViaAgent(ctx), orgID, likeEscape(strings.ToLower(strings.TrimSpace(q))), at, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoSummary{}
	for rows.Next() {
		var rs RepoSummary
		var lvl int
		if err := rows.Scan(&rs.ID, &rs.Name, &rs.Slug, &rs.Description, &rs.FolderID, &rs.DefaultBranch, &rs.OpenPulls,
			&lvl, &rs.PushedAt, &rs.UpdatedAt); err != nil {
			return nil, err
		}
		rs.MyLevel = access.Level(lvl).String()
		out = append(out, rs)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// listRepos is GET /v1/repos/orgs/{orgID}/repos?q=&cursor=: repositories
// in the org the caller can see, most recently pushed first.
func (s *Service) listRepos(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	orgID, err := httpx.URLParamUUID(r, "orgID")
	if err != nil {
		return err
	}
	if role, err := orgs.RoleOf(r.Context(), s.d.Pool, orgID, me.UserID); err != nil {
		return err
	} else if role == "" {
		return httpx.NotFound("Workspace not found.")
	}
	p := httpx.ParsePage(r)
	var c listCursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	var after *listCursor
	if !c.T.IsZero() {
		after = &c
	}
	list, err := s.visibleRepos(r.Context(), me.UserID, &orgID, r.URL.Query().Get("q"), after, p.Limit+1)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(rs RepoSummary) any { return listCursor{T: rs.UpdatedAt, ID: rs.ID} }))
	return nil
}

// ---- creating ------------------------------------------------------------------

type createRepoReq struct {
	OrgID         uuid.UUID  `json:"org_id"`
	FolderID      *uuid.UUID `json:"folder_id"` // nil: the top of the caller's own drive
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	DefaultBranch string     `json:"default_branch"`
	Readme        bool       `json:"readme"` // start with a README.md commit
}

var slugStrip = regexp.MustCompile(`[^a-z0-9._-]+`)

// slugify makes a URL name from a repository name.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := slugStrip.ReplaceAllString(b.String(), "-")
	s = strings.Trim(regexp.MustCompile(`-{2,}`).ReplaceAllString(s, "-"), "-._")
	s = strings.TrimSuffix(s, ".git")
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-._")
	}
	if s == "" {
		s = "repository"
	}
	return s
}

// uniqueSlug returns base, or base-2, base-3… free in the org.
func uniqueSlug(ctx context.Context, q db.Querier, orgID uuid.UUID, base string) (string, error) {
	rows, err := q.Query(ctx, `SELECT slug FROM repos_repos WHERE org_id = $1 AND (slug = $2 OR slug LIKE $3)`, orgID, base, likeEscape(base)+`-%`)
	if err != nil {
		return "", err
	}
	taken, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, t := range taken {
		used[t] = true
	}
	if !used[base] {
		return base, nil
	}
	for n := 2; n < 10000; n++ {
		if c := fmt.Sprintf("%s-%d", base, n); !used[c] {
			return c, nil
		}
	}
	return base + "-" + uuid.NewString()[:8], nil
}

// cleanRepoName validates a drive name for a repository.
func cleanRepoName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	if n := len([]rune(name)); n == 0 || n > 100 {
		return "", httpx.BadRequest("Name the repository (up to 100 characters).")
	}
	if strings.ContainsAny(name, "/\\\x00") || name == "." || name == ".." {
		return "", httpx.BadRequest("A repository name can't contain slashes.")
	}
	return name, nil
}

// newRepo creates the drive item, the row, the default branch's protection
// rule and the bare repository, in tx. The caller commits; if the
// transaction fails afterwards, the sweeper removes the directory.
func (s *Service) newRepo(ctx context.Context, tx pgx.Tx, me *auth.Identity, orgID uuid.UUID, folderID *uuid.UUID, name, description, branch string) (*Repo, error) {
	item, err := files.CreateItem(ctx, tx, files.NewItem{OrgID: orgID, ParentID: folderID, Type: ItemType, Name: name, CreatedBy: me.UserID})
	if err != nil {
		return nil, err
	}
	if err := s.registerRepoResource(ctx, tx, item.ID, orgID); err != nil {
		return nil, err
	}
	slug, err := uniqueSlug(ctx, tx, orgID, slugify(name))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO repos_repos (id, org_id, slug, default_branch, description, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, item.ID, orgID, slug, branch, description, me.UserID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO repos_protections (repo_id, pattern) VALUES ($1, $2)`, item.ID, branch); err != nil {
		return nil, err
	}
	if err := s.git.Init(ctx, item.ID, branch); err != nil {
		return nil, err
	}
	return s.loadRepo(ctx, tx, item.ID)
}

// registerRepoResource creates the repository's access resource under its
// drive item, inheriting, with the item's owner (so a repository in a
// person's My drive stays private from org admins, like the item).
func (s *Service) registerRepoResource(ctx context.Context, tx pgx.Tx, itemID, orgID uuid.UUID) error {
	item, err := access.Lookup(ctx, tx, access.Ref{Kind: itemKind, ID: itemID})
	if err != nil {
		return err
	}
	owner := uuid.Nil
	if item.OwnerID != nil {
		owner = *item.OwnerID
	}
	return access.CreateResource(ctx, tx, access.NewResource{
		Kind: RepoKind, ID: itemID, OrgID: orgID, OwnerID: owner,
		Parent: &access.Ref{Kind: itemKind, ID: itemID},
	})
}

// canCreateIn checks the org permission and edit on the folder.
func (s *Service) canCreateIn(ctx context.Context, me *auth.Identity, orgID uuid.UUID, folderID *uuid.UUID) error {
	if _, ok := auth.AgentFrom(ctx); ok {
		return errAgentForbidden
	}
	if role, err := orgs.RoleOf(ctx, s.d.Pool, orgID, me.UserID); err != nil {
		return err
	} else if role == "" {
		return httpx.NotFound("Workspace not found.")
	}
	ok, err := access.Allowed(ctx, s.d.Pool, orgID, me.UserID, PermCreate)
	if err != nil {
		return err
	}
	if !ok {
		e := httpx.NewError(http.StatusForbidden, "permission_required", "Your workspace doesn't let you create repositories. Ask an admin.")
		e.Details = map[string]any{"permission": PermCreate}
		return e
	}
	if folderID != nil {
		if _, err := access.Require(ctx, s.d.Pool, me.UserID, itemKind, *folderID, access.Edit); err != nil {
			return err
		}
		item, err := files.GetItem(ctx, s.d.Pool, me.UserID, *folderID)
		if err != nil {
			return err
		}
		if item.OrgID != orgID || item.Type != files.TypeFolder {
			return httpx.BadRequest("Choose a folder in this workspace.")
		}
	}
	return nil
}

func (s *Service) createRepo(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var req createRepoReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanRepoName(req.Name)
	if err != nil {
		return err
	}
	desc := strings.TrimSpace(req.Description)
	if len([]rune(desc)) > 350 {
		return httpx.BadRequest("Keep the description under 350 characters.")
	}
	branch := strings.TrimSpace(req.DefaultBranch)
	if branch == "" {
		branch = "main"
	}
	if err := s.git.CheckRefFormat(r.Context(), "heads", branch); err != nil {
		return httpx.BadRequest("That isn't a valid branch name.")
	}
	if err := s.canCreateIn(r.Context(), me, req.OrgID, req.FolderID); err != nil {
		return err
	}
	var repo *Repo
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if repo, err = s.newRepo(r.Context(), tx, me, req.OrgID, req.FolderID, name, desc, branch); err != nil {
			return err
		}
		if !req.Readme {
			return nil
		}
		readme := "# " + name + "\n"
		if desc != "" {
			readme += "\n" + desc + "\n"
		}
		who := s.person(me)
		sha, err := s.git.CommitFiles(r.Context(), repo.ID, "", []importFile{{Path: "README.md", Content: []byte(readme)}}, "Initial commit", who, who)
		if err != nil {
			return err
		}
		if err := s.git.UpdateRef(r.Context(), repo.ID, "refs/heads/"+branch, sha, ZeroOID, "create repository"); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `UPDATE repos_repos SET pushed_at = now() WHERE id = $1`, repo.ID)
		return err
	})
	if err != nil {
		if repo != nil {
			_ = s.git.Remove(repo.ID)
		}
		return err
	}
	repo, err = s.loadRepo(r.Context(), s.d.Pool, repo.ID)
	if err != nil {
		return err
	}
	v, err := s.view(r.Context(), repo, access.Manage)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, v)
	return nil
}

// person is who in-app commits are by: the display name and a no-reply
// address, so git history never carries account emails.
func (s *Service) person(me *auth.Identity) Person {
	name := strings.TrimSpace(me.DisplayName)
	if name == "" {
		name = strings.Split(me.Email, "@")[0]
	}
	at := time.Now()
	if !s.seedNow.IsZero() {
		at = s.seedNow
	}
	return Person{Name: name, Email: me.UserID.String() + "@users.noreply." + s.noreplyHost, At: at}
}

// ---- settings ------------------------------------------------------------------

type updateRepoReq struct {
	Description         *string `json:"description"`
	DefaultBranch       *string `json:"default_branch"`
	Slug                *string `json:"slug"`
	AllowMergeCommit    *bool   `json:"allow_merge_commit"`
	AllowSquash         *bool   `json:"allow_squash"`
	DeleteBranchOnMerge *bool   `json:"delete_branch_on_merge"`
}

// updateRepo is PATCH /v1/repos/{repoID}: manage level. The name is the
// drive item's (rename it in the drive).
func (s *Service) updateRepo(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	repo, lvl, err := s.repoLevelFromRoute(r, levelManage)
	if err != nil {
		return err
	}
	var req updateRepoReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	desc, branch, slug := repo.Description, repo.DefaultBranch, repo.Slug
	merge, squash, del := repo.AllowMergeCommit, repo.AllowSquash, repo.DeleteBranchOnMerge
	if req.Description != nil {
		desc = strings.TrimSpace(*req.Description)
		if len([]rune(desc)) > 350 {
			return httpx.BadRequest("Keep the description under 350 characters.")
		}
	}
	if req.DefaultBranch != nil && *req.DefaultBranch != branch {
		branch = strings.TrimSpace(*req.DefaultBranch)
		sha, err := s.git.RefValue(r.Context(), repo.ID, "refs/heads/"+branch)
		if err != nil {
			return err
		}
		if sha == "" {
			return httpx.BadRequest("Choose an existing branch as the default.")
		}
	}
	if req.Slug != nil && *req.Slug != slug {
		slug = strings.ToLower(strings.TrimSpace(*req.Slug))
		if slugify(slug) != slug {
			return httpx.BadRequest("Use lowercase letters, numbers, dots, dashes and underscores for the URL name.")
		}
	}
	if req.AllowMergeCommit != nil {
		merge = *req.AllowMergeCommit
	}
	if req.AllowSquash != nil {
		squash = *req.AllowSquash
	}
	if req.DeleteBranchOnMerge != nil {
		del = *req.DeleteBranchOnMerge
	}
	if !merge && !squash {
		return httpx.BadRequest("Allow at least one way to merge.")
	}
	err = s.withRepoLock(r.Context(), repo.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `
			UPDATE repos_repos SET description = $2, default_branch = $3, slug = $4, allow_merge_commit = $5,
			       allow_squash = $6, delete_branch_on_merge = $7, updated_at = now()
			WHERE id = $1`, repo.ID, desc, branch, slug, merge, squash, del)
		if db.IsUniqueViolation(err, "repos_repos_slug_key") {
			return httpx.Conflict("Another repository in this workspace uses that URL name.")
		}
		if err != nil {
			return err
		}
		if branch != repo.DefaultBranch {
			return s.git.SetHead(r.Context(), repo.ID, branch)
		}
		return nil
	})
	if err != nil {
		return err
	}
	_ = me
	repo, err = s.loadRepo(r.Context(), s.d.Pool, repo.ID)
	if err != nil {
		return err
	}
	s.repoChanged(r.Context(), repo.ID)
	v, err := s.view(r.Context(), repo, lvl)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

// ---- people --------------------------------------------------------------------

// Person row for reviewer pickers.
type PersonRef struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	AvatarURL   *string   `json:"avatar_url"`
	Level       string    `json:"level"`
}

// repoPeople is GET /v1/repos/{repoID}/people?q=: org members who can view
// the repository (reviewer suggestions), at most 20.
func (s *Service) repoPeople(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	q := likeEscape(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))))
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT u.id, u.display_name, u.avatar_url, l.level FROM org_members m
		JOIN users u ON u.id = m.user_id AND u.status IN ('active', 'suspended')
		CROSS JOIN LATERAL (SELECT access_level(u.id, $2, $3) AS level) l
		WHERE m.org_id = $1 AND l.level >= 1 AND ($4 = '' OR lower(u.display_name) LIKE '%' || $4 || '%')
		ORDER BY lower(u.display_name), u.id LIMIT 20`, repo.OrgID, RepoKind, repo.ID, q)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []PersonRef{}
	for rows.Next() {
		var p PersonRef
		var lvl int
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.AvatarURL, &lvl); err != nil {
			return err
		}
		p.Level = access.Level(lvl).String()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}
