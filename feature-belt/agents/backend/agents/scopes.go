package agents

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// FeatureScope is one feature or external service in settings.
type FeatureScope struct {
	Feature           string `json:"feature"`
	Label             string `json:"label"`
	Level             *Level `json:"level"`     // its own setting; null follows the default
	Effective         Level  `json:"effective"` // what applies (ignoring "turn off all")
	ReadTools         int    `json:"read_tools"`
	WriteTools        int    `json:"write_tools"`
	ResourceOverrides int    `json:"resource_overrides"` // items in it with their own setting
	Connected         bool   `json:"connected"`          // services: still connected
}

// AgentInfo is an agent that has acted for the user.
type AgentInfo struct {
	AgentRef
	CreatedAt  time.Time `json:"created_at"` // first seen
	LastSeenAt time.Time `json:"last_seen_at"`
	Pending    int       `json:"pending"`
}

// Settings is GET /v1/agents/settings.
type Settings struct {
	DefaultLevel       Level          `json:"default_level"`
	Paused             bool           `json:"paused"`          // the user turned off all agent access
	PausedByStaff      bool           `json:"paused_by_staff"` // support did; only they turn it back on
	Features           []FeatureScope `json:"features"`
	ResourceOverrides  int            `json:"resource_overrides"`
	PendingSuggestions int            `json:"pending_suggestions"`
	Agents             []AgentInfo    `json:"agents"`
	Surfaces           []string       `json:"surfaces"` // installed agent features: mcp, assistant, connectors
}

func (s *Service) settings(ctx context.Context, userID uuid.UUID) (*Settings, error) {
	st, err := s.state(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	out := &Settings{DefaultLevel: st.Default, Paused: st.UserPaused, PausedByStaff: st.StaffPaused,
		Features: []FeatureScope{}, Agents: []AgentInfo{}, Surfaces: []string{}}
	for _, name := range []string{"mcp", "assistant", "connectors"} {
		if s.installed(name) {
			out.Surfaces = append(out.Surfaces, name)
		}
	}
	own, counts, err := s.featureOverrides(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, e := range s.featureCatalog() {
		if _, set := own[e.Feature]; featureHidden(e.Feature) && !set {
			continue
		}
		fs := FeatureScope{Feature: e.Feature, Label: e.Label, Effective: st.Default, ReadTools: e.ReadTools,
			WriteTools: e.WriteTools, ResourceOverrides: counts[e.Feature], Connected: true}
		if o, ok := own[e.Feature]; ok {
			l := o.level
			fs.Level, fs.Effective = &l, l
		}
		out.Features = append(out.Features, fs)
	}
	if err := s.d.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM agents_resource_scopes WHERE user_id = $1),
		       (SELECT count(*) FROM agents_suggestions WHERE user_id = $1 AND status IN ('pending', 'accepting') AND expires_at > now())`,
		userID).Scan(&out.ResourceOverrides, &out.PendingSuggestions); err != nil {
		return nil, err
	}
	out.Agents, err = s.agentsSeen(ctx, s.d.Pool, userID)
	return out, err
}

type featureOverride struct {
	level Level
	label string
}

// featureOverrides returns the user's feature settings and how many resource
// overrides each feature has.
func (s *Service) featureOverrides(ctx context.Context, userID uuid.UUID) (map[string]featureOverride, map[string]int, error) {
	own := map[string]featureOverride{}
	rows, err := s.d.Pool.Query(ctx, `SELECT feature, level, label FROM agents_feature_scopes WHERE user_id = $1`, userID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var f string
		var o featureOverride
		if err := rows.Scan(&f, &o.level, &o.label); err != nil {
			rows.Close()
			return nil, nil, err
		}
		own[f] = o
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	counts := map[string]int{}
	rows, err = s.d.Pool.Query(ctx, `SELECT feature, count(*) FROM agents_resource_scopes WHERE user_id = $1 GROUP BY feature`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		var n int
		if err := rows.Scan(&f, &n); err != nil {
			return nil, nil, err
		}
		counts[f] = n
	}
	return own, counts, rows.Err()
}

func (s *Service) agentsSeen(ctx context.Context, q db.Querier, userID uuid.UUID) ([]AgentInfo, error) {
	rows, err := q.Query(ctx, `
		SELECT c.via, c.client, c.client_key, c.created_at, c.last_seen_at,
		       (SELECT count(*) FROM agents_suggestions x
		         WHERE x.user_id = c.user_id AND x.via = c.via AND x.client_key = c.client_key
		           AND x.status IN ('pending', 'accepting') AND x.expires_at > now())
		FROM agents_clients c WHERE c.user_id = $1
		ORDER BY c.last_seen_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentInfo{}
	for rows.Next() {
		var a AgentInfo
		if err := rows.Scan(&a.Via, &a.Client, &a.ClientKey, &a.CreatedAt, &a.LastSeenAt, &a.Pending); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	out, err := s.settings(r.Context(), me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// putSettings changes the default level and/or turns all agent access off or
// back on (the user's own switch; a staff pause stays until staff lift it).
func (s *Service) putSettings(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var in struct {
		DefaultLevel *Level `json:"default_level"`
		Paused       *bool  `json:"paused"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if in.DefaultLevel == nil && in.Paused == nil {
		return httpx.BadRequest("Send default_level or paused.")
	}
	if in.DefaultLevel != nil && !in.DefaultLevel.Valid() {
		return httpx.BadRequest("default_level must be off, read, suggest or edit.")
	}
	if in.Paused != nil && !*in.Paused {
		st, err := s.state(r.Context(), s.d.Pool, me.UserID)
		if err != nil {
			return err
		}
		if st.StaffPaused {
			return errStaffPaused
		}
	}
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO agents_settings (user_id, default_level, paused, paused_at)
		VALUES ($1, COALESCE($2, 'read'), COALESCE($3, false), CASE WHEN $3 THEN now() END)
		ON CONFLICT (user_id) DO UPDATE SET
		  default_level = COALESCE($2, agents_settings.default_level),
		  paused = COALESCE($3, agents_settings.paused),
		  paused_at = CASE WHEN $3 IS NULL THEN agents_settings.paused_at WHEN $3 THEN now() END,
		  updated_at = now()`, me.UserID, in.DefaultLevel, in.Paused); err != nil {
		return err
	}
	s.scopesChanged(r.Context(), me.UserID)
	out, err := s.settings(r.Context(), me.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// listServices returns the user's connected external services (from the
// connectors feature's per-user tools) with their settings, plus services
// they set before and no longer have connected (connected: false).
func (s *Service) listServices(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	st, err := s.state(r.Context(), s.d.Pool, me.UserID)
	if err != nil {
		return err
	}
	own, _, err := s.featureOverrides(r.Context(), me.UserID)
	if err != nil {
		return err
	}
	items := []FeatureScope{}
	seen := map[string]bool{}
	for _, e := range s.serviceCatalog(r.Context(), me) {
		fs := FeatureScope{Feature: e.Feature, Label: e.Label, Effective: st.Default, ReadTools: e.ReadTools, WriteTools: e.WriteTools, Connected: true}
		if o, ok := own[e.Feature]; ok {
			l := o.level
			fs.Level, fs.Effective = &l, l
		}
		seen[e.Feature] = true
		items = append(items, fs)
	}
	for f, o := range own {
		if !isService(f) || seen[f] {
			continue
		}
		l := o.level
		label := o.label
		if label == "" {
			label = featureLabel(f)
		}
		items = append(items, FeatureScope{Feature: f, Label: label, Level: &l, Effective: l})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Connected != items[j].Connected {
			return items[i].Connected
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	httpx.OK(w, map[string]any{"items": items})
	return nil
}

// ---- one scope (the ⋮ menu) ------------------------------------------------------

// Inherited is what a feature or item gets without its own setting.
type Inherited struct {
	Level Level  `json:"level"`
	From  string `json:"from"`            // parent | feature | default
	Kind  string `json:"kind,omitempty"`  // parent: its kind and id
	ID    string `json:"id,omitempty"`    //
	Title string `json:"title,omitempty"` // parent: its title, when the user can open it
	Noun  string `json:"noun,omitempty"`  // parent: "folder", "page"
}

// ScopeInfo is the scope of a feature (kind and id empty), an external
// service, or one item.
type ScopeInfo struct {
	Feature      string    `json:"feature"`
	FeatureLabel string    `json:"feature_label"`
	Kind         string    `json:"kind,omitempty"`
	ID           string    `json:"id,omitempty"`
	Level        Level     `json:"level"`    // what applies now (off while paused)
	Override     *Level    `json:"override"` // its own setting
	Inherited    Inherited `json:"inherited"`
	Paused       bool      `json:"paused"`
	// MyLevel is the user's own access to an access resource (view …
	// manage): an agent never goes past it. Null for other kinds.
	MyLevel *string `json:"my_level"`
}

type scopeRef struct{ feature, kind, id string }

// normalize: an external service is scoped as a whole, so a kind and id
// sent with it (its connection's ⋮ menu) are dropped.
func (ref scopeRef) normalize() scopeRef {
	if isService(ref.feature) {
		ref.kind, ref.id = "", ""
	}
	return ref
}

func (ref scopeRef) validate(s *Service) error {
	if !validFeature(ref.feature) {
		return httpx.BadRequest("feature must be a feature name or connectors:<id>.")
	}
	if ref.kind == "" && ref.id == "" {
		return nil
	}
	if !kindRE.MatchString(ref.kind) || !idRE.MatchString(ref.id) {
		return httpx.BadRequest("kind and id must name one item (kind like docs.page).")
	}
	if !s.featureOwnsKind(ref.feature, ref.kind) {
		return httpx.BadRequest("kind must belong to feature (" + ref.feature + ".…) or be one its tools act on.")
	}
	return nil
}

// resolve answers several refs in one query.
func (s *Service) resolve(ctx context.Context, userID uuid.UUID, refs []scopeRef) ([]ScopeInfo, error) {
	st, err := s.state(ctx, s.d.Pool, userID)
	if err != nil {
		return nil, err
	}
	features, kinds, ids := make([]string, len(refs)), make([]string, len(refs)), make([]string, len(refs))
	for i, r := range refs {
		features[i], kinds[i], ids[i] = r.feature, r.kind, r.id
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT x.ord, o.agent_level, i.agent_level, i.from_type, i.from_kind, i.from_id,
		       CASE WHEN x.kind = '' THEN f.level ELSE r.level END,
		       CASE WHEN x.kind <> '' AND x.id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		            THEN access_level($1, x.kind, x.id::uuid) END
		FROM unnest($2::text[], $3::text[], $4::text[]) WITH ORDINALITY AS x (feature, kind, id, ord)
		CROSS JOIN LATERAL agents_resolve($1, x.feature, x.kind, x.id, false, true) o
		CROSS JOIN LATERAL agents_resolve($1, x.feature, x.kind, x.id, true, true) i
		LEFT JOIN agents_feature_scopes f ON x.kind = '' AND f.user_id = $1 AND f.feature = x.feature
		LEFT JOIN agents_resource_scopes r ON x.kind <> '' AND r.user_id = $1 AND r.kind = x.kind AND r.resource_id = x.id
		ORDER BY x.ord`, userID, features, kinds, ids)
	if err != nil {
		return nil, err
	}
	type raw struct {
		ord                    int
		level, inherited       int
		from, fromKind, fromID string
		override               *Level
		myLevel                *int
	}
	var list []raw
	for rows.Next() {
		var x raw
		if err := rows.Scan(&x.ord, &x.level, &x.inherited, &x.from, &x.fromKind, &x.fromID, &x.override, &x.myLevel); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ScopeInfo, len(refs))
	for i, x := range list {
		ref := refs[i]
		info := ScopeInfo{Feature: ref.feature, FeatureLabel: featureLabel(ref.feature), Kind: ref.kind, ID: ref.id,
			Level: rankLevel(x.level), Override: x.override, Paused: st.Paused(),
			Inherited: Inherited{Level: rankLevel(x.inherited), From: x.from}}
		if info.Paused {
			info.Level = LevelOff
		}
		if x.from == "parent" {
			info.Inherited.Kind, info.Inherited.ID = x.fromKind, x.fromID
			info.Inherited.Noun, info.Inherited.Title = s.parentTitle(ctx, userID, x.fromKind, x.fromID)
		}
		if _, ok := access.KindOf(ref.kind); ok {
			l := access.None
			if x.myLevel != nil {
				l = access.Level(*x.myLevel)
			}
			name := l.String()
			info.MyLevel = &name
		}
		out[i] = info
	}
	return out, nil
}

// parentTitle names the ancestor a level comes from, when the user can open it.
func (s *Service) parentTitle(ctx context.Context, userID uuid.UUID, kind, id string) (noun, title string) {
	k, ok := access.KindOf(kind)
	if !ok {
		return "item", ""
	}
	rid, err := uuid.Parse(id)
	if err != nil {
		return k.Label, ""
	}
	var lvl int
	if err := s.d.Pool.QueryRow(ctx, `SELECT access_level($1, $2, $3, false)`, userID, kind, rid).Scan(&lvl); err != nil || lvl < int(access.View) {
		return k.Label, ""
	}
	t, err := k.Title(ctx, s.d.Pool, rid)
	if err != nil {
		return k.Label, ""
	}
	return k.Label, truncate(t, 80)
}

// resolveScopes is GET /agents/scope?r=feature,kind,id (repeat up to 50; kind
// and id empty for a whole feature).
func (s *Service) resolveScopes(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	raw := r.URL.Query()["r"]
	if len(raw) == 0 || len(raw) > maxResolveBatch {
		return httpx.BadRequest("Send 1 to 50 r=feature,kind,id values.")
	}
	refs := make([]scopeRef, 0, len(raw))
	for _, v := range raw {
		parts := strings.Split(v, ",")
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		if len(parts) != 3 {
			return httpx.BadRequest("Each r is feature,kind,id.")
		}
		ref := scopeRef{feature: strings.TrimSpace(parts[0]), kind: strings.TrimSpace(parts[1]), id: strings.TrimSpace(parts[2])}.normalize()
		if err := ref.validate(s); err != nil {
			return err
		}
		refs = append(refs, ref)
	}
	out, err := s.resolve(r.Context(), me.UserID, refs)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// putScope sets or clears (level null) the user's own setting for a
// feature, an external service or one item. Items registered with access
// must be ones the user can open; for other kinds the client passes the
// title and app path to show in the list.
func (s *Service) putScope(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	var in struct {
		Feature string `json:"feature"`
		Kind    string `json:"kind"`
		ID      string `json:"id"`
		Level   *Level `json:"level"`
		Title   string `json:"title"`
		Link    string `json:"link"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	ref := scopeRef{feature: strings.TrimSpace(in.Feature), kind: strings.TrimSpace(in.Kind), id: strings.TrimSpace(in.ID)}.normalize()
	if err := ref.validate(s); err != nil {
		return err
	}
	if !s.knownFeature(ref.feature) {
		return httpx.NotFound("That feature isn't in this app.")
	}
	if in.Level != nil && !in.Level.Valid() {
		return httpx.BadRequest("level must be off, read, suggest, edit or null.")
	}
	title := truncate(strings.TrimSpace(in.Title), 200)
	link := strings.TrimSpace(in.Link)
	if link != "" && !appPath(link) {
		return httpx.BadRequest("link must be an app path such as /docs/….")
	}
	ctx := r.Context()

	if ref.kind == "" {
		var err error
		if in.Level == nil {
			_, err = s.d.Pool.Exec(ctx, `DELETE FROM agents_feature_scopes WHERE user_id = $1 AND feature = $2`, me.UserID, ref.feature)
		} else {
			_, err = s.d.Pool.Exec(ctx, `
				INSERT INTO agents_feature_scopes (user_id, feature, level, label) VALUES ($1, $2, $3, $4)
				ON CONFLICT (user_id, feature) DO UPDATE SET level = EXCLUDED.level,
				  label = CASE WHEN EXCLUDED.label = '' THEN agents_feature_scopes.label ELSE EXCLUDED.label END, updated_at = now()`,
				me.UserID, ref.feature, *in.Level, truncate(title, 60))
		}
		if err != nil {
			return err
		}
	} else {
		if _, ok := access.KindOf(ref.kind); ok {
			rid, err := uuid.Parse(ref.id)
			if err != nil {
				return errNotFound("Item")
			}
			if _, err := access.Require(ctx, s.d.Pool, me.UserID, ref.kind, rid, access.View); err != nil {
				return err
			}
			title, link = "", "" // read live from the registry
		}
		err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
			if in.Level == nil {
				_, err := tx.Exec(ctx, `DELETE FROM agents_resource_scopes WHERE user_id = $1 AND kind = $2 AND resource_id = $3`, me.UserID, ref.kind, ref.id)
				return err
			}
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM agents_resource_scopes WHERE user_id = $1`, me.UserID).Scan(&n); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `
				UPDATE agents_resource_scopes SET level = $4,
				       title = CASE WHEN $5 = '' THEN title ELSE $5 END, link = CASE WHEN $6 = '' THEN link ELSE $6 END, updated_at = now()
				WHERE user_id = $1 AND kind = $2 AND resource_id = $3`, me.UserID, ref.kind, ref.id, *in.Level, title, link)
			if err != nil || tag.RowsAffected() > 0 {
				return err
			}
			if n >= maxResourceScopes {
				return httpx.NewError(http.StatusConflict, "scope_limit", "You have 2,000 items with their own agent setting. Remove some before adding more.")
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO agents_resource_scopes (user_id, feature, kind, resource_id, level, title, link)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (user_id, kind, resource_id) DO UPDATE SET level = EXCLUDED.level, updated_at = now()`,
				me.UserID, ref.feature, ref.kind, ref.id, *in.Level, title, link)
			return err
		})
		if err != nil {
			return err
		}
	}
	s.scopesChanged(ctx, me.UserID)
	out, err := s.resolve(ctx, me.UserID, []scopeRef{ref})
	if err != nil {
		return err
	}
	httpx.OK(w, out[0])
	return nil
}

// ---- items with their own setting ------------------------------------------------

// ResourceScope is one item override in the settings list.
type ResourceScope struct {
	ID           uuid.UUID `json:"id"`
	Feature      string    `json:"feature"`
	FeatureLabel string    `json:"feature_label"`
	Kind         string    `json:"kind"`
	Noun         string    `json:"noun"` // "folder"
	ResourceID   string    `json:"resource_id"`
	Title        string    `json:"title"`
	Link         string    `json:"link"`
	Level        Level     `json:"level"`
	Inherits     bool      `json:"inherits"` // passes its level to items under it (access kinds)
	Missing      bool      `json:"missing"`  // deleted, or the user can no longer open it
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Service) listResourceScopes(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	p := httpx.ParsePage(r)
	var cur cursor
	if err := httpx.DecodeCursor(p.Cursor, &cur); err != nil {
		return err
	}
	var after *time.Time
	if !cur.T.IsZero() {
		after = &cur.T
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT id, feature, kind, resource_id, level, title, link, created_at,
		       CASE WHEN resource_id ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		            THEN access_level($1, kind, resource_id::uuid) END
		FROM agents_resource_scopes
		WHERE user_id = $1 AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
		ORDER BY created_at DESC, id DESC LIMIT $4`, me.UserID, after, cur.ID, p.Limit+1)
	if err != nil {
		return err
	}
	type row struct {
		ResourceScope
		access *int
	}
	var list []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.Feature, &x.Kind, &x.ResourceID, &x.Level, &x.Title, &x.Link, &x.CreatedAt, &x.access); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	out := make([]ResourceScope, 0, len(list))
	for _, x := range list {
		rs := x.ResourceScope
		rs.FeatureLabel = featureLabel(rs.Feature)
		rs.Noun = "item"
		if k, ok := access.KindOf(rs.Kind); ok {
			rs.Noun, rs.Inherits = k.Label, true
			rs.Title, rs.Link = "", ""
			rid, err := uuid.Parse(rs.ResourceID)
			if err != nil || x.access == nil || *x.access < int(access.View) {
				rs.Missing = true
			} else {
				rs.Link = k.Link(rid)
				if t, err := k.Title(r.Context(), s.d.Pool, rid); err == nil {
					rs.Title = truncate(t, 120)
				} else if db.IsNoRows(err) {
					rs.Missing = true
				}
			}
		}
		out = append(out, rs)
	}
	httpx.OK(w, httpx.NewPage(out, p.Limit, func(rs ResourceScope) any { return cursor{T: rs.CreatedAt, ID: rs.ID} }))
	return nil
}

func (s *Service) deleteResourceScope(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context())
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errNotFound("Setting")
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM agents_resource_scopes WHERE id = $1 AND user_id = $2`, id, me.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errNotFound("Setting")
	}
	s.scopesChanged(r.Context(), me.UserID)
	httpx.NoContent(w)
	return nil
}

// rankLevel reads an SQL rank (0–3).
func rankLevel(n int) Level { return LevelOf(app.AgentLevel(n)) }
