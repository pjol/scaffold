package repos

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/httpx"
)

// Connected repositories: GitHub and GitLab (also self-managed) repositories
// a person reaches through their own connection (app.Connections), shown in
// the same screens as the app's own. Every screen and tool talks to one
// shape: the app's own repositories are served by the git engine below
// (git.go, browse.go, pulls.go …); a connected one by a RepoSource, the
// adapter that maps a platform's REST API onto exactly the views the engine
// returns (RefView, TreeEntry, BlobView, Commit, Diff, Comparison,
// Mergeability) plus string-id twins of the pull request views (XPull,
// XComment …). The routes pick the implementation once, from the repository
// id (route() in connected_routes.go): a UUID is the app's own, an id like
// github~<connection>~acme~web is connected. Content is read live with the
// person's token and never stored; only links people make (repos_links) are.

// RepoSource is one connected repository, opened for one person through
// one of their connections. Methods answer the engine's view types;
// failures are *httpx.Error or *platformError (translated by remoteErr).
type RepoSource interface {
	Info(ctx context.Context) (*XRepoView, error)
	Refs(ctx context.Context, counts bool) ([]RefView, string, error)
	Resolve(ctx context.Context, ref string) (resolved, error)
	Tree(ctx context.Context, at resolved, dir string) ([]TreeEntry, error)
	TreeCommits(ctx context.Context, at resolved, dir string, names []string) (map[string]Commit, error)
	// File reads one file at a commit: its entry and up to max bytes
	// (content nil when the file is larger than max).
	File(ctx context.Context, at resolved, path string, max int64) (TreeEntry, []byte, error)
	Blame(ctx context.Context, at resolved, path string) ([]BlameRange, error)
	Paths(ctx context.Context, at resolved) ([]string, bool, error)
	SearchCode(ctx context.Context, q string, at resolved) ([]GrepMatch, bool, error)
	// Log lists commits from at (page from 1), true when there are more.
	Log(ctx context.Context, at resolved, path string, page, limit int) ([]Commit, bool, error)
	Commit(ctx context.Context, sha, path string) (*CommitView, error)
	Compare(ctx context.Context, base, head, path string) (*Comparison, error)
	Checks(ctx context.Context, sha string) (*Checks, error)

	CommitChanges(ctx context.Context, in remoteCommit) (*CommitResult, error)
	CreateBranch(ctx context.Context, name, from string) (*RefView, error)
	DeleteBranch(ctx context.Context, name string) error

	Pulls(ctx context.Context, q pullQuery) ([]XPull, bool, error)
	Pull(ctx context.Context, n int) (*XPull, error)
	OpenPull(ctx context.Context, in OpenPullInput) (*XPull, error)
	UpdatePull(ctx context.Context, n int, in updatePullReq) (*XPull, error)
	SetPullOpen(ctx context.Context, n int, open bool) (*XPull, error)
	PullCommits(ctx context.Context, n int) ([]Commit, error)
	PullDiff(ctx context.Context, n int, path string) (*XPullDiff, error)
	Timeline(ctx context.Context, n int) ([]XTimelineItem, error)
	Threads(ctx context.Context, n int) ([]XThread, string, error)
	Mergeability(ctx context.Context, n int) (*Mergeability, error)
	Merge(ctx context.Context, n int, in mergeReq) (*XPull, error)
	Comment(ctx context.Context, n int, in xCommentInput) (*XComment, error)
	EditComment(ctx context.Context, n int, id, body string) (*XComment, error)
	DeleteComment(ctx context.Context, n int, id string) error
	ResolveThread(ctx context.Context, n int, id string, resolved bool) error
	Review(ctx context.Context, n int, in xReviewInput) error
}

// Provider ids and the capabilities repos declares for them.
const (
	ProviderGitHub = "github"
	ProviderGitLab = "gitlab"

	CapGitHubRead  = "repos.github.read"
	CapGitHubWrite = "repos.github.write"
	CapGitLabRead  = "repos.gitlab.read"
	CapGitLabWrite = "repos.gitlab.write"

	// ExternalKind is the agent-scope kind of a connected repository; its id
	// is "<connection id>:<owner/repo>" (externalTarget).
	ExternalKind = "repos.external"
)

var platformNames = map[string]string{ProviderGitHub: "GitHub", ProviderGitLab: "GitLab"}

func capability(provider string, write bool) string {
	if write {
		return "repos." + provider + ".write"
	}
	return "repos." + provider + ".read"
}

// Capabilities implements app.CapabilityProvider. GitHub OAuth apps have no
// read-only scope for private repositories, so the read capability asks for
// repo too; repos itself keeps reads and writes apart (every write needs the
// write capability granted on the connection).
func (s *Service) Capabilities() []app.Capability {
	return []app.Capability{
		{Key: CapGitHubRead, Module: "repos", Provider: ProviderGitHub, Label: "GitHub repositories in Repositories", Scopes: []string{"repo", "read:org"}},
		{Key: CapGitHubWrite, Module: "repos", Provider: ProviderGitHub, Label: "Commit, review and merge on GitHub from Repositories", Scopes: []string{"repo", "read:org", "workflow"}, Write: true},
		{Key: CapGitLabRead, Module: "repos", Provider: ProviderGitLab, Label: "GitLab projects in Repositories", Scopes: []string{"read_api", "read_repository", "read_user"}},
		{Key: CapGitLabWrite, Module: "repos", Provider: ProviderGitLab, Label: "Commit, review and merge on GitLab from Repositories", Scopes: []string{"api", "write_repository"}, Write: true},
	}
}

// ---- ids -----------------------------------------------------------------------

// extID names a connected repository in routes and links:
// "<provider>~<connection id>~<path with / as ~>" (github~c1~acme~web,
// gitlab~c2~acme~platform~api). "~" can't appear in GitHub or GitLab paths
// and isn't escaped in URLs, so the id is one clean path segment.
type extID struct {
	Provider string
	ConnID   string
	Path     string // owner/repo, or group/subgroup/project
}

var (
	connIDRE  = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	pathSegRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
)

func parseExtID(s string) (extID, bool) {
	parts := strings.Split(s, "~")
	if len(parts) < 4 || len(parts) > 24 {
		return extID{}, false
	}
	x := extID{Provider: parts[0], ConnID: parts[1]}
	if _, ok := platformNames[x.Provider]; !ok || !connIDRE.MatchString(x.ConnID) {
		return extID{}, false
	}
	segs := parts[2:]
	if x.Provider == ProviderGitHub && len(segs) != 2 {
		return extID{}, false
	}
	for _, p := range segs {
		if !pathSegRE.MatchString(p) || p == "." || p == ".." {
			return extID{}, false
		}
	}
	x.Path = strings.Join(segs, "/")
	return x, true
}

func newExtID(provider, connID, path string) (extID, bool) {
	return parseExtID(provider + "~" + connID + "~" + strings.ReplaceAll(path, "/", "~"))
}

func (x extID) String() string {
	return x.Provider + "~" + x.ConnID + "~" + strings.ReplaceAll(x.Path, "/", "~")
}

// target is the agent-scope id: "<connection id>:<owner/repo>".
func (x extID) target() string { return app.ExternalTarget(x.ConnID, x.Path) }

// isExternalID tells whether a route's repository id is a connected one.
func isExternalID(s string) bool {
	p, _, _ := strings.Cut(s, "~")
	_, ok := platformNames[p]
	return ok && strings.Contains(s, "~")
}

// ---- views ---------------------------------------------------------------------

// Supports says what a repository's host offers, so screens show only
// what works. The app's own repositories leave it out (everything but
// checks); connected ones always send it.
type Supports struct {
	Settings       bool `json:"settings"`         // settings and protection rules in the app
	Credentials    bool `json:"credentials"`      // git credentials from this app clone it
	Upload         bool `json:"upload"`           // upload files in the browser
	Blame          bool `json:"blame"`            // line-by-line blame
	ResolveThreads bool `json:"resolve_threads"`  // resolve line threads
	RequestChanges bool `json:"request_changes"`  // "request changes" reviews
	Reviewers      bool `json:"reviewers"`        // ask people for reviews
	Realtime       bool `json:"realtime"`         // live updates
	Checks         bool `json:"checks"`           // CI status checks / pipelines
	CodeSearchRefs bool `json:"code_search_refs"` // code search on any ref (else the default branch only)
	TreeCommits    bool `json:"tree_commits"`     // last commit per entry in folder listings
	Import         bool `json:"import"`           // copy into the app (Import into the app)
}

// XRepoView is a connected repository as clients see it: RepoView's JSON
// plus where it comes from.
type XRepoView struct {
	ID                  string         `json:"id"`
	OrgID               string         `json:"org_id"`
	Name                string         `json:"name"`
	Slug                string         `json:"slug"`
	FullName            string         `json:"full_name"`
	Description         string         `json:"description"`
	FolderID            *string        `json:"folder_id"`
	DefaultBranch       string         `json:"default_branch"`
	CloneURL            string         `json:"clone_url"`
	Link                string         `json:"link"`
	Empty               bool           `json:"empty"`
	MyLevel             string         `json:"my_level"`
	CanPush             bool           `json:"can_push"`
	CanManage           bool           `json:"can_manage"`
	AllowMergeCommit    bool           `json:"allow_merge_commit"`
	AllowSquash         bool           `json:"allow_squash"`
	AllowRebase         bool           `json:"allow_rebase"`
	DeleteBranchOnMerge bool           `json:"delete_branch_on_merge"`
	OpenPulls           int            `json:"open_pulls"`
	SizeBytes           int64          `json:"size_bytes"`
	PushedAt            *time.Time     `json:"pushed_at"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	Private             bool           `json:"private"`
	Archived            bool           `json:"archived"`
	WebURL              string         `json:"web_url"`
	Source              *app.SourceRef `json:"source"`
	Supports            Supports       `json:"supports"`
	// GrantWriteURL is the app path that turns on writing for this
	// connection, when only reading is granted.
	GrantWriteURL string `json:"grant_write_url,omitempty"`
}

// XRepoSummary is a row of a connection's repository list.
type XRepoSummary struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Slug          string         `json:"slug"`
	FullName      string         `json:"full_name"`
	Owner         string         `json:"owner"`
	Description   string         `json:"description"`
	FolderID      *string        `json:"folder_id"`
	DefaultBranch string         `json:"default_branch"`
	OpenPulls     int            `json:"open_pulls"`
	MyLevel       string         `json:"my_level"`
	Private       bool           `json:"private"`
	PushedAt      *time.Time     `json:"pushed_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Source        *app.SourceRef `json:"source"`
}

// XUser is a platform account (never an app user id): "github:1001".
type XUser struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"` // always null: outside images tell their host who reads the page
	Login       string  `json:"login"`
}

type XReviewer struct {
	User      XUser  `json:"user"`
	Requested bool   `json:"requested"`
	State     string `json:"state"`
	Stale     bool   `json:"stale"`
}

// XPull is a pull request (GitHub) or merge request (GitLab) in Pull's JSON.
type XPull struct {
	ID              string         `json:"id"`
	RepoID          string         `json:"repo_id"`
	Number          int            `json:"number"`
	Title           string         `json:"title"`
	Body            string         `json:"body"`
	State           string         `json:"state"` // open | closed | merged
	HeadBranch      string         `json:"head_branch"`
	BaseBranch      string         `json:"base_branch"`
	HeadSHA         string         `json:"head_sha"`
	BaseSHA         string         `json:"base_sha"`
	Author          *XUser         `json:"author"`
	MergeMethod     *string        `json:"merge_method"`
	MergeCommitSHA  *string        `json:"merge_commit_sha"`
	MergedBy        *XUser         `json:"merged_by"`
	MergedAt        *time.Time     `json:"merged_at"`
	ClosedBy        *XUser         `json:"closed_by"`
	ClosedAt        *time.Time     `json:"closed_at"`
	CommentCount    int            `json:"comment_count"`
	Approvals       int            `json:"approvals"`
	ChangesRequests int            `json:"changes_requests"`
	Reviewers       []XReviewer    `json:"reviewers"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	CanEdit         bool           `json:"can_edit"`
	IsAuthor        bool           `json:"is_author"`
	Draft           bool           `json:"draft"`
	WebURL          string         `json:"web_url"`
	Source          *app.SourceRef `json:"source"`
}

type XComment struct {
	ID         string     `json:"id"`
	Author     *XUser     `json:"author"`
	Body       string     `json:"body"`
	Path       *string    `json:"path"`
	Line       *int       `json:"line"`
	Side       *string    `json:"side"`
	CommitSHA  *string    `json:"commit_sha"`
	ReplyTo    *string    `json:"reply_to"`
	ReviewID   *string    `json:"review_id"`
	ResolvedAt *time.Time `json:"resolved_at"`
	ResolvedBy *XUser     `json:"resolved_by"`
	EditedAt   *time.Time `json:"edited_at"`
	Deleted    bool       `json:"deleted"`
	CreatedAt  time.Time  `json:"created_at"`
	CanEdit    bool       `json:"can_edit"`
}

type XReview struct {
	ID          string     `json:"id"`
	Author      *XUser     `json:"author"`
	State       string     `json:"state"`
	Body        string     `json:"body"`
	CommitSHA   string     `json:"commit_sha"`
	DismissedAt *time.Time `json:"dismissed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type XEvent struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Actor     *XUser         `json:"actor"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

type XThreadSummary struct {
	ID      string `json:"id"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Side    string `json:"side"`
	Body    string `json:"body"`
	Replies int    `json:"replies"`
	Author  *XUser `json:"author"`
}

type XTimelineItem struct {
	Type    string           `json:"type"` // comment | review | thread | event
	At      time.Time        `json:"at"`
	Comment *XComment        `json:"comment,omitempty"`
	Review  *XReview         `json:"review,omitempty"`
	Event   *XEvent          `json:"event,omitempty"`
	Thread  *XThreadSummary  `json:"thread,omitempty"`
	Threads []XThreadSummary `json:"threads,omitempty"`
}

type XThread struct {
	Root     XComment   `json:"root"`
	Replies  []XComment `json:"replies"`
	Outdated bool       `json:"outdated"`
}

type XPullDiff struct {
	Base string `json:"base"`
	Head string `json:"head"`
	Diff *Diff  `json:"diff"`
}

// Check is one CI status check (GitHub check run or commit status, GitLab
// pipeline job), read-only.
type Check struct {
	Name         string `json:"name"`
	Status       string `json:"status"` // queued | in_progress | success | failure | neutral | cancelled | skipped
	Description  string `json:"description"`
	URL          string `json:"url"`
	AllowFailure bool   `json:"allow_failure"`
}

// Checks is every check on one commit and their overall state.
type Checks struct {
	SHA   string  `json:"sha"`
	State string  `json:"state"` // success | failure | pending | none
	Items []Check `json:"items"`
	URL   string  `json:"url"` // the pipeline or checks page on the platform
}

func overallState(items []Check) string {
	if len(items) == 0 {
		return "none"
	}
	state := "success"
	for _, c := range items {
		switch c.Status {
		case "failure", "cancelled":
			if !c.AllowFailure {
				return "failure"
			}
		case "queued", "in_progress":
			state = "pending"
		}
	}
	return state
}

// pullQuery filters a pull request list.
type pullQuery struct {
	State          string // open | closed | merged | all
	Q              string
	Author         bool // the viewer's
	ReviewRequired bool // asking the viewer
	Head, Base     string
	Page, Limit    int
}

// remoteCommit is a commit to a connected repository.
type remoteCommit struct {
	CommitInput
	agent string // "Made by … (AI agent) for …" trailer, when an agent commits
}

type xCommentInput struct {
	Body    string `json:"body"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Side    string `json:"side"` // new (default) | old
	ReplyTo string `json:"reply_to"`
}

type xReviewInput struct {
	State    string          `json:"state"` // approve | request_changes | comment
	Body     string          `json:"body"`
	Comments []xCommentInput `json:"comments"`
}

var errNotSupported = func(platform, what string) error {
	return httpx.NewError(http.StatusNotImplemented, "not_supported", platform+" doesn't offer "+what+" through its API.")
}
