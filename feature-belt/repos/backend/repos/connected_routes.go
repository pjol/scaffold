package repos

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// The {repoID} routes serve both kinds of repository: route() sends a
// connected id (github~…, gitlab~…) to the remote handler and anything else
// to the engine's own. Remote handlers answer the same JSON as the engine's,
// so clients don't branch.

type remoteHandler func(w http.ResponseWriter, r *http.Request, src RepoSource, rr *remoteRepo) error

// route picks the handler for a {repoID} route. write opens the connection
// with the write capability. A nil remote answers 404 for connected ids
// (settings, protection and git credentials stay the app's own).
func (s *Service) route(local func(http.ResponseWriter, *http.Request) error, remote remoteHandler, write bool) http.HandlerFunc {
	return httpx.Handler(func(w http.ResponseWriter, r *http.Request) error {
		id := chi.URLParam(r, "repoID")
		if !isExternalID(id) {
			return local(w, r)
		}
		x, ok := parseExtID(id)
		if !ok || remote == nil {
			return errRepoNotFound
		}
		me := auth.MustFromContext(r.Context())
		src, rr, err := s.openRemote(r.Context(), me.UserID, x, write)
		if err != nil {
			return err
		}
		return s.remoteErr(remote(w, r, src, rr), rr)
	})
}

// viaRead and viaWrite name the two kinds of {repoID} routes in MountUser.
func (s *Service) viaRead(local func(http.ResponseWriter, *http.Request) error, remote remoteHandler) http.HandlerFunc {
	return s.route(local, remote, false)
}

func (s *Service) viaWrite(local func(http.ResponseWriter, *http.Request) error, remote remoteHandler) http.HandlerFunc {
	return s.route(local, remote, true)
}

func pullNumber(r *http.Request) (int, error) {
	n, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil || n < 1 {
		return 0, httpx.NotFound("Pull request not found.")
	}
	return n, nil
}

func decodeJSON(r *http.Request, dst any) error { return httpx.Decode(r, dst) }

// ---- repository and reading ----------------------------------------------------

func (s *Service) xGetRepo(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	v, err := src.Info(r.Context())
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

func (s *Service) xRefs(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	refs, def, err := src.Refs(r.Context(), r.URL.Query().Get("counts") == "true")
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": refs, "default_branch": def})
	return nil
}

// xBlob builds the engine's BlobView from a file's bytes.
func (s *Service) xBlob(ctx context.Context, src RepoSource, at resolved, p string) (*BlobView, error) {
	max := s.maxEditBytes
	ext := strings.ToLower(path.Ext(p))
	_, isImage := imageTypes[ext]
	if isImage {
		max = maxImageBytes
	}
	e, data, err := src.File(ctx, at, p, max)
	if err != nil {
		return nil, err
	}
	b := &BlobView{Path: p, Name: path.Base(p), SHA: at.SHA, Ref: at.Ref, OID: e.OID, Mode: e.Mode, Size: e.Size, Language: languageOf(p),
		Symlink: e.Type == "symlink", Submodule: e.Type == "commit"}
	if b.Submodule {
		return b, nil
	}
	if isImage && ext != ".svg" {
		b.Image, b.Binary = true, true
		if data == nil {
			b.TooLarge = true
			return b, nil
		}
		b.Encoding, b.Content = "base64", base64.StdEncoding.EncodeToString(data)
		return b, nil
	}
	if data == nil {
		b.TooLarge = true
		return b, nil
	}
	if b.Size <= 0 {
		b.Size = int64(len(data))
	}
	if isBinary(data) {
		b.Binary = true
		return b, nil
	}
	b.Encoding, b.Content = "utf-8", string(data)
	b.Lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.Lines++
	}
	b.Editable = !b.Symlink
	return b, nil
}

func (s *Service) xTree(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	dir, err := queryPath(r)
	if err != nil {
		return err
	}
	entries, err := src.Tree(ctx, at, dir)
	if err != nil {
		return err
	}
	v := TreeView{Ref: at.Ref, SHA: at.SHA, Path: dir, Entries: entries}
	for _, e := range entries {
		if e.Type == "blob" && isReadme(e.Name) {
			if b, err := s.xBlob(ctx, src, at, e.Path); err == nil {
				v.Readme = b
			}
			break
		}
	}
	if list, _, err := src.Log(ctx, at, dir, 1, 1); err == nil && len(list) > 0 {
		v.LastCommit = &list[0]
	}
	httpx.OK(w, v)
	return nil
}

func (s *Service) xTreeCommits(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	dir, err := queryPath(r)
	if err != nil {
		return err
	}
	entries, err := src.Tree(ctx, at, dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	commits, err := src.TreeCommits(ctx, at, dir, names)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "commits": commits})
	return nil
}

func (s *Service) xGetBlob(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file to open.")
	}
	b, err := s.xBlob(ctx, src, at, p)
	if err != nil {
		return err
	}
	httpx.OK(w, b)
	return nil
}

func (s *Service) xFileBytes(r *http.Request, src RepoSource) (string, []byte, error) {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return "", nil, err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return "", nil, httpx.BadRequest("Name the file.")
	}
	_, data, err := src.File(ctx, at, p, s.maxCommit)
	if err != nil {
		return "", nil, err
	}
	if data == nil {
		return "", nil, httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "This file is too large to download here. Open it on the platform.")
	}
	return p, data, nil
}

func (s *Service) xRaw(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	p, data, err := s.xFileBytes(r, src)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(p)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
	return nil
}

func (s *Service) xDownload(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	p, data, err := s.xFileBytes(r, src)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"name": path.Base(p), "size": len(data), "content_base64": base64.StdEncoding.EncodeToString(data)})
	return nil
}

func (s *Service) xBlame(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil || p == "" {
		return httpx.BadRequest("Name the file.")
	}
	ranges, err := src.Blame(ctx, at, p)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "path": p, "ranges": ranges})
	return nil
}

func (s *Service) xPaths(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	paths, truncated, err := src.Paths(ctx, at)
	if err != nil {
		return err
	}
	hits := rankPaths(paths, strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))), maxFinderHits)
	if hits == nil {
		hits = []string{}
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "items": hits, "truncated": truncated})
	return nil
}

func (s *Service) xSearch(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 200 {
		return httpx.BadRequest("Search for 2 to 200 characters.")
	}
	at, err := src.Resolve(ctx, r.URL.Query().Get("ref"))
	if err != nil {
		return err
	}
	matches, truncated, err := src.SearchCode(ctx, q, at)
	if err != nil {
		return err
	}
	if matches == nil {
		matches = []GrepMatch{}
	}
	httpx.OK(w, map[string]any{"sha": at.SHA, "ref": at.Ref, "items": matches, "truncated": truncated})
	return nil
}

func (s *Service) xCommits(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	ctx := r.Context()
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	page := httpx.ParsePage(r)
	limit := min(page.Limit, maxLogPage)
	var c struct {
		SHA  string `json:"sha"`
		Page int    `json:"p"`
	}
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	at := resolved{Ref: c.SHA, SHA: c.SHA}
	if c.SHA == "" || !IsOID(c.SHA) {
		if at, err = src.Resolve(ctx, r.URL.Query().Get("ref")); err != nil {
			return err
		}
	}
	if c.Page < 1 {
		c.Page = 1
	}
	list, more, err := src.Log(ctx, at, p, c.Page, limit)
	if err != nil {
		return err
	}
	out := httpx.Page[Commit]{Items: list}
	if out.Items == nil {
		out.Items = []Commit{}
	}
	if more {
		out.NextCursor = httpx.EncodeCursor(map[string]any{"sha": at.SHA, "p": c.Page + 1})
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) xCommit(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	v, err := src.Commit(r.Context(), chi.URLParam(r, "sha"), p)
	if err != nil {
		return err
	}
	httpx.OK(w, v)
	return nil
}

func (s *Service) xChecks(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	sha := chi.URLParam(r, "sha")
	if !IsOID(sha) {
		at, err := src.Resolve(r.Context(), sha)
		if err != nil {
			return err
		}
		sha = at.SHA
	}
	c, err := src.Checks(r.Context(), sha)
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

// getChecks is the app's own repositories' answer: they have no CI.
func (s *Service) getChecks(w http.ResponseWriter, r *http.Request) error {
	if _, err := s.repoFromRoute(r, levelView); err != nil {
		return err
	}
	httpx.OK(w, Checks{SHA: chi.URLParam(r, "sha"), State: "none", Items: []Check{}})
	return nil
}

func (s *Service) xCompare(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	q := r.URL.Query()
	head := strings.TrimSpace(q.Get("head"))
	if head == "" {
		return httpx.BadRequest("Choose a branch to compare.")
	}
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	cmp, err := src.Compare(r.Context(), strings.TrimSpace(q.Get("base")), head, p)
	if err != nil {
		return err
	}
	httpx.OK(w, cmp)
	return nil
}

// ---- writing -------------------------------------------------------------------

// agentLine is the trailer agent commits carry, as on the app's own.
func (s *Service) agentLine(ctx context.Context, me *auth.Identity) string {
	agent, ok := auth.AgentFrom(ctx)
	if !ok {
		return ""
	}
	return "Made by " + agentName(agent) + " (AI agent) for " + s.person(me).Name + "."
}

func (s *Service) xCommitChanges(w http.ResponseWriter, r *http.Request, src RepoSource, rr *remoteRepo) error {
	me := auth.MustFromContext(r.Context())
	var in CommitInput
	body := http.MaxBytesReader(w, r.Body, s.maxCommit*4/3+(1<<20))
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("This commit is larger than %s.", formatBytes(s.maxCommit)))
		}
		if errors.Is(err, io.EOF) {
			return httpx.BadRequest("Describe the commit.")
		}
		return httpx.BadRequest("Invalid JSON: " + err.Error())
	}
	res, err := src.CommitChanges(r.Context(), remoteCommit{CommitInput: in, agent: s.agentLine(r.Context(), me)})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, res)
	return nil
}

func (s *Service) xCreateBranch(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	var in struct {
		Name string `json:"name"`
		From string `json:"from"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	ref, err := src.CreateBranch(r.Context(), strings.TrimSpace(in.Name), strings.TrimSpace(in.From))
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, ref)
	return nil
}

func (s *Service) xDeleteBranch(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	if err := src.DeleteBranch(r.Context(), strings.TrimSpace(r.URL.Query().Get("name"))); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- pull requests -------------------------------------------------------------

func (s *Service) xPulls(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	q := r.URL.Query()
	page := httpx.ParsePage(r)
	var c struct {
		P int `json:"p"`
	}
	if err := httpx.DecodeCursor(page.Cursor, &c); err != nil {
		return err
	}
	list, more, err := src.Pulls(r.Context(), pullQuery{State: q.Get("state"), Q: strings.TrimPrefix(strings.TrimSpace(q.Get("q")), "#"),
		Author: q.Get("author") == "me", ReviewRequired: q.Get("review_requested") == "me", Page: max(c.P, 1), Limit: page.Limit})
	if err != nil {
		return err
	}
	out := httpx.Page[XPull]{Items: list}
	if more {
		out.NextCursor = httpx.EncodeCursor(map[string]int{"p": max(c.P, 1) + 1})
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) xOpenPull(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	var in struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Head      string   `json:"head"`
		Base      string   `json:"base"`
		Reviewers []string `json:"reviewers"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	p, err := src.OpenPull(r.Context(), OpenPullInput{Title: in.Title, Body: in.Body, Head: strings.TrimSpace(in.Head), Base: strings.TrimSpace(in.Base)})
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func (s *Service) xPull(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	p, err := src.Pull(r.Context(), n)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) xUpdatePull(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in updatePullReq
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	p, err := src.UpdatePull(r.Context(), n, in)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) xSetOpen(open bool) remoteHandler {
	return func(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
		n, err := pullNumber(r)
		if err != nil {
			return err
		}
		p, err := src.SetPullOpen(r.Context(), n, open)
		if err != nil {
			return err
		}
		httpx.OK(w, p)
		return nil
	}
}

func (s *Service) xTimeline(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	items, err := src.Timeline(r.Context(), n)
	if err != nil {
		return err
	}
	if items == nil {
		items = []XTimelineItem{}
	}
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

func (s *Service) xPullCommits(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	list, err := src.PullCommits(r.Context(), n)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) xPullDiff(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	p, err := queryPath(r)
	if err != nil {
		return err
	}
	if c := strings.TrimSpace(r.URL.Query().Get("commit")); c != "" {
		v, err := src.Commit(r.Context(), c, p)
		if err != nil {
			return err
		}
		base := ""
		if len(v.Parents) > 0 {
			base = v.Parents[0]
		}
		httpx.OK(w, XPullDiff{Base: base, Head: v.SHA, Diff: v.Diff})
		return nil
	}
	d, err := src.PullDiff(r.Context(), n, p)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

func (s *Service) xThreads(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	list, head, err := src.Threads(r.Context(), n)
	if err != nil {
		return err
	}
	if list == nil {
		list = []XThread{}
	}
	httpx.OK(w, map[string]any{"items": list, "head_sha": head})
	return nil
}

func (s *Service) xMergeability(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	m, err := src.Mergeability(r.Context(), n)
	if err != nil {
		return err
	}
	httpx.OK(w, m)
	return nil
}

// xMerge merges as a person; agents never merge (no tool, and 403 here).
func (s *Service) xMerge(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	if _, ok := auth.AgentFrom(r.Context()); ok {
		return errAgentForbidden
	}
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in mergeReq
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	if in.Method != "" && in.Method != "merge" && in.Method != "squash" && in.Method != "rebase" {
		return httpx.BadRequest("method is merge, squash or rebase.")
	}
	p, err := src.Merge(r.Context(), n, in)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) xAddComment(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in xCommentInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	c, err := src.Comment(r.Context(), n, in)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, c)
	return nil
}

func (s *Service) xEditComment(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	c, err := src.EditComment(r.Context(), n, chi.URLParam(r, "commentID"), in.Body)
	if err != nil {
		return err
	}
	httpx.OK(w, c)
	return nil
}

func (s *Service) xDeleteComment(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	if err := src.DeleteComment(r.Context(), n, chi.URLParam(r, "commentID")); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) xResolve(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in struct {
		Resolved bool `json:"resolved"`
	}
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	if err := src.ResolveThread(r.Context(), n, chi.URLParam(r, "commentID"), in.Resolved); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (s *Service) xReview(w http.ResponseWriter, r *http.Request, src RepoSource, _ *remoteRepo) error {
	n, err := pullNumber(r)
	if err != nil {
		return err
	}
	var in xReviewInput
	if err := decodeJSON(r, &in); err != nil {
		return err
	}
	if err := src.Review(r.Context(), n, in); err != nil {
		return err
	}
	p, err := src.Pull(r.Context(), n)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// xPeople: reviewers on a platform are its own accounts, picked there.
func (s *Service) xPeople(w http.ResponseWriter, _ *http.Request, _ RepoSource, _ *remoteRepo) error {
	httpx.OK(w, map[string]any{"items": []any{}})
	return nil
}
