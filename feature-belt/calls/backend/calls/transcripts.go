package calls

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Transcripts and saved chat. Captions run in each speaker's own browser
// (speech recognition, opt-in per workspace and per person); while a host
// has the transcript on, each speaker's client also sends its own final
// lines here, so nobody's speech is saved without them captioning it. A
// server-side provider can add lines through AddTranscriptLines instead.
// Saved chat works the same way: with the meeting's save_chat on, each
// sender stores their own message. Both are read under the records rule.

// Transcript is a call's saved transcript.
type Transcript struct {
	SessionID uuid.UUID        `json:"session_id"`
	MeetingID uuid.UUID        `json:"meeting_id"`
	Title     string           `json:"title"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   *time.Time       `json:"ended_at"`
	Lines     []TranscriptLine `json:"lines"`
}

// TranscriptLine is one spoken line.
type TranscriptLine struct {
	Name     string     `json:"name"`
	UserID   *uuid.UUID `json:"user_id"`
	Text     string     `json:"text"`
	SpokenAt time.Time  `json:"spoken_at"`
}

// ChatMessage is one saved chat message.
type ChatMessage struct {
	ID     uuid.UUID  `json:"id"`
	Name   string     `json:"name"`
	UserID *uuid.UUID `json:"user_id"`
	Text   string     `json:"text"`
	SentAt time.Time  `json:"sent_at"`
}

// TranscriptReady is what OnTranscriptReady hooks get when a call that
// saved a transcript ends (e.g. to have the assistant summarise it into a
// docs page linked from the calendar event: Source says which event).
type TranscriptReady struct {
	MeetingID uuid.UUID
	SessionID uuid.UUID
	Title     string
	HostID    uuid.UUID
	OrgID     *uuid.UUID
	Source    *Source
	StartedAt time.Time
	EndedAt   time.Time
	Lines     int
}

var (
	transcriptHooksMu sync.RWMutex
	transcriptHooks   []func(context.Context, TranscriptReady)
)

// OnTranscriptReady registers fn to run (in the background, best-effort)
// after a call with a saved transcript ends. Read it with ReadTranscript.
func OnTranscriptReady(fn func(ctx context.Context, t TranscriptReady)) {
	transcriptHooksMu.Lock()
	defer transcriptHooksMu.Unlock()
	transcriptHooks = append(transcriptHooks, fn)
}

// ReadTranscript returns a session's transcript. It doesn't check who is
// asking: the caller has (or checks CanReadRecords).
func ReadTranscript(ctx context.Context, q db.Querier, sessionID uuid.UUID) (Transcript, error) {
	var t Transcript
	err := q.QueryRow(ctx, `SELECT ss.id, ss.meeting_id, m.title, ss.started_at, ss.ended_at
		FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id WHERE ss.id = $1`, sessionID).
		Scan(&t.SessionID, &t.MeetingID, &t.Title, &t.StartedAt, &t.EndedAt)
	if db.IsNoRows(err) {
		return Transcript{}, errCallHistoryGone
	}
	if err != nil {
		return Transcript{}, err
	}
	rows, err := q.Query(ctx, `SELECT l.name, l.user_id, u.display_name, u.status, l.body, l.spoken_at
		FROM calls_transcript_lines l LEFT JOIN users u ON u.id = l.user_id
		WHERE l.session_id = $1 ORDER BY l.spoken_at, l.id LIMIT 20000`, sessionID)
	if err != nil {
		return Transcript{}, err
	}
	defer rows.Close()
	t.Lines = []TranscriptLine{}
	for rows.Next() {
		var l TranscriptLine
		var uname, ustatus *string
		if err := rows.Scan(&l.Name, &l.UserID, &uname, &ustatus, &l.Text, &l.SpokenAt); err != nil {
			return Transcript{}, err
		}
		if l.UserID != nil {
			l.Name = userRef(*l.UserID, deref(uname), nil, deref(ustatus)).DisplayName
		}
		t.Lines = append(t.Lines, l)
	}
	return t, rows.Err()
}

// TranscriptLineInput is a line a server-side transcriber adds.
type TranscriptLineInput struct {
	Identity string // "u:<user id>" or "g:<lobby id>"
	Name     string
	Text     string
	SpokenAt time.Time
}

// AddTranscriptLines stores lines for a live session from a server-side
// transcriber (a provider behind your own keys). The meeting's workspace
// must allow transcripts and a host must have the transcript on.
func AddTranscriptLines(ctx context.Context, q db.Querier, sessionID uuid.UUID, lines []TranscriptLineInput) error {
	var on bool
	if err := q.QueryRow(ctx, `SELECT ss.transcribing AND ss.ended_at IS NULL AND COALESCE(os.transcripts, false)
		FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id LEFT JOIN calls_org_settings os ON os.org_id = m.org_id
		WHERE ss.id = $1`, sessionID).Scan(&on); err != nil {
		return err
	}
	if !on {
		return errNotTranscribing
	}
	for _, l := range lines {
		guest, id, ok := parseIdentity(l.Identity)
		if !ok {
			return httpx.BadRequest("Each line needs a participant identity.")
		}
		var uid *uuid.UUID
		if !guest {
			uid = &id
		}
		if err := insertLine(ctx, q, sessionID, l.Identity, uid, l.Name, l.Text, l.SpokenAt); err != nil {
			return err
		}
	}
	return nil
}

var errNotTranscribing = httpx.NewError(http.StatusConflict, "calls_not_transcribing", "The transcript isn't being saved.")

func cleanText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) {
		return "", false
	}
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s, true
}

func insertLine(ctx context.Context, q db.Querier, sessionID uuid.UUID, identity string, uid *uuid.UUID, name, text string, at time.Time) error {
	body, ok := cleanText(text, 2000)
	if !ok {
		return nil
	}
	now := time.Now()
	if at.IsZero() || at.After(now) || at.Before(now.Add(-10*time.Minute)) {
		at = now
	}
	_, err := q.Exec(ctx, `INSERT INTO calls_transcript_lines (session_id, identity, user_id, name, body, spoken_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, identity, uid, name, body, at.UTC())
	return err
}

// setTranscribing: PUT /calls/meetings/{id}/transcribing {on} (hosts, live;
// the workspace must allow transcripts). Everyone sees a notice.
func (s *Service) setTranscribing(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req struct {
		On bool `json:"on"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.On {
		org, err := orgSettings(ctx, s.d.Pool, m.OrgID)
		if err != nil {
			return err
		}
		if !org.Transcripts {
			return httpx.NewError(http.StatusForbidden, "calls_transcripts_off", "Saving transcripts is turned off for this "+orgs.Noun()+"'s calls.")
		}
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE calls_sessions SET transcribing = $2 WHERE id = $1`, *m.LiveSession, req.On); err != nil {
		return err
	}
	s.syncRoom(ctx, m.ID)
	httpx.NoContent(w)
	return nil
}

// postTranscript: POST …/transcript {lines: [{text, spoken_at}]} (anyone in
// the call, their own speech only, while the transcript is on).
func (s *Service) postTranscript(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireLive(); err != nil {
		return err
	}
	var req struct {
		Lines []struct {
			Text     string    `json:"text"`
			SpokenAt time.Time `json:"spoken_at"`
		} `json:"lines"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Lines) == 0 || len(req.Lines) > 20 {
		return httpx.BadRequest("Send 1 to 20 lines.")
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var on bool
		if err := tx.QueryRow(ctx, `SELECT ss.transcribing AND COALESCE(os.transcripts, false) FROM calls_sessions ss
			JOIN calls_meetings m ON m.id = ss.meeting_id LEFT JOIN calls_org_settings os ON os.org_id = m.org_id WHERE ss.id = $1`,
			*a.m.LiveSession).Scan(&on); err != nil {
			return err
		}
		if !on {
			return errNotTranscribing
		}
		for _, l := range req.Lines {
			if err := insertLine(ctx, tx, *a.m.LiveSession, a.identity, a.userID, a.name, l.Text, l.SpokenAt); err != nil {
				return err
			}
		}
		httpx.NoContent(w)
		return nil
	})
}

// postChat: POST …/chat {text} (anyone in the call allowed to chat, while
// the meeting saves chat): keeps a copy of a message the sender sent.
func (s *Service) postChat(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireLive(); err != nil {
		return err
	}
	if !a.m.Rules.SaveChat {
		return httpx.NewError(http.StatusConflict, "calls_chat_not_saved", "This meeting doesn't save chat.")
	}
	if !a.m.Rules.AllowChat && !a.host {
		return httpx.NewError(http.StatusForbidden, "calls_chat_off", "A host turned off chat.")
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body, ok := cleanText(req.Text, 2000)
	if !ok {
		return httpx.BadRequest("Write a message.")
	}
	if _, err := s.d.Pool.Exec(ctx, `INSERT INTO calls_chat_messages (session_id, identity, user_id, name, body) VALUES ($1, $2, $3, $4, $5)`,
		*a.m.LiveSession, a.identity, a.userID, a.name, body); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// sessionRecords loads a session the caller may read records of (404 otherwise).
func (s *Service) sessionRecords(ctx context.Context, sessionID, userID uuid.UUID) (meetingID uuid.UUID, host bool, err error) {
	var hostID uuid.UUID
	err = s.d.Pool.QueryRow(ctx, `SELECT m.id, m.host_id FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id
		WHERE ss.id = $2 AND `+recordsVisibleSQL("$1", "ss.id"), userID, sessionID).Scan(&meetingID, &hostID)
	if db.IsNoRows(err) {
		return uuid.Nil, false, errCallHistoryGone
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	host, err = isMeetingHost(ctx, s.d.Pool, meetingID, hostID, userID)
	return meetingID, host, err
}

// CanReadRecords reports whether userID may open a session's recordings,
// transcript and saved chat (the meeting's records rule).
func CanReadRecords(ctx context.Context, q db.Querier, sessionID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id
		WHERE ss.id = $2 AND `+recordsVisibleSQL("$1", "ss.id")+`)`, userID, sessionID).Scan(&ok)
	return ok, err
}

// getTranscript: GET /calls/history/{id}/transcript (the records rule).
func (s *Service) getTranscript(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errCallHistoryGone
	}
	if _, _, err := s.sessionRecords(ctx, id, me.UserID); err != nil {
		return err
	}
	t, err := ReadTranscript(ctx, s.d.Pool, id)
	if err != nil {
		return err
	}
	httpx.OK(w, t)
	return nil
}

// getSavedChat: GET /calls/history/{id}/chat (the records rule).
func (s *Service) getSavedChat(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errCallHistoryGone
	}
	if _, _, err := s.sessionRecords(ctx, id, me.UserID); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT c.id, c.name, c.user_id, u.display_name, u.status, c.body, c.sent_at
		FROM calls_chat_messages c LEFT JOIN users u ON u.id = c.user_id WHERE c.session_id = $1 ORDER BY c.sent_at, c.id LIMIT 5000`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var c ChatMessage
		var uname, ustatus *string
		if err := rows.Scan(&c.ID, &c.Name, &c.UserID, &uname, &ustatus, &c.Text, &c.SentAt); err != nil {
			return err
		}
		if c.UserID != nil {
			c.Name = userRef(*c.UserID, deref(uname), nil, deref(ustatus)).DisplayName
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.Page[ChatMessage]{Items: out})
	return nil
}

// attendanceCSV: GET /calls/history/{id}/attendance.csv (hosts and
// co-hosts): one row per person with first join, last leave, minutes and joins.
func (s *Service) attendanceCSV(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return errCallHistoryGone
	}
	d, err := s.callDetail(ctx, id, me.UserID)
	if err != nil {
		return err
	}
	var hostID uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `SELECT host_id FROM calls_meetings WHERE id = $1`, d.Meeting.ID).Scan(&hostID); err != nil {
		return err
	}
	host, err := isMeetingHost(ctx, s.d.Pool, d.Meeting.ID, hostID, me.UserID)
	if err != nil {
		return err
	}
	if !host {
		return httpx.Forbidden("Only the meeting's hosts can download attendance.")
	}
	title := d.Meeting.Title
	if title == "" {
		title = "call"
	}
	name := strings.NewReplacer("/", "-", "\\", "-", "\"", "", "\n", " ", ",", " ").Replace(title) + " " + d.StartedAt.UTC().Format("2006-01-02") + " attendance.csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"Name", "Guest", "First joined (UTC)", "Last left (UTC)", "Minutes", "Joins"})
	for _, p := range d.Participants {
		left := ""
		if p.LastLeftAt != nil {
			left = p.LastLeftAt.UTC().Format(time.RFC3339)
		}
		nm := p.Name
		if p.User != nil {
			nm = p.User.DisplayName
		}
		_ = cw.Write([]string{csvSafe(nm), strconv.FormatBool(p.Guest), p.FirstJoinedAt.UTC().Format(time.RFC3339), left,
			strconv.FormatFloat(float64(p.Seconds)/60, 'f', 1, 64), strconv.Itoa(p.Joins)})
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe keeps a typed name from running as a spreadsheet formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

// afterSessionClosed runs the transcript hooks for a session that just
// ended with saved lines (once: callers pass sessions they closed).
func (s *Service) afterSessionClosed(ctx context.Context, sessionID uuid.UUID) {
	transcriptHooksMu.RLock()
	hooks := append([]func(context.Context, TranscriptReady){}, transcriptHooks...)
	transcriptHooksMu.RUnlock()
	if len(hooks) == 0 || sessionID == uuid.Nil {
		return
	}
	var t TranscriptReady
	var srcKind, srcID *string
	var ended *time.Time
	err := s.d.Pool.QueryRow(ctx, `SELECT ss.meeting_id, ss.id, m.title, m.host_id, m.org_id, m.source_kind, m.source_id, ss.started_at, ss.ended_at,
		(SELECT count(*) FROM calls_transcript_lines l WHERE l.session_id = ss.id)
		FROM calls_sessions ss JOIN calls_meetings m ON m.id = ss.meeting_id WHERE ss.id = $1`, sessionID).
		Scan(&t.MeetingID, &t.SessionID, &t.Title, &t.HostID, &t.OrgID, &srcKind, &srcID, &t.StartedAt, &ended, &t.Lines)
	if err != nil || t.Lines == 0 || ended == nil {
		return
	}
	t.EndedAt = *ended
	if srcKind != nil && srcID != nil {
		t.Source = &Source{Kind: *srcKind, ID: *srcID}
	}
	bg.Go(ctx, "calls.transcript_ready", func(ctx context.Context) error {
		for _, fn := range hooks {
			func() {
				defer func() {
					if p := recover(); p != nil {
						slog.ErrorContext(ctx, "calls: transcript hook panicked", "panic", p)
					}
				}()
				fn(ctx, t)
			}()
		}
		return nil
	})
}
