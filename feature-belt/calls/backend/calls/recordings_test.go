package calls

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
)

// The LiveKit egress client sends a room-composite MP4 request to S3 with a
// roomRecord token and reads LiveKit's EgressInfo (proto names, int64s as
// strings, enums as names) — checked against a fake Twirp egress.
func TestLiveKitStartRecordingRequest(t *testing.T) {
	var got map[string]any
	var path, authz string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, authz = r.URL.Path, r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"egress_id":"EG_abc","room_id":"RM_1","room_name":"room-1","status":"EGRESS_STARTING","started_at":"0"}`))
	}))
	defer srv.Close()
	lk := newLiveKit("ws://x", srv.URL, "key", testSecret)
	info, err := lk.StartRecording(context.Background(), "room-1", RecordingOutput{Filepath: "calls_recording/m/r.mp4", Layout: "speaker",
		S3: S3Target{AccessKey: "ak", Secret: "sk", Region: "us-east-1", Endpoint: "http://minio:9000", Bucket: "b", ForcePathStyle: true}})
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "EG_abc" || info.Status != RecStarting || info.Room != "room-1" {
		t.Fatalf("info = %+v", info)
	}
	if path != "/twirp/livekit.Egress/StartRoomCompositeEgress" {
		t.Fatalf("path = %s", path)
	}
	var claims liveKitClaims
	if err := verifyHS256([]byte(testSecret), strings.TrimPrefix(authz, "Bearer "), time.Now(), &claims); err != nil || claims.Video == nil || !claims.Video.RoomRecord {
		t.Fatalf("token: %v %+v", err, claims.Video)
	}
	outs, _ := got["file_outputs"].([]any)
	if got["room_name"] != "room-1" || got["layout"] != "speaker" || len(outs) != 1 {
		t.Fatalf("body = %v", got)
	}
	out := outs[0].(map[string]any)
	s3 := out["s3"].(map[string]any)
	if out["file_type"] != "MP4" || out["filepath"] != "calls_recording/m/r.mp4" || s3["bucket"] != "b" || s3["endpoint"] != "http://minio:9000" || s3["force_path_style"] != true {
		t.Fatalf("output = %v", out)
	}
}

// LiveKit's egress_ended webhook (camelCase protojson) parses into a
// finished recording with its size and duration.
func TestParseEgressWebhook(t *testing.T) {
	lk := newLiveKit("ws://x", "http://x", "testkey", testSecret)
	body := `{"event":"egress_ended","id":"EV_1","createdAt":"1793479917","egressInfo":{"egressId":"EG_9","roomName":"r","status":"EGRESS_COMPLETE",
		"startedAt":"1793479000000000000","endedAt":"1793479900000000000","fileResults":[{"filename":"a.mp4","size":"2048","duration":"90000000000"}]}}`
	ev, err := lk.ParseWebhook(signedWebhook(t, body), []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != EventEgressEnded || ev.Recording == nil || ev.Room != "r" {
		t.Fatalf("event = %+v", ev)
	}
	r := ev.Recording
	if r.ID != "EG_9" || r.Status != RecComplete || r.SizeBytes != 2048 || r.DurationMs != 90000 || r.EndedAt.Unix() != 1793479900 {
		t.Fatalf("recording = %+v", r)
	}
	if _, err := lk.ParseWebhook(signedWebhook(t, body), []byte(body+" ")); err != errWebhookAuth {
		t.Fatalf("tampered body: %v", err)
	}
	for in, want := range map[string]string{"EGRESS_ACTIVE": RecActive, "EGRESS_ENDING": RecEnding, "EGRESS_FAILED": RecFailed, "EGRESS_ABORTED": RecFailed, "EGRESS_LIMIT_REACHED": RecComplete, "1": RecActive} {
		if got := egressStatus(in); got != want {
			t.Errorf("egressStatus(%s) = %s, want %s", in, got, want)
		}
	}
}

// A host records; the room's metadata says so; egress webhooks finish the
// row once (duplicates ignored), set its retention and tell the starter.
// Failed and empty egresses end as failed.
func TestRecordingLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	guest := newUser(t, pool, "Ana")
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Design review", Access: AccessOpen, InviteeIDs: []uuid.UUID{guest}})
	if err != nil {
		t.Fatal(err)
	}
	goLive(t, s, f, m.ID, host, guest)

	// Only hosts start recordings.
	if w := call(t, httpx.Handler(s.startRecording), guest, "POST", "", "id", m.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("invitee start = %d %s", w.Code, w.Body)
	}
	w := call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String())
	if w.Code != http.StatusCreated {
		t.Fatalf("start = %d %s", w.Code, w.Body)
	}
	rec := decode[Recording](t, w)
	if rec.Status != "starting" {
		t.Fatalf("status = %s", rec.Status)
	}
	if meta := f.meta(t, m.ID.String()); meta.Recording == nil || meta.Recording.ID != rec.ID {
		t.Fatalf("room metadata = %+v", meta)
	}
	if w := call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String()); w.Code != http.StatusConflict {
		t.Fatalf("second start = %d", w.Code)
	}
	on, err := recordingNow(ctx, pool, m.ID)
	if err != nil || !on {
		t.Fatalf("recordingNow = %v %v", on, err)
	}

	hook := func(id, event, status, extra string) int {
		body := `{"event":"` + event + `","id":"` + id + `","egressInfo":{"egressId":"EG_1","roomName":"` + m.ID.String() + `","status":"` + status + `"` + extra + `}}`
		rw := httptest.NewRecorder()
		s.webhook(rw, signedWebhook(t, body))
		return rw.Code
	}
	if c := hook("EV_a", "egress_started", "EGRESS_ACTIVE", ""); c != http.StatusNoContent {
		t.Fatalf("webhook = %d", c)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM calls_recordings WHERE id = $1`, rec.ID).Scan(&status)
	if status != "recording" {
		t.Fatalf("after egress_started: %s", status)
	}
	if w := call(t, httpx.Handler(s.stopRecording), host, "DELETE", "", "id", m.ID.String()); w.Code != http.StatusOK {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	if meta := f.meta(t, m.ID.String()); meta.Recording != nil {
		t.Fatalf("room still recording: %+v", meta.Recording)
	}
	extra := `,"fileResults":[{"filename":"x.mp4","size":"4096","duration":"61000000000"}]`
	hook("EV_b", "egress_ended", "EGRESS_COMPLETE", extra)
	hook("EV_b", "egress_ended", "EGRESS_COMPLETE", extra) // duplicate delivery
	var size, ms int64
	var expires *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, size_bytes, duration_ms, expires_at FROM calls_recordings WHERE id = $1`, rec.ID).Scan(&status, &size, &ms, &expires); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || size != 4096 || ms != 61000 || expires == nil || time.Until(*expires) < 23*time.Hour {
		t.Fatalf("ready row: %s %d %d %v", status, size, ms, expires)
	}
	// A late failure doesn't undo a finished recording.
	hook("EV_c", "egress_updated", "EGRESS_FAILED", `,"error":"late"`)
	_ = pool.QueryRow(ctx, `SELECT status FROM calls_recordings WHERE id = $1`, rec.ID).Scan(&status)
	if status != "ready" {
		t.Fatalf("late failure changed it: %s", status)
	}

	// The next recording fails at the SFU; an empty one fails too.
	w = call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String())
	rec2 := decode[Recording](t, w)
	body := `{"event":"egress_ended","id":"EV_d","egressInfo":{"egressId":"EG_2","roomName":"` + m.ID.String() + `","status":"EGRESS_FAILED","error":"no S3 access"}}`
	s.webhook(httptest.NewRecorder(), signedWebhook(t, body))
	var errText *string
	_ = pool.QueryRow(ctx, `SELECT status, error FROM calls_recordings WHERE id = $1`, rec2.ID).Scan(&status, &errText)
	if status != "failed" || errText == nil || *errText != "no S3 access" {
		t.Fatalf("failed row: %s %v", status, errText)
	}
	w = call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String())
	rec3 := decode[Recording](t, w)
	body = `{"event":"egress_ended","id":"EV_e","egressInfo":{"egressId":"EG_3","roomName":"` + m.ID.String() + `","status":"EGRESS_COMPLETE","fileResults":[{"size":"0"}]}}`
	s.webhook(httptest.NewRecorder(), signedWebhook(t, body))
	_ = pool.QueryRow(ctx, `SELECT status FROM calls_recordings WHERE id = $1`, rec3.ID).Scan(&status)
	if status != "failed" {
		t.Fatalf("empty recording: %s", status)
	}

	// Unsigned webhooks are refused; unknown egress ids are ignored.
	bad := httptest.NewRequest(http.MethodPost, "/webhooks/livekit", strings.NewReader(body))
	rw := httptest.NewRecorder()
	s.webhook(rw, bad)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d", rw.Code)
	}
	if c := hook("EV_f", "egress_ended", "EGRESS_COMPLETE", `,"egressId":"EG_other"`); c != http.StatusNoContent {
		t.Fatalf("unknown egress = %d", c)
	}

	// Recording unavailable without S3 or the env switch.
	s.d.Cfg.S3Bucket = ""
	if w := call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String()); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no bucket = %d", w.Code)
	}
}

// The records rule: hosts and the call's participants open its recordings;
// invitees only with records_access invited or org; workspace members only
// with org; everyone else gets 404, as does someone who stopped being
// invited. Only hosts and whoever recorded it delete.
func TestRecordingPermissions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	attendee := newUser(t, pool, "Attendee")
	invitee := newUser(t, pool, "Invitee")
	member := newUser(t, pool, "Member")
	stranger := newUser(t, pool, "Stranger")
	var org uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO orgs (name, slug, created_by) VALUES ('Acme', 'acme-rec', $1) RETURNING id`, host).Scan(&org); err != nil {
		t.Fatal(err)
	}
	for _, u := range []uuid.UUID{host, member} {
		role := "member"
		if u == host {
			role = "owner"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)`, org, u, role); err != nil {
			t.Fatal(err)
		}
	}
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, OrgID: &org, Title: "Board", Access: AccessInvited, InviteeIDs: []uuid.UUID{invitee}})
	if err != nil {
		t.Fatal(err)
	}
	session := goLive(t, s, f, m.ID, host, attendee)
	w := call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String())
	rec := decode[Recording](t, w)
	if _, err := pool.Exec(ctx, `UPDATE calls_recordings SET status = 'ready', size_bytes = 10 WHERE id = $1`, rec.ID); err != nil {
		t.Fatal(err)
	}
	can := func(u uuid.UUID) int {
		return call(t, httpx.Handler(s.getRecording), u, "GET", "", "recordingID", rec.ID.String()).Code
	}
	listed := func(u uuid.UUID) int {
		w := call(t, httpx.Handler(s.listMeetingRecordings), u, "GET", "", "id", m.ID.String())
		return len(decode[httpx.Page[Recording]](t, w).Items)
	}
	expect := func(label string, want map[uuid.UUID]int) {
		t.Helper()
		for u, code := range want {
			if got := can(u); got != code {
				t.Errorf("%s: user %s got %d, want %d", label, u, got, code)
			}
			n := 0
			if code == http.StatusOK {
				n = 1
			}
			if got := listed(u); got != n {
				t.Errorf("%s: user %s lists %d, want %d", label, u, got, n)
			}
		}
	}
	expect("participants", map[uuid.UUID]int{host: 200, attendee: 200, invitee: 404, member: 404, stranger: 404})
	if _, err := pool.Exec(ctx, `UPDATE calls_meetings SET records_access = 'invited' WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	expect("invited", map[uuid.UUID]int{host: 200, attendee: 200, invitee: 200, member: 404, stranger: 404})
	if _, err := pool.Exec(ctx, `UPDATE calls_meetings SET records_access = 'org' WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	expect("org", map[uuid.UUID]int{host: 200, attendee: 200, invitee: 200, member: 200, stranger: 404})

	// Transcripts follow the same rule.
	if ok, _ := CanReadRecords(ctx, pool, session, stranger); ok {
		t.Fatal("stranger may read records")
	}
	if ok, _ := CanReadRecords(ctx, pool, session, member); !ok {
		t.Fatal("member may not read records under org")
	}

	// Ready recordings come with playback links only for people who may open them.
	got := decode[Recording](t, call(t, httpx.Handler(s.getRecording), attendee, "GET", "", "recordingID", rec.ID.String()))
	if got.CanDelete {
		t.Fatal("an attendee may delete")
	}
	if w := call(t, httpx.Handler(s.deleteRecording), attendee, "DELETE", "", "recordingID", rec.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("attendee delete = %d", w.Code)
	}
	if w := call(t, httpx.Handler(s.deleteRecording), stranger, "DELETE", "", "recordingID", rec.ID.String()); w.Code != http.StatusNotFound {
		t.Fatalf("stranger delete = %d", w.Code)
	}
	if w := call(t, httpx.Handler(s.deleteRecording), host, "DELETE", "", "recordingID", rec.ID.String()); w.Code != http.StatusNoContent {
		t.Fatalf("host delete = %d %s", w.Code, w.Body)
	}
	if can(host) != http.StatusNotFound {
		t.Fatal("deleted recording still there")
	}
}

// Host rules reach the room: tokens leave out what people may not publish,
// changing a rule re-applies permissions to everyone but hosts, and asking
// someone to unmute lets them while unmuting is off.
func TestRoomRulesAndPermissions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	ana := newUser(t, pool, "Ana")
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "All hands", Access: AccessOpen, LobbyEnabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	session := goLive(t, s, f, m.ID, host, ana)
	w := call(t, httpx.Handler(s.updateMeeting), host, "PATCH", `{"allow_screen_share":false,"allow_unmute":false,"mute_on_entry":true}`, "id", m.ID.String())
	if w.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", w.Code, w.Body)
	}
	d := decode[MeetingDetail](t, w)
	if d.AllowScreenShare || d.AllowUnmute || !d.MuteOnEntry {
		t.Fatalf("rules = %+v", d.MeetingRules)
	}
	meta := f.meta(t, m.ID.String())
	if meta.Rules.ScreenShare || meta.Rules.Unmute || !meta.Rules.MuteOnEntry {
		t.Fatalf("room rules = %+v", meta.Rules)
	}
	anaKey := m.ID.String() + "/" + userIdentity(ana)
	if got := f.permissions[anaKey]; strings.Join(got, ",") != SourceCamera {
		t.Fatalf("ana's sources = %v", got)
	}
	if _, ok := f.permissions[m.ID.String()+"/"+userIdentity(host)]; ok {
		t.Fatal("host permissions were changed")
	}
	// Ana's next token carries the same limits.
	tok := decode[JoinToken](t, call(t, httpx.Handler(s.join), ana, "POST", "", "id", m.ID.String()))
	var claims liveKitClaims
	if err := verifyHS256([]byte(testSecret), tok.Token, time.Now(), &claims); err != nil {
		t.Fatal(err)
	}
	if strings.Join(claims.Video.CanPublishSources, ",") != SourceCamera {
		t.Fatalf("token sources = %v", claims.Video.CanPublishSources)
	}
	// Asking Ana to unmute lets her and tells only her.
	if w := call(t, httpx.Handler(s.askUnmute), host, "POST", `{"identity":"`+userIdentity(ana)+`"}`, "id", m.ID.String()); w.Code != http.StatusNoContent {
		t.Fatalf("ask = %d %s", w.Code, w.Body)
	}
	if got := f.permissions[anaKey]; strings.Join(got, ",") != SourceCamera+","+SourceMicrophone {
		t.Fatalf("after ask: %v", got)
	}
	last := f.sent[len(f.sent)-1]
	if last.Payload.Type != SignalAskUnmute || len(last.To) != 1 || last.To[0] != userIdentity(ana) {
		t.Fatalf("signal = %+v", last)
	}
	// Spotlight reaches the room metadata; non-hosts can't.
	if w := call(t, httpx.Handler(s.spotlight), ana, "PUT", `{"identity":"`+userIdentity(ana)+`"}`, "id", m.ID.String()); w.Code != http.StatusForbidden {
		t.Fatalf("ana spotlight = %d", w.Code)
	}
	call(t, httpx.Handler(s.spotlight), host, "PUT", `{"identity":"`+userIdentity(ana)+`"}`, "id", m.ID.String())
	if f.meta(t, m.ID.String()).Spotlight != userIdentity(ana) {
		t.Fatal("spotlight not in metadata")
	}
	var speakers []string
	_ = pool.QueryRow(ctx, `SELECT speakers FROM calls_sessions WHERE id = $1`, session).Scan(&speakers)
	if len(speakers) != 1 {
		t.Fatalf("speakers = %v", speakers)
	}
}

// Export includes what someone said, wrote, asked and voted, and recordings
// of meetings they host; deletion removes their lines, messages, questions
// and votes and keeps the recording for the others.
func TestAccountExportAndDeleteRecords(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	f := newFakeMedia()
	s := testService(t, pool, f)
	host := newUser(t, pool, "Host")
	ana := newUser(t, pool, "Ana")
	m, err := CreateMeeting(ctx, pool, MeetingInput{HostID: host, Title: "Retro", Access: AccessOpen, LobbyEnabled: ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	session := goLive(t, s, f, m.ID, host, ana)
	if _, err := pool.Exec(ctx, `INSERT INTO calls_transcript_lines (session_id, identity, user_id, name, body, spoken_at) VALUES ($1, $2, $3, 'Ana', 'Hello there', now())`, session, userIdentity(ana), ana); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO calls_chat_messages (session_id, identity, user_id, name, body) VALUES ($1, $2, $3, 'Ana', 'Link?')`, session, userIdentity(ana), ana); err != nil {
		t.Fatal(err)
	}
	p := decode[Poll](t, call(t, s.userAct(s.postPoll), host, "POST", `{"question":"Again?","options":["Yes","No"],"open":true}`, "id", m.ID.String()))
	call(t, s.userAct(s.votePoll), ana, "POST", `{"choices":[1]}`, "id", m.ID.String(), "pollID", p.ID.String())
	call(t, s.userAct(s.askQuestion), ana, "POST", `{"body":"Next date?"}`, "id", m.ID.String())
	rec := decode[Recording](t, call(t, httpx.Handler(s.startRecording), host, "POST", "", "id", m.ID.String()))

	out, err := s.ExportAccount(ctx, ana)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	for _, want := range []string{`"Hello there"`, `"Link?"`, `"Next date?"`, `"choices":["No"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("export lacks %s: %s", want, b)
		}
	}
	hostOut, err := s.ExportAccount(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if hb, _ := json.Marshal(hostOut); !strings.Contains(string(hb), `"recordings_of_meetings_hosted":[{`) {
		t.Errorf("host export lacks the recording: %s", hb)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, tx, ana); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM calls_transcript_lines WHERE user_id = $1) + (SELECT count(*) FROM calls_chat_messages WHERE user_id = $1)
		+ (SELECT count(*) FROM calls_questions WHERE user_id = $1) + (SELECT count(*) FROM calls_poll_votes WHERE user_id = $1)`, ana).Scan(&left)
	if left != 0 {
		t.Fatalf("%d rows of Ana's remain", left)
	}
	var kept int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM calls_recordings WHERE id = $1`, rec.ID).Scan(&kept)
	if kept != 1 {
		t.Fatal("the recording went with Ana's account")
	}
}
