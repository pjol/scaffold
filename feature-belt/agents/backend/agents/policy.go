package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Refusal codes a tool call gets (as a tool error, "code: message").
const (
	CodeScopeOff      = "agent_scope_off"
	CodeScopeReadOnly = "agent_scope_read_only"
	CodeForbidden     = "agent_forbidden"
	CodeLimit         = "suggestion_limit"
)

var _ app.AgentPolicy = (*Service)(nil)

// userState is a user's settings row (or the defaults when there is none).
type userState struct {
	Default     Level
	UserPaused  bool // the user turned off all agent access
	StaffPaused bool // staff did
}

// Paused: all agent access is off, by the user or by staff.
func (st userState) Paused() bool { return st.UserPaused || st.StaffPaused }

func (s *Service) state(ctx context.Context, q db.Querier, userID uuid.UUID) (userState, error) {
	st := userState{Default: LevelRead}
	err := q.QueryRow(ctx, `SELECT default_level, paused, staff_paused_by IS NOT NULL FROM agents_settings WHERE user_id = $1`, userID).
		Scan(&st.Default, &st.UserPaused, &st.StaffPaused)
	if db.IsNoRows(err) {
		return st, nil
	}
	return st, err
}

// FeatureLevel is app.AgentPolicy: the feature's own level (its override,
// else the default) and the highest any item override reaches on the kinds
// the feature works with: its own "<feature>.*" kinds and the other
// features' kinds its tools target (foreignKinds), so Edit on one drive item
// lists a repos feature's write tools. Both are off while all agent access
// is turned off.
func (s *Service) FeatureLevel(ctx context.Context, caller *auth.Identity, feature string) (base, top app.AgentLevel, err error) {
	if caller == nil {
		return app.AgentOff, app.AgentOff, nil
	}
	var (
		def    Level
		paused bool
		own    *Level
		maxRes *int
	)
	err = s.d.Pool.QueryRow(ctx, `
		SELECT COALESCE(st.default_level, 'read'), COALESCE(st.paused OR st.staff_paused_by IS NOT NULL, false), f.level,
		       (SELECT max(agents_level_rank(r.level)) FROM agents_resource_scopes r
		         WHERE r.user_id = x.uid AND (r.feature = $2 OR split_part(r.kind, '.', 1) = $2 OR r.kind = ANY($3::text[])))
		FROM (SELECT $1::uuid AS uid) x
		LEFT JOIN agents_settings st ON st.user_id = x.uid
		LEFT JOIN agents_feature_scopes f ON f.user_id = x.uid AND f.feature = $2`, caller.UserID, feature, s.foreignKinds(feature)).
		Scan(&def, &paused, &own, &maxRes)
	if err != nil {
		return app.AgentOff, app.AgentOff, err
	}
	if paused {
		return app.AgentOff, app.AgentOff, nil
	}
	base = def.Agent()
	if own != nil {
		base = own.Agent()
	}
	top = base
	if maxRes != nil && app.AgentLevel(*maxRes) > top {
		top = app.AgentLevel(*maxRes)
	}
	return base, top, nil
}

// ResourceLevel is app.AgentPolicy: the resource's override, else the
// nearest access ancestor's, else the feature's, else the default.
func (s *Service) ResourceLevel(ctx context.Context, caller *auth.Identity, feature, kind, id string) (app.AgentLevel, error) {
	if caller == nil {
		return app.AgentOff, nil
	}
	var lvl int
	if err := s.d.Pool.QueryRow(ctx, `SELECT agents_level($1, $2, $3, $4)`, caller.UserID, feature, kind, id).Scan(&lvl); err != nil {
		return app.AgentOff, err
	}
	return app.AgentLevel(lvl), nil
}

// ResourceLevels is app.AgentPolicy: ResourceLevel for a page of ids of one
// kind, in one query. Every id gets an entry (off when paused or invalid).
func (s *Service) ResourceLevels(ctx context.Context, caller *auth.Identity, feature, kind string, ids []string) (map[string]app.AgentLevel, error) {
	out := make(map[string]app.AgentLevel, len(ids))
	if caller == nil || len(ids) == 0 {
		for _, id := range ids {
			out[id] = app.AgentOff
		}
		return out, nil
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT x.id, agents_level($1, $2, $3, x.id) FROM unnest($4::text[]) AS x (id)`, caller.UserID, feature, kind, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var lvl int
		if err := rows.Scan(&id, &lvl); err != nil {
			return nil, err
		}
		out[id] = app.AgentLevel(lvl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			out[id] = app.AgentOff
		}
	}
	return out, nil
}

// GateTool is app.AgentPolicy, called before every agent tool call. Reads
// run when the level (of the target, or the feature's highest for tools
// without one) is at least read. Writes run at edit, become a suggestion at
// suggest, and are refused below. Sharing, permission and credential
// changes are refused at every level.
func (s *Service) GateTool(ctx context.Context, caller *auth.Identity, feature string, t app.Tool, input json.RawMessage) (app.ToolDecision, error) {
	if caller == nil {
		return refuse(CodeScopeOff, "Sign in again."), nil
	}
	if t.Feature != "" {
		feature = t.Feature
	}
	if feature == "" {
		feature, _, _ = strings.Cut(t.Name, "_")
	}
	agent, _ := auth.AgentFrom(ctx)
	s.touchClient(ctx, caller.UserID, agent)

	st, err := s.state(ctx, s.d.Pool, caller.UserID)
	if err != nil {
		return app.ToolDecision{}, err
	}
	if st.StaffPaused {
		return refuse(CodeScopeOff, "Support turned off all AI agent access for this account."), nil
	}
	if st.UserPaused {
		return refuse(CodeScopeOff, "All AI agent access is turned off for this account. The person can turn it back on in Settings → AI agents."), nil
	}
	if !t.ReadOnly && forbiddenTool(feature, t.Name) {
		return refuse(CodeForbidden, "AI agents can't change sharing, permissions, membership or credentials. Ask the person to do it in the app."), nil
	}

	kind, id, bad := targetOf(ctx, t, input)
	if bad && !t.ReadOnly {
		// A write names an item by an id that isn't one: never let it fall
		// back to the feature's level.
		return refuse("invalid_input", "That id doesn't name an item. Pass the id a read tool returned."), nil
	}
	var lvl app.AgentLevel
	if kind != "" && id != "" {
		lvl, err = s.ResourceLevel(ctx, caller, feature, kind, id)
	} else {
		var base, top app.AgentLevel
		base, top, err = s.FeatureLevel(ctx, caller, feature)
		lvl = base
		if t.ReadOnly {
			// Tools spanning many resources filter rows themselves (access
			// helpers, app.AgentAllows), so they run if anything is readable.
			lvl = top
		}
	}
	if err != nil {
		return app.ToolDecision{}, err
	}

	label := featureLabel(feature)
	if t.ReadOnly {
		if lvl >= app.AgentRead {
			return app.ToolDecision{Action: app.ToolRun}, nil
		}
		return refuse(CodeScopeOff, offMessage(label, kind != "")), nil
	}
	switch lvl {
	case app.AgentEdit:
		return app.ToolDecision{Action: app.ToolRun}, nil
	case app.AgentSuggest:
		return s.suggest(ctx, caller, agent, feature, t, kind, id, input)
	case app.AgentRead:
		return refuse(CodeScopeReadOnly, fmt.Sprintf("The person lets AI agents read %s but not change it. They can allow suggestions or edits in Settings → AI agents.", scopeNoun(label, kind != ""))), nil
	}
	return refuse(CodeScopeOff, offMessage(label, kind != "")), nil
}

func refuse(code, msg string) app.ToolDecision {
	return app.ToolDecision{Action: app.ToolRefuse, Code: code, Message: msg}
}

func scopeNoun(label string, item bool) string {
	if item {
		return "this item"
	}
	return label
}

func offMessage(label string, item bool) string {
	return fmt.Sprintf("The person hasn't given AI agents access to %s. They can change it in Settings → AI agents.", scopeNoun(label, item))
}

// targetOf reads Tool.Target defensively (feature code; a panic or a
// malformed id means "no single target"). A UUID in any spelling the
// handler's uuid.Parse accepts (upper case, braces, urn:uuid:) is
// canonicalised, so an item's own setting can't be stepped around by
// respelling its id. bad reports a kind with an id that isn't one.
func targetOf(ctx context.Context, t app.Tool, input json.RawMessage) (kind, id string, bad bool) {
	if t.Target == nil {
		return "", "", false
	}
	defer func() {
		if r := recover(); r != nil {
			slog.ErrorContext(ctx, "agents: tool target panicked", "tool", t.Name, "panic", fmt.Sprint(r))
			kind, id, bad = "", "", false
		}
	}()
	kind, id = t.Target(input)
	kind, id = strings.TrimSpace(kind), strings.TrimSpace(id)
	if u, err := uuid.Parse(id); err == nil {
		id = u.String()
	}
	if !kindRE.MatchString(kind) || !idRE.MatchString(id) {
		return "", "", kindRE.MatchString(kind) && id != ""
	}
	return kind, id, false
}

// ---- tools agents never get --------------------------------------------------

// Features whose writes agents never make: who can see what (access),
// how agents connect (mcp), credentials to outside services (connectors'
// own tools; its per-service tools are scoped as "connectors:<id>"), and
// agent scope itself.
var forbiddenFeatures = map[string]bool{"access": true, "mcp": true, "connectors": true, "agents": true}

// Words in a write tool's name that mean it changes who can see or do
// something, or touches a secret. Matched on whole words of the name
// (snake_case, kebab-case or camelCase), so "share_page" and "addMember" are
// caught and "booking_link" is not.
var forbiddenWords = map[string]bool{
	"share": true, "shares": true, "shared": true, "sharing": true, "unshare": true,
	"grant": true, "grants": true, "revoke": true, "permission": true, "permissions": true,
	"role": true, "roles": true, "invite": true, "invites": true, "invitation": true, "invitations": true,
	"member": true, "members": true, "membership": true, "owner": true, "ownership": true,
	"credential": true, "credentials": true, "token": true, "tokens": true, "secret": true, "secrets": true,
	"password": true, "passwords": true, "apikey": true, "oauth": true, "visibility": true,
}

var extraForbidden = map[string]bool{}

// ForbidTool adds a tool agents may never call at any level, for writes the
// word rule misses (call from stitching code before requests are served).
func ForbidTool(name string) { extraForbidden[name] = true }

func forbiddenTool(feature, name string) bool {
	if forbiddenFeatures[feature] || extraForbidden[name] {
		return true
	}
	words := nameWords(name)
	for i, w := range words {
		if forbiddenWords[w] || (w == "api" && i+1 < len(words) && (words[i+1] == "key" || words[i+1] == "keys")) {
			return true
		}
	}
	return false
}

// nameWords splits snake_case, kebab-case and camelCase into lower words.
func nameWords(name string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	prevLower := false
	for _, r := range name {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ':
			flush()
			prevLower = false
		case unicode.IsUpper(r) && prevLower:
			flush()
			cur = append(cur, r)
			prevLower = false
		default:
			cur = append(cur, r)
			prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
		}
	}
	flush()
	return words
}

// ---- agents seen -------------------------------------------------------------

// clientKey identifies an agent across calls: the key or OAuth client id,
// else its name.
func clientKey(a auth.Agent) string {
	switch {
	case a.ClientID != "":
		return truncate(a.ClientID, 200)
	case a.Client != "":
		return truncate(a.Client, 200)
	}
	return a.Via
}

func clientName(a auth.Agent) string {
	if n := strings.TrimSpace(a.Client); n != "" {
		return truncate(n, 120)
	}
	if a.Via == "assistant" {
		return "Assistant"
	}
	return "AI agent"
}

func clientVia(a auth.Agent) string {
	if a.Via == "" {
		return "agent"
	}
	return truncate(a.Via, 40)
}

// touchClient records that an agent acted for the user (at most once a
// minute per agent per instance). Best-effort.
func (s *Service) touchClient(ctx context.Context, userID uuid.UUID, a auth.Agent) {
	key := userID.String() + "|" + clientVia(a) + "|" + clientKey(a)
	now := time.Now()
	if last, ok := s.touched.Load(key); ok && now.Sub(last.(time.Time)) < clientTouchInterval {
		return
	}
	s.touched.Store(key, now)
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO agents_clients (user_id, via, client_key, client) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, via, client_key) DO UPDATE SET last_seen_at = now(), client = EXCLUDED.client`,
		userID, clientVia(a), clientKey(a), clientName(a)); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "agents: record agent", "err", err)
	}
}

// errNotFound is the single answer for missing and foreign ids.
func errNotFound(what string) *httpx.Error { return httpx.NotFound(what + " not found.") }

var errStaffPaused = httpx.NewError(http.StatusForbidden, "agents_paused_by_staff",
	"Support turned off AI agent access for this account. Contact support to turn it back on.")
