package calls

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// Automatic assignment puts everyone in a room, room sizes differ by at
// most one, and nobody is left out or counted twice.
func TestAssignEvenly(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for people := 0; people <= 23; people++ {
		for rooms := 1; rooms <= 7; rooms++ {
			ids := make([]string, people)
			for i := range ids {
				ids[i] = fmt.Sprintf("u:%d", i)
			}
			got := assignEvenly(ids, rooms, r.Shuffle)
			if len(got) != people {
				t.Fatalf("%d people, %d rooms: %d assigned", people, rooms, len(got))
			}
			sizes := make([]int, rooms)
			for _, id := range ids {
				idx, ok := got[id]
				if !ok || idx < 0 || idx >= rooms {
					t.Fatalf("%s → %d (%v)", id, idx, ok)
				}
				sizes[idx]++
			}
			lo, hi := people, 0
			for _, n := range sizes {
				lo, hi = min(lo, n), max(hi, n)
			}
			if people >= rooms && hi-lo > 1 {
				t.Fatalf("%d people, %d rooms: sizes %v", people, rooms, sizes)
			}
		}
	}
	if len(assignEvenly([]string{"a"}, 0, r.Shuffle)) != 0 {
		t.Fatal("zero rooms assigned someone")
	}
}

// A host opens rooms with automatic assignment (hosts stay out of it);
// people get tokens for their own room only, or any room when they may
// choose; attendance in a room counts toward the session, which survives
// the main room emptying; closing ends the rooms after the countdown.
func TestBreakoutFlow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	people := []uuid.UUID{newUser(t, pool, "A"), newUser(t, pool, "B"), newUser(t, pool, "C"), newUser(t, pool, "D")}
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Workshop", Access: AccessOpen, LobbyEnabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	session := goLive(t, s, f, m.ID, append([]uuid.UUID{host}, people...)...)

	if w := call(t, s.userAct(s.createBreakouts), people[0], "POST", `{"count":2}`, "id", m.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("non-host create = %d", w.Code)
	}
	w := call(t, s.userAct(s.createBreakouts), host, "POST", `{"count":2,"assign":"auto","minutes":10}`, "id", m.ID.String())
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body)
	}
	b := decode[Breakout](t, w)
	if len(b.Rooms) != 2 || b.EndsAt == nil || b.Rooms[0].Name != "Room 1" {
		t.Fatalf("breakout = %+v", b)
	}
	total := 0
	for _, r := range b.Rooms {
		total += len(r.Assigned)
		if len(r.Assigned) != 2 {
			t.Fatalf("room %s has %d people", r.Name, len(r.Assigned))
		}
		for _, p := range r.Assigned {
			if p.Identity == userIdentity(host) {
				t.Fatal("the host was assigned")
			}
		}
	}
	if total != 4 {
		t.Fatalf("assigned %d", total)
	}
	if w := call(t, s.userAct(s.createBreakouts), host, "POST", `{"count":2}`, "id", m.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("second create = %d", w.Code)
	}
	meta := f.meta(t, m.ID.String())
	if meta.Breakout == nil || len(meta.Breakout.Rooms) != 2 {
		t.Fatalf("room metadata breakout = %+v", meta.Breakout)
	}

	// Each person's room, from their own view.
	roomOf := map[uuid.UUID]uuid.UUID{}
	for _, p := range people {
		st := decode[RoomState](t, call(t, s.userAct(s.roomState), p, "GET", "", "id", m.ID.String()))
		if st.Breakout == nil || st.Breakout.Room == nil {
			t.Fatalf("%s has no room: %+v", p, st.Breakout)
		}
		roomOf[p] = st.Breakout.Room.ID
	}
	a := people[0]
	var other uuid.UUID
	for _, r := range b.Rooms {
		if r.ID != roomOf[a] {
			other = r.ID
		}
	}
	tok := decode[JoinToken](t, call(t, s.userAct(s.joinBreakout), a, "POST", `{}`, "id", m.ID.String()))
	if tok.Room != roomOf[a].String() {
		t.Fatalf("token room %s, want %s", tok.Room, roomOf[a])
	}
	if meta := f.meta(t, tok.Room); meta.Breakout == nil || meta.Breakout.Room == nil || meta.Breakout.Room.ID != roomOf[a] {
		t.Fatalf("breakout room metadata = %+v", meta.Breakout)
	}
	if w := call(t, s.userAct(s.joinBreakout), a, "POST", `{"room_id":"`+other.String()+`"}`, "id", m.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("other room without choosing = %d", w.Code)
	}
	if w := call(t, s.userAct(s.joinBreakout), host, "POST", `{"room_id":"`+other.String()+`"}`, "id", m.ID.String()); w.Code != http.StatusOK {
		t.Fatalf("host visit = %d %s", w.Code, w.Body)
	}
	// Letting people choose: A moves and is reassigned.
	call(t, s.userAct(s.patchBreakouts), host, "PATCH", `{"allow_choose":true}`, "id", m.ID.String())
	if w := call(t, s.userAct(s.joinBreakout), a, "POST", `{"room_id":"`+other.String()+`"}`, "id", m.ID.String()); w.Code != http.StatusOK {
		t.Fatalf("choose = %d %s", w.Code, w.Body)
	}
	st := decode[RoomState](t, call(t, s.userAct(s.roomState), a, "GET", "", "id", m.ID.String()))
	if st.Breakout.Room.ID != other {
		t.Fatal("choosing didn't reassign")
	}
	// A late joiner is unassigned until a host moves them.
	late := newUser(t, pool, "Late")
	if w := call(t, s.userAct(s.joinBreakout), late, "POST", `{}`, "id", m.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("unassigned join = %d", w.Code)
	}
	call(t, s.userAct(s.patchBreakouts), host, "PATCH", `{"assignments":{"`+userIdentity(late)+`":"`+other.String()+`"}}`, "id", m.ID.String())
	if w := call(t, s.userAct(s.joinBreakout), late, "POST", `{}`, "id", m.ID.String()); w.Code != http.StatusOK {
		t.Fatalf("after assignment = %d %s", w.Code, w.Body)
	}

	// Attendance in a breakout room counts toward the session; the main room
	// emptying keeps the session open while rooms are open.
	hook := func(id, body string) {
		rw := httptest.NewRecorder()
		s.webhook(rw, signedWebhook(t, body))
		if rw.Code != http.StatusNoContent {
			t.Fatalf("webhook %s = %d %s", id, rw.Code, rw.Body)
		}
	}
	hook("j", `{"event":"participant_joined","id":"EV_j","room":{"sid":"RM_b","name":"`+other.String()+`"},"participant":{"sid":"PA_b1","identity":"`+userIdentity(a)+`"}}`)
	var inRoom int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM calls_participants WHERE session_id = $1 AND breakout_room_id = $2`, session, other).Scan(&inRoom)
	if inRoom != 1 {
		t.Fatalf("breakout attendance rows = %d", inRoom)
	}
	if _, err := pool.Exec(ctx, `UPDATE calls_sessions SET room_sid = 'RM_main' WHERE id = $1`, session); err != nil {
		t.Fatal(err)
	}
	hook("f", `{"event":"room_finished","id":"EV_f","room":{"sid":"RM_main","name":"`+m.ID.String()+`"}}`)
	var ended *time.Time
	var sid *string
	_ = pool.QueryRow(ctx, `SELECT ended_at, room_sid FROM calls_sessions WHERE id = $1`, session).Scan(&ended, &sid)
	if ended != nil || sid != nil {
		t.Fatalf("main room finishing ended the session (%v) or kept its sid (%v)", ended, sid)
	}
	// The next main room instance adopts the session.
	hook("s", `{"event":"room_started","id":"EV_s","room":{"sid":"RM_main2","name":"`+m.ID.String()+`"}}`)
	_ = pool.QueryRow(ctx, `SELECT ended_at, room_sid FROM calls_sessions WHERE id = $1`, session).Scan(&ended, &sid)
	if ended != nil || sid == nil || *sid != "RM_main2" {
		t.Fatalf("adoption: ended %v sid %v", ended, sid)
	}

	// Broadcast reaches the main room and every breakout room.
	before := len(f.sent)
	if w := call(t, s.userAct(s.broadcastBreakouts), host, "POST", `{"text":"5 minutes left"}`, "id", m.ID.String()); w.Code != http.StatusNoContent {
		t.Fatalf("broadcast = %d", w.Code)
	}
	if n := len(f.sent) - before; n != 3 {
		t.Fatalf("broadcast went to %d rooms", n)
	}

	// Closing: a countdown, then the sweep ends the rooms.
	if w := call(t, s.userAct(s.closeBreakouts), host, "POST", `{}`, "id", m.ID.String()); w.Code != http.StatusNoContent {
		t.Fatalf("close = %d", w.Code)
	}
	if meta := f.meta(t, m.ID.String()); meta.Breakout == nil || meta.Breakout.Status != "closing" || meta.Breakout.ClosingAt == nil {
		t.Fatalf("closing metadata = %+v", meta.Breakout)
	}
	if w := call(t, s.userAct(s.joinBreakout), a, "POST", `{}`, "id", m.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("join while closing = %d", w.Code)
	}
	if _, err := pool.Exec(ctx, `UPDATE calls_breakouts SET closing_at = now() - interval '1 second' WHERE session_id = $1`, session); err != nil {
		t.Fatal(err)
	}
	if err := s.sweepBreakouts(ctx); err != nil {
		t.Fatal(err)
	}
	if meta := f.meta(t, m.ID.String()); meta.Breakout != nil {
		t.Fatalf("rooms still open: %+v", meta.Breakout)
	}
	if len(f.ended) != 2 {
		t.Fatalf("ended rooms = %v", f.ended)
	}
	// A timer that runs out starts the countdown.
	call(t, s.userAct(s.createBreakouts), host, "POST", `{"count":1,"assign":"choose","minutes":1}`, "id", m.ID.String())
	if _, err := pool.Exec(ctx, `UPDATE calls_breakouts SET ends_at = now() - interval '1 second' WHERE session_id = $1 AND status = 'open'`, session); err != nil {
		t.Fatal(err)
	}
	if err := s.sweepBreakouts(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM calls_breakouts WHERE session_id = $1 ORDER BY created_at DESC LIMIT 1`, session).Scan(&status)
	if status != "closing" {
		t.Fatalf("timer: %s", status)
	}
}

// Polls: hosts prepare and launch; people vote once (changeable while
// open); results stay hidden until shared; anonymous polls hide voters.
// Questions: moderation holds them for hosts; upvotes; guests act through
// their lobby key.
func TestPollsAndQuestions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	ana := newUser(t, pool, "Ana")
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Town hall", Access: AccessOpen, LobbyEnabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	// Drafts before the call; launching needs it live.
	w := call(t, s.userAct(s.postPoll), host, "POST", `{"question":"Lunch?","options":["Pizza","Salad"," "]}`, "id", m.ID.String())
	if w.Code != http.StatusCreated {
		t.Fatalf("draft = %d %s", w.Code, w.Body)
	}
	p := decode[Poll](t, w)
	if p.Status != "draft" || len(p.Options) != 2 || !p.Anonymous {
		t.Fatalf("poll = %+v", p)
	}
	if w := call(t, s.userAct(s.patchPoll), host, "PATCH", `{"status":"open"}`, "id", m.ID.String(), "pollID", p.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("launch while not live = %d", w.Code)
	}
	goLive(t, s, f, m.ID, host, ana)
	if w := call(t, s.userAct(s.postPoll), ana, "POST", `{"question":"x","options":["a","b"]}`, "id", m.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("non-host poll = %d", w.Code)
	}
	st := decode[RoomState](t, call(t, s.userAct(s.roomState), ana, "GET", "", "id", m.ID.String()))
	if len(st.Polls) != 0 {
		t.Fatal("ana sees a draft")
	}
	call(t, s.userAct(s.patchPoll), host, "PATCH", `{"status":"open"}`, "id", m.ID.String(), "pollID", p.ID.String())
	if last := f.sent[len(f.sent)-1]; last.Payload.Type != SignalPolls {
		t.Fatalf("signal = %+v", last)
	}
	if w := call(t, s.userAct(s.votePoll), ana, "POST", `{"choices":[0,1]}`, "id", m.ID.String(), "pollID", p.ID.String()); w.Code != http.StatusBadRequest {
		t.Fatalf("two choices on a single-choice poll = %d", w.Code)
	}
	call(t, s.userAct(s.votePoll), ana, "POST", `{"choices":[0]}`, "id", m.ID.String(), "pollID", p.ID.String())
	got := decode[Poll](t, call(t, s.userAct(s.votePoll), ana, "POST", `{"choices":[1]}`, "id", m.ID.String(), "pollID", p.ID.String()))
	if len(got.MyChoices) != 1 || got.MyChoices[0] != 1 || got.Counts != nil {
		t.Fatalf("ana's view = %+v", got)
	}
	hp := decode[RoomState](t, call(t, s.userAct(s.roomState), host, "GET", "", "id", m.ID.String())).Polls[0]
	if hp.Voters != 1 || hp.Counts[1] != 1 || hp.Counts[0] != 0 || len(hp.Votes) != 0 {
		t.Fatalf("host's view = %+v", hp)
	}
	call(t, s.userAct(s.patchPoll), host, "PATCH", `{"status":"closed","results_shared":true}`, "id", m.ID.String(), "pollID", p.ID.String())
	ap := decode[RoomState](t, call(t, s.userAct(s.roomState), ana, "GET", "", "id", m.ID.String())).Polls[0]
	if ap.Counts == nil || ap.Counts[1] != 1 {
		t.Fatalf("shared results = %+v", ap)
	}
	if w := call(t, s.userAct(s.votePoll), ana, "POST", `{"choices":[0]}`, "id", m.ID.String(), "pollID", p.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("vote on a closed poll = %d", w.Code)
	}

	// Questions with moderation.
	call(t, httpx.Handler(s.updateMeeting), host, "PATCH", `{"moderate_questions":true}`, "id", m.ID.String())
	q := decode[Question](t, call(t, s.userAct(s.askQuestion), ana, "POST", `{"body":"When is the launch?","anonymous":true}`, "id", m.ID.String()))
	if q.Status != "pending" || q.Name != "Anonymous" || !q.Mine {
		t.Fatalf("question = %+v", q)
	}
	// A guest, admitted, sees open questions only and votes with their key.
	gk := knockAsGuest(t, s, m.Code, "Guest Gil")
	gst := decode[RoomState](t, guestCall(t, s.guestAct(s.roomState), gk, `{}`))
	if len(gst.Questions) != 0 {
		t.Fatalf("guest sees a pending question: %+v", gst.Questions)
	}
	call(t, s.userAct(s.patchQuestion), host, "PATCH", `{"status":"open"}`, "id", m.ID.String(), "questionID", q.ID.String())
	gq := decode[Question](t, guestCall(t, s.guestAct(s.voteQuestion), gk, `{"up":true}`, "questionID", q.ID.String()))
	if gq.Votes != 1 || !gq.Voted || gq.Name != "Anonymous" {
		t.Fatalf("guest vote = %+v", gq)
	}
	if w := guestCall(t, s.guestAct(s.roomState), GuestKnock{LobbyEntry: gk.LobbyEntry, Key: "cgk_wrong"}, `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("wrong key = %d", w.Code)
	}
	if w := call(t, s.userAct(s.deleteQuestion), host, "DELETE", "", "id", m.ID.String(), "questionID", q.ID.String()); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", w.Code)
	}
}

func knockAsGuest(t *testing.T, s *Service, code, name string) GuestKnock {
	t.Helper()
	w := call(t, httpx.Handler(s.guestKnock), uuid.Nil, "POST", `{"name":"`+name+`"}`, "code", code)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("knock = %d %s", w.Code, w.Body)
	}
	return decode[GuestKnock](t, w)
}

func guestCall(t *testing.T, h http.HandlerFunc, k GuestKnock, body string, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	full := `{"key":"` + k.Key + `"`
	if inner := body[1 : len(body)-1]; inner != "" {
		full += "," + inner
	}
	full += "}"
	return call(t, h, uuid.Nil, "POST", full, append([]string{"id", k.ID.String()}, params...)...)
}
