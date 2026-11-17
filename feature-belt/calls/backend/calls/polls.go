package calls

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Polls and questions belong to a meeting (polls can be prepared before the
// call and are kept after it). Everyone in the call reads them through
// GET /calls/meetings/{id}/room (guests: POST /calls/public/lobby/{id}/room);
// changes reach the room as SignalPolls / SignalQuestions, and clients refetch.

const (
	maxPollsPerMeeting     = 50
	maxQuestionsPerMeeting = 500
	maxPollOptions         = 10
	maxOptionChars         = 100
	maxQuestionChars       = 500
)

// Poll is a question with options people vote on.
type Poll struct {
	ID            uuid.UUID   `json:"id"`
	MeetingID     uuid.UUID   `json:"meeting_id"`
	Question      string      `json:"question"`
	Options       []string    `json:"options"`
	Multiple      bool        `json:"multiple"`
	Anonymous     bool        `json:"anonymous"` // hosts don't see who voted for what
	Status        string      `json:"status"`    // draft | open | closed
	ResultsShared bool        `json:"results_shared"`
	Counts        []int       `json:"counts"` // per option; hosts always, others once results are shared
	Voters        int         `json:"voters"`
	MyChoices     []int       `json:"my_choices"`
	Votes         []PollVoter `json:"votes,omitempty"` // hosts, polls that aren't anonymous
	OpenedAt      *time.Time  `json:"opened_at"`
	ClosedAt      *time.Time  `json:"closed_at"`
	CreatedAt     time.Time   `json:"created_at"`
}

// PollVoter is one person's vote on a poll that isn't anonymous.
type PollVoter struct {
	Name    string `json:"name"`
	Choices []int  `json:"choices"`
}

// Question is a question someone asked in a call.
type Question struct {
	ID        uuid.UUID `json:"id"`
	MeetingID uuid.UUID `json:"meeting_id"`
	Body      string    `json:"body"`
	Name      string    `json:"name"` // "Anonymous" for anonymous questions
	Anonymous bool      `json:"anonymous"`
	Status    string    `json:"status"` // pending | open | answered | hidden
	Votes     int       `json:"votes"`
	Voted     bool      `json:"voted"`
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

// RoomState is what someone in the call needs beyond the room metadata.
type RoomState struct {
	Identity  string      `json:"identity"`
	Host      bool        `json:"host"`
	Polls     []Poll      `json:"polls"`
	Questions []Question  `json:"questions"`
	Breakout  *MyBreakout `json:"breakout"`
}

// roomState: GET /calls/meetings/{id}/room, POST /calls/public/lobby/{id}/room.
func (s *Service) roomState(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	polls, err := s.listPolls(ctx, s.d.Pool, a, a.m.ID)
	if err != nil {
		return err
	}
	qs, err := listQuestions(ctx, s.d.Pool, a)
	if err != nil {
		return err
	}
	b, err := s.myBreakout(ctx, a)
	if err != nil {
		return err
	}
	httpx.OK(w, RoomState{Identity: a.identity, Host: a.host, Polls: polls, Questions: qs, Breakout: b})
	return nil
}

// ---- polls ----------------------------------------------------------------------

func (s *Service) listPolls(ctx context.Context, q db.Querier, a *actor, meetingID uuid.UUID) ([]Poll, error) {
	where := `WHERE p.meeting_id = $1`
	if !a.host {
		where += ` AND (p.status = 'open' OR (p.status = 'closed' AND p.results_shared))`
	}
	rows, err := q.Query(ctx, `
		SELECT p.id, p.meeting_id, p.question, p.options, p.multiple, p.anonymous, p.status, p.results_shared,
		       p.opened_at, p.closed_at, p.created_at,
		       (SELECT array_agg(c::int) FROM calls_poll_votes v, unnest(v.choices) c WHERE v.poll_id = p.id AND v.identity = $2),
		       (SELECT count(*) FROM calls_poll_votes v WHERE v.poll_id = p.id)
		FROM calls_polls p `+where+` ORDER BY p.created_at, p.id LIMIT 100`, meetingID, a.identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Poll{}
	for rows.Next() {
		var p Poll
		var mine []int
		if err := rows.Scan(&p.ID, &p.MeetingID, &p.Question, &p.Options, &p.Multiple, &p.Anonymous, &p.Status, &p.ResultsShared,
			&p.OpenedAt, &p.ClosedAt, &p.CreatedAt, &mine, &p.Voters); err != nil {
			return nil, err
		}
		p.MyChoices = mine
		if p.MyChoices == nil {
			p.MyChoices = []int{}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		p := &out[i]
		if !a.host && !p.ResultsShared {
			p.Voters = 0
			continue
		}
		p.Counts = make([]int, len(p.Options))
		vr, err := q.Query(ctx, `SELECT name, choices FROM calls_poll_votes WHERE poll_id = $1 ORDER BY created_at`, p.ID)
		if err != nil {
			return nil, err
		}
		for vr.Next() {
			var v PollVoter
			var choices []int16
			if err := vr.Scan(&v.Name, &choices); err != nil {
				vr.Close()
				return nil, err
			}
			for _, c := range choices {
				if int(c) < len(p.Counts) {
					p.Counts[c]++
				}
				v.Choices = append(v.Choices, int(c))
			}
			if a.host && !p.Anonymous {
				p.Votes = append(p.Votes, v)
			}
		}
		vr.Close()
		if err := vr.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PollInput is a new poll.
type PollInput struct {
	Question  string   `json:"question"`
	Options   []string `json:"options"`
	Multiple  bool     `json:"multiple"`
	Anonymous *bool    `json:"anonymous"` // default true
	Open      bool     `json:"open"`      // launch now (the call must be live)
}

func cleanPoll(in *PollInput) error {
	in.Question = strings.Join(strings.Fields(in.Question), " ")
	if in.Question == "" || len([]rune(in.Question)) > 300 {
		return httpx.BadRequest("Write a question of at most 300 characters.")
	}
	opts := []string{}
	for _, o := range in.Options {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			continue
		}
		if len([]rune(o)) > maxOptionChars {
			return httpx.BadRequest("Options can be at most 100 characters.")
		}
		if slices.Contains(opts, o) {
			return httpx.BadRequest("Each option must be different.")
		}
		opts = append(opts, o)
	}
	if len(opts) < 2 || len(opts) > maxPollOptions {
		return httpx.BadRequest("A poll needs 2 to 10 options.")
	}
	in.Options = opts
	return nil
}

// createPoll adds a poll to meeting m as a host (hosts are checked by the caller).
func (s *Service) createPoll(ctx context.Context, m *meetingRow, by *uuid.UUID, in PollInput) (uuid.UUID, error) {
	if err := cleanPoll(&in); err != nil {
		return uuid.Nil, err
	}
	if in.Open && m.LiveSession == nil {
		return uuid.Nil, httpx.NewError(http.StatusConflict, "calls_not_live", "Start the call to launch a poll. You can save it for later.")
	}
	anon := true
	if in.Anonymous != nil {
		anon = *in.Anonymous
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calls_polls WHERE meeting_id = $1`, m.ID).Scan(&n); err != nil {
		return uuid.Nil, err
	}
	if n >= maxPollsPerMeeting {
		return uuid.Nil, httpx.BadRequest("A meeting can have at most 50 polls.")
	}
	status, session := "draft", (*uuid.UUID)(nil)
	if in.Open {
		status, session = "open", m.LiveSession
	}
	var id uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO calls_polls (meeting_id, session_id, question, options, multiple, anonymous, status, created_by, opened_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CASE WHEN $7 = 'open' THEN now() END) RETURNING id`,
		m.ID, session, in.Question, in.Options, in.Multiple, anon, status, by).Scan(&id)
	if err != nil {
		return uuid.Nil, err
	}
	if in.Open {
		s.signalRoom(ctx, m, signal{Type: SignalPolls})
	}
	return id, nil
}

func (s *Service) respondPoll(w http.ResponseWriter, r *http.Request, a *actor, id uuid.UUID, status int) error {
	list, err := s.listPolls(r.Context(), s.d.Pool, a, a.m.ID)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.ID == id {
			httpx.JSON(w, status, p)
			return nil
		}
	}
	return httpx.NotFound("Poll not found.")
}

// postPoll: POST /calls/meetings/{id}/polls (hosts) → Poll.
func (s *Service) postPoll(w http.ResponseWriter, r *http.Request, a *actor) error {
	if err := a.requireHost(); err != nil {
		return err
	}
	var in PollInput
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	id, err := s.createPoll(r.Context(), a.m, a.userID, in)
	if err != nil {
		return err
	}
	return s.respondPoll(w, r, a, id, http.StatusCreated)
}

func pollParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "pollID"))
	if err != nil {
		return uuid.Nil, httpx.NotFound("Poll not found.")
	}
	return id, nil
}

// patchPoll: PATCH /calls/meetings/{id}/polls/{pollID} (hosts) {question?,
// options?, multiple?, anonymous? (drafts only), status? open|closed, results_shared?}.
func (s *Service) patchPoll(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireHost(); err != nil {
		return err
	}
	id, err := pollParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Question      *string   `json:"question"`
		Options       *[]string `json:"options"`
		Multiple      *bool     `json:"multiple"`
		Anonymous     *bool     `json:"anonymous"`
		Status        *string   `json:"status"`
		ResultsShared *bool     `json:"results_shared"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var in PollInput
		var status string
		var anon bool
		if err := tx.QueryRow(ctx, `SELECT question, options, multiple, anonymous, status FROM calls_polls WHERE id = $1 AND meeting_id = $2 FOR UPDATE`,
			id, a.m.ID).Scan(&in.Question, &in.Options, &in.Multiple, &anon, &status); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("Poll not found.")
			}
			return err
		}
		if req.Question != nil || req.Options != nil || req.Multiple != nil || req.Anonymous != nil {
			if status != "draft" {
				return httpx.Conflict("Polls can be edited until they are launched.")
			}
			if req.Question != nil {
				in.Question = *req.Question
			}
			if req.Options != nil {
				in.Options = *req.Options
			}
			if req.Multiple != nil {
				in.Multiple = *req.Multiple
			}
			if req.Anonymous != nil {
				anon = *req.Anonymous
			}
			if err := cleanPoll(&in); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE calls_polls SET question = $2, options = $3, multiple = $4, anonymous = $5, updated_at = now() WHERE id = $1`,
				id, in.Question, in.Options, in.Multiple, anon); err != nil {
				return err
			}
		}
		if req.Status != nil && *req.Status != status {
			switch {
			case *req.Status == "open" && status == "draft":
				if a.m.LiveSession == nil {
					return httpx.NewError(http.StatusConflict, "calls_not_live", "Start the call to launch a poll.")
				}
				if _, err := tx.Exec(ctx, `UPDATE calls_polls SET status = 'open', session_id = $2, opened_at = now(), updated_at = now() WHERE id = $1`,
					id, a.m.LiveSession); err != nil {
					return err
				}
			case *req.Status == "closed" && status == "open":
				if _, err := tx.Exec(ctx, `UPDATE calls_polls SET status = 'closed', closed_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
					return err
				}
			default:
				return httpx.BadRequest("A poll goes from draft to open to closed.")
			}
		}
		if req.ResultsShared != nil {
			if _, err := tx.Exec(ctx, `UPDATE calls_polls SET results_shared = $2, updated_at = now() WHERE id = $1`, id, *req.ResultsShared); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalPolls})
	return s.respondPoll(w, r, a, id, http.StatusOK)
}

// deletePoll: DELETE /calls/meetings/{id}/polls/{pollID} (hosts).
func (s *Service) deletePoll(w http.ResponseWriter, r *http.Request, a *actor) error {
	if err := a.requireHost(); err != nil {
		return err
	}
	id, err := pollParam(r)
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM calls_polls WHERE id = $1 AND meeting_id = $2`, id, a.m.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Poll not found.")
	}
	s.signalRoom(r.Context(), a.m, signal{Type: SignalPolls})
	httpx.NoContent(w)
	return nil
}

// votePoll: POST …/polls/{pollID}/vote {choices} (anyone in the call; the
// vote can change while the poll is open).
func (s *Service) votePoll(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	id, err := pollParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Choices []int `json:"choices"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var options []string
	var multiple bool
	var status string
	if err := s.d.Pool.QueryRow(ctx, `SELECT options, multiple, status FROM calls_polls WHERE id = $1 AND meeting_id = $2`, id, a.m.ID).
		Scan(&options, &multiple, &status); err != nil {
		if db.IsNoRows(err) {
			return httpx.NotFound("Poll not found.")
		}
		return err
	}
	if status != "open" {
		return httpx.Conflict("This poll isn't open.")
	}
	choices := []int16{}
	for _, c := range req.Choices {
		if c < 0 || c >= len(options) {
			return httpx.BadRequest("Pick one of the poll's options.")
		}
		if !slices.Contains(choices, int16(c)) {
			choices = append(choices, int16(c))
		}
	}
	if len(choices) == 0 || (!multiple && len(choices) > 1) {
		return httpx.BadRequest("Pick one option.")
	}
	slices.Sort(choices)
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO calls_poll_votes (poll_id, identity, user_id, name, choices) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (poll_id, identity) DO UPDATE SET choices = $5, name = $4, updated_at = now()`,
		id, a.identity, a.userID, a.name, choices); err != nil {
		return err
	}
	// Hosts watch results come in; others only see shared results.
	s.signalRoom(ctx, a.m, signal{Type: SignalPolls})
	return s.respondPoll(w, r, a, id, http.StatusOK)
}

// ---- questions --------------------------------------------------------------------

func listQuestions(ctx context.Context, q db.Querier, a *actor) ([]Question, error) {
	where := `WHERE qn.meeting_id = $1`
	if !a.host {
		where += ` AND (qn.status IN ('open', 'answered') OR qn.identity = $2)`
	}
	rows, err := q.Query(ctx, `
		SELECT qn.id, qn.meeting_id, qn.body, qn.name, qn.anonymous, qn.status, qn.identity = $2, qn.created_at,
		       (SELECT count(*) FROM calls_question_votes v WHERE v.question_id = qn.id),
		       EXISTS (SELECT 1 FROM calls_question_votes v WHERE v.question_id = qn.id AND v.identity = $2)
		FROM calls_questions qn `+where+`
		ORDER BY CASE qn.status WHEN 'pending' THEN 0 WHEN 'open' THEN 1 WHEN 'answered' THEN 2 ELSE 3 END,
		         (SELECT count(*) FROM calls_question_votes v WHERE v.question_id = qn.id) DESC, qn.created_at, qn.id
		LIMIT 500`, a.m.ID, a.identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Question{}
	for rows.Next() {
		var qn Question
		if err := rows.Scan(&qn.ID, &qn.MeetingID, &qn.Body, &qn.Name, &qn.Anonymous, &qn.Status, &qn.Mine, &qn.CreatedAt, &qn.Votes, &qn.Voted); err != nil {
			return nil, err
		}
		if qn.Anonymous {
			qn.Name = "Anonymous"
		}
		out = append(out, qn)
	}
	return out, rows.Err()
}

func (s *Service) respondQuestion(w http.ResponseWriter, r *http.Request, a *actor, id uuid.UUID, status int) error {
	list, err := listQuestions(r.Context(), s.d.Pool, a)
	if err != nil {
		return err
	}
	for _, q := range list {
		if q.ID == id {
			httpx.JSON(w, status, q)
			return nil
		}
	}
	return httpx.NotFound("Question not found.")
}

// askQuestion: POST …/questions {body, anonymous} (anyone in the live call).
// With moderation on, it waits for a host (only hosts and you see it).
func (s *Service) askQuestion(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireLive(); err != nil {
		return err
	}
	var req struct {
		Body      string `json:"body"`
		Anonymous bool   `json:"anonymous"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	body := strings.TrimSpace(req.Body)
	if body == "" || len([]rune(body)) > maxQuestionChars {
		return httpx.BadRequest("Write a question of at most 500 characters.")
	}
	var n int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM calls_questions WHERE meeting_id = $1`, a.m.ID).Scan(&n); err != nil {
		return err
	}
	if n >= maxQuestionsPerMeeting {
		return httpx.BadRequest("This meeting has reached its limit of questions.")
	}
	status := "open"
	if a.m.Rules.ModerateQuestions && !a.host {
		status = "pending"
	}
	var id uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO calls_questions (meeting_id, session_id, identity, user_id, name, anonymous, body, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		a.m.ID, a.m.LiveSession, a.identity, a.userID, a.name, req.Anonymous, body, status).Scan(&id); err != nil {
		return err
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalQuestions})
	return s.respondQuestion(w, r, a, id, http.StatusCreated)
}

func questionParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "questionID"))
	if err != nil {
		return uuid.Nil, httpx.NotFound("Question not found.")
	}
	return id, nil
}

// voteQuestion: POST …/questions/{questionID}/vote {up} (anyone in the call;
// visible questions only).
func (s *Service) voteQuestion(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	id, err := questionParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Up bool `json:"up"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var status, owner string
	if err := s.d.Pool.QueryRow(ctx, `SELECT status, identity FROM calls_questions WHERE id = $1 AND meeting_id = $2`, id, a.m.ID).Scan(&status, &owner); err != nil {
		if db.IsNoRows(err) {
			return httpx.NotFound("Question not found.")
		}
		return err
	}
	if status != "open" && !a.host && owner != a.identity {
		return httpx.NotFound("Question not found.")
	}
	if req.Up {
		_, err = s.d.Pool.Exec(ctx, `INSERT INTO calls_question_votes (question_id, identity, user_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			id, a.identity, a.userID)
	} else {
		_, err = s.d.Pool.Exec(ctx, `DELETE FROM calls_question_votes WHERE question_id = $1 AND identity = $2`, id, a.identity)
	}
	if err != nil {
		return err
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalQuestions})
	return s.respondQuestion(w, r, a, id, http.StatusOK)
}

// patchQuestion: PATCH /calls/meetings/{id}/questions/{questionID} {status}
// (hosts): approve (open), answered, hidden.
func (s *Service) patchQuestion(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	if err := a.requireHost(); err != nil {
		return err
	}
	id, err := questionParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Status != "open" && req.Status != "answered" && req.Status != "hidden" {
		return httpx.BadRequest("status must be open, answered or hidden.")
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE calls_questions SET status = $3, updated_at = now() WHERE id = $1 AND meeting_id = $2`, id, a.m.ID, req.Status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Question not found.")
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalQuestions})
	return s.respondQuestion(w, r, a, id, http.StatusOK)
}

// deleteQuestion: DELETE …/questions/{questionID} (hosts, or whoever asked it).
func (s *Service) deleteQuestion(w http.ResponseWriter, r *http.Request, a *actor) error {
	ctx := r.Context()
	id, err := questionParam(r)
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM calls_questions WHERE id = $1 AND meeting_id = $2 AND ($3 OR identity = $4)`, id, a.m.ID, a.host, a.identity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("Question not found.")
	}
	s.signalRoom(ctx, a.m, signal{Type: SignalQuestions})
	httpx.NoContent(w)
	return nil
}
