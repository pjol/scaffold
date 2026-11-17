package repos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Archives (zip / tar.gz of a ref) and insights (contributors, weekly
// commit activity).

// ---- archive -------------------------------------------------------------------

var archiveNameStrip = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Archive streams `git archive` of a commit to w.
func (g *Git) Archive(ctx context.Context, id uuid.UUID, sha, format, prefix string, w io.Writer, timeout time.Duration) error {
	if !IsOID(sha) {
		return ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// git 2.38+ gzips tar.gz itself (no outside gzip process).
	args := []string{"--git-dir=" + g.Dir(id), "archive", "--format=" + format, "--prefix=" + prefix + "/", sha}
	cmd := exec.CommandContext(ctx, g.Bin, args...)
	cmd.Env = g.baseEnv()
	cmd.Stdout = w
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

// createArchive is POST /repos/{repoID}/archive {ref, format}: view level,
// a person. It answers a one-time link valid for two minutes (a browser
// download can't carry the session header).
func (s *Service) createArchive(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	var req struct {
		Ref    string `json:"ref"`
		Format string `json:"format"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Format == "" {
		req.Format = "zip"
	}
	if req.Format != "zip" && req.Format != "tar.gz" {
		return httpx.BadRequest("format is zip or tar.gz.")
	}
	at, err := s.resolve(r.Context(), repo, req.Ref)
	if err != nil {
		return err
	}
	label := at.Ref
	if label == "" || IsOID(label) {
		label = at.SHA[:7]
	}
	name := repo.Slug + "-" + strings.Trim(archiveNameStrip.ReplaceAllString(label, "-"), "-")
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	if _, err := s.d.Pool.Exec(r.Context(), `INSERT INTO repos_archive_tokens (token_hash, user_id, repo_id, sha, name, format, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + interval '2 minutes')`, hashCredential(token), me.UserID, repo.ID, at.SHA, name, req.Format); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"url": s.publicAPI + "/git-archive/" + token, "name": name + "." + req.Format, "sha": at.SHA})
	return nil
}

// serveArchive is GET /git-archive/{token} (root): the token is used up,
// and the person's access is checked again before anything streams.
func (s *Service) serveArchive(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, msg string) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, msg+"\n")
	}
	if s.git == nil || s.gitProblem != "" {
		fail(http.StatusServiceUnavailable, "Repositories are unavailable on this server.")
		return
	}
	token := chi.URLParam(r, "token")
	if len(token) != 64 {
		fail(http.StatusNotFound, "This download link is invalid. Start the download again from the repository.")
		return
	}
	ctx := r.Context()
	var userID, repoID uuid.UUID
	var sha, name, format string
	err := s.d.Pool.QueryRow(ctx, `DELETE FROM repos_archive_tokens WHERE token_hash = $1 AND expires_at > now()
		RETURNING user_id, repo_id, sha, name, format`, hashCredential(token)).Scan(&userID, &repoID, &sha, &name, &format)
	if err != nil {
		fail(http.StatusNotFound, "This download link expired or was used. Start the download again from the repository.")
		return
	}
	var status string
	if err := s.d.Pool.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, userID).Scan(&status); err != nil || (status != "active" && status != "suspended") {
		fail(http.StatusNotFound, "Repository not found.")
		return
	}
	lvl, err := access.LevelOf(ctx, s.d.Pool, userID, RepoKind, repoID)
	if err != nil || lvl < access.View {
		fail(http.StatusNotFound, "Repository not found.")
		return
	}
	if _, err := s.loadRepo(ctx, s.d.Pool, repoID); err != nil {
		fail(http.StatusNotFound, "Repository not found.")
		return
	}
	ct := "application/zip"
	if format == "tar.gz" {
		ct = "application/gzip"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+"."+format+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	dw := &deadlineWriter{ResponseWriter: w, deadline: time.Now().Add(s.httpTimeout)}
	if err := s.git.Archive(ctx, repoID, sha, format, name, dw, s.httpTimeout); err != nil {
		slog.WarnContext(ctx, "repos: archive failed", "repo_id", repoID, "err", err)
	}
}

// pruneArchiveTokens removes expired download tokens.
func (s *Service) pruneArchiveTokens(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `DELETE FROM repos_archive_tokens WHERE expires_at < now() - interval '1 hour'`)
	return err
}

// ---- insights ------------------------------------------------------------------

// Contributor is one author's commits on the default branch.
type Contributor struct {
	Name      string    `json:"name"`
	User      *UserRef  `json:"user"` // when the commits came from the app (no-reply address)
	Commits   int       `json:"commits"`
	Additions int       `json:"additions"`
	Deletions int       `json:"deletions"`
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
}

// Insights is the default branch's history in numbers.
type Insights struct {
	Branch       string        `json:"branch"`
	SHA          string        `json:"sha"`
	TotalCommits int           `json:"total_commits"`
	Truncated    bool          `json:"truncated"`
	Contributors []Contributor `json:"contributors"`
	Weeks        []WeekCount   `json:"weeks"` // the last 52 weeks, oldest first
	OpenIssues   int           `json:"open_issues"`
	ClosedIssues int           `json:"closed_issues"`
	OpenPulls    int           `json:"open_pulls"`
	MergedPulls  int           `json:"merged_pulls"`
}

// WeekCount is commits in a week starting Monday (UTC).
type WeekCount struct {
	Week    string `json:"week"` // YYYY-MM-DD
	Commits int    `json:"commits"`
}

const maxInsightCommits = 20000

// insightsCache keeps each repository's latest computed insights (one
// entry per repository, replaced when its default branch moves).
var insightsCache sync.Map // repo id → *Insights

// getInsights is GET /repos/{repoID}/insights (view).
func (s *Service) getInsights(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	ctx := r.Context()
	sha, err := s.git.RefValue(ctx, repo.ID, "refs/heads/"+repo.DefaultBranch)
	if err != nil {
		return err
	}
	in := &Insights{Branch: repo.DefaultBranch, SHA: sha, Contributors: []Contributor{}, Weeks: weeksBack(time.Now(), 52)}
	if sha != "" {
		key := repo.ID
		if v, ok := insightsCache.Load(key); ok && v.(*Insights).SHA == sha {
			cached := *v.(*Insights)
			in = &cached
		} else {
			if err := s.computeInsights(ctx, repo, sha, in); err != nil {
				return err
			}
			insightsCache.Store(key, in)
			copyIn := *in
			in = &copyIn
		}
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM repos_issues WHERE repo_id = $1 AND state = 'open'),
		(SELECT count(*) FROM repos_issues WHERE repo_id = $1 AND state = 'closed'),
		(SELECT count(*) FROM repos_pulls WHERE repo_id = $1 AND state = 'open'),
		(SELECT count(*) FROM repos_pulls WHERE repo_id = $1 AND state = 'merged')`, repo.ID).
		Scan(&in.OpenIssues, &in.ClosedIssues, &in.OpenPulls, &in.MergedPulls); err != nil {
		return err
	}
	httpx.OK(w, in)
	return nil
}

func weeksBack(now time.Time, n int) []WeekCount {
	now = now.UTC()
	monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	monday = monday.AddDate(0, 0, -((int(monday.Weekday()) + 6) % 7))
	out := make([]WeekCount, n)
	for i := 0; i < n; i++ {
		out[i] = WeekCount{Week: monday.AddDate(0, 0, -7*(n-1-i)).Format("2006-01-02")}
	}
	return out
}

func (s *Service) computeInsights(ctx context.Context, repo *Repo, sha string, in *Insights) error {
	out, _, err := s.git.run(ctx, repo.ID, runOpts{max: 32 << 20}, "log", "--no-merges", "--numstat", "--format=%x1e%an%x00%ae%x00%at",
		"--max-count="+fmt.Sprint(maxInsightCommits), "--end-of-options", sha)
	if err != nil && err != ErrTooLarge {
		return err
	}
	in.Truncated = err == ErrTooLarge
	type agg struct {
		Contributor
		email string
	}
	byKey := map[string]*agg{}
	weekIdx := map[string]int{}
	for i, wk := range in.Weeks {
		weekIdx[wk.Week] = i
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		head, stats, _ := strings.Cut(rec, "\n")
		f := strings.SplitN(head, "\x00", 3)
		if len(f) < 3 {
			continue
		}
		at := unixTime(f[2])
		key := strings.ToLower(f[1])
		a := byKey[key]
		if a == nil {
			a = &agg{Contributor: Contributor{Name: f[0], First: at, Last: at}, email: f[1]}
			byKey[key] = a
		}
		a.Commits++
		if at.Before(a.First) {
			a.First = at
		}
		if at.After(a.Last) {
			a.Last = at
		}
		for _, line := range strings.Split(stats, "\n") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			var add, del int
			fmt.Sscan(parts[0], &add)
			fmt.Sscan(parts[1], &del)
			a.Additions += add
			a.Deletions += del
		}
		in.TotalCommits++
		wk := weeksBack(at, 1)[0].Week
		if i, ok := weekIdx[wk]; ok {
			in.Weeks[i].Commits++
		}
	}
	// In-app commits use <user id>@users.noreply.<host>: show the person.
	var ids []uuid.UUID
	for _, a := range byKey {
		if local, host, ok := strings.Cut(a.email, "@"); ok && strings.HasPrefix(host, "users.noreply.") {
			if id, err := uuid.Parse(local); err == nil {
				ids = append(ids, id)
			}
		}
	}
	people := map[uuid.UUID]UserRef{}
	if len(ids) > 0 {
		rows, err := s.d.Pool.Query(ctx, `SELECT `+userRefCols+` FROM users u WHERE u.id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[UserRef])
		if err != nil {
			return err
		}
		for _, u := range list {
			people[u.ID] = u
		}
	}
	for _, a := range byKey {
		c := a.Contributor
		if local, _, ok := strings.Cut(a.email, "@"); ok {
			if id, err := uuid.Parse(local); err == nil {
				if u, ok := people[id]; ok {
					uu := u
					c.User = &uu
					c.Name = u.DisplayName
				}
			}
		}
		in.Contributors = append(in.Contributors, c)
	}
	sort.Slice(in.Contributors, func(i, j int) bool {
		if in.Contributors[i].Commits != in.Contributors[j].Commits {
			return in.Contributors[i].Commits > in.Contributors[j].Commits
		}
		return in.Contributors[i].Name < in.Contributors[j].Name
	})
	if len(in.Contributors) > 100 {
		in.Contributors = in.Contributors[:100]
	}
	return nil
}
