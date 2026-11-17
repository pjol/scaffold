package repos

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Commit statuses. CI posts the state of a job (context) for a commit with a
// per-repository CI token that a manager makes in the repository's settings
// (shown once, sha256 kept, revocable). The token opens one route,
// POST /v1/repos/{repoID}/statuses/{sha}; session tokens, git credentials
// and agent credentials are refused there, and CI tokens open nothing
// else. Statuses show on commits and pull requests; branch protection can
// require contexts to succeed before merging.

const (
	ciPrefix       = "scaffoldapp_ci_"
	maxCITokens    = 10
	maxContextsSHA = 100
)

// CIToken is a CI token's metadata (never the secret).
type CIToken struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (s *Service) listCITokens(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.webhookRepo(r) // manage, people only
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `SELECT id, name, prefix, last_used_at, created_at FROM repos_ci_tokens
		WHERE repo_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC, id DESC`, repo.ID)
	if err != nil {
		return err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[CIToken])
	if err != nil {
		return err
	}
	if list == nil {
		list = []CIToken{}
	}
	httpx.OK(w, map[string]any{"items": list, "status_url": s.publicAPI + "/v1/repos/" + repo.ID.String() + "/statuses/{sha}"})
	return nil
}

func (s *Service) createCIToken(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	repo, err := s.webhookRepo(r)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name := strings.Join(strings.Fields(req.Name), " ")
	if n := utf8.RuneCountInString(name); n == 0 || n > 60 {
		return httpx.BadRequest("Name the token after the CI that uses it (up to 60 characters).")
	}
	var n int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM repos_ci_tokens WHERE repo_id = $1 AND revoked_at IS NULL`, repo.ID).Scan(&n); err != nil {
		return err
	}
	if n >= maxCITokens {
		return httpx.Conflict("A repository holds up to 10 CI tokens. Revoke one first.")
	}
	secret, hash, prefix := newSecret(ciPrefix)
	var t CIToken
	if err := s.d.Pool.QueryRow(r.Context(), `INSERT INTO repos_ci_tokens (repo_id, name, prefix, secret_hash, created_by) VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, prefix, last_used_at, created_at`, repo.ID, name, prefix, hash, me.UserID).Scan(&t.ID, &t.Name, &t.Prefix, &t.LastUsedAt, &t.CreatedAt); err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret})
	return nil
}

func (s *Service) revokeCIToken(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.webhookRepo(r)
	if err != nil {
		return err
	}
	id, err := httpx.URLParamUUID(r, "tokenID")
	if err != nil {
		return httpx.NotFound("Token not found.")
	}
	tag, err := s.d.Pool.Exec(r.Context(), `UPDATE repos_ci_tokens SET revoked_at = now() WHERE id = $1 AND repo_id = $2 AND revoked_at IS NULL`, id, repo.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Token not found.")
	}
	httpx.NoContent(w)
	return nil
}

// ---- posting -------------------------------------------------------------------

// Status is a commit status.
type Status struct {
	Context     string    `json:"context"`
	State       string    `json:"state"` // pending | success | failure | error
	Description string    `json:"description"`
	TargetURL   *string   `json:"target_url"`
	Creator     string    `json:"creator"` // the CI token's name
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type statusReq struct {
	State       string  `json:"state"`
	Context     string  `json:"context"`
	Description string  `json:"description"`
	TargetURL   *string `json:"target_url"`
}

func ciFail(w http.ResponseWriter, r *http.Request, status int, code, msg string) error {
	httpx.Fail(w, r, httpx.NewError(status, code, msg))
	return nil
}

// postStatus is POST /v1/repos/{repoID}/statuses/{sha} (public group; the
// CI token in Authorization: Bearer is the only way in).
func (s *Service) postStatus(w http.ResponseWriter, r *http.Request) error {
	if s.git == nil || s.gitProblem != "" {
		return httpx.NewError(http.StatusServiceUnavailable, "git_unavailable", "Repositories are unavailable on this server.")
	}
	h := r.Header.Get("Authorization")
	token := ""
	if v, ok := strings.CutPrefix(h, "Bearer "); ok {
		token = strings.TrimSpace(v)
	} else if v, ok := strings.CutPrefix(h, "token "); ok {
		token = strings.TrimSpace(v)
	}
	switch {
	case token == "":
		return ciFail(w, r, http.StatusUnauthorized, "unauthorized", "Send a CI token from the repository's settings as Authorization: Bearer <token>.")
	case auth.IsAgentCredential(token):
		return ciFail(w, r, http.StatusUnauthorized, "agent_credential", "Agent credentials only work at the agent endpoint.")
	case strings.HasPrefix(token, credPrefix):
		return ciFail(w, r, http.StatusForbidden, "wrong_credential", "Git credentials clone and push. CI posts statuses with a CI token from the repository's settings.")
	case !strings.HasPrefix(token, ciPrefix) || len(token) != len(ciPrefix)+credSecretLength:
		return ciFail(w, r, http.StatusUnauthorized, "unauthorized", "That isn't a CI token. Make one in the repository's settings → CI tokens.")
	}
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		return errRepoNotFound
	}
	var tokenID uuid.UUID
	var tokenName string
	err = s.d.Pool.QueryRow(r.Context(), `SELECT t.id, t.name FROM repos_ci_tokens t JOIN files_items i ON i.id = t.repo_id
		WHERE t.secret_hash = $1 AND t.repo_id = $2 AND t.revoked_at IS NULL AND i.trashed_at IS NULL`, hashCredential(token), repoID).Scan(&tokenID, &tokenName)
	if db.IsNoRows(err) {
		return ciFail(w, r, http.StatusUnauthorized, "unauthorized", "This CI token was revoked or belongs to another repository.")
	}
	if err != nil {
		return err
	}
	repo, err := s.loadRepo(r.Context(), s.d.Pool, repoID)
	if err != nil {
		return err
	}
	sha := strings.ToLower(chi.URLParam(r, "sha"))
	if !IsOID(sha) {
		return httpx.BadRequest("Use the commit's full id (40 hex characters).")
	}
	if typ, err := s.git.objectType(r.Context(), repo.ID, sha); err != nil || typ != "commit" {
		return httpx.NotFound("There's no commit " + sha[:12] + " in this repository. Push it first.")
	}
	var req statusReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	st, err := cleanStatus(req)
	if err != nil {
		return err
	}
	var n int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM repos_statuses WHERE repo_id = $1 AND sha = $2 AND context <> $3`, repo.ID, sha, st.Context).Scan(&n); err != nil {
		return err
	}
	if n >= maxContextsSHA {
		return httpx.Conflict("A commit holds up to 100 status contexts.")
	}
	err = s.d.Pool.QueryRow(r.Context(), `
		INSERT INTO repos_statuses (repo_id, sha, context, state, description, target_url, token_id) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT ON CONSTRAINT repos_statuses_key DO UPDATE SET state = EXCLUDED.state, description = EXCLUDED.description,
		  target_url = EXCLUDED.target_url, token_id = EXCLUDED.token_id, updated_at = now()
		RETURNING created_at, updated_at`, repo.ID, sha, st.Context, st.State, st.Description, st.TargetURL, tokenID).Scan(&st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		return err
	}
	st.Creator = tokenName
	_, _ = s.d.Pool.Exec(r.Context(), `UPDATE repos_ci_tokens SET last_used_at = now() WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, tokenID)
	s.statusChanged(context.WithoutCancel(r.Context()), repo, sha, st)
	httpx.JSON(w, http.StatusCreated, st)
	return nil
}

func cleanStatus(req statusReq) (*Status, error) {
	switch req.State {
	case "pending", "success", "failure", "error":
	default:
		return nil, httpx.BadRequest("state is pending, success, failure or error.")
	}
	ctxName := strings.TrimSpace(req.Context)
	if ctxName == "" {
		ctxName = "default"
	}
	if utf8.RuneCountInString(ctxName) > 100 {
		return nil, httpx.BadRequest("Keep context under 100 characters.")
	}
	desc := truncateUTF8(strings.Join(strings.Fields(req.Description), " "), 140)
	st := &Status{Context: ctxName, State: req.State, Description: desc}
	if req.TargetURL != nil && strings.TrimSpace(*req.TargetURL) != "" {
		u, err := url.Parse(strings.TrimSpace(*req.TargetURL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(*req.TargetURL) > 2000 {
			return nil, httpx.BadRequest("target_url is an http(s) link to the job (up to 2,000 characters).")
		}
		v := u.String()
		st.TargetURL = &v
	}
	return st, nil
}

// statusChanged refreshes open pull requests at this head and runs auto-merge.
func (s *Service) statusChanged(ctx context.Context, repo *Repo, sha string, st *Status) {
	rows, err := s.d.Pool.Query(ctx, `SELECT number FROM repos_pulls WHERE repo_id = $1 AND head_sha = $2 AND state = 'open'`, repo.ID, sha)
	if err == nil {
		nums, _ := pgx.CollectRows(rows, pgx.RowTo[int])
		for _, n := range nums {
			s.pullChanged(ctx, repo.ID, n)
			s.tryAutoMerge(ctx, repo, n)
		}
	}
	s.publish(ctx, repo.ID, EventStatusChanged, map[string]any{"sha": sha})
	s.emitWebhook(ctx, repo, nil, "status", st.State, map[string]any{"sha": sha, "context": st.Context, "state": st.State,
		"description": st.Description, "target_url": st.TargetURL})
}

// ---- reading -------------------------------------------------------------------

// statusesFor reads the latest status of each context on a commit.
func (s *Service) statusesFor(ctx context.Context, repoID uuid.UUID, sha string) ([]Status, error) {
	rows, err := s.d.Pool.Query(ctx, `SELECT st.context, st.state, st.description, st.target_url, COALESCE(t.name, 'Deleted token'), st.created_at, st.updated_at
		FROM repos_statuses st LEFT JOIN repos_ci_tokens t ON t.id = st.token_id
		WHERE st.repo_id = $1 AND st.sha = $2 ORDER BY lower(st.context)`, repoID, sha)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Status])
	if list == nil {
		list = []Status{}
	}
	return list, err
}

// combinedFor is the overall state of each commit (for commit lists).
func (s *Service) combinedFor(ctx context.Context, repoID uuid.UUID, shas []string) (map[string]string, error) {
	out := map[string]string{}
	if len(shas) == 0 {
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT sha, array_agg(state) FROM repos_statuses WHERE repo_id = $1 AND sha = ANY($2) GROUP BY sha`, repoID, shas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sha string
		var states []string
		if err := rows.Scan(&sha, &states); err != nil {
			return nil, err
		}
		out[sha] = CombinedState(states)
	}
	return out, rows.Err()
}

// statusChecks maps statuses onto the Checks shape the screens use for
// connected platforms too, marking required contexts and listing required
// ones never reported.
func statusChecks(sha string, list []Status, required []string) Checks {
	c := Checks{SHA: sha, Items: []Check{}}
	req := map[string]bool{}
	for _, r := range required {
		req[r] = true
	}
	seen := map[string]bool{}
	var states []string
	for _, st := range list {
		status := map[string]string{"pending": "in_progress", "success": "success", "failure": "failure", "error": "failure"}[st.State]
		u := ""
		if st.TargetURL != nil {
			u = *st.TargetURL
		}
		c.Items = append(c.Items, Check{Name: st.Context, Status: status, Description: st.Description, URL: u, Required: req[st.Context]})
		states = append(states, st.State)
		seen[st.Context] = true
	}
	for _, r := range required {
		if !seen[r] {
			c.Items = append(c.Items, Check{Name: r, Status: "queued", Description: "Expected: required before merging, not reported yet.", Required: true})
			states = append(states, "pending")
		}
	}
	c.State = CombinedState(states)
	if c.State == "" {
		c.State = "none"
	}
	return c
}

// getChecks is GET /commits/{sha}/checks for the app's own repositories:
// the commit's statuses, with the required contexts of branch (optional
// ?branch=, the rule a pull request's base follows).
func (s *Service) getChecks(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	sha := strings.ToLower(chi.URLParam(r, "sha"))
	if !IsOID(sha) {
		resolved, err := s.git.ResolveCommit(r.Context(), repo.ID, sha)
		if err != nil {
			return httpx.NotFound("There's no commit " + sha + ".")
		}
		sha = resolved
	}
	list, err := s.statusesFor(r.Context(), repo.ID, sha)
	if err != nil {
		return err
	}
	var required []string
	if b := r.URL.Query().Get("branch"); b != "" {
		rules, err := s.protections(r.Context(), s.d.Pool, repo.ID)
		if err != nil {
			return err
		}
		if rule := protectionFor(rules, b); rule != nil {
			required = rule.RequiredChecks
		}
	}
	httpx.OK(w, statusChecks(sha, list, required))
	return nil
}

// newSecret makes prefix + 40 base62 characters, its sha256 and the shown prefix.
func newSecret(prefix string) (secret, hash, shown string) {
	secret, _, _ = newCredentialSecret()
	secret = prefix + strings.TrimPrefix(secret, credPrefix)
	return secret, hashCredential(secret), secret[:len(prefix)+credShownChars]
}

// commitStates is GET /repos/{repoID}/commit-states?shas=a,b: the combined
// status of each commit (commit lists), up to 100.
func (s *Service) commitStates(w http.ResponseWriter, r *http.Request) error {
	repo, err := s.repoFromRoute(r, levelView)
	if err != nil {
		return err
	}
	var shas []string
	for _, v := range strings.Split(r.URL.Query().Get("shas"), ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); IsOID(v) && len(shas) < 100 {
			shas = append(shas, v)
		}
	}
	states, err := s.combinedFor(r.Context(), repo.ID, shas)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"states": states})
	return nil
}
