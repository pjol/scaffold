package channels

// A person's own workspace preferences (sidebar sort and sections, keywords,
// skin tone, profile title and time zone) and the workspace's settings
// (link previews, retention) that owners and admins change.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// UserPrefs are one person's preferences in one workspace.
type UserPrefs struct {
	SidebarSort    string     `json:"sidebar_sort"` // alpha | recent
	Collapsed      []string   `json:"collapsed"`    // built-in sections: starred, channels, direct
	Keywords       []string   `json:"keywords"`     // notify like a mention
	SkinTone       int        `json:"skin_tone"`    // 0 = default, 1–5 = light → dark
	Title          string     `json:"title"`        // shown on the profile card
	Timezone       string     `json:"timezone"`     // IANA; the client reports it
	ActivityReadAt *time.Time `json:"activity_read_at"`
}

// SidebarSection is a custom group of channels in a person's sidebar.
type SidebarSection struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Position  int       `json:"position"`
	Collapsed bool      `json:"collapsed"`
}

// WorkspaceSettings are org-wide channel settings.
type WorkspaceSettings struct {
	UnfurlLinks   bool `json:"unfurl_links"`
	RetentionDays *int `json:"retention_days"` // null = keep forever
}

var builtinSections = []string{"starred", "channels", "direct"}

const (
	maxKeywords = 30
	maxSections = 30
)

func defaultPrefs() UserPrefs {
	return UserPrefs{SidebarSort: "alpha", Collapsed: []string{}, Keywords: []string{}}
}

// prefsOf reads a person's preferences in an org (defaults when unset).
func prefsOf(ctx context.Context, q db.Querier, userID, orgID uuid.UUID) (UserPrefs, error) {
	p := defaultPrefs()
	err := q.QueryRow(ctx, `
		SELECT sidebar_sort, collapsed, keywords, skin_tone, title, timezone, activity_read_at
		FROM channel_user_prefs WHERE user_id = $1 AND org_id = $2`, userID, orgID).
		Scan(&p.SidebarSort, &p.Collapsed, &p.Keywords, &p.SkinTone, &p.Title, &p.Timezone, &p.ActivityReadAt)
	if db.IsNoRows(err) {
		return defaultPrefs(), nil
	}
	return p, err
}

func sectionsOf(ctx context.Context, q db.Querier, userID, orgID uuid.UUID) ([]SidebarSection, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, position, collapsed FROM channel_sidebar_sections
		WHERE user_id = $1 AND org_id = $2 ORDER BY position, created_at, id`, userID, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SidebarSection])
}

func settingsOf(ctx context.Context, q db.Querier, orgID uuid.UUID) (WorkspaceSettings, error) {
	ws := WorkspaceSettings{UnfurlLinks: true}
	err := q.QueryRow(ctx, `SELECT unfurl_links, retention_days FROM channel_workspace_settings WHERE org_id = $1`, orgID).
		Scan(&ws.UnfurlLinks, &ws.RetentionDays)
	if db.IsNoRows(err) {
		return WorkspaceSettings{UnfurlLinks: true}, nil
	}
	return ws, err
}

// ---- preferences ---------------------------------------------------------------------

type prefsUpdate struct {
	SidebarSort *string   `json:"sidebar_sort"`
	Collapsed   *[]string `json:"collapsed"`
	Keywords    *[]string `json:"keywords"`
	SkinTone    *int      `json:"skin_tone"`
	Title       *string   `json:"title"`
	Timezone    *string   `json:"timezone"`
}

// cleanKeywords lowercases, trims and de-duplicates keywords (2–40 characters each).
func cleanKeywords(in []string) ([]string, error) {
	out := []string{}
	for _, k := range in {
		k = strings.ToLower(strings.Join(strings.Fields(k), " "))
		if k == "" || slices.Contains(out, k) {
			continue
		}
		if n := utf8.RuneCountInString(k); n < 2 || n > 40 {
			return nil, httpx.BadRequest("Keywords are 2 to 40 characters.")
		}
		out = append(out, k)
	}
	if len(out) > maxKeywords {
		return nil, httpx.BadRequest("Add up to 30 keywords.")
	}
	return out, nil
}

// validZone accepts IANA zone names the server knows.
func validZone(tz string) bool {
	if tz == "" || len(tz) > 64 || strings.ContainsAny(tz, " \\") {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil && tz != "Local"
}

// PUT /channels/orgs/{orgID}/prefs — returns the preferences.
func (s *Service) updatePrefs(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req prefsUpdate
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := prefsOf(ctx, s.d.Pool, me, m.OrgID)
	if err != nil {
		return err
	}
	if req.SidebarSort != nil {
		if *req.SidebarSort != "alpha" && *req.SidebarSort != "recent" {
			return httpx.BadRequest("sidebar_sort must be alpha or recent.")
		}
		p.SidebarSort = *req.SidebarSort
	}
	if req.Collapsed != nil {
		p.Collapsed = []string{}
		for _, c := range *req.Collapsed {
			if !slices.Contains(builtinSections, c) {
				return httpx.BadRequest("collapsed takes starred, channels and direct.")
			}
			if !slices.Contains(p.Collapsed, c) {
				p.Collapsed = append(p.Collapsed, c)
			}
		}
	}
	if req.Keywords != nil {
		if p.Keywords, err = cleanKeywords(*req.Keywords); err != nil {
			return err
		}
	}
	if req.SkinTone != nil {
		if *req.SkinTone < 0 || *req.SkinTone > 5 {
			return httpx.BadRequest("skin_tone is 0 to 5.")
		}
		p.SkinTone = *req.SkinTone
	}
	if req.Title != nil {
		t := strings.Join(strings.Fields(*req.Title), " ")
		if utf8.RuneCountInString(t) > 80 {
			return httpx.BadRequest("Titles can be up to 80 characters.")
		}
		if err := checkText(ctx, t); err != nil {
			return err
		}
		p.Title = t
	}
	if req.Timezone != nil {
		if *req.Timezone != "" && !validZone(*req.Timezone) {
			return httpx.BadRequest("Unknown time zone.")
		}
		p.Timezone = *req.Timezone
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO channel_user_prefs (user_id, org_id, sidebar_sort, collapsed, keywords, skin_tone, title, timezone)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, org_id) DO UPDATE SET sidebar_sort = EXCLUDED.sidebar_sort, collapsed = EXCLUDED.collapsed,
		  keywords = EXCLUDED.keywords, skin_tone = EXCLUDED.skin_tone, title = EXCLUDED.title, timezone = EXCLUDED.timezone,
		  updated_at = now()`, me, m.OrgID, p.SidebarSort, p.Collapsed, p.Keywords, p.SkinTone, p.Title, p.Timezone); err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: m.OrgID, Reason: "prefs"})
	httpx.OK(w, p)
	return nil
}

// ---- sidebar sections ----------------------------------------------------------------

type sectionReq struct {
	Name      *string `json:"name"`
	Position  *int    `json:"position"`
	Collapsed *bool   `json:"collapsed"`
}

func cleanSectionName(raw string) (string, error) {
	n := strings.Join(strings.Fields(raw), " ")
	if n == "" || utf8.RuneCountInString(n) > 40 {
		return "", httpx.BadRequest("Section names are 1 to 40 characters.")
	}
	return n, nil
}

// POST /channels/orgs/{orgID}/sections {name} — 201 with the section.
func (s *Service) createSection(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req sectionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanSectionName(deref(req.Name))
	if err != nil {
		return err
	}
	var sec SidebarSection
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n, next int
		if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position) + 1, 0) FROM channel_sidebar_sections WHERE user_id = $1 AND org_id = $2`,
			me, m.OrgID).Scan(&n, &next); err != nil {
			return err
		}
		if n >= maxSections {
			return httpx.BadRequest("You can have up to 30 sections.")
		}
		return tx.QueryRow(ctx, `
			INSERT INTO channel_sidebar_sections (user_id, org_id, name, position) VALUES ($1, $2, $3, $4)
			RETURNING id, name, position, collapsed`, me, m.OrgID, name, next).Scan(&sec.ID, &sec.Name, &sec.Position, &sec.Collapsed)
	})
	if err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: m.OrgID, Reason: "prefs"})
	httpx.JSON(w, http.StatusCreated, sec)
	return nil
}

// PATCH /channels/sections/{id} {name?, position?, collapsed?}
func (s *Service) updateSection(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req sectionReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var name *string
	if req.Name != nil {
		n, err := cleanSectionName(*req.Name)
		if err != nil {
			return err
		}
		name = &n
	}
	if req.Position != nil && (*req.Position < 0 || *req.Position > 10000) {
		return httpx.BadRequest("position is 0 to 10000.")
	}
	var sec SidebarSection
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `
		UPDATE channel_sidebar_sections SET name = COALESCE($3, name), position = COALESCE($4, position),
		  collapsed = COALESCE($5, collapsed), updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, name, position, collapsed, org_id`, id, me, name, req.Position, req.Collapsed).
		Scan(&sec.ID, &sec.Name, &sec.Position, &sec.Collapsed, &orgID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Section not found.")
	}
	if err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "prefs"})
	httpx.OK(w, sec)
	return nil
}

// DELETE /channels/sections/{id} — its channels go back to Channels / Direct messages.
func (s *Service) deleteSection(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var orgID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `DELETE FROM channel_sidebar_sections WHERE id = $1 AND user_id = $2 RETURNING org_id`, id, me).Scan(&orgID)
	if db.IsNoRows(err) {
		return httpx.NotFound("Section not found.")
	}
	if err != nil {
		return err
	}
	s.toUser(ctx, me, EventSidebar, SidebarEvent{OrgID: orgID, Reason: "prefs"})
	httpx.NoContent(w)
	return nil
}

// ---- workspace settings ---------------------------------------------------------------

type settingsReq struct {
	UnfurlLinks   *bool `json:"unfurl_links"`
	RetentionDays *int  `json:"retention_days"` // 0 = keep forever
}

// GET /channels/orgs/{orgID}/settings
func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) error {
	m, _ := orgs.FromContext(r.Context())
	ws, err := settingsOf(r.Context(), s.d.Pool, m.OrgID)
	if err != nil {
		return err
	}
	httpx.OK(w, ws)
	return nil
}

// PATCH /channels/orgs/{orgID}/settings — ActManageWorkspace; audited.
func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	var req settingsReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActManageWorkspace, OrgID: m.OrgID, UserID: me, OrgRole: m.Role})
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("Only the " + orgs.Noun() + "'s owners and admins can change its channel settings.")
	}
	if req.RetentionDays != nil && (*req.RetentionDays < 0 || *req.RetentionDays > 36500) {
		return httpx.BadRequest("Keep messages for 1 to 36500 days, or 0 to keep them forever.")
	}
	var out WorkspaceSettings
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		before, err := settingsOf(ctx, tx, m.OrgID)
		if err != nil {
			return err
		}
		out = before
		if req.UnfurlLinks != nil {
			out.UnfurlLinks = *req.UnfurlLinks
		}
		if req.RetentionDays != nil {
			out.RetentionDays = nil
			if *req.RetentionDays > 0 {
				d := *req.RetentionDays
				out.RetentionDays = &d
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO channel_workspace_settings (org_id, unfurl_links, retention_days, updated_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (org_id) DO UPDATE SET unfurl_links = EXCLUDED.unfurl_links, retention_days = EXCLUDED.retention_days,
			  updated_by = EXCLUDED.updated_by, updated_at = now()`, m.OrgID, out.UnfurlLinks, out.RetentionDays, me); err != nil {
			return err
		}
		return audit.Log(ctx, tx, me, "channels.settings.update", "org", m.OrgID.String(), map[string]any{"before": before, "after": out})
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- profile cards ----------------------------------------------------------------------

// ProfileCard is what a person's card shows to other members of the org.
// Presence and status come from the presence feature on the client.
type ProfileCard struct {
	UserRef
	OrgRole  string  `json:"org_role"`
	Title    string  `json:"title"`
	Timezone string  `json:"timezone"` // IANA; "" when unknown
	Teams    []Group `json:"teams"`    // when the app's GroupResolver lists memberships
}

// GroupMembership is optional on a GroupResolver: the groups a person is in,
// shown on their profile card.
type GroupMembership interface {
	GroupsOf(ctx context.Context, orgID, userID uuid.UUID) ([]Group, error)
}

// UserZone returns a person's time zone from elsewhere in the app (a
// calendar setting), or nil.
type UserZone func(ctx context.Context, userID uuid.UUID) *time.Location

var userZone atomic.Pointer[UserZone]

// SetUserZone lets profile cards and reminders use the app's own time zone
// setting when the person's client hasn't reported one.
func SetUserZone(fn UserZone) {
	if fn != nil {
		userZone.Store(&fn)
	}
}

// zoneFor resolves the zone for a person: an explicit client value, their
// stored zone in the org, the app's setting, else UTC.
func zoneFor(ctx context.Context, q db.Querier, userID, orgID uuid.UUID, explicit string) *time.Location {
	if explicit != "" && validZone(explicit) {
		if loc, err := time.LoadLocation(explicit); err == nil {
			return loc
		}
	}
	var tz string
	if err := q.QueryRow(ctx, `SELECT timezone FROM channel_user_prefs WHERE user_id = $1 AND org_id = $2`, userID, orgID).Scan(&tz); err == nil && tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	if fn := userZone.Load(); fn != nil {
		if loc := (*fn)(ctx, userID); loc != nil {
			return loc
		}
	}
	return time.UTC
}

// GET /channels/orgs/{orgID}/people/{userID}
func (s *Service) profileCard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	m, _ := orgs.FromContext(ctx)
	uid, err := httpx.URLParamUUID(r, "userID")
	if err != nil {
		return err
	}
	var card ProfileCard
	var name, status string
	var avatar *string
	err = s.d.Pool.QueryRow(ctx, `
		SELECT u.display_name, u.avatar_url, u.status, om.role, COALESCE(p.title, ''), COALESCE(p.timezone, '')
		FROM org_members om JOIN users u ON u.id = om.user_id
		LEFT JOIN channel_user_prefs p ON p.user_id = om.user_id AND p.org_id = om.org_id
		WHERE om.org_id = $1 AND om.user_id = $2`, m.OrgID, uid).Scan(&name, &avatar, &status, &card.OrgRole, &card.Title, &card.Timezone)
	if db.IsNoRows(err) {
		return httpx.NotFound("That person isn't in this " + orgs.Noun() + ".")
	}
	if err != nil {
		return err
	}
	card.UserRef = userRef(uid, name, avatar, status)
	card.Teams = []Group{}
	if card.Deleted {
		card.Title, card.Timezone = "", ""
	} else {
		if card.Timezone == "" {
			if fn := userZone.Load(); fn != nil {
				if loc := (*fn)(ctx, uid); loc != nil {
					card.Timezone = loc.String()
				}
			}
		}
		if gm, ok := currentGroupResolver().(GroupMembership); ok {
			teams, err := gm.GroupsOf(ctx, m.OrgID, uid)
			if err != nil {
				return err
			}
			for _, g := range teams {
				if groupIDPattern.MatchString(g.ID) && handlePattern.MatchString(g.Handle) {
					card.Teams = append(card.Teams, g)
				}
			}
		}
	}
	httpx.OK(w, card)
	return nil
}
