package repos

import (
	"context"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// Global search: repositories by name or description, and file paths in
// the default branch of the few repositories the caller changed most
// recently (path lists are cached per commit, so repeated searches are
// cheap). Both go through visibleRepos, the list routes' own query, so an
// agent searching as someone sees exactly their repositories within its
// scope.

const (
	searchPathRepos = 5     // repositories whose files are searched
	searchPathCache = 64    // path lists kept in memory
	searchPathMax   = 20000 // paths read per repository
)

type pathCacheEntry struct {
	paths []string
	at    time.Time
}

var (
	pathCacheMu sync.Mutex
	pathCache   = map[string]pathCacheEntry{} // "<repo id>:<commit>"
)

func (s *Service) cachedPaths(ctx context.Context, repoID uuid.UUID, sha string) ([]string, error) {
	key := repoID.String() + ":" + sha
	pathCacheMu.Lock()
	if e, ok := pathCache[key]; ok {
		pathCacheMu.Unlock()
		return e.paths, nil
	}
	pathCacheMu.Unlock()
	paths, _, err := s.git.ListPaths(ctx, repoID, sha, searchPathMax)
	if err != nil {
		return nil, err
	}
	pathCacheMu.Lock()
	if len(pathCache) >= searchPathCache {
		var oldest string
		var at time.Time
		for k, e := range pathCache {
			if oldest == "" || e.at.Before(at) {
				oldest, at = k, e.at
			}
		}
		delete(pathCache, oldest)
	}
	pathCache[key] = pathCacheEntry{paths: paths, at: time.Now()}
	pathCacheMu.Unlock()
	return paths, nil
}

// Search implements app.SearchProvider: repositories by name and file paths in
// the most recently changed ones, in the org the search is scoped to
// (app.SearchOrg) when it is. Context is the org (then the repository, for a
// file).
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	q := strings.TrimSpace(query)
	if s.git == nil || s.gitProblem != "" || len([]rune(q)) < 2 {
		return nil, nil
	}
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	repos, err := s.visibleRepos(ctx, caller.UserID, scope, q, nil, limit)
	if err != nil {
		return nil, err
	}
	var recent []RepoSummary
	if len(repos) < limit {
		// File paths in the most recently changed repositories.
		recent, _ = s.visibleRepos(ctx, caller.UserID, scope, "", nil, searchPathRepos)
	}
	orgs, err := s.repoOrgs(ctx, append(append([]RepoSummary{}, repos...), recent...))
	if err != nil {
		return nil, err
	}
	out := make([]app.SearchResult, 0, limit)
	lq := strings.ToLower(q)
	for _, r := range repos {
		score := 0.6
		if strings.EqualFold(r.Name, q) {
			score = 0.95
		} else if strings.HasPrefix(strings.ToLower(r.Name), lq) {
			score = 0.8
		}
		o := orgs[r.ID]
		out = append(out, app.SearchResult{Kind: RepoKind, ID: r.ID.String(), Title: r.Name, Snippet: r.Description,
			Link: "/repos/" + r.ID.String(), Icon: "git-branch", Score: score, At: r.UpdatedAt,
			OrgID: o.id, Context: app.SearchContext(o.name)})
	}
	if len(out) >= limit {
		return out, nil
	}
	for _, r := range recent {
		if len(out) >= limit {
			break
		}
		sha, err := s.git.RefValue(ctx, r.ID, "refs/heads/"+r.DefaultBranch)
		if err != nil || sha == "" {
			continue
		}
		paths, err := s.cachedPaths(ctx, r.ID, sha)
		if err != nil {
			continue
		}
		for _, p := range rankPaths(paths, lq, limit-len(out)) {
			if !strings.Contains(strings.ToLower(p), lq) {
				continue
			}
			score := 0.4
			if strings.Contains(strings.ToLower(path.Base(p)), lq) {
				score = 0.5
			}
			o := orgs[r.ID]
			out = append(out, app.SearchResult{Kind: "repos.file", ID: r.ID.String() + ":" + p, Title: path.Base(p), Snippet: p,
				Link: "/repos/" + r.ID.String() + "/blob?path=" + url.QueryEscape(p), Icon: "file-code", Score: score, At: r.UpdatedAt,
				OrgID: o.id, Context: app.SearchContext(o.name, r.Name)})
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

type repoOrg struct{ id, name string }

// repoOrgs is each repository's org (id and name), for search context.
func (s *Service) repoOrgs(ctx context.Context, repos []RepoSummary) (map[uuid.UUID]repoOrg, error) {
	out := map[uuid.UUID]repoOrg{}
	if len(repos) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(repos))
	for i, r := range repos {
		ids[i] = r.ID
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT r.id, o.id, o.name FROM repos_repos r JOIN orgs o ON o.id = r.org_id WHERE r.id = ANY ($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, orgID uuid.UUID
		var name string
		if err := rows.Scan(&id, &orgID, &name); err != nil {
			return nil, err
		}
		out[id] = repoOrg{orgID.String(), name}
	}
	return out, rows.Err()
}
