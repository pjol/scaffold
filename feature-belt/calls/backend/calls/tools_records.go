package calls

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// TranscriptRef is a saved transcript in a list (agents).
type TranscriptRef struct {
	SessionID    uuid.UUID `json:"session_id"`
	MeetingID    uuid.UUID `json:"meeting_id"`
	MeetingTitle string    `json:"meeting_title"`
	StartedAt    time.Time `json:"started_at"`
	Lines        int       `json:"lines"`
}

// recordTools: recordings and transcripts the user may open (the records
// rule, then the AI agent scope per meeting), reading one transcript, and
// creating a poll in a meeting the user hosts.
func (s *Service) recordTools() []app.Tool {
	return []app.Tool{
		{
			Name:        "calls_list_recordings",
			Description: "List call recordings and saved transcripts the user can open, newest first, optionally for one meeting (meeting_id). Recordings come without playback links; open them in the app.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"meeting_id":{"type":"string","format":"uuid"},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					MeetingID *uuid.UUID `json:"meeting_id"`
					Limit     int        `json:"limit"`
				}
				if err := json.Unmarshal(input, &in); err != nil && len(strings.TrimSpace(string(input))) > 0 {
					return nil, httpx.BadRequest("Invalid input: " + err.Error())
				}
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 20
				}
				where, args := `true`, []any{}
				if in.MeetingID != nil {
					where, args = `r.meeting_id = $2`, []any{*in.MeetingID}
				}
				recs, err := visibleRecordings(ctx, s.d.Pool, caller.UserID, where, args...)
				if err != nil {
					return nil, err
				}
				out := struct {
					Recordings  []Recording     `json:"recordings"`
					Transcripts []TranscriptRef `json:"transcripts"`
				}{[]Recording{}, []TranscriptRef{}}
				for i := range recs {
					if len(out.Recordings) >= in.Limit {
						break
					}
					if ok, err := s.agentMay(ctx, caller, recs[i].MeetingID); err != nil || !ok {
						if err != nil {
							return nil, err
						}
						continue
					}
					if err := s.finishRecording(ctx, &recs[i], caller.UserID, false); err != nil {
						return nil, err
					}
					out.Recordings = append(out.Recordings, recs[i].Recording)
				}
				rows, err := s.d.Pool.Query(ctx, `
					SELECT ss.id, ss.meeting_id, m.title, ss.started_at, (SELECT count(*) FROM calls_transcript_lines l WHERE l.session_id = ss.id)::int
					FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id
					WHERE EXISTS (SELECT 1 FROM calls_transcript_lines l WHERE l.session_id = ss.id)
					  AND ($2::uuid IS NULL OR ss.meeting_id = $2) AND `+recordsVisibleSQL("$1", "ss.id")+`
					ORDER BY ss.started_at DESC, ss.id DESC LIMIT 200`, caller.UserID, in.MeetingID)
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				for rows.Next() {
					var t TranscriptRef
					if err := rows.Scan(&t.SessionID, &t.MeetingID, &t.MeetingTitle, &t.StartedAt, &t.Lines); err != nil {
						return nil, err
					}
					if len(out.Transcripts) >= in.Limit {
						break
					}
					if ok, err := s.agentMay(ctx, caller, t.MeetingID); err != nil || !ok {
						if err != nil {
							return nil, err
						}
						continue
					}
					out.Transcripts = append(out.Transcripts, t)
				}
				return out, rows.Err()
			},
		},
		{
			Name:        "calls_read_transcript",
			Description: "Read the saved transcript of one call (session_id from calls_list_recordings or calls_list_recent): who said what, in order.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","format":"uuid"}},"required":["session_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					SessionID uuid.UUID `json:"session_id"`
				}
				if err := json.Unmarshal(input, &in); err != nil || in.SessionID == uuid.Nil {
					return nil, httpx.BadRequest("session_id must be a call's id.")
				}
				meetingID, _, err := s.sessionRecords(ctx, in.SessionID, caller.UserID)
				if err != nil {
					return nil, err
				}
				if ok, err := s.agentMay(ctx, caller, meetingID); err != nil || !ok {
					if err != nil {
						return nil, err
					}
					return nil, errCallHistoryGone
				}
				return ReadTranscript(ctx, s.d.Pool, in.SessionID)
			},
		},
		{
			Name:        "calls_create_poll",
			Description: "Create a poll in a meeting the user hosts or co-hosts (meeting_id). Saved for later unless launch is true and the call is live; the user launches saved polls from the call.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{
				"meeting_id":{"type":"string","format":"uuid"},
				"question":{"type":"string","maxLength":300},
				"options":{"type":"array","items":{"type":"string","maxLength":100},"minItems":2,"maxItems":10},
				"multiple":{"type":"boolean","description":"people may pick more than one option"},
				"anonymous":{"type":"boolean","description":"hosts don't see who voted for what (default true)"},
				"launch":{"type":"boolean","description":"show it to everyone in the live call now"}},
				"required":["meeting_id","question","options"],"additionalProperties":false}`),
			ReadOnly: false,
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					MeetingID string `json:"meeting_id"`
				}
				_ = json.Unmarshal(input, &in)
				return AgentKindMeeting, in.MeetingID
			},
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in pollToolInput
				_ = json.Unmarshal(input, &in)
				title := "a meeting"
				if in.MeetingID != uuid.Nil {
					if m, err := loadMeeting(ctx, s.d.Pool, in.MeetingID); err == nil {
						if a, err := s.accessFor(ctx, s.d.Pool, m, caller.UserID); err == nil && a.canJoin() {
							title = "“" + m.Title + "”"
							if m.Title == "" {
								title = "meeting " + m.Code
							}
						}
					}
				}
				return app.ToolPreview{Summary: pollSummary(in, title), After: pollDetails(in), Link: "/calls"}, nil
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				if !caller.Active() {
					return nil, httpx.ErrSuspended
				}
				var in pollToolInput
				if err := json.Unmarshal(input, &in); err != nil {
					return nil, httpx.BadRequest("Invalid input: " + err.Error())
				}
				m, err := loadMeeting(ctx, s.d.Pool, in.MeetingID)
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
				if !a.isHost() {
					return nil, errNotHost
				}
				// Tools bypass route limits: count here (the REST route allows 60 a minute).
				var recent int
				if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calls_polls WHERE created_by = $1 AND created_at > now() - interval '1 minute'`, caller.UserID).Scan(&recent); err != nil {
					return nil, err
				}
				if recent >= 20 {
					return nil, httpx.ErrRateLimited
				}
				uid := caller.UserID
				id, err := s.createPoll(ctx, m, &uid, PollInput{Question: in.Question, Options: in.Options, Multiple: in.Multiple, Anonymous: in.Anonymous, Open: in.Launch})
				if err != nil {
					return nil, err
				}
				polls, err := s.listPolls(ctx, s.d.Pool, &actor{m: m, identity: userIdentity(uid), userID: &uid, host: true}, m.ID)
				if err != nil {
					return nil, err
				}
				for _, p := range polls {
					if p.ID == id {
						return p, nil
					}
				}
				return map[string]any{"id": id}, nil
			},
		},
	}
}

type pollToolInput struct {
	MeetingID uuid.UUID `json:"meeting_id"`
	Question  string    `json:"question"`
	Options   []string  `json:"options"`
	Multiple  bool      `json:"multiple"`
	Anonymous *bool     `json:"anonymous"`
	Launch    bool      `json:"launch"`
}

// pollSummary: "Create a poll “Lunch?” in “Team sync”" (+ " and launch it now").
func pollSummary(in pollToolInput, meeting string) string {
	q := strings.Join(strings.Fields(in.Question), " ")
	if q == "" {
		q = "(no question)"
	}
	out := "Create a poll “" + q + "” in " + meeting
	if in.Launch {
		out += " and launch it now"
	}
	return out
}

// pollDetails lists the options and how voting works, for the reviewer.
func pollDetails(in pollToolInput) string {
	lines := []string{}
	for i, o := range in.Options {
		lines = append(lines, string(rune('A'+i))+". "+strings.TrimSpace(o))
	}
	if in.Multiple {
		lines = append(lines, "People may pick more than one.")
	}
	if in.Anonymous == nil || *in.Anonymous {
		lines = append(lines, "Anonymous: hosts see totals, not who voted.")
	} else {
		lines = append(lines, "Hosts see who voted for what.")
	}
	if !in.Launch {
		lines = append(lines, "Saved for later; nobody sees it until a host launches it.")
	}
	return strings.Join(lines, "\n")
}
