package calls

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// fakeMedia is an in-memory SFU for tests: rooms with participants, room
// metadata, data messages, permissions and recordings. Tokens and webhook
// verification are LiveKit's real code (the embedded liveKit), so signed
// webhooks built by signedWebhook go through the production parser.
type fakeMedia struct {
	*liveKit
	mu          sync.Mutex
	rooms       map[string][]MediaParticipant
	metadata    map[string]string
	sent        []fakeData
	permissions map[string][]string // room/identity → sources
	recordings  map[string]string   // egress id → room
	ended       []string
	startErr    error
}

type fakeData struct {
	Room, Topic string
	Payload     signal
	To          []string
}

const testSecret = "0123456789abcdef0123456789abcdef0123"

func newFakeMedia() *fakeMedia {
	return &fakeMedia{liveKit: newLiveKit("ws://sfu.test", "http://sfu.test", "testkey", testSecret),
		rooms: map[string][]MediaParticipant{}, metadata: map[string]string{}, permissions: map[string][]string{}, recordings: map[string]string{}}
}

func (f *fakeMedia) EnsureRoom(_ context.Context, room string, o RoomOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rooms[room]; !ok {
		f.rooms[room] = []MediaParticipant{}
		f.metadata[room] = o.Metadata
	}
	return nil
}

func (f *fakeMedia) join(room, identity, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rooms[room] = append(f.rooms[room], MediaParticipant{Identity: identity, Name: name})
}

func (f *fakeMedia) ListParticipants(_ context.Context, room string) ([]MediaParticipant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]MediaParticipant{}, f.rooms[room]...), nil
}
func (f *fakeMedia) MuteTrack(context.Context, string, string, string) error { return nil }
func (f *fakeMedia) RemoveParticipant(_ context.Context, room, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.rooms[room]
	for i, p := range list {
		if p.Identity == identity {
			f.rooms[room] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return errMediaNotFound
}
func (f *fakeMedia) UpdateParticipant(context.Context, string, string, string, map[string]string) error { return nil }
func (f *fakeMedia) EndRoom(_ context.Context, room string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rooms, room)
	f.ended = append(f.ended, room)
	return nil
}
func (f *fakeMedia) LiveRooms(_ context.Context, rooms []string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]bool{}
	for _, r := range rooms {
		_, out[r] = f.rooms[r]
	}
	return out, nil
}

func (f *fakeMedia) UpdateRoomMetadata(_ context.Context, room, metadata string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metadata[room] = metadata
	return nil
}
func (f *fakeMedia) SendData(_ context.Context, room, topic string, payload []byte, identities []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var sig signal
	_ = json.Unmarshal(payload, &sig)
	f.sent = append(f.sent, fakeData{Room: room, Topic: topic, Payload: sig, To: identities})
	return nil
}
func (f *fakeMedia) UpdatePermissions(_ context.Context, room, identity string, sources []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.permissions[room+"/"+identity] = sources
	return nil
}
func (f *fakeMedia) StartRecording(_ context.Context, room string, out RecordingOutput) (RecordingInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return RecordingInfo{}, f.startErr
	}
	id := fmt.Sprintf("EG_%d", len(f.recordings)+1)
	f.recordings[id] = room
	return RecordingInfo{ID: id, Room: room, Status: RecStarting}, nil
}
func (f *fakeMedia) StopRecording(_ context.Context, id string) (RecordingInfo, error) {
	return RecordingInfo{ID: id, Status: RecEnding}, nil
}

func (f *fakeMedia) meta(t *testing.T, room string) RoomMeta {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var m RoomMeta
	if err := json.Unmarshal([]byte(f.metadata[room]), &m); err != nil {
		t.Fatalf("room %s metadata %q: %v", room, f.metadata[room], err)
	}
	return m
}

// signedWebhook builds a LiveKit webhook request for body, signed like
// LiveKit does (Authorization: <jwt> with the body's base64 SHA-256).
func signedWebhook(t *testing.T, body string) *http.Request {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	now := time.Now()
	tok, err := signHS256([]byte(testSecret), liveKitClaims{
		registeredClaims: registeredClaims{Issuer: "testkey", NotBefore: now.Add(-time.Second).Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Sha256:           base64.StdEncoding.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/webhooks/livekit", strings.NewReader(body))
	r.Header.Set("Authorization", tok)
	r.Header.Set("Content-Type", "application/webhook+json")
	return r
}

// testService builds the module on a throwaway database with a fake SFU.
func testService(t *testing.T, pool *pgxpool.Pool, media Media) *Service {
	t.Helper()
	broker := realtime.NewMemory()
	s := &Service{d: &app.Deps{Pool: pool, Broker: broker, Notify: notify.Realtime{Broker: broker},
		Cfg: &config.Config{PublicWebURL: "https://app.test", S3Bucket: "bucket", S3Region: "us-east-1"}},
		cfg: settings{tokenTTL: 10 * time.Minute, ringTimeout: 45 * time.Second, maxParticipants: 100},
		rec: recordingSettings{enabled: true, retention: 24 * time.Hour, layout: "speaker"}, media: media}
	current.Store(s)
	return s
}

func newUser(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (auth_subject, email, display_name) VALUES ($1, $2, $3) RETURNING id`,
		"test_"+strings.ToLower(name)+"_"+uuid.NewString()[:8], strings.ToLower(name)+"@example.test", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// call runs a handler as user with URL params and a JSON body.
func call(t *testing.T, h http.HandlerFunc, user uuid.UUID, method, body string, params ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/", strings.NewReader(body))
	rc := chi.NewRouteContext()
	for i := 0; i+1 < len(params); i += 2 {
		rc.URLParams.Add(params[i], params[i+1])
	}
	ctx := context.WithValue(context.Background(), chi.RouteCtxKey, rc)
	if user != uuid.Nil {
		ctx = auth.WithIdentity(ctx, &auth.Identity{UserID: user, Status: "active", DisplayName: "Tester"})
	}
	r = r.WithContext(ctx)
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %T from %d %s: %v", v, w.Code, w.Body.String(), err)
	}
	return v
}

// goLive opens a session for a meeting with people in its room, as the
// SFU's webhooks would.
func goLive(t *testing.T, s *Service, f *fakeMedia, meetingID uuid.UUID, people ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	session, err := ensureSession(ctx, s.d.Pool, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	room := meetingID.String()
	_ = f.EnsureRoom(ctx, room, RoomOptions{})
	for i, u := range people {
		f.join(room, userIdentity(u), "Person")
		if _, err := s.d.Pool.Exec(ctx, `INSERT INTO calls_participants (session_id, identity, user_id, participant_sid, joined_at) VALUES ($1, $2, $3, $4, now())`,
			session, userIdentity(u), u, fmt.Sprintf("PA_%s_%d", session.String()[:8], i)); err != nil {
			t.Fatal(err)
		}
	}
	return session
}
