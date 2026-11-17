package channels

// Polls: a message whose body is the question, with 2–10 options, one or
// several choices, named or anonymous votes, closed by its author or a
// channel/org admin. Anonymous votes are stored (one per person) but voters
// are never returned.

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

const (
	minPollOptions = 2
	maxPollOptions = 10
)

// Poll is a message's poll as the viewer sees it.
type Poll struct {
	Multiple  bool         `json:"multiple"`
	Anonymous bool         `json:"anonymous"`
	Closed    bool         `json:"closed"`
	Options   []PollOption `json:"options"`
	Voters    int          `json:"voters"` // people who voted
	// MyVotes are the viewer's choices; null in live events, which go to
	// everyone (clients keep their own).
	MyVotes []uuid.UUID `json:"my_votes"`
}

// PollOption is one choice. VoterIDs is empty for anonymous polls.
type PollOption struct {
	ID       uuid.UUID   `json:"id"`
	Text     string      `json:"text"`
	Count    int         `json:"count"`
	VoterIDs []uuid.UUID `json:"voter_ids"`
}

type pollIn struct {
	options   []string
	multiple  bool
	anonymous bool
}

type pollReq struct {
	ClientID     string     `json:"client_id"`
	Question     string     `json:"question"`
	Options      []string   `json:"options"`
	Multiple     bool       `json:"multiple"`
	Anonymous    bool       `json:"anonymous"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
}

// cleanPoll validates a poll's question and options.
func cleanPoll(question string, options []string) (string, []string, error) {
	q := strings.TrimSpace(question)
	if q == "" || utf8.RuneCountInString(q) > 300 {
		return "", nil, httpx.BadRequest("Questions are 1 to 300 characters.")
	}
	out := []string{}
	for _, o := range options {
		o = strings.Join(strings.Fields(o), " ")
		if o == "" {
			continue
		}
		if utf8.RuneCountInString(o) > 200 {
			return "", nil, httpx.BadRequest("Options can be up to 200 characters.")
		}
		if slices.Contains(out, o) {
			return "", nil, httpx.BadRequest("Each option can appear once.")
		}
		out = append(out, o)
	}
	if len(out) < minPollOptions || len(out) > maxPollOptions {
		return "", nil, httpx.BadRequest("Give 2 to 10 options.")
	}
	return q, out, nil
}

// POST /channels/channels/{id}/polls — 201 with the message (200 for a
// repeated client_id).
func (s *Service) createPoll(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	channelID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req pollReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	msg, created, err := s.postPoll(ctx, me, channelID, req)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, msg)
	return nil
}

func (s *Service) postPoll(ctx context.Context, me, channelID uuid.UUID, req pollReq) (*Message, bool, error) {
	q, opts, err := cleanPoll(req.Question, req.Options)
	if err != nil {
		return nil, false, err
	}
	if err := checkText(ctx, opts...); err != nil {
		return nil, false, err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, false, err
	}
	if a.Layout == layoutForum && req.ThreadRootID == nil {
		return nil, false, httpx.BadRequest("Polls go in a forum post's replies.")
	}
	return s.post(ctx, me, channelID, sendReq{ClientID: req.ClientID, Body: q, ThreadRootID: req.ThreadRootID,
		poll: &pollIn{options: opts, multiple: req.Multiple, anonymous: req.Anonymous}})
}

func insertPoll(ctx context.Context, tx pgx.Tx, msgID uuid.UUID, p *pollIn) error {
	var q string
	if err := tx.QueryRow(ctx, `SELECT body FROM channel_messages WHERE id = $1`, msgID).Scan(&q); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channel_polls (message_id, question, multiple, anonymous) VALUES ($1, $2, $3, $4)`,
		msgID, truncate(q, 300), p.multiple, p.anonymous); err != nil {
		return err
	}
	for i, o := range p.options {
		if _, err := tx.Exec(ctx, `INSERT INTO channel_poll_options (message_id, position, text) VALUES ($1, $2, $3)`, msgID, i, o); err != nil {
			return err
		}
	}
	return nil
}

// hydratePolls fills Poll on messages that have one (MyVotes for viewer;
// nil when there is no viewer).
func (s *Service) hydratePolls(ctx context.Context, q db.Querier, byID map[uuid.UUID]*Message, ids []uuid.UUID, viewer uuid.UUID) error {
	rows, err := q.Query(ctx, `
		SELECT p.message_id, p.multiple, p.anonymous, p.closed_at IS NOT NULL,
		       (SELECT count(DISTINCT v.user_id) FROM channel_poll_votes v WHERE v.message_id = p.message_id)::int
		FROM channel_polls p WHERE p.message_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	polls := map[uuid.UUID]*Poll{}
	pollIDs := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		p := &Poll{Options: []PollOption{}}
		if err := rows.Scan(&id, &p.Multiple, &p.Anonymous, &p.Closed, &p.Voters); err != nil {
			rows.Close()
			return err
		}
		if viewer != uuid.Nil {
			p.MyVotes = []uuid.UUID{}
		}
		polls[id] = p
		pollIDs = append(pollIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(pollIDs) == 0 {
		return err
	}
	rows, err = q.Query(ctx, `
		SELECT o.message_id, o.id, o.text,
		       COALESCE(array_agg(v.user_id ORDER BY v.created_at) FILTER (WHERE v.user_id IS NOT NULL), '{}')
		FROM channel_poll_options o LEFT JOIN channel_poll_votes v ON v.option_id = o.id
		WHERE o.message_id = ANY($1)
		GROUP BY o.message_id, o.id, o.text, o.position ORDER BY o.message_id, o.position`, pollIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID uuid.UUID
		var o PollOption
		var voters []uuid.UUID
		if err := rows.Scan(&msgID, &o.ID, &o.Text, &voters); err != nil {
			return err
		}
		p := polls[msgID]
		o.Count = len(voters)
		o.VoterIDs = []uuid.UUID{}
		if !p.Anonymous {
			o.VoterIDs = voters
		}
		if viewer != uuid.Nil && slices.Contains(voters, viewer) {
			p.MyVotes = append(p.MyVotes, o.ID)
		}
		p.Options = append(p.Options, o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for id, p := range polls {
		if m := byID[id]; m != nil {
			m.Poll = p
		}
	}
	return nil
}

type voteReq struct {
	OptionIDs []uuid.UUID `json:"option_ids"` // replaces your votes; empty clears them
}

// pollFor loads a message's poll for a vote or close: the message must be
// readable and writable by the caller.
func (s *Service) pollFor(ctx context.Context, me, id uuid.UUID) (*messageAccess, bool, bool, error) {
	m, err := s.reactable(ctx, me, id)
	if err != nil {
		return nil, false, false, err
	}
	var multiple, closed bool
	err = s.d.Pool.QueryRow(ctx, `SELECT multiple, closed_at IS NOT NULL FROM channel_polls WHERE message_id = $1`, id).Scan(&multiple, &closed)
	if db.IsNoRows(err) {
		return nil, false, false, httpx.NotFound("That message has no poll.")
	}
	return m, multiple, closed, err
}

// PUT /channels/messages/{id}/vote {option_ids} — returns the message.
func (s *Service) vote(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req voteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	opts := dedupe(req.OptionIDs)
	m, multiple, closed, err := s.pollFor(ctx, me, id)
	if err != nil {
		return err
	}
	switch {
	case closed:
		return httpx.Conflict("This poll is closed.")
	case !multiple && len(opts) > 1:
		return httpx.BadRequest("Choose one option.")
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if len(opts) > 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM channel_poll_options WHERE message_id = $1 AND id = ANY($2)`, id, opts).Scan(&n); err != nil {
				return err
			}
			if n != len(opts) {
				return httpx.BadRequest("Choose options from this poll.")
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM channel_poll_votes WHERE message_id = $1 AND user_id = $2`, id, me); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO channel_poll_votes (option_id, message_id, user_id) SELECT o, $1, $2 FROM unnest($3::uuid[]) o`, id, me, opts)
		return err
	})
	if err != nil {
		return err
	}
	return s.respondPoll(w, r, m, id, me)
}

// POST /channels/messages/{id}/poll/close — the author, or whoever may
// delete others' messages (ActEdit).
func (s *Service) closePoll(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	m, _, closed, err := s.pollFor(ctx, me, id)
	if err != nil {
		return err
	}
	if m.SenderID == nil || *m.SenderID != me {
		if err := s.require(ctx, s.d.Pool, m.access, ActEdit, "Only the poll's author and admins can close it."); err != nil {
			return err
		}
	}
	if !closed {
		if _, err := s.d.Pool.Exec(ctx, `UPDATE channel_polls SET closed_at = $2, closed_by = $3 WHERE message_id = $1 AND closed_at IS NULL`,
			id, time.Now(), me); err != nil {
			return err
		}
	}
	return s.respondPoll(w, r, m, id, me)
}

func (s *Service) respondPoll(w http.ResponseWriter, r *http.Request, m *messageAccess, id, me uuid.UUID) error {
	ctx := r.Context()
	if live, err := s.message(ctx, s.d.Pool, id, uuid.Nil); err == nil {
		s.publishUpdated(ctx, m.access, live, false)
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	httpx.OK(w, msg)
	return nil
}
