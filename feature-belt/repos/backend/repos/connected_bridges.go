package repos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/files"
	"scaffoldapp/backend/internal/httpx"
)

// Bridges between connected repositories and the app:
//   - Import: clone a connected repository into a new repository here
//     (branches and tags), server-side, through the person's connection.
//   - Links: a drive item (type repo_link) pointing at a connected
//     repository; whoever opens it is checked live with their own connection.

// LinkItemType is the drive item type of a link to a connected repository.
const LinkItemType = "repo_link"

func (s *Service) registerLinkType() {
	files.RegisterItemType(files.ItemType{
		Type:  LinkItemType,
		Title: "Repository link",
		Icon:  "link",
		Link:  func(id uuid.UUID) string { return "/repos/links/" + id.String() },
		OnDelete: func(ctx context.Context, tx pgx.Tx, itemID uuid.UUID) error {
			_, err := tx.Exec(ctx, `DELETE FROM repos_links WHERE id = $1`, itemID)
			return err
		},
	})
}

// ---- import --------------------------------------------------------------------

type importConnectedReq struct {
	RepoID   string     `json:"repo_id"` // the connected id (github~…)
	OrgID    uuid.UUID  `json:"org_id"`
	FolderID *uuid.UUID `json:"folder_id"`
	Name     string     `json:"name"`
}

// importConnected is POST /v1/repos/connected/import: a copy of a connected
// repository's branches and tags as a new repository in the drive.
func (s *Service) importConnected(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if s.git == nil || s.gitProblem != "" {
		return httpx.NewError(http.StatusServiceUnavailable, "git_unavailable", "Repositories are unavailable on this server.")
	}
	var in importConnectedReq
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	x, ok := parseExtID(in.RepoID)
	if !ok {
		return httpx.BadRequest("repo_id must be a connected repository's id.")
	}
	if err := s.canCreateIn(r.Context(), me, in.OrgID, in.FolderID); err != nil {
		return err
	}
	ctx := app.WithConnectionOrg(r.Context(), in.OrgID)
	src, rr, err := s.openRemote(ctx, me.UserID, x, false)
	if err != nil {
		return err
	}
	info, err := src.Info(ctx)
	if err != nil {
		return s.remoteErr(err, rr)
	}
	if info.SizeBytes > s.maxPushBytes {
		return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("%s is %s; repositories here take up to %s.", info.FullName, formatBytes(info.SizeBytes), formatBytes(s.maxPushBytes)))
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = info.Name
	}
	if name, err = cleanRepoName(name); err != nil {
		return err
	}
	branch := info.DefaultBranch
	if branch == "" || s.git.CheckRefFormat(ctx, "heads", branch) != nil {
		branch = "main"
	}

	tmp, err := os.MkdirTemp(s.tmpDir, "import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	mirror := filepath.Join(tmp, "src.git")
	if err := s.cloneThrough(ctx, rr, mirror); err != nil {
		return err
	}

	desc := info.Description
	if len([]rune(desc)) > 350 {
		desc = string([]rune(desc)[:350])
	}
	var repo *Repo
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if repo, err = s.newRepo(ctx, tx, me, in.OrgID, in.FolderID, name, desc, branch); err != nil {
			return err
		}
		if _, err = s.git.output(ctx, repo.ID, "fetch", "--quiet", "--no-write-fetch-head", mirror, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE repos_repos SET pushed_at = now() WHERE id = $1`, repo.ID)
		return err
	})
	if err != nil {
		return err
	}
	s.updateSizeSoon(ctx, repo.ID)
	lvl, _ := access.LevelOf(ctx, s.d.Pool, me.UserID, RepoKind, repo.ID)
	v, err := s.view(ctx, repo, lvl)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, v)
	return nil
}

// cloneThrough clones the platform repository with git over HTTPS through
// a loopback relay: git talks to 127.0.0.1, the relay forwards only the
// fetch endpoints to the platform's git host with the connection's client
// (which signs them), so the token never leaves app.Connections.
func (s *Service) cloneThrough(ctx context.Context, rr *remoteRepo, dst string) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	var sent atomic.Int64
	limit := s.maxPushBytes
	prefix := "/" + rr.id.Path + ".git/"
	relay := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rest, ok := strings.CutPrefix(req.URL.Path, prefix)
		allowed := ok && ((req.Method == http.MethodGet && rest == "info/refs" && req.URL.Query().Get("service") == "git-upload-pack") ||
			(req.Method == http.MethodPost && rest == "git-upload-pack"))
		if !allowed {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		target := rr.web + prefix + rest
		if req.URL.RawQuery != "" {
			target += "?" + req.URL.RawQuery
		}
		out, err := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for _, h := range []string{"Content-Type", "Content-Encoding", "Accept", "Git-Protocol"} {
			if v := req.Header.Get(h); v != "" {
				out.Header.Set(h, v)
			}
		}
		out.Header.Set("User-Agent", "git/2 (Scaffold App import)")
		resp, err := rr.hc.Do(out)
		if err != nil {
			slog.WarnContext(ctx, "repos: import relay failed", "provider", rr.id.Provider, "err", err)
			http.Error(w, "upstream failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Encoding", "Cache-Control"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		buf := make([]byte, 32<<10)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if sent.Add(int64(n)) > limit {
					return // git sees a cut pack and fails; the size check below explains
				}
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	})}
	go func() { _ = relay.Serve(ln) }()
	defer relay.Close()

	timeout := min(s.httpTimeout, 15*time.Minute)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.git.Bin, "-c", "protocol.version=2", "-c", "http.followRedirects=false", "clone", "--bare", "--quiet",
		"http://"+ln.Addr().String()+prefix[:len(prefix)-1], dst)
	cmd.Env = append(s.git.baseEnv(), "GIT_ASKPASS=/bin/false")
	cmd.WaitDelay = 2 * time.Second
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	if err := cmd.Run(); err != nil {
		switch {
		case sent.Load() > limit:
			return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("This repository is larger than %s, the most this app imports.", formatBytes(limit)))
		case errors.Is(cctx.Err(), context.DeadlineExceeded):
			return httpx.NewError(http.StatusGatewayTimeout, "timeout", "Copying took too long. Try again, or clone and push it yourself.")
		case strings.Contains(stderr.String(), "401") || strings.Contains(stderr.String(), "403") || strings.Contains(stderr.String(), "Authentication"):
			return httpx.NewError(http.StatusBadGateway, app.ConnectionHostRefused, rr.platform+" didn't let this connection clone the repository. Reconnect it, or check it has repository access.")
		}
		slog.WarnContext(ctx, "repos: import clone failed", "provider", rr.id.Provider, "err", err, "stderr", truncate(stderr.String(), 500))
		return httpx.NewError(http.StatusBadGateway, app.ConnectionUnavailable, "Copying from "+rr.platform+" failed. Try again.")
	}
	return nil
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n <= 0 {
		return len(p), nil
	}
	q := p
	if len(q) > l.n {
		q = q[:l.n]
	}
	l.n -= len(q)
	_, _ = l.w.Write(q)
	return len(p), nil
}

// ---- links ---------------------------------------------------------------------

type createLinkReq struct {
	RepoID   string     `json:"repo_id"`
	OrgID    uuid.UUID  `json:"org_id"`
	FolderID *uuid.UUID `json:"folder_id"`
}

// LinkView is a link to a connected repository as clients see it.
type LinkView struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	FullName string         `json:"full_name"`
	WebURL   string         `json:"web_url"`
	FolderID *uuid.UUID     `json:"folder_id"`
	RepoID   *string        `json:"repo_id"` // the opener's connected id; null when they can't open it
	Reason   string         `json:"reason,omitempty"`
	FixURL   string         `json:"fix_url,omitempty"`
	Source   *app.SourceRef `json:"source,omitempty"`
}

// createLink is POST /v1/repos/links: a drive item in a folder pointing at
// a connected repository the caller can open now.
func (s *Service) createLink(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	var in createLinkReq
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	x, ok := parseExtID(in.RepoID)
	if !ok {
		return httpx.BadRequest("repo_id must be a connected repository's id.")
	}
	ctx := app.WithConnectionOrg(r.Context(), in.OrgID)
	src, rr, err := s.openRemote(ctx, me.UserID, x, false)
	if err != nil {
		return err
	}
	info, err := src.Info(ctx)
	if err != nil {
		return s.remoteErr(err, rr)
	}
	var item files.Item
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		item, err = files.CreateItem(ctx, tx, files.NewItem{OrgID: in.OrgID, ParentID: in.FolderID, Type: LinkItemType, Name: info.Name + " (" + rr.platform + ")", CreatedBy: me.UserID, AutoRename: true})
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO repos_links (id, org_id, provider, web_base, path, created_by) VALUES ($1, $2, $3, $4, $5, $6)`,
			item.ID, in.OrgID, x.Provider, rr.web, x.Path, me.UserID)
		return err
	})
	if err != nil {
		return err
	}
	id := x.String()
	httpx.JSON(w, http.StatusCreated, LinkView{ID: item.ID.String(), Name: item.Name, Provider: x.Provider, FullName: info.FullName, WebURL: info.WebURL,
		FolderID: in.FolderID, RepoID: &id, Source: info.Source})
	return nil
}

// openLink is GET /v1/repos/links/{linkID}: the opener's way into the
// linked repository, through the first of their own connections to that
// platform that can see it (checked live), or why they can't.
func (s *Service) openLink(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "linkID"))
	if err != nil {
		return httpx.NotFound("Link not found.")
	}
	item, err := files.GetItem(r.Context(), s.d.Pool, me.UserID, id)
	if err != nil {
		return err
	}
	if item.TrashedAt != nil {
		return httpx.NotFound("This link is in the trash.")
	}
	var provider, web, p string
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(r.Context(), `SELECT provider, web_base, path, org_id FROM repos_links WHERE id = $1`, id).Scan(&provider, &web, &p, &orgID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Link not found.")
	}
	if err != nil {
		return err
	}
	v := LinkView{ID: id.String(), Name: item.Name, Provider: provider, FullName: p, WebURL: web + "/" + p, FolderID: item.ParentID,
		FixURL: "/settings/connections?capability=" + capability(provider, false) + "&return_to=/repos/links/" + id.String()}
	conns := s.connections()
	if conns == nil {
		v.Reason = "unavailable"
		httpx.OK(w, v)
		return nil
	}
	list, err := conns.For(r.Context(), me.UserID, orgID, capability(provider, false))
	if err != nil {
		return err
	}
	ctx := app.WithConnectionOrg(r.Context(), orgID)
	v.Reason = "no_connection"
	for _, c := range list {
		if c.Provider != provider || c.Status != "active" {
			continue
		}
		if cw := func() string { _, wb, _ := endpoints(c); return wb }(); !strings.EqualFold(cw, web) {
			continue
		}
		v.Reason = "no_access"
		x, ok := newExtID(provider, c.ID, p)
		if !ok {
			continue
		}
		src, rr, err := s.openRemote(ctx, me.UserID, x, false)
		if err != nil {
			continue
		}
		info, err := src.Info(ctx)
		if err != nil {
			if code := app.ConnectionErrorCode(s.remoteErr(err, rr)); code == app.ConnectionRateLimited {
				return s.remoteErr(err, rr)
			}
			continue
		}
		rid := x.String()
		v.RepoID, v.Reason, v.FixURL, v.Source, v.WebURL = &rid, "", "", info.Source, info.WebURL
		break
	}
	httpx.OK(w, v)
	return nil
}
