package repos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
)

// Git smart HTTP at /git/<org slug>/<repo slug>.git (clone, fetch, pull,
// push). git http-backend does the protocol work behind net/http/cgi; this
// handler authenticates, authorises and constrains each request first:
//
//   - HTTP Basic with a personal git credential (any username). Agent
//     credentials (agt_…), session tokens and bearer tokens are refused, so an
//     agent can't bypass its scope with git push.
//   - Access through the access feature on every request: fetch needs view,
//     push needs edit (and a read-write credential and an active account).
//     No view answers 404, like the rest of the API.
//   - The request body is spooled to disk (CGI can't take chunked bodies),
//     capped at REPOS_MAX_PUSH_BYTES; receive.maxInputSize caps the pack too.
//   - Pushes run a pre-receive check (hook_unix.go) that enforces branch
//     protection with the incoming objects in quarantine.
//   - Only the three smart endpoints exist; the dumb protocol is off.

const gitRealm = `Basic realm="Scaffold App git", charset="UTF-8"`

// gitRoute is a parsed /git/… path.
type gitRoute struct {
	owner, name string // org slug and repo slug, or "-" and the repo id
	rest        string // info/refs | git-upload-pack | git-receive-pack
}

func parseGitPath(p string) (gitRoute, bool) {
	p = strings.TrimPrefix(p, "/git/")
	owner, after, ok := strings.Cut(p, "/")
	if !ok || owner == "" {
		return gitRoute{}, false
	}
	for _, rest := range []string{"info/refs", "git-upload-pack", "git-receive-pack"} {
		name, found := strings.CutSuffix(after, "/"+rest)
		if !found {
			continue
		}
		name = strings.TrimSuffix(name, ".git")
		if name == "" || strings.Contains(name, "/") {
			return gitRoute{}, false
		}
		return gitRoute{owner: owner, name: name, rest: rest}, true
	}
	return gitRoute{}, false
}

// gitFail answers in plain text, which git prints to the person.
func gitFail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", gitRealm)
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg+"\n")
}

func (s *Service) serveGit(w http.ResponseWriter, r *http.Request) {
	if s.gitProblem != "" {
		gitFail(w, http.StatusServiceUnavailable, s.gitProblem)
		return
	}
	route, ok := parseGitPath(r.URL.Path)
	if !ok {
		gitFail(w, http.StatusNotFound, "Not found. Clone URLs look like "+s.cloneURLExample()+".")
		return
	}
	service := route.rest
	switch {
	case route.rest == "info/refs" && r.Method == http.MethodGet:
		service = r.URL.Query().Get("service")
		if service != "git-upload-pack" && service != "git-receive-pack" {
			gitFail(w, http.StatusForbidden, "This server speaks git's smart HTTP protocol only. Update git and try again.")
			return
		}
	case (route.rest == "git-upload-pack" || route.rest == "git-receive-pack") && r.Method == http.MethodPost:
	default:
		gitFail(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	push := service == "git-receive-pack"

	// ---- who
	p, msg := s.gitAuthenticate(r)
	if p == nil {
		status := http.StatusUnauthorized
		if msg != "" {
			status = http.StatusForbidden
		} else {
			msg = "Sign in with a git credential from Settings → Git credentials (any username, the credential as the password)."
		}
		gitFail(w, status, msg)
		return
	}
	ctx := r.Context()

	// ---- which repository, and may they
	repo, err := s.repoBySlug(ctx, route.owner, route.name)
	if err != nil && !errors.Is(err, errRepoNotFound) {
		slog.ErrorContext(ctx, "repos: git lookup failed", "err", err)
		gitFail(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	level := access.None
	if repo != nil {
		level, err = access.LevelOf(ctx, s.d.Pool, p.identity.UserID, RepoKind, repo.ID)
		if err != nil {
			slog.ErrorContext(ctx, "repos: git access check failed", "err", err)
			gitFail(w, http.StatusInternalServerError, "Something went wrong. Try again.")
			return
		}
	}
	if repo == nil || level < access.View || !s.git.Exists(repo.ID) {
		gitFail(w, http.StatusNotFound, "Repository not found. Check the URL, and that the account this credential belongs to can open it.")
		return
	}
	if push {
		switch {
		case level < access.Edit:
			gitFail(w, http.StatusForbidden, "You can view this repository but not push to it. Ask someone who manages it for edit access.")
			return
		case p.identity.Status != "active":
			gitFail(w, http.StatusForbidden, "Your account is suspended, so it can't push. Contact support.")
			return
		case !p.write:
			gitFail(w, http.StatusForbidden, "This git credential is read-only. Make a read-write one in Settings → Git credentials to push.")
			return
		}
	}

	// ---- the body: spooled, capped
	if r.Method == http.MethodPost {
		limit := s.maxPushBytes
		if !push {
			limit = 64 << 20 // fetch negotiation (wants and haves)
		}
		body, n, err := s.spool(r.Body, limit)
		if err != nil {
			if errors.Is(err, ErrTooLarge) {
				gitFail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("This push is larger than the %s limit. Push fewer commits at a time, or leave large files out of the repository.", formatBytes(s.maxPushBytes)))
				return
			}
			slog.WarnContext(ctx, "repos: reading git request failed", "err", err)
			gitFail(w, http.StatusBadRequest, "The request body couldn't be read.")
			return
		}
		defer func() {
			_ = body.Close()
			_ = os.Remove(body.Name())
		}()
		r.Body = body
		r.ContentLength = n
		r.TransferEncoding = nil
		r.Header.Del("Transfer-Encoding")
	}

	env := []string{
		"GIT_PROJECT_ROOT=" + s.git.Root,
		"GIT_HTTP_EXPORT_ALL=1",
		"GIT_HTTP_MAX_REQUEST_BUFFER=64m",
		"REMOTE_USER=" + p.identity.UserID.String(),
	}
	env = append(env, s.git.baseEnv()...)
	config := [][2]string{
		{"http.uploadpack", "true"},
		{"http.getanyfile", "false"},
		{"uploadpack.allowFilter", "true"},
		{"uploadpack.keepAlive", "5"},
		{"core.hooksPath", s.hooksDir},
	}
	if push {
		config = append(config,
			[2]string{"http.receivepack", "true"},
			[2]string{"receive.maxInputSize", strconv.FormatInt(s.maxPushBytes, 10)},
			[2]string{"receive.keepAlive", "5"},
			[2]string{"receive.advertisePushOptions", "false"},
		)
	} else {
		config = append(config, [2]string{"http.receivepack", "false"})
	}
	env = append(env, gitConfigEnv(config)...)

	// The pre-receive check talks to this request through a FIFO pair.
	var hook *pushHook
	if push && route.rest == "git-receive-pack" {
		hook, err = s.newPushHook(ctx, repo)
		if err != nil {
			slog.ErrorContext(ctx, "repos: push check setup failed", "err", err)
			gitFail(w, http.StatusServiceUnavailable, "Pushing isn't available right now. Try again shortly.")
			return
		}
		env = append(env, hook.env()...)
		defer hook.close()
	}

	// ---- hand over to git http-backend
	req := r.Clone(ctx)
	req.URL.Path = "/" + repo.ID.String() + ".git/" + route.rest
	req.URL.RawPath = ""
	req.Header.Del("Authorization") // the credential never reaches git's environment
	req.Header.Del("Cookie")
	h := &cgi.Handler{
		Path:   s.git.Bin,
		Args:   []string{"http-backend"},
		Dir:    s.git.Root,
		Env:    env,
		Stderr: s.cgiStderr,
		Logger: log.New(s.cgiStderr, "", 0),
	}
	dw := &deadlineWriter{ResponseWriter: w, deadline: time.Now().Add(s.httpTimeout)}
	h.ServeHTTP(dw, req)

	if hook != nil {
		if accepted := hook.wait(); len(accepted) > 0 {
			s.afterPush(context.WithoutCancel(ctx), repo, p.identity, accepted)
		}
	}
}

// gitAuthenticate reads HTTP Basic. It returns nil with an explanation for
// credentials that must never work here, and nil with "" for a missing or
// unknown credential (401: git asks again).
func (s *Service) gitAuthenticate(r *http.Request) (*gitPrincipal, string) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return nil, "Sign-in and agent tokens don't work with git. Use a git credential from Settings → Git credentials."
	}
	user, pass, ok := r.BasicAuth()
	if !ok {
		return nil, ""
	}
	if auth.IsAgentCredential(pass) || auth.IsAgentCredential(user) {
		return nil, "Agent credentials only work at the agent endpoint. AI agents change repositories through their tools, which follow your AI agent scope."
	}
	if strings.HasPrefix(pass, "dev:") || strings.Count(pass, ".") == 2 && len(pass) > 100 {
		return nil, "Sign-in tokens don't work with git. Use a git credential from Settings → Git credentials."
	}
	secret := pass
	if secret == "" && strings.HasPrefix(user, credPrefix) {
		secret = user // some clients put a token in the username
	}
	p, err := s.resolveCredential(r.Context(), secret)
	if err != nil {
		slog.ErrorContext(r.Context(), "repos: credential lookup failed", "err", err)
		return nil, ""
	}
	return p, ""
}

// spool copies a request body to a temp file, up to limit bytes.
func (s *Service) spool(body io.Reader, limit int64) (*os.File, int64, error) {
	f, err := os.CreateTemp(s.tmpDir, "body-")
	if err != nil {
		return nil, 0, err
	}
	n, err := io.Copy(f, io.LimitReader(body, limit+1))
	if err == nil && n > limit {
		err = ErrTooLarge
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, 0, err
	}
	return f, n, nil
}

// gitConfigEnv passes config to every git process of the request (and the
// hooks they run) without touching the repository's config file.
func gitConfigEnv(kv [][2]string) []string {
	env := []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(len(kv))}
	for i, p := range kv {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, p[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, p[1]))
	}
	return env
}

// deadlineWriter fails writes after the deadline. net/http/cgi kills the
// git process when a write fails, and git writes at least every 5 seconds
// (keepalives), so this bounds every request's run time.
type deadlineWriter struct {
	http.ResponseWriter
	deadline time.Time
}

var errGitDeadline = errors.New("repos: git request took too long")

func (d *deadlineWriter) Write(p []byte) (int, error) {
	if time.Now().After(d.deadline) {
		return 0, errGitDeadline
	}
	return d.ResponseWriter.Write(p)
}

func (d *deadlineWriter) Flush() {
	if f, ok := d.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// slogWriter sends http-backend's stderr to the log, a line at a time.
type slogWriter struct{}

func (slogWriter) Write(p []byte) (int, error) {
	if msg := strings.TrimSpace(string(p)); msg != "" {
		slog.Warn("repos: git http-backend", "msg", truncateUTF8(msg, 500))
	}
	return len(p), nil
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GB", n>>30)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

// repoBySlug finds a repository from a clone URL: "<org slug>/<repo slug>"
// or "-/<repo id>". Trashed repositories don't resolve.
func (s *Service) repoBySlug(ctx context.Context, owner, name string) (*Repo, error) {
	if owner == "-" {
		id, err := uuid.Parse(name)
		if err != nil {
			return nil, errRepoNotFound
		}
		return s.loadRepo(ctx, s.d.Pool, id)
	}
	var id uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `
		SELECT r.id FROM repos_repos r JOIN orgs o ON o.id = r.org_id
		WHERE o.slug = $1 AND r.slug = $2`, strings.ToLower(owner), strings.ToLower(name)).Scan(&id)
	if db.IsNoRows(err) {
		return nil, errRepoNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.loadRepo(ctx, s.d.Pool, id)
}
