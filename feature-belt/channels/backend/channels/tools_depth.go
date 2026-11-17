package channels

// Agent tools added in 1.2: schedule a message, set a reminder, create a
// poll. Same checks as the REST routes, narrowed by the caller's agent scope;
// each names its target and previews exactly what it would do, so a Suggest
// scope turns it into a suggestion.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

type scheduleInput struct {
	ChannelID    uuid.UUID  `json:"channel_id"`
	Text         string     `json:"text"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
	SendAt       *time.Time `json:"send_at"`
	When         string     `json:"when"`
	TZ           string     `json:"tz"`
}

type remindInput struct {
	OrgID     *uuid.UUID `json:"org_id"`
	MessageID *uuid.UUID `json:"message_id"`
	Text      string     `json:"text"`
	SendAt    *time.Time `json:"send_at"`
	When      string     `json:"when"`
	TZ        string     `json:"tz"`
}

type pollInput struct {
	ChannelID    uuid.UUID  `json:"channel_id"`
	Question     string     `json:"question"`
	Options      []string   `json:"options"`
	Multiple     bool       `json:"multiple"`
	Anonymous    bool       `json:"anonymous"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
}

// agentWriteGate: active account, the agent may read the channel, and the
// per-person message rate (the routes' limit).
func (s *Service) agentWriteGate(ctx context.Context, caller *auth.Identity, channelID uuid.UUID) error {
	if caller.Status != "active" {
		return httpx.ErrSuspended
	}
	if err := s.agentRequire(ctx, caller, channelID); err != nil {
		return err
	}
	var recent int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM channel_messages WHERE sender_id = $1 AND created_at > now() - interval '1 minute'`,
		caller.UserID).Scan(&recent); err != nil {
		return err
	}
	if recent >= s.cfg.perMinute {
		return httpx.NewError(http.StatusTooManyRequests, "rate_limited", "Too many messages. Try again in a minute.")
	}
	return nil
}

// whereName is "#design" or a DM's people, as the caller sees it.
func (s *Service) whereName(ctx context.Context, caller *auth.Identity, channelID uuid.UUID) (string, *access, error) {
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, caller.UserID)
	if err != nil {
		return "", nil, err
	}
	refs, err := s.channelRefs(ctx, caller.UserID, []uuid.UUID{channelID})
	if err != nil {
		return "", nil, err
	}
	if a.direct() {
		return refs[channelID].Name, a, nil
	}
	return "#" + refs[channelID].Name, a, nil
}

func previewTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon 2 Jan 2006, 15:04 MST")
}

func (s *Service) toolSchedule(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[scheduleInput](input)
	if err != nil {
		return nil, err
	}
	if err := s.agentWriteGate(ctx, caller, in.ChannelID); err != nil {
		return nil, err
	}
	item, err := s.schedule(ctx, caller.UserID, in.ChannelID, scheduleReq{Body: in.Text, ThreadRootID: in.ThreadRootID, SendAt: in.SendAt, When: in.When, TZ: in.TZ})
	if err != nil {
		return nil, err
	}
	return map[string]any{"scheduled": map[string]any{"id": item.ID, "channel": item.Channel, "send_at": item.SendAt, "text": plainMentions(item.Body, item.Mentions)},
		"link": "/channels/drafts?tab=scheduled"}, nil
}

func (s *Service) schedulePreview(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, err := decodeTool[scheduleInput](input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	where, a, err := s.whereName(ctx, caller, in.ChannelID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	loc := zoneFor(ctx, s.d.Pool, caller.UserID, a.OrgID, in.TZ)
	at, err := resolveSendAt(in.SendAt, in.When, time.Now(), loc)
	if err != nil {
		return app.ToolPreview{}, err
	}
	what := "Schedule a message in " + where
	if in.ThreadRootID != nil {
		what = "Schedule a thread reply in " + where
	}
	return app.ToolPreview{Summary: what + " for " + previewTime(at, loc), After: in.Text, Link: "/channels/" + in.ChannelID.String()}, nil
}

func (s *Service) toolRemind(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[remindInput](input)
	if err != nil {
		return nil, err
	}
	orgID, err := s.remindOrg(ctx, caller, in)
	if err != nil {
		return nil, err
	}
	rem, err := s.remind(ctx, caller.UserID, orgID, reminderReq{MessageID: in.MessageID, Text: in.Text, At: in.SendAt, When: in.When, TZ: in.TZ})
	if err != nil {
		return nil, err
	}
	return map[string]any{"reminder": map[string]any{"id": rem.ID, "text": rem.Text, "remind_at": rem.RemindAt, "message_id": in.MessageID},
		"link": "/channels/later?tab=reminders"}, nil
}

// remindOrg finds the org of a reminder: the message's, else org_id (a
// member's), checking the agent may read the message's channel.
func (s *Service) remindOrg(ctx context.Context, caller *auth.Identity, in remindInput) (uuid.UUID, error) {
	if in.MessageID != nil {
		m, err := s.loadMessage(ctx, s.d.Pool, *in.MessageID, caller.UserID)
		if err != nil {
			return uuid.Nil, err
		}
		if err := s.agentRequire(ctx, caller, m.ChannelID); err != nil {
			return uuid.Nil, errMessageNotFound
		}
		return m.OrgID, nil
	}
	if in.OrgID == nil {
		return uuid.Nil, httpx.BadRequest("Give message_id, or org_id with text.")
	}
	role, err := orgs.RoleOf(ctx, s.d.Pool, *in.OrgID, caller.UserID)
	if err != nil {
		return uuid.Nil, err
	}
	if role == "" {
		return uuid.Nil, httpx.NotFound(orgs.NounTitle() + " not found.")
	}
	return *in.OrgID, nil
}

// remindTarget: a reminder about a message acts on that message's channel
// for agent scopes; a text reminder on the feature.
func remindTarget(json.RawMessage) (kind, id string) {
	return agentKind, ""
}

func (s *Service) remindPreview(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, err := decodeTool[remindInput](input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	orgID, err := s.remindOrg(ctx, caller, in)
	if err != nil {
		return app.ToolPreview{}, err
	}
	loc := zoneFor(ctx, s.d.Pool, caller.UserID, orgID, in.TZ)
	at, err := resolveSendAt(in.SendAt, in.When, time.Now(), loc)
	if err != nil {
		return app.ToolPreview{}, err
	}
	summary := "Remind you " + previewTime(at, loc)
	after := strings.TrimSpace(in.Text)
	link := "/channels/later?tab=reminders"
	if in.MessageID != nil {
		msg, err := s.message(ctx, s.d.Pool, *in.MessageID, caller.UserID)
		if err != nil {
			return app.ToolPreview{}, err
		}
		summary += " about a message"
		if after == "" {
			after = preview(msg)
		}
		link = "/channels/" + msg.ChannelID.String() + "?message=" + msg.ID.String()
	}
	return app.ToolPreview{Summary: summary, After: after, Link: link}, nil
}

func (s *Service) toolPoll(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	in, err := decodeTool[pollInput](input)
	if err != nil {
		return nil, err
	}
	if err := s.agentWriteGate(ctx, caller, in.ChannelID); err != nil {
		return nil, err
	}
	msg, _, err := s.postPoll(ctx, caller.UserID, in.ChannelID, pollReq{Question: in.Question, Options: in.Options, Multiple: in.Multiple,
		Anonymous: in.Anonymous, ThreadRootID: in.ThreadRootID})
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": toToolMessage(msg), "link": "/channels/" + msg.ChannelID.String() + "?message=" + msg.ID.String()}, nil
}

func (s *Service) pollPreview(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	in, err := decodeTool[pollInput](input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	where, _, err := s.whereName(ctx, caller, in.ChannelID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	q, opts, err := cleanPoll(in.Question, in.Options)
	if err != nil {
		return app.ToolPreview{}, err
	}
	kind := "one choice"
	if in.Multiple {
		kind = "several choices"
	}
	if in.Anonymous {
		kind += ", anonymous"
	}
	lines := []string{q + " (" + kind + ")"}
	for _, o := range opts {
		lines = append(lines, "• "+o)
	}
	return app.ToolPreview{Summary: "Post a poll in " + where, After: strings.Join(lines, "\n"), Link: "/channels/" + in.ChannelID.String()}, nil
}
