package calls

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// AgentKindMeeting is the resource kind agent scopes and ⋮ menus use for a meeting.
const AgentKindMeeting = "calls.meeting"

// Tools exposes calls to agents acting as the caller: their recent calls
// and upcoming meetings, a meeting's details (only when the caller may join
// it, the REST rule), and creating a meeting link (no invitations: those
// notify other people). Every read also drops meetings the user's AI agent
// scope keeps from agents (agentMay).
func (s *Service) Tools() []app.Tool {
	return []app.Tool{
		{
			Name:        "calls_list_recent",
			Description: "List the user's recent calls (newest first): calls they were in with duration and participants, and calls they missed.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Limit int `json:"limit"`
				}
				_ = json.Unmarshal(input, &in)
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				items, err := s.history(ctx, s.d.Pool, caller.UserID, cursor{}, in.Limit)
				if err != nil {
					return nil, err
				}
				if len(items) > in.Limit {
					items = items[:in.Limit]
				}
				out := []HistoryItem{}
				for _, h := range items {
					ok, err := s.agentMay(ctx, caller, h.Meeting.ID)
					if err != nil {
						return nil, err
					}
					if ok {
						out = append(out, h)
					}
				}
				return out, nil
			},
		},
		{
			Name:        "calls_list_upcoming",
			Description: "List scheduled meetings the user hosts or is invited to that haven't ended, soonest first, with their join links.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, _ json.RawMessage) (any, error) {
				list, err := s.upcoming(ctx, caller.UserID, cursor{}, 50)
				if err != nil {
					return nil, err
				}
				if len(list) > 50 {
					list = list[:50]
				}
				out := []Meeting{}
				for _, m := range list {
					ok, err := s.agentMay(ctx, caller, m.ID)
					if err != nil {
						return nil, err
					}
					if ok {
						out = append(out, m)
					}
				}
				return out, nil
			},
		},
		{
			Name:        "calls_get_meeting",
			Description: "Get a meeting's details (title, schedule, access, host, invitees, join link) by its code (abc-defg-hij), link or id. Only works for meetings the user can join without asking.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"meeting":{"type":"string","description":"Meeting code, join link or id"}},"required":["meeting"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					Meeting string `json:"meeting"`
				}
				if err := json.Unmarshal(input, &in); err != nil || strings.TrimSpace(in.Meeting) == "" {
					return nil, httpx.BadRequest("meeting must be a meeting code, link or id.")
				}
				m, err := loadMeetingRef(ctx, s.d.Pool, strings.TrimSpace(in.Meeting))
				if err != nil {
					return nil, err
				}
				a, err := s.accessFor(ctx, s.d.Pool, m, caller.UserID)
				if err != nil {
					return nil, err
				}
				if !a.canJoin() {
					return nil, errMeetingNotFound
				}
				if ok, err := s.agentMay(ctx, caller, m.ID); err != nil || !ok {
					if err != nil {
						return nil, err
					}
					return nil, errMeetingNotFound
				}
				return s.detail(ctx, s.d.Pool, m, a.Role)
			},
		},
		{
			Name:        "calls_create_meeting",
			Description: "Create a video meeting link the user hosts and return its code and join URL. Optionally scheduled (RFC 3339 times) and scoped to one of the user's " + orgs.Nouns() + ". Nobody is notified; share the link.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"title":{"type":"string","maxLength":200},
				"org_id":{"type":"string","format":"uuid"},
				"access":{"type":"string","enum":["org","invited","open"],"description":"org: members of org_id join; invited: only invitees join, others ask; open: anyone with the link can ask to join (default)"},
				"starts_at":{"type":"string","format":"date-time"},
				"ends_at":{"type":"string","format":"date-time"}},
				"additionalProperties":false}`),
			ReadOnly: false,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in createPreview
				_ = json.Unmarshal(input, &in)
				// The reviewer's zone, else the UTC offset the agent wrote the time in.
				var loc *time.Location
				if in.StartsAt != nil {
					loc = in.StartsAt.Location()
				}
				loc = zoneFor(ctx, caller.UserID, loc)
				org := ""
				if in.OrgID != nil {
					if err := s.d.Pool.QueryRow(ctx, `SELECT o.name FROM orgs o JOIN org_members m ON m.org_id = o.id AND m.user_id = $2 WHERE o.id = $1`,
						*in.OrgID, caller.UserID).Scan(&org); err != nil && !db.IsNoRows(err) {
						return app.ToolPreview{}, err
					}
				}
				return app.ToolPreview{Summary: createSummary(in, loc), After: createDetails(in, org, loc)}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				if !caller.Active() {
					return nil, httpx.ErrSuspended
				}
				var in struct {
					Title    string     `json:"title"`
					OrgID    *uuid.UUID `json:"org_id"`
					Access   string     `json:"access"`
					StartsAt *time.Time `json:"starts_at"`
					EndsAt   *time.Time `json:"ends_at"`
				}
				if err := json.Unmarshal(input, &in); err != nil {
					return nil, httpx.BadRequest("Invalid input: " + err.Error())
				}
				if in.OrgID != nil {
					role, err := orgs.RoleOf(ctx, s.d.Pool, *in.OrgID, caller.UserID)
					if err != nil {
						return nil, err
					}
					if role == "" {
						return nil, httpx.NotFound(orgs.NounTitle() + " not found.")
					}
				}
				// The REST route allows 30 per hour; tools bypass route limits, so count here.
				var recent int
				if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calls_meetings WHERE host_id = $1 AND created_at > now() - interval '1 hour'`,
					caller.UserID).Scan(&recent); err != nil {
					return nil, err
				}
				if recent >= 30 {
					return nil, httpx.ErrRateLimited
				}
				var m Meeting
				err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
					var err error
					m, err = createMeeting(ctx, tx, MeetingInput{HostID: caller.UserID, OrgID: in.OrgID, Title: in.Title, Access: in.Access, StartsAt: in.StartsAt, EndsAt: in.EndsAt})
					return err
				})
				return m, err
			},
		},
	}
}

// Search finds meetings by title that the caller can join without asking
// (each hit re-checked with accessFor, the REST rule, and the agent scope
// when an agent searches). Hits name their org (OrgID, Context "Org ·
// Calls"); a search scoped to an org (app.SearchOrg) keeps that org's
// meetings and ones in none.
func (s *Service) Search(ctx context.Context, caller *auth.Identity, query string, limit int) ([]app.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return []app.SearchResult{}, nil
	}
	var scope *uuid.UUID
	if id, ok := app.SearchOrg(ctx); ok {
		scope = &id
	}
	pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
	rows, err := s.d.Pool.Query(ctx, meetingSelect+`
		WHERE m.canceled_at IS NULL AND m.title <> '' AND m.title ILIKE $2
		  AND ($4::uuid IS NULL OR m.org_id IS NULL OR m.org_id = $4)
		  AND (m.host_id = $1
		       OR EXISTS (SELECT 1 FROM calls_invitees i WHERE i.meeting_id = m.id AND i.user_id = $1)
		       OR (m.org_id IS NOT NULL AND m.access IN ('org', 'open')
		           AND EXISTS (SELECT 1 FROM org_members om WHERE om.org_id = m.org_id AND om.user_id = $1)))
		ORDER BY COALESCE(m.starts_at, m.created_at) DESC, m.id DESC LIMIT $3`, caller.UserID, pattern, limit, scope)
	if err != nil {
		return nil, err
	}
	list, err := scanMeetings(rows)
	if err != nil {
		return nil, err
	}
	out := []app.SearchResult{}
	lower := strings.ToLower(query)
	zone := zoneFor(ctx, caller.UserID, nil)
	for i := range list {
		m := &list[i]
		a, err := s.accessFor(ctx, s.d.Pool, m, caller.UserID)
		if err != nil {
			return nil, err
		}
		if !a.canJoin() {
			continue
		}
		if ok, err := s.agentMay(ctx, caller, m.ID); err != nil || !ok {
			if err != nil {
				return nil, err
			}
			continue
		}
		score := 0.5
		if strings.HasPrefix(strings.ToLower(m.Title), lower) {
			score = 0.8
		}
		at := m.CreatedAt
		snippet := m.Code
		if m.StartsAt != nil {
			at = *m.StartsAt
			snippet = whenText(*m.StartsAt, m.EndsAt, zone) + " · " + m.Code
		}
		hit := app.SearchResult{
			Kind: AgentKindMeeting, ID: m.ID.String(), Title: m.Title, Snippet: snippet,
			Link: "/meet/" + m.Code, Icon: "video", Score: score, At: at, Context: app.SearchContext("Calls"),
		}
		if m.OrgID != nil {
			orgName := ""
			if m.OrgName != nil {
				orgName = *m.OrgName
			}
			hit.OrgID, hit.Context = m.OrgID.String(), app.SearchContext(orgName, "Calls")
		}
		out = append(out, hit)
	}
	return out, nil
}

// createPreview is the part of a calls_create_meeting call its preview reads.
type createPreview struct {
	Title    string     `json:"title"`
	OrgID    *uuid.UUID `json:"org_id"`
	Access   string     `json:"access"`
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

// createDetails is everything the meeting would be, for the reviewer: who
// can join matters as much as the title (an open link takes guests).
func createDetails(in createPreview, org string, loc *time.Location) string {
	title := trimTitle(in.Title)
	if title == "" {
		title = "(no title)"
	}
	when := "Unscheduled link"
	if in.StartsAt != nil {
		when = whenText(*in.StartsAt, in.EndsAt, loc)
	}
	lines := []string{"Title: " + title, "When: " + when}
	if org != "" {
		lines = append(lines, orgs.NounTitle()+": "+org)
	}
	var who string
	switch in.Access {
	case AccessOrg:
		who = "members of the " + orgs.Noun() + " join directly; nobody else"
	case AccessInvited:
		who = "only invited people join; others ask the host"
	default:
		who = "anyone with the link can ask to join, guests without an account too; the host lets them in"
		if org != "" {
			who += " (members of " + org + " join directly)"
		}
	}
	lines = append(lines, "Who can join: "+who, "Nobody is invited or notified.")
	return strings.Join(lines, "\n")
}

// createSummary words a create call for its reviewer, times in loc:
// "Create a meeting “Design review” on Mon, Nov 2, 2026 · 7:00 AM – 8:00 AM PST".
func createSummary(in createPreview, loc *time.Location) string {
	summary := "Create a meeting link"
	if t := trimTitle(in.Title); t != "" {
		summary = "Create a meeting “" + t + "”"
	}
	if in.StartsAt != nil {
		summary += " on " + whenText(*in.StartsAt, in.EndsAt, loc)
	}
	return summary
}

// agentMay applies the user's AI agent scope to one meeting: true unless
// the request is an agent call the scope keeps away from it (agents feature).
func (s *Service) agentMay(ctx context.Context, caller *auth.Identity, meetingID uuid.UUID) (bool, error) {
	return app.AgentAllows(ctx, s.d.Modules, caller, "calls", AgentKindMeeting, meetingID.String(), app.AgentRead)
}
