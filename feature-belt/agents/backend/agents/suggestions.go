package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notifications"
	"scaffoldapp/backend/internal/notify"
)

// Suggestion statuses. accepting is held while an accepted tool runs.
const (
	StatusPending   = "pending"
	StatusAccepting = "accepting"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusFailed    = "failed"
	StatusExpired   = "expired"
)

// AgentRef names the agent behind a suggestion.
type AgentRef struct {
	Via       string `json:"via"` // mcp | assistant
	Client    string `json:"client"`
	ClientKey string `json:"client_key"`
}

// Target is the one resource a suggested write acts on.
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// SuggestionError is why accepting failed (or the suggestion expired).
type SuggestionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Suggestion is a write an agent left for the user to accept or reject.
type Suggestion struct {
	ID           uuid.UUID        `json:"id"`
	Status       string           `json:"status"`
	Tool         string           `json:"tool"`
	ToolTitle    string           `json:"tool_title"`
	Feature      string           `json:"feature"`
	FeatureLabel string           `json:"feature_label"`
	Source       string           `json:"source"` // outside service ("Linear"); "" for the app's own
	Summary      string           `json:"summary"`
	Before       string           `json:"before"`
	After        string           `json:"after"`
	Link         string           `json:"link"`
	Target       *Target          `json:"target"`
	Input        json.RawMessage  `json:"input"`
	Agent        AgentRef         `json:"agent"`
	Result       json.RawMessage  `json:"result"`
	Error        *SuggestionError `json:"error"`
	CreatedAt    time.Time        `json:"created_at"`
	ExpiresAt    time.Time        `json:"expires_at"`
	DecidedAt    *time.Time       `json:"decided_at"`
}

// suggestionCols reads a row; pending past its expiry reads as expired.
const suggestionCols = `id,
	CASE WHEN status = 'pending' AND expires_at <= now() THEN 'expired' ELSE status END,
	tool, feature, source, summary, before_text, after_text, link, target_kind, target_id, input,
	via, client, client_key, result, error_code, error_message, created_at, expires_at, decided_at`

func scanSuggestion(row pgx.Row) (*Suggestion, error) {
	var (
		sg             Suggestion
		kind, id       string
		input, result  []byte
		errCode, errMs string
	)
	if err := row.Scan(&sg.ID, &sg.Status, &sg.Tool, &sg.Feature, &sg.Source, &sg.Summary, &sg.Before, &sg.After, &sg.Link,
		&kind, &id, &input, &sg.Agent.Via, &sg.Agent.Client, &sg.Agent.ClientKey, &result, &errCode, &errMs,
		&sg.CreatedAt, &sg.ExpiresAt, &sg.DecidedAt); err != nil {
		return nil, err
	}
	sg.Input = json.RawMessage(input)
	if len(result) > 0 {
		sg.Result = json.RawMessage(result)
	}
	if kind != "" {
		sg.Target = &Target{Kind: kind, ID: id}
	}
	if errCode != "" {
		sg.Error = &SuggestionError{Code: errCode, Message: errMs}
	}
	if sg.Status == StatusExpired && sg.Error == nil {
		sg.Error = &SuggestionError{Code: "suggestion_expired", Message: "This suggestion expired before anyone accepted it."}
	}
	sg.ToolTitle = toolTitle(sg.Feature, sg.Tool)
	sg.FeatureLabel = featureLabel(sg.Feature)
	if sg.Source != "" {
		sg.FeatureLabel = sg.Source
	}
	return &sg, nil
}

func (s *Service) loadSuggestion(ctx context.Context, q db.Querier, userID, id uuid.UUID) (*Suggestion, error) {
	sg, err := scanSuggestion(q.QueryRow(ctx, `SELECT `+suggestionCols+` FROM agents_suggestions WHERE id = $1 AND user_id = $2`, id, userID))
	if db.IsNoRows(err) {
		return nil, errNotFound("Suggestion")
	}
	return sg, err
}

// ---- creating (from GateTool) ------------------------------------------------

// suggest stores a write the agent may only suggest, notifies the user and
// tells the agent the suggestion's id. The preview runs as the user with the
// agent marker (so it sees what the agent may read); targets the user can
// only view are refused here, since no write of theirs could succeed.
func (s *Service) suggest(ctx context.Context, caller *auth.Identity, agent auth.Agent, feature string, t app.Tool, kind, id string, input json.RawMessage) (app.ToolDecision, error) {
	input = bytes.TrimSpace(input)
	if len(input) == 0 || string(input) == "null" {
		input = json.RawMessage(`{}`)
	}
	if len(input) > maxInputBytes {
		return refuse("suggestion_too_large", "This change is too large to suggest. Split it into smaller changes."), nil
	}
	if !json.Valid(input) || input[0] != '{' {
		return refuse("invalid_input", "Arguments must be a JSON object."), nil
	}

	link := ""
	if k, ok := access.KindOf(kind); ok {
		rid, err := uuid.Parse(id)
		if err != nil {
			return refuse("not_found", "Not found."), nil
		}
		var lvl int
		if err := s.d.Pool.QueryRow(ctx, `SELECT access_level($1, $2, $3, false)`, caller.UserID, kind, rid).Scan(&lvl); err != nil {
			return app.ToolDecision{}, err
		}
		switch {
		case lvl < int(access.View):
			return refuse("not_found", capitalize(k.Label)+" not found."), nil
		case lvl < int(access.Comment):
			return refuse("access_level_required", fmt.Sprintf("The person can only view this %s, so changes to it can't be suggested.", k.Label)), nil
		}
		link = k.Link(rid)
	}

	pv, err := s.preview(ctx, caller, feature, t, input)
	if err != nil {
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) && apiErr.Status < 500 {
			return refuse(apiErr.Code, apiErr.Message), nil
		}
		return app.ToolDecision{}, err
	}
	if pv.Link == "" {
		pv.Link = link
	}

	var sg *Suggestion
	var limitMsg string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		// One creator at a time per user, so the caps hold under concurrency.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('agents.suggest:' || $1::text, 0))`, caller.UserID); err != nil {
			return err
		}
		var pending, lastHour int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status IN ('pending', 'accepting') AND expires_at > now()),
			       count(*) FILTER (WHERE created_at > now() - interval '1 hour')
			FROM agents_suggestions WHERE user_id = $1 AND created_at > now() - $2::interval`,
			caller.UserID, fmt.Sprintf("%d seconds", int(s.ttl.Seconds()))).Scan(&pending, &lastHour); err != nil {
			return err
		}
		switch {
		case pending >= s.maxPending:
			limitMsg = fmt.Sprintf("The person has %d suggestions waiting. Wait for them to review those before suggesting more.", pending)
			return nil
		case lastHour >= s.perHour:
			limitMsg = "Too many suggestions in the last hour. Try again later."
			return nil
		}
		var err error
		sg, err = scanSuggestion(tx.QueryRow(ctx, `
			INSERT INTO agents_suggestions (user_id, via, client_key, client, tool, feature, source, input,
			       target_kind, target_id, summary, before_text, after_text, link, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now() + $15::interval)
			RETURNING `+suggestionCols,
			caller.UserID, clientVia(agent), clientKey(agent), clientName(agent), t.Name, feature, truncate(strings.TrimSpace(t.Source), 60),
			[]byte(input), kind, id, pv.Summary, pv.Before, pv.After, pv.Link, fmt.Sprintf("%d seconds", int(s.ttl.Seconds()))))
		return err
	})
	if err != nil {
		return app.ToolDecision{}, err
	}
	if limitMsg != "" {
		return refuse(CodeLimit, limitMsg), nil
	}
	s.announce(ctx, caller.UserID, sg)
	return app.ToolDecision{Action: app.ToolSuggested, SuggestionID: sg.ID.String()}, nil
}

// preview runs the tool's Preview (bounded, recovered) and cleans it up; a
// tool without one is described by its feature and name.
func (s *Service) preview(ctx context.Context, caller *auth.Identity, feature string, t app.Tool, input json.RawMessage) (pv app.ToolPreview, err error) {
	fallback := featureLabel(feature)
	if t.Source != "" {
		fallback = truncate(t.Source, 60)
	}
	fallback += ": " + toolTitle(feature, t.Name)
	if t.Preview != nil {
		pctx, cancel := context.WithTimeout(ctx, previewTimeout)
		defer cancel()
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.ErrorContext(ctx, "agents: tool preview panicked", "tool", t.Name, "panic", fmt.Sprint(r))
					err = errors.New("tool preview panicked")
				}
			}()
			pv, err = t.Preview(pctx, caller, input)
		}()
		if err != nil {
			return app.ToolPreview{}, err
		}
	}
	pv.Summary = truncate(strings.Join(strings.Fields(pv.Summary), " "), 300)
	if pv.Summary == "" {
		pv.Summary = fallback
	}
	pv.Before, pv.After = truncate(pv.Before, 20000), truncate(pv.After, 20000)
	if !appPath(pv.Link) {
		pv.Link = ""
	}
	return pv, nil
}

// announce tells the user: a platform notification (inbox, push, email per
// their preferences; bursts push once, see foldPushes) and the live event.
func (s *Service) announce(ctx context.Context, userID uuid.UUID, sg *Suggestion) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	s.publish(ctx, userID, EventSuggestion, map[string]any{"suggestion": sg})
	if s.d.Notify == nil {
		return
	}
	body := sg.FeatureLabel
	if sg.Before != "" || sg.After != "" {
		body += " · Review the change before it's made."
	}
	n := notify.Notification{
		UserID: userID,
		Kind:   KindSuggestion,
		Title:  truncate(sg.Agent.Client+" suggests: "+sg.Summary, 200),
		Body:   body,
		Link:   "/agents/suggestions/" + sg.ID.String(),
		Data:   map[string]any{"suggestion_id": sg.ID.String(), "via": sg.Agent.Via, "client_key": sg.Agent.ClientKey, "client": sg.Agent.Client},
	}
	if err := s.d.Notify.Send(ctx, n); err != nil {
		slog.WarnContext(ctx, "agents: notify failed", "suggestion_id", sg.ID, "err", err)
	}
}

// foldPushes is a notifications delivery filter: the first suggestion from
// an agent pushes; more from it within pushFoldWindow land in the inbox and
// arrive live, without another push or email.
func (s *Service) foldPushes(ctx context.Context, n notify.Notification) (notifications.Hold, error) {
	if n.Kind != KindSuggestion {
		return notifications.Hold{}, nil
	}
	via, _ := n.Data["via"].(string)
	key, _ := n.Data["client_key"].(string)
	client, _ := n.Data["client"].(string)
	if via == "" || key == "" {
		return notifications.Hold{}, nil
	}
	if client == "" {
		client = "AI agent"
	}
	var claimed bool
	err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO agents_clients (user_id, via, client_key, client, last_pushed_at) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_id, via, client_key) DO UPDATE SET last_pushed_at = now()
		WHERE agents_clients.last_pushed_at IS NULL OR agents_clients.last_pushed_at < now() - make_interval(secs => $5)
		RETURNING true`, n.UserID, via, key, client, pushFoldWindow.Seconds()).Scan(&claimed)
	if db.IsNoRows(err) {
		return notifications.Hold{Push: true, Email: true}, nil
	}
	return notifications.Hold{}, err
}

// ---- reading -------------------------------------------------------------------

var validStatus = map[string]bool{StatusPending: true, StatusAccepted: true, StatusRejected: true, StatusFailed: true, StatusExpired: true}

// listSuggestions: newest first; ?status=pending (also in progress),
// accepted, rejected, failed, expired, comma-separated; ?via=&client_key=
// for one agent.
func (s *Service) listSuggestions(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var cur cursor
	if err := httpx.DecodeCursor(p.Cursor, &cur); err != nil {
		return err
	}
	var statuses []string
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		for _, st := range strings.Split(raw, ",") {
			st = strings.TrimSpace(st)
			if !validStatus[st] {
				return httpx.BadRequest("status must be pending, accepted, rejected, failed or expired.")
			}
			statuses = append(statuses, st)
			if st == StatusPending {
				statuses = append(statuses, StatusAccepting)
			}
		}
	}
	via, key := r.URL.Query().Get("via"), r.URL.Query().Get("client_key")
	var after *time.Time
	if !cur.T.IsZero() {
		after = &cur.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+suggestionCols+` FROM agents_suggestions
		WHERE user_id = $1
		  AND ($2::text[] IS NULL
		       OR (status = ANY($2) AND NOT (status = 'pending' AND expires_at <= now()))
		       OR ('expired' = ANY($2) AND status = 'pending' AND expires_at <= now()))
		  AND ($3 = '' OR (via = $3 AND client_key = $4))
		  AND ($5::timestamptz IS NULL OR (created_at, id) < ($5, $6))
		ORDER BY created_at DESC, id DESC LIMIT $7`,
		me.UserID, statuses, via, key, after, cur.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list, err := collectSuggestions(rows)
	if err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(sg Suggestion) any { return cursor{T: sg.CreatedAt, ID: sg.ID} }))
	return nil
}

func collectSuggestions(rows pgx.Rows) ([]Suggestion, error) {
	defer rows.Close()
	out := []Suggestion{}
	for rows.Next() {
		sg, err := scanSuggestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sg)
	}
	return out, rows.Err()
}

func (s *Service) getSuggestion(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errNotFound("Suggestion")
	}
	sg, err := s.loadSuggestion(r.Context(), s.d.Pool, me.UserID, id)
	if err != nil {
		return err
	}
	httpx.OK(w, sg)
	return nil
}

// Summary is the small state the clients keep warm for badges.
type Summary struct {
	Pending int  `json:"pending"`
	Paused  bool `json:"paused"`
}

func (s *Service) summary(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	out := Summary{}
	if err := s.d.Pool.QueryRow(r.Context(), `
		SELECT (SELECT count(*) FROM agents_suggestions WHERE user_id = $1 AND status IN ('pending', 'accepting') AND expires_at > now()),
		       COALESCE((SELECT paused OR staff_paused_by IS NOT NULL FROM agents_settings WHERE user_id = $1), false)`,
		me.UserID).Scan(&out.Pending, &out.Paused); err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- deciding ------------------------------------------------------------------

var (
	errExpired = httpx.NewError(http.StatusConflict, "suggestion_expired", "This suggestion expired. Ask your agent again if you still want the change.")
)

func errDecided(status string) *httpx.Error {
	return httpx.NewError(http.StatusConflict, "suggestion_decided", "This suggestion was already "+status+".")
}

func (s *Service) acceptRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errNotFound("Suggestion")
	}
	var in struct {
		Force bool `json:"force"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	sg, err := s.accept(r.Context(), me, id, in.Force)
	if err != nil {
		return err
	}
	httpx.OK(w, sg)
	return nil
}

// accept re-runs the suggested tool as the user, without the agent marker,
// so their permissions now apply. Failures are recorded on the suggestion
// and returned with it (status failed): the tool is gone, the target was
// deleted, the user lost access. If the target changed since the agent
// suggested the change (its preview's "before" differs), it answers 409
// target_changed with the fresh preview unless force is set.
func (s *Service) accept(ctx context.Context, me *auth.Identity, id uuid.UUID, force bool) (*Suggestion, error) {
	if _, ok := auth.AgentFrom(ctx); ok {
		return nil, errAgentForbidden
	}
	sg, err := s.loadSuggestion(ctx, s.d.Pool, me.UserID, id)
	if err != nil {
		return nil, err
	}
	switch sg.Status {
	case StatusPending:
	case StatusExpired:
		return nil, errExpired
	case StatusAccepting:
		return nil, httpx.NewError(http.StatusConflict, "suggestion_running", "This suggestion is being applied. Check again in a moment.")
	default:
		return nil, errDecided(sg.Status)
	}

	t, ok := s.findTool(ctx, me, sg.Tool)
	if !ok || t.feature != sg.Feature {
		return s.finish(ctx, me.UserID, sg.ID, nil, &SuggestionError{Code: "tool_unavailable",
			Message: "This action isn't available anymore: the feature was removed or the service disconnected."})
	}
	if t.ReadOnly {
		return s.finish(ctx, me.UserID, sg.ID, nil, &SuggestionError{Code: "tool_unavailable", Message: "This action changed and can't be applied."})
	}

	// Has the target changed since? Compare the state the agent saw.
	if !force && t.Preview != nil && sg.Before != "" {
		pv, err := s.preview(ctx, me, sg.Feature, t.Tool, sg.Input)
		if err != nil {
			var apiErr *httpx.Error
			if errors.As(err, &apiErr) && apiErr.Status < 500 {
				return s.finish(ctx, me.UserID, sg.ID, nil, &SuggestionError{Code: apiErr.Code, Message: apiErr.Message})
			}
			return nil, err
		}
		if pv.Before != sg.Before {
			e := httpx.NewError(http.StatusConflict, "target_changed", "This changed since your agent suggested it. Review the new version before accepting.")
			e.Details = map[string]any{"summary": pv.Summary, "before": pv.Before, "after": pv.After}
			return nil, e
		}
	}

	// Claim it: a double click or a second device runs the tool once.
	tag, err := s.d.Pool.Exec(ctx, `
		UPDATE agents_suggestions SET status = 'accepting', updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'pending' AND expires_at > now()`, sg.ID, me.UserID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		cur, err := s.loadSuggestion(ctx, s.d.Pool, me.UserID, sg.ID)
		if err != nil {
			return nil, err
		}
		if cur.Status == StatusExpired {
			return nil, errExpired
		}
		return nil, errDecided(cur.Status)
	}

	// The tool runs to the end even if the client goes away.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolTimeout)
	defer cancel()
	out, runErr := runTool(rctx, t.Tool, me, sg.Input)
	if runErr != nil {
		return s.finish(ctx, me.UserID, sg.ID, nil, toolError(rctx, sg.Tool, runErr))
	}
	return s.finish(ctx, me.UserID, sg.ID, out, nil)
}

func runTool(ctx context.Context, t app.Tool, caller *auth.Identity, input json.RawMessage) (out any, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "agents: tool panicked", "tool", t.Name, "panic", fmt.Sprint(r))
			err = errors.New("tool panicked")
		}
	}()
	return t.Handler(ctx, caller, input)
}

func toolError(ctx context.Context, tool string, err error) *SuggestionError {
	var apiErr *httpx.Error
	switch {
	case errors.As(err, &apiErr):
		return &SuggestionError{Code: apiErr.Code, Message: apiErr.Message}
	case errors.Is(err, context.DeadlineExceeded):
		return &SuggestionError{Code: "timeout", Message: "This took too long and may not have finished. Check the item before trying again."}
	}
	slog.ErrorContext(ctx, "agents: accepted tool failed", "tool", tool, "err", err)
	return &SuggestionError{Code: "internal", Message: "Something went wrong applying this change. Try again later."}
}

// finish records the outcome (accepted with the tool's output, or failed)
// and tells the user's other devices.
func (s *Service) finish(ctx context.Context, userID, id uuid.UUID, out any, failure *SuggestionError) (*Suggestion, error) {
	ctx = context.WithoutCancel(ctx)
	status, code, msg := StatusAccepted, "", ""
	var result []byte
	if failure != nil {
		status, code, msg = StatusFailed, failure.Code, truncate(failure.Message, 500)
	} else if out != nil {
		if b, err := json.Marshal(out); err == nil && len(b) <= maxResultBytes {
			result = b
		}
	}
	sg, err := scanSuggestion(s.d.Pool.QueryRow(ctx, `
		UPDATE agents_suggestions SET status = $3, result = $4, error_code = $5, error_message = $6,
		       decided_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status IN ('pending', 'accepting')
		RETURNING `+suggestionCols, id, userID, status, result, code, msg))
	if db.IsNoRows(err) {
		return s.loadSuggestion(ctx, s.d.Pool, userID, id)
	}
	if err != nil {
		return nil, err
	}
	s.publish(ctx, userID, EventSuggestionUpdated, map[string]any{"suggestion": sg})
	return sg, nil
}

func (s *Service) rejectRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errNotFound("Suggestion")
	}
	sg, err := s.reject(r.Context(), me.UserID, id)
	if err != nil {
		return err
	}
	httpx.OK(w, sg)
	return nil
}

func (s *Service) reject(ctx context.Context, userID, id uuid.UUID) (*Suggestion, error) {
	sg, err := scanSuggestion(s.d.Pool.QueryRow(ctx, `
		UPDATE agents_suggestions SET status = 'rejected', decided_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'pending' AND expires_at > now()
		RETURNING `+suggestionCols, id, userID))
	if db.IsNoRows(err) {
		cur, err := s.loadSuggestion(ctx, s.d.Pool, userID, id)
		if err != nil {
			return nil, err
		}
		if cur.Status == StatusExpired {
			return nil, errExpired
		}
		return nil, errDecided(cur.Status)
	}
	if err != nil {
		return nil, err
	}
	s.publish(ctx, userID, EventSuggestionUpdated, map[string]any{"suggestion": sg})
	return sg, nil
}

// BulkResult is what one "accept all / reject all from this agent" did.
type BulkResult struct {
	Accepted int          `json:"accepted"`
	Rejected int          `json:"rejected"`
	Failed   int          `json:"failed"`
	Skipped  int          `json:"skipped"` // changed since suggested: left pending for review
	Items    []Suggestion `json:"items"`
}

// bulkRoute accepts or rejects up to 50 pending suggestions from one agent,
// oldest first. Accepting never forces: a changed target stays pending.
func (s *Service) bulkRoute(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var in struct {
		Action    string `json:"action"`
		Via       string `json:"via"`
		ClientKey string `json:"client_key"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.Action != "accept" && in.Action != "reject" {
		return httpx.BadRequest("action must be accept or reject.")
	}
	if in.Via == "" || in.ClientKey == "" {
		return httpx.BadRequest("via and client_key name the agent.")
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT id FROM agents_suggestions
		WHERE user_id = $1 AND via = $2 AND client_key = $3 AND status = 'pending' AND expires_at > now()
		ORDER BY created_at, id LIMIT $4`, me.UserID, in.Via, in.ClientKey, maxBulk)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	out := BulkResult{Items: []Suggestion{}}
	for _, id := range ids {
		var sg *Suggestion
		var err error
		if in.Action == "reject" {
			sg, err = s.reject(r.Context(), me.UserID, id)
		} else {
			sg, err = s.accept(r.Context(), me, id, false)
		}
		var apiErr *httpx.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
			out.Skipped++
			continue
		case err != nil:
			return err
		}
		switch sg.Status {
		case StatusAccepted:
			out.Accepted++
		case StatusRejected:
			out.Rejected++
		case StatusFailed:
			out.Failed++
		}
		out.Items = append(out.Items, *sg)
	}
	httpx.OK(w, out)
	return nil
}

// ---- upkeep --------------------------------------------------------------------

// upkeep expires pending suggestions, fails ones stuck mid-accept, deletes
// decided ones past retention and overrides on deleted access resources.
func (s *Service) upkeep(ctx context.Context) error {
	for _, q := range []struct{ sql, why string }{
		{`UPDATE agents_suggestions SET status = 'expired', decided_at = now(), updated_at = now()
			WHERE id IN (SELECT id FROM agents_suggestions WHERE status = 'pending' AND expires_at <= now() LIMIT 1000 FOR UPDATE SKIP LOCKED)
			RETURNING user_id, ` + suggestionCols, "expire"},
		{`UPDATE agents_suggestions SET status = 'failed', error_code = 'interrupted',
			error_message = 'Applying this was interrupted and may not have finished. Check the item before trying again.',
			decided_at = now(), updated_at = now()
			WHERE id IN (SELECT id FROM agents_suggestions WHERE status = 'accepting' AND updated_at < now() - interval '10 minutes'
			             LIMIT 1000 FOR UPDATE SKIP LOCKED)
			RETURNING user_id, ` + suggestionCols, "stuck"},
	} {
		rows, err := s.d.Pool.Query(ctx, q.sql)
		if err != nil {
			return fmt.Errorf("%s: %w", q.why, err)
		}
		type changed struct {
			user uuid.UUID
			sg   *Suggestion
		}
		var list []changed
		for rows.Next() {
			var user uuid.UUID
			sg, err := scanSuggestion(prefixed{rows, &user})
			if err != nil {
				rows.Close()
				return err
			}
			list = append(list, changed{user, sg})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, c := range list {
			s.publish(ctx, c.user, EventSuggestionUpdated, map[string]any{"suggestion": c.sg})
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `
		DELETE FROM agents_suggestions WHERE id IN (
			SELECT id FROM agents_suggestions WHERE decided_at < now() - make_interval(secs => $1) LIMIT 5000)`,
		s.retention.Seconds()); err != nil {
		return fmt.Errorf("retention: %w", err)
	}
	if err := s.dropOrphanedScopes(ctx); err != nil {
		return err
	}
	cutoff := time.Now().Add(-10 * clientTouchInterval)
	s.touched.Range(func(k, v any) bool {
		if v.(time.Time).Before(cutoff) {
			s.touched.Delete(k)
		}
		return true
	})
	return nil
}

// dropOrphanedScopes deletes overrides on access resources that no longer
// exist (the registry row went with the feature's row).
func (s *Service) dropOrphanedScopes(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT DISTINCT kind FROM agents_resource_scopes`)
	if err != nil {
		return err
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	var kinds []string
	for _, k := range all {
		if _, ok := access.KindOf(k); ok {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	_, err = s.d.Pool.Exec(ctx, `
		DELETE FROM agents_resource_scopes r
		WHERE r.kind = ANY($1)
		  AND NOT EXISTS (SELECT 1 FROM access_resources a WHERE a.kind = r.kind AND a.id::text = r.resource_id)`, kinds)
	return err
}

// prefixed scans a leading user_id column before a suggestion's columns.
type prefixed struct {
	row  pgx.Row
	user *uuid.UUID
}

func (p prefixed) Scan(dest ...any) error { return p.row.Scan(append([]any{p.user}, dest...)...) }

// appPath accepts in-app paths only ("/docs/…"), never another origin.
func appPath(p string) bool {
	return p != "" && len(p) <= 500 && strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.ContainsAny(p, " \\\n\r\t")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
