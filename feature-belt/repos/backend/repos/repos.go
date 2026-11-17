// Package repos adds git repositories to the drive: a folder-like item of
// type "repo" (files) whose content is a bare git repository on disk. People
// clone, fetch and push over HTTPS with personal git credentials, browse and
// edit in the app (server-side commits without worktrees), and review
// changes in pull requests with line comments, approvals, branch protection
// and merges. Every permission comes from the access feature through the
// repository's drive item (kind files.item), so a repository inherits its
// folder's sharing and AI agent scopes. See FEATURE.md.
package repos

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/files"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/jobs"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
	"scaffoldapp/backend/internal/storage"
)

// ItemType is the drive item type of a repository.
const ItemType = "repo"

// RepoKind is a repository's own access kind. Its resource sits under the
// repository's drive item (files.item) and inherits from it, so sharing
// comes from the folder while AI agent scopes key on this feature: every
// check, list and tool uses RepoKind.
const RepoKind = "repos.repository"

// itemKind is the access kind of drive items (the repository's item and folders).
const itemKind = files.Kind

// Notification kinds.
const (
	KindReviewRequested = "repos.review_requested"
	KindPullComment     = "repos.pull_comment"
	KindPullReview      = "repos.pull_review"
	KindPullMerged      = "repos.pull_merged"
	KindIssueAssigned   = "repos.issue_assigned"
	KindIssueActivity   = "repos.issue_activity"
	KindMentioned       = "repos.mentioned"
)

// Realtime events on repos:<repo id> (anyone who can view the repository).
const (
	EventRefsChanged   = "repos.refs_changed"   // {repo_id}: a branch or tag moved
	EventPullChanged   = "repos.pull_changed"   // {repo_id, number}: anything on a pull request
	EventRepoChanged   = "repos.repo_changed"   // {repo_id}: settings, protection, labels, milestones
	EventIssueChanged  = "repos.issue_changed"  // {repo_id, number}: anything on an issue
	EventStatusChanged = "repos.status_changed" // {repo_id, sha}: a commit status was posted
)

// PermCreate is the org permission to make repositories.
const PermCreate = "repos.create"

// Levels, named for routes.
const (
	levelView    = access.View
	levelComment = access.Comment
	levelEdit    = access.Edit
	levelManage  = access.Manage
)

const (
	repoWritesPerHour    = 60
	commitsPerHour       = 600
	commentsPerHour      = 300
	pullWritesPerHour    = 120
	credentialsPerHour   = 30
	maxReviewRequests    = 15
	maxCommentsPerReview = 100
)

// Service is the repos module.
type Service struct {
	d   *app.Deps
	git *Git

	gitVersion   string
	gitProblem   string // non-empty: git is missing or too old (development keeps running)
	hooksDir     string
	tmpDir       string
	trashDir     string
	cgiStderr    io.Writer
	maxPushBytes int64
	maxEditBytes int64
	maxCommit    int64
	httpTimeout  time.Duration
	publicAPI    string    // clone URLs start here
	noreplyHost  string    // in-app commits use <user id>@users.noreply.<host>
	seedNow      time.Time // cmd/seed only: backdates the seed's commits
	rcache       *ttlCache // connected repositories: short per-person answers
}

// New reads the settings, checks git and registers the drive item type,
// notification kinds, the org permission and the access change hook.
func New(d *app.Deps) *Service {
	s := &Service{
		d:            d,
		maxPushBytes: int64(config.Int("REPOS_MAX_PUSH_BYTES", 100<<20)),
		maxEditBytes: int64(config.Int("REPOS_MAX_EDIT_BYTES", 1<<20)),
		maxCommit:    int64(config.Int("REPOS_MAX_COMMIT_BYTES", 25<<20)),
		httpTimeout:  config.Duration("REPOS_HTTP_TIMEOUT", 30*time.Minute),
		cgiStderr:    slogWriter{},
		rcache:       newTTLCache(),
	}
	dir := config.RequiredOutsideDev("REPOS_DIR", ".data/repos")
	bin := config.String("REPOS_GIT_BIN", "git")
	timeout := config.Duration("REPOS_GIT_TIMEOUT", 30*time.Second)
	switch {
	case s.maxPushBytes < 1<<20:
		config.Problem("REPOS_MAX_PUSH_BYTES must be at least 1048576 (1 MB)")
	case s.maxEditBytes < 64<<10 || s.maxEditBytes > 16<<20:
		config.Problem("REPOS_MAX_EDIT_BYTES must be between 65536 and 16777216")
	case s.maxCommit < s.maxEditBytes:
		config.Problem("REPOS_MAX_COMMIT_BYTES must be at least REPOS_MAX_EDIT_BYTES")
	case timeout < time.Second:
		config.Problem("REPOS_GIT_TIMEOUT must be at least 1s")
	}
	if d.Cfg != nil {
		s.publicAPI = strings.TrimRight(d.Cfg.PublicAPIURL, "/")
		if u, err := url.Parse(d.Cfg.PublicWebURL); err == nil && u.Hostname() != "" {
			s.noreplyHost = u.Hostname()
		}
	}
	if s.noreplyHost == "" {
		s.noreplyHost = "localhost"
	}

	if dir != "" {
		g, err := NewGit(bin, dir, timeout)
		if err != nil {
			config.Problem("REPOS_DIR %s can't be used: %v", dir, err)
		} else {
			s.git = g
			s.hooksDir = filepath.Join(g.Root, ".hooks")
			s.tmpDir = filepath.Join(g.Root, ".tmp")
			s.trashDir = filepath.Join(g.Root, ".deleted")
			for _, p := range []string{s.tmpDir, s.trashDir} {
				if err := os.MkdirAll(p, 0o750); err != nil {
					config.Problem("REPOS_DIR: %v", err)
				}
			}
			if err := writeHookScript(s.hooksDir); err != nil {
				config.Problem("REPOS_DIR: writing the push check failed: %v", err)
			}
			s.checkGit(d)
		}
	}

	files.RegisterItemType(files.ItemType{
		Type:  ItemType,
		Title: "Repository",
		Icon:  "git-branch",
		Link:  func(id uuid.UUID) string { return "/repos/" + id.String() },
		// "New → Repository" in a folder, and "Turn into repository" on one.
		CreatePath:  "/repos/new?folder={folder}",
		ConvertPath: "/repos/new?from_folder={folder}",
		OnDelete:    s.onItemDeleted,
	})
	s.registerLinkType()
	// An agent reading the inbox sees these only while it may read the
	// repository (notifyIfVisible puts repo_id in every one).
	repoTarget := notifications.DataTarget(RepoKind, "repo_id")
	notifications.RegisterKind(notifications.Kind{ID: KindReviewRequested, Label: "Review requests", Push: true, Email: true, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindPullComment, Label: "Pull request comments", Push: true, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindPullReview, Label: "Reviews of your pull requests", Push: true, Email: true, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindPullMerged, Label: "Merged pull requests", Push: false, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindIssueAssigned, Label: "Issues and pull requests assigned to you", Push: true, Email: true, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindIssueActivity, Label: "Activity on issues you're in", Push: true, AgentTarget: repoTarget})
	notifications.RegisterKind(notifications.Kind{ID: KindMentioned, Label: "Mentions in repositories", Push: true, Email: true, AgentTarget: repoTarget})
	storage.RegisterPurpose(storage.Purpose{Name: AssetPurpose, AnyType: true, MaxBytes: s.maxPushBytes})
	access.RegisterPermission(access.Permission{
		Key: PermCreate, Title: "Create repositories",
		Info:         "Make git repositories in folders they can edit, and turn folders into repositories.",
		DefaultRoles: []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember},
	})
	access.RegisterKind(access.Kind{
		Name: RepoKind, Label: "repository",
		Title: func(ctx context.Context, q db.Querier, id uuid.UUID) (string, error) {
			var name string
			err := q.QueryRow(ctx, `SELECT name FROM files_items WHERE id = $1`, id).Scan(&name)
			return name, err
		},
		Link: func(id uuid.UUID) string { return "/repos/" + id.String() },
	})
	access.OnChange(s.accessChanged)
	return s
}

// checkGit reports the git version at boot; outside development a missing
// or old git fails the boot (config.Problem).
func (s *Service) checkGit(d *app.Deps) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, ok, err := s.git.Version(ctx)
	dev := d.Cfg == nil || d.Cfg.IsDevelopment()
	switch {
	case err != nil:
		s.gitProblem = "Git isn't installed on the server, so repositories are unavailable."
		if !dev {
			config.Problem("repos: git (REPOS_GIT_BIN) can't run: %v", err)
		}
	case !ok:
		s.gitProblem = "The server's git is too old (" + v + "); repositories need git 2.38 or newer."
		if !dev {
			config.Problem("repos: git %s is too old; install git 2.38 or newer", v)
		}
	}
	s.gitVersion = v
	slog.Info("repos: git", "version", v, "ok", ok && err == nil, "dir", s.git.Root, "push", pushSupported)
}

func (s *Service) Name() string { return "repos" }

// ---- routes --------------------------------------------------------------------

func (s *Service) MountRoot(r chi.Router) {
	r.HandleFunc("/git/*", s.serveGit)
	r.Get("/git-archive/{token}", s.serveArchive)
}

// MountPublic serves the one route CI tokens open (they authenticate
// themselves; nothing else accepts them).
func (s *Service) MountPublic(r chi.Router) {
	r.With(auth.RateLimit(600, time.Minute)).Post("/repos/{repoID}/statuses/{sha}", httpx.Handler(s.postStatus))
}

func (s *Service) MountUser(r chi.Router) {
	r = r.With(s.requireGit)
	active := r.With(auth.RequireActive)
	repoWrite := active.With(auth.RateLimit(repoWritesPerHour, time.Hour))
	commit := active.With(auth.RateLimit(commitsPerHour, time.Hour))
	comment := active.With(auth.RateLimit(commentsPerHour, time.Hour))
	pullWrite := active.With(auth.RateLimit(pullWritesPerHour, time.Hour))

	// Repositories.
	r.Get("/repos/orgs/{orgID}/repos", httpx.Handler(s.listRepos))
	repoWrite.Post("/repos", httpx.Handler(s.createRepo))
	repoWrite.Post("/repos/import", httpx.Handler(s.importFolder))
	r.Get("/repos/folders/{folderID}/preview", httpx.Handler(s.importPreview))

	// Connected platforms (GitHub, GitLab): sources, each account's
	// repositories, and the bridges into the app.
	r.Get("/repos/sources", httpx.Handler(s.listSources))
	r.Get("/repos/connections/{connectionID}/repos", httpx.Handler(s.listConnectedRepos))
	repoWrite.Post("/repos/connected/import", httpx.Handler(s.importConnected))
	repoWrite.Post("/repos/links", httpx.Handler(s.createLink))
	r.Get("/repos/links/{linkID}", httpx.Handler(s.openLink))

	// Every {repoID} route serves the app's own repositories and connected
	// ones (viaRead / viaWrite pick the implementation from the id).
	r.Get("/repos/{repoID}", s.viaRead(s.getRepo, s.xGetRepo))
	repoWrite.Patch("/repos/{repoID}", httpx.Handler(s.updateRepo))
	r.Get("/repos/{repoID}/protections", httpx.Handler(s.listProtections))
	repoWrite.Post("/repos/{repoID}/protections", httpx.Handler(s.createProtection))
	repoWrite.Patch("/repos/{repoID}/protections/{protectionID}", httpx.Handler(s.updateProtection))
	repoWrite.Delete("/repos/{repoID}/protections/{protectionID}", httpx.Handler(s.deleteProtection))

	// Browsing.
	r.Get("/repos/{repoID}/refs", s.viaRead(s.listRefs, s.xRefs))
	r.Get("/repos/{repoID}/tree", s.viaRead(s.getTree, s.xTree))
	r.Get("/repos/{repoID}/tree/commits", s.viaRead(s.getTreeCommits, s.xTreeCommits))
	r.Get("/repos/{repoID}/blob", s.viaRead(s.getBlob, s.xGetBlob))
	r.Get("/repos/{repoID}/raw", s.viaRead(s.getRaw, s.xRaw))
	r.Get("/repos/{repoID}/download", s.viaRead(s.getDownload, s.xDownload))
	r.Get("/repos/{repoID}/blame", s.viaRead(s.getBlame, s.xBlame))
	r.Get("/repos/{repoID}/paths", s.viaRead(s.findPaths, s.xPaths))
	r.With(auth.RateLimit(240, time.Hour)).Get("/repos/{repoID}/search", s.viaRead(s.searchCode, s.xSearch))
	r.Get("/repos/{repoID}/commits", s.viaRead(s.listCommits, s.xCommits))
	r.Get("/repos/{repoID}/commits/{sha}", s.viaRead(s.getCommit, s.xCommit))
	r.Get("/repos/{repoID}/commits/{sha}/checks", s.viaRead(s.getChecks, s.xChecks))
	r.Get("/repos/{repoID}/compare", s.viaRead(s.compare, s.xCompare))

	// Writing.
	commit.Post("/repos/{repoID}/commits", s.viaWrite(s.commitChanges, s.xCommitChanges))
	repoWrite.Post("/repos/{repoID}/branches", s.viaWrite(s.createBranch, s.xCreateBranch))
	repoWrite.Delete("/repos/{repoID}/branches", s.viaWrite(s.deleteBranch, s.xDeleteBranch))

	// Pull requests (GitLab: merge requests).
	r.Get("/repos/{repoID}/pulls", s.viaRead(s.listPulls, s.xPulls))
	pullWrite.Post("/repos/{repoID}/pulls", s.viaWrite(s.openPull, s.xOpenPull))
	r.Get("/repos/{repoID}/pulls/{number}", s.viaRead(s.getPull, s.xPull))
	pullWrite.Patch("/repos/{repoID}/pulls/{number}", s.viaWrite(s.updatePull, s.xUpdatePull))
	pullWrite.Post("/repos/{repoID}/pulls/{number}/close", s.viaWrite(s.closePull, s.xSetOpen(false)))
	pullWrite.Post("/repos/{repoID}/pulls/{number}/reopen", s.viaWrite(s.reopenPull, s.xSetOpen(true)))
	r.Get("/repos/{repoID}/pulls/{number}/timeline", s.viaRead(s.pullTimeline, s.xTimeline))
	r.Get("/repos/{repoID}/pulls/{number}/commits", s.viaRead(s.pullCommits, s.xPullCommits))
	r.Get("/repos/{repoID}/pulls/{number}/diff", s.viaRead(s.pullDiff, s.xPullDiff))
	r.Get("/repos/{repoID}/pulls/{number}/threads", s.viaRead(s.pullThreads, s.xThreads))
	r.Get("/repos/{repoID}/pulls/{number}/mergeability", s.viaRead(s.pullMergeability, s.xMergeability))
	pullWrite.Post("/repos/{repoID}/pulls/{number}/merge", s.viaWrite(s.mergePull, s.xMerge))
	comment.Post("/repos/{repoID}/pulls/{number}/comments", s.viaWrite(s.addComment, s.xAddComment))
	comment.Patch("/repos/{repoID}/pulls/{number}/comments/{commentID}", s.viaWrite(s.editComment, s.xEditComment))
	comment.Delete("/repos/{repoID}/pulls/{number}/comments/{commentID}", s.viaWrite(s.deleteComment, s.xDeleteComment))
	comment.Put("/repos/{repoID}/pulls/{number}/comments/{commentID}/resolved", s.viaWrite(s.resolveThread, s.xResolve))
	comment.Post("/repos/{repoID}/pulls/{number}/reviews", s.viaWrite(s.submitReview, s.xReview))
	pullWrite.Post("/repos/{repoID}/pulls/{number}/reviewers", httpx.Handler(s.requestReviewers))
	pullWrite.Delete("/repos/{repoID}/pulls/{number}/reviewers/{userID}", httpx.Handler(s.removeReviewer))
	r.Get("/repos/{repoID}/people", s.viaRead(s.repoPeople, s.xPeople))

	// Pull request depth (the app's own repositories).
	pullWrite.Post("/repos/{repoID}/pulls/{number}/ready", httpx.Handler(s.setDraft(false)))
	pullWrite.Post("/repos/{repoID}/pulls/{number}/draft", httpx.Handler(s.setDraft(true)))
	pullWrite.Put("/repos/{repoID}/pulls/{number}/labels", httpx.Handler(s.setPullLabels))
	pullWrite.Put("/repos/{repoID}/pulls/{number}/assignees", httpx.Handler(s.setPullAssignees))
	pullWrite.Put("/repos/{repoID}/pulls/{number}/auto-merge", httpx.Handler(s.enableAutoMerge))
	pullWrite.Delete("/repos/{repoID}/pulls/{number}/auto-merge", httpx.Handler(s.disableAutoMerge))
	commit.Post("/repos/{repoID}/pulls/{number}/update-branch", httpx.Handler(s.updateBranch))
	commit.Post("/repos/{repoID}/pulls/{number}/suggestions", httpx.Handler(s.applySuggestions))
	r.Get("/repos/{repoID}/pulls/{number}/viewed", httpx.Handler(s.viewedFiles))
	comment.Put("/repos/{repoID}/pulls/{number}/viewed", httpx.Handler(s.setViewed(true)))
	comment.Delete("/repos/{repoID}/pulls/{number}/viewed", httpx.Handler(s.setViewed(false)))
	r.Get("/repos/{repoID}/pulls/{number}/reviews/pending", httpx.Handler(s.pendingReview))
	comment.Delete("/repos/{repoID}/pulls/{number}/reviews/pending", httpx.Handler(s.discardPendingReview))

	// Issues, labels, milestones, reactions, templates.
	issueWrite := active.With(auth.RateLimit(issuesPerHour, time.Hour))
	r.Get("/repos/{repoID}/issues", httpx.Handler(s.listIssues))
	issueWrite.Post("/repos/{repoID}/issues", httpx.Handler(s.createIssueRoute))
	r.Get("/repos/{repoID}/issues/{number}", httpx.Handler(s.getIssue))
	pullWrite.Patch("/repos/{repoID}/issues/{number}", httpx.Handler(s.updateIssueRoute))
	pullWrite.Delete("/repos/{repoID}/issues/{number}", httpx.Handler(s.deleteIssue))
	pullWrite.Put("/repos/{repoID}/issues/{number}/labels", httpx.Handler(s.setIssueLabels))
	pullWrite.Put("/repos/{repoID}/issues/{number}/assignees", httpx.Handler(s.setIssueAssignees))
	r.Get("/repos/{repoID}/issues/{number}/timeline", httpx.Handler(s.issueTimeline))
	comment.Post("/repos/{repoID}/issues/{number}/comments", httpx.Handler(s.addIssueCommentRoute))
	comment.Patch("/repos/{repoID}/issues/{number}/comments/{commentID}", httpx.Handler(s.editIssueComment))
	comment.Delete("/repos/{repoID}/issues/{number}/comments/{commentID}", httpx.Handler(s.deleteIssueComment))
	comment.Put("/repos/{repoID}/reactions", httpx.Handler(s.react(true)))
	comment.Delete("/repos/{repoID}/reactions", httpx.Handler(s.react(false)))
	r.Get("/repos/{repoID}/templates", httpx.Handler(s.listTemplates))
	r.Get("/repos/{repoID}/labels", httpx.Handler(s.listLabels))
	repoWrite.Post("/repos/{repoID}/labels", httpx.Handler(s.createLabel))
	repoWrite.Patch("/repos/{repoID}/labels/{labelID}", httpx.Handler(s.updateLabel))
	repoWrite.Delete("/repos/{repoID}/labels/{labelID}", httpx.Handler(s.deleteLabel))
	r.Get("/repos/{repoID}/milestones", httpx.Handler(s.listMilestones))
	repoWrite.Post("/repos/{repoID}/milestones", httpx.Handler(s.saveMilestone))
	r.Get("/repos/{repoID}/milestones/{milestoneID}", httpx.Handler(s.getMilestone))
	repoWrite.Patch("/repos/{repoID}/milestones/{milestoneID}", httpx.Handler(s.saveMilestone))
	repoWrite.Delete("/repos/{repoID}/milestones/{milestoneID}", httpx.Handler(s.deleteMilestone))

	// Statuses (read), CI tokens, releases, tags, archives, insights, webhooks.
	r.Get("/repos/{repoID}/commit-states", httpx.Handler(s.commitStates))
	r.Get("/repos/{repoID}/ci-tokens", httpx.Handler(s.listCITokens))
	repoWrite.Post("/repos/{repoID}/ci-tokens", httpx.Handler(s.createCIToken))
	repoWrite.Delete("/repos/{repoID}/ci-tokens/{tokenID}", httpx.Handler(s.revokeCIToken))
	r.Get("/repos/{repoID}/releases", httpx.Handler(s.listReleases))
	repoWrite.Post("/repos/{repoID}/releases", httpx.Handler(s.createRelease))
	r.Get("/repos/{repoID}/releases/notes", httpx.Handler(s.releaseNotes))
	r.Get("/repos/{repoID}/releases/{releaseID}", httpx.Handler(s.getRelease))
	repoWrite.Patch("/repos/{repoID}/releases/{releaseID}", httpx.Handler(s.updateRelease))
	repoWrite.Delete("/repos/{repoID}/releases/{releaseID}", httpx.Handler(s.deleteRelease))
	repoWrite.Post("/repos/{repoID}/releases/{releaseID}/assets", httpx.Handler(s.addAsset))
	r.Get("/repos/{repoID}/releases/{releaseID}/assets/{assetID}", httpx.Handler(s.downloadAsset))
	repoWrite.Delete("/repos/{repoID}/releases/{releaseID}/assets/{assetID}", httpx.Handler(s.deleteAsset))
	repoWrite.Post("/repos/{repoID}/tags", httpx.Handler(s.createTag))
	repoWrite.Delete("/repos/{repoID}/tags", httpx.Handler(s.deleteTag))
	r.With(auth.RateLimit(120, time.Hour)).Post("/repos/{repoID}/archive", httpx.Handler(s.createArchive))
	r.Get("/repos/{repoID}/insights", httpx.Handler(s.getInsights))
	r.Get("/repos/{repoID}/webhooks", httpx.Handler(s.listWebhooks))
	repoWrite.Post("/repos/{repoID}/webhooks", httpx.Handler(s.createWebhook))
	repoWrite.Patch("/repos/{repoID}/webhooks/{hookID}", httpx.Handler(s.updateWebhook))
	repoWrite.Delete("/repos/{repoID}/webhooks/{hookID}", httpx.Handler(s.deleteWebhook))
	repoWrite.Post("/repos/{repoID}/webhooks/{hookID}/ping", httpx.Handler(s.pingWebhook))
	r.Get("/repos/{repoID}/webhooks/{hookID}/deliveries", httpx.Handler(s.listDeliveries))
	r.Get("/repos/{repoID}/webhooks/{hookID}/deliveries/{deliveryID}", httpx.Handler(s.getDelivery))
	repoWrite.Post("/repos/{repoID}/webhooks/{hookID}/deliveries/{deliveryID}/redeliver", httpx.Handler(s.redeliver))

	// Personal git credentials (people only, never agents).
	r.Get("/repos/credentials", httpx.Handler(s.listCredentials))
	active.With(auth.RateLimit(credentialsPerHour, time.Hour)).Post("/repos/credentials", httpx.Handler(s.createCredential))
	r.Delete("/repos/credentials/{id}", httpx.Handler(s.revokeCredential))
	r.Get("/repos/git", httpx.Handler(s.gitInfo))
}

// requireGit answers 503 when git can't run (development keeps booting).
func (s *Service) requireGit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.git == nil || s.gitProblem != "" {
			msg := s.gitProblem
			if msg == "" {
				msg = "Repositories aren't configured on this server (REPOS_DIR)."
			}
			// Credentials and connected repositories don't need git.
			if !strings.HasPrefix(r.URL.Path, "/v1/repos/credentials") && !connectedPath(r.URL.Path) {
				httpx.Fail(w, r, httpx.NewError(http.StatusServiceUnavailable, "git_unavailable", msg))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// connectedPath is a route that only talks to connected platforms.
func connectedPath(p string) bool {
	rest, ok := strings.CutPrefix(p, "/v1/repos/")
	if !ok {
		return false
	}
	seg, _, _ := strings.Cut(rest, "/")
	return isExternalID(seg) || seg == "sources" || seg == "connections" || seg == "links"
}

// GitInfo tells clients how to clone.
type GitInfo struct {
	Version  string `json:"version"`
	BaseURL  string `json:"base_url"`
	MaxPush  int64  `json:"max_push_bytes"`
	MaxEdit  int64  `json:"max_edit_bytes"`
	MaxBatch int64  `json:"max_commit_bytes"`
}

func (s *Service) gitInfo(w http.ResponseWriter, _ *http.Request) error {
	httpx.OK(w, GitInfo{Version: s.gitVersion, BaseURL: s.publicAPI + "/git", MaxPush: s.maxPushBytes, MaxEdit: s.maxEditBytes, MaxBatch: s.maxCommit})
	return nil
}

func (s *Service) cloneURLExample() string { return s.publicAPI + "/git/<workspace>/<repository>.git" }

// ---- realtime ------------------------------------------------------------------

// AuthorizeTopic owns repos:<repo id>: anyone who can view the repository.
func (s *Service) AuthorizeTopic(ctx context.Context, userID uuid.UUID, _ bool, topic string) (bool, bool) {
	rest, ok := strings.CutPrefix(topic, "repos:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_repos WHERE id = $1)`, id).Scan(&exists); err != nil || !exists {
		return true, false
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, userID, RepoKind, id)
	return true, err == nil && lvl >= access.View
}

// accessChanged keeps the repository resource's owner equal to its drive
// item's (ownership moves with the drive), and re-checks repos:<id>
// subscribers when sharing changes.
func (s *Service) accessChanged(ctx context.Context, ref access.Ref, users []uuid.UUID) {
	if ref.Kind != itemKind && ref.Kind != RepoKind {
		return
	}
	var isRepo bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM repos_repos WHERE id = $1)`, ref.ID).Scan(&isRepo); err != nil || !isRepo {
		return
	}
	if ref.Kind == itemKind {
		if err := s.syncOwner(ctx, ref.ID); err != nil {
			slog.WarnContext(ctx, "repos: owner sync failed", "repo_id", ref.ID, "err", err)
		}
	}
	if s.d.Broker == nil {
		return
	}
	topic := realtime.Topic("repos", ref.ID)
	if users == nil {
		_ = realtime.RevokeTopic(ctx, s.d.Broker, topic)
		return
	}
	for _, u := range users {
		_ = realtime.Revoke(ctx, s.d.Broker, u, topic)
	}
}

// syncOwner gives the repository resource its drive item's owner
// (access.SetOwner; uuid.Nil when the item has none).
func (s *Service) syncOwner(ctx context.Context, id uuid.UUID) error {
	item, err := access.Lookup(ctx, s.d.Pool, access.Ref{Kind: itemKind, ID: id})
	if err != nil {
		return err
	}
	repo, err := access.Lookup(ctx, s.d.Pool, access.Ref{Kind: RepoKind, ID: id})
	if err != nil {
		return err
	}
	if equalOwner(item.OwnerID, repo.OwnerID) {
		return nil
	}
	owner := uuid.Nil
	if item.OwnerID != nil {
		owner = *item.OwnerID
	}
	return access.SetOwner(ctx, s.d.Pool, access.Ref{Kind: RepoKind, ID: id}, owner)
}

func equalOwner(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Service) publish(ctx context.Context, repoID uuid.UUID, event string, data map[string]any) {
	if s.d.Broker == nil {
		return
	}
	data["repo_id"] = repoID
	if err := s.d.Broker.Publish(ctx, realtime.Topic("repos", repoID), event, data); err != nil {
		slog.WarnContext(ctx, "repos: realtime publish failed", "event", event, "err", err)
	}
}

func (s *Service) repoChanged(ctx context.Context, repoID uuid.UUID) {
	s.publish(ctx, repoID, EventRepoChanged, map[string]any{})
}

func (s *Service) pullChanged(ctx context.Context, repoID uuid.UUID, number int) {
	s.publish(ctx, repoID, EventPullChanged, map[string]any{"number": number})
}

func (s *Service) send(ctx context.Context, n notify.Notification) {
	if s.d.Notify == nil {
		return
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "repos: notify failed", "kind", n.Kind, "err", err)
	}
}

// ---- workers -------------------------------------------------------------------

// Start runs the disk sweeper: repositories whose drive item was deleted
// are removed from disk, and leftovers of failed requests are cleaned up.
func (s *Service) Start(ctx context.Context) {
	if s.git == nil {
		return
	}
	go jobs.Every(ctx, s.d.Pool, "repos.sweep", 10*time.Minute, s.sweep)
}

// onItemDeleted runs in files' transaction when a repository item is
// deleted for good. The directory moves aside now and the sweeper removes
// it once the deletion committed (or moves it back if it rolled back).
func (s *Service) onItemDeleted(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) error {
	if s.git == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, `DELETE FROM repos_repos WHERE id = $1`, itemID); err != nil {
		return err
	}
	if err := access.DeleteResource(ctx, tx, access.Ref{Kind: RepoKind, ID: itemID}); err != nil {
		return err
	}
	src := s.git.Dir(itemID)
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	return os.Rename(src, filepath.Join(s.trashDir, filepath.Base(src)))
}

func (s *Service) sweep(ctx context.Context) error {
	ids := func(dir string) map[uuid.UUID]string {
		out := map[uuid.UUID]string{}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".git")
			if id, err := uuid.Parse(name); ok && err == nil && e.IsDir() {
				out[id] = filepath.Join(dir, e.Name())
			}
		}
		return out
	}
	live, trashed := ids(s.git.Root), ids(s.trashDir)
	all := make([]uuid.UUID, 0, len(live)+len(trashed))
	for id := range live {
		all = append(all, id)
	}
	for id := range trashed {
		all = append(all, id)
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM repos_repos WHERE id = ANY($1)`, all)
	if err != nil {
		return err
	}
	known, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	has := map[uuid.UUID]bool{}
	for _, id := range known {
		has[id] = true
	}
	for id, p := range trashed {
		if has[id] { // the deletion rolled back: restore
			if _, err := os.Stat(s.git.Dir(id)); os.IsNotExist(err) {
				_ = os.Rename(p, s.git.Dir(id))
			}
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			slog.WarnContext(ctx, "repos: removing a deleted repository failed", "repo_id", id, "err", err)
		}
	}
	// A directory without a row an hour after it was made is an orphan
	// (its org was deleted, or creating it failed after the directory).
	for id, p := range live {
		if has[id] {
			continue
		}
		if st, err := os.Stat(p); err == nil && time.Since(st.ModTime()) > time.Hour {
			_ = os.Rename(p, filepath.Join(s.trashDir, filepath.Base(p)))
		}
	}
	if err := s.pruneDeliveries(ctx); err != nil {
		slog.WarnContext(ctx, "repos: pruning webhook deliveries failed", "err", err)
	}
	if err := s.pruneArchiveTokens(ctx); err != nil {
		slog.WarnContext(ctx, "repos: pruning archive tokens failed", "err", err)
	}
	// Spooled bodies and FIFO pairs older than a day.
	entries, _ := os.ReadDir(s.tmpDir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.RemoveAll(filepath.Join(s.tmpDir, e.Name()))
		}
	}
	return nil
}

// ---- small helpers -------------------------------------------------------------

type cursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

var errAgentForbidden = httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't do this. Do it yourself in the app.")

// withRepoLock serialises the app's own writes to one repository (commits,
// merges, branch changes) so its rows and refs move together. Pushes are
// serialised by git's ref locks and update-ref's old-value checks.
func (s *Service) withRepoLock(ctx context.Context, repoID uuid.UUID, fn func(tx pgx.Tx) error) error {
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('repos.write:' || $1::text, 0))`, repoID); err != nil {
			return err
		}
		return fn(tx)
	})
}
