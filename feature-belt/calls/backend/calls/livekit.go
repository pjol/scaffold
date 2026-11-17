package calls

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// liveKit implements Media against a LiveKit server (LiveKit Cloud or
// self-hosted): access tokens are HS256 JWTs with a video grant, host
// actions go to the RoomService Twirp API, webhooks are JWT-signed with a
// body hash. https://docs.livekit.io/home/server/
type liveKit struct {
	clientURL string // wss://… handed to clients
	apiURL    string // https://… for RoomService
	key       string
	secret    []byte
	http      *http.Client
}

func newLiveKit(clientURL, apiURL, key, secret string) *liveKit {
	return &liveKit{
		clientURL: strings.TrimRight(clientURL, "/"),
		apiURL:    strings.TrimRight(apiURL, "/"),
		key:       key,
		secret:    []byte(secret),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

// liveKitAPIURL derives the RoomService origin from the client URL
// (wss://x → https://x, ws://x → http://x).
func liveKitAPIURL(clientURL string) string {
	u := strings.TrimRight(strings.TrimSpace(clientURL), "/")
	switch {
	case strings.HasPrefix(u, "wss://"):
		return "https://" + strings.TrimPrefix(u, "wss://")
	case strings.HasPrefix(u, "ws://"):
		return "http://" + strings.TrimPrefix(u, "ws://")
	}
	return u
}

func (l *liveKit) Name() string      { return "livekit" }
func (l *liveKit) ClientURL() string { return l.clientURL }

// videoGrant is LiveKit's "video" claim. Booleans that default to true in
// LiveKit are pointers so false is sent explicitly.
type videoGrant struct {
	RoomJoin             bool     `json:"roomJoin,omitempty"`
	RoomCreate           bool     `json:"roomCreate,omitempty"`
	RoomList             bool     `json:"roomList,omitempty"`
	RoomAdmin            bool     `json:"roomAdmin,omitempty"`
	RoomRecord           bool     `json:"roomRecord,omitempty"`
	Room                 string   `json:"room,omitempty"`
	CanPublish           *bool    `json:"canPublish,omitempty"`
	CanSubscribe         *bool    `json:"canSubscribe,omitempty"`
	CanPublishData       *bool    `json:"canPublishData,omitempty"`
	CanPublishSources    []string `json:"canPublishSources,omitempty"`
	CanUpdateOwnMetadata *bool    `json:"canUpdateOwnMetadata,omitempty"`
}

type liveKitClaims struct {
	registeredClaims
	Name       string            `json:"name,omitempty"`
	Metadata   string            `json:"metadata,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Video      *videoGrant       `json:"video,omitempty"`
	Sha256     string            `json:"sha256,omitempty"`
}

func ptr[T any](v T) *T { return &v }

// Token mints a join token. Participants publish camera, microphone and
// screen share, subscribe, and send data (chat, reactions). They can't
// change their own name, metadata or attributes: names come from accounts
// or the lobby, and roles and raised hands are set by the server.
func (l *liveKit) Token(g Grant) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(g.TTL)
	sources := g.Sources
	if sources == nil {
		sources = allSources
	}
	tok, err := signHS256(l.secret, liveKitClaims{
		registeredClaims: registeredClaims{Issuer: l.key, Subject: g.Identity, NotBefore: now.Add(-5 * time.Second).Unix(), ExpiresAt: exp.Unix()},
		Name:             g.Name,
		Metadata:         g.Metadata,
		Attributes:       g.Attributes,
		Video: &videoGrant{
			RoomJoin:             true,
			Room:                 g.Room,
			CanPublish:           ptr(true),
			CanSubscribe:         ptr(true),
			CanPublishData:       ptr(true),
			CanPublishSources:    sources,
			CanUpdateOwnMetadata: ptr(false),
		},
	})
	return tok, exp, err
}

// twirp calls one RoomService method with a one-minute admin token.
func (l *liveKit) twirp(ctx context.Context, method, room string, req, resp any) error {
	return l.rpc(ctx, "livekit.RoomService", method, &videoGrant{RoomAdmin: true, RoomCreate: true, RoomList: true, Room: room}, req, resp)
}

// rpc calls one Twirp method of a LiveKit service with a one-minute token
// carrying grant.
func (l *liveKit) rpc(ctx context.Context, service, method string, grant *videoGrant, req, resp any) error {
	now := time.Now()
	tok, err := signHS256(l.secret, liveKitClaims{
		registeredClaims: registeredClaims{Issuer: l.key, NotBefore: now.Add(-5 * time.Second).Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Video:            grant,
	})
	if err != nil {
		return err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, l.apiURL+"/twirp/"+service+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+tok)
	res, err := l.http.Do(hr)
	if err != nil {
		return fmt.Errorf("livekit %s: %w", method, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var te struct {
			Code string `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(raw, &te)
		if te.Code == "not_found" {
			return errMediaNotFound
		}
		return fmt.Errorf("livekit %s: status %d %s: %s", method, res.StatusCode, te.Code, te.Msg)
	}
	if resp == nil {
		return nil
	}
	return json.Unmarshal(raw, resp)
}

func (l *liveKit) EnsureRoom(ctx context.Context, room string, o RoomOptions) error {
	req := map[string]any{
		"name":              room,
		"max_participants":  o.MaxParticipants,
		"empty_timeout":     int(o.EmptyTimeout.Seconds()),
		"departure_timeout": int(o.DepartureTimeout.Seconds()),
	}
	if o.Metadata != "" {
		req["metadata"] = o.Metadata
	}
	return l.twirp(ctx, "CreateRoom", room, req, nil)
}

func (l *liveKit) UpdateRoomMetadata(ctx context.Context, room, metadata string) error {
	return l.twirp(ctx, "UpdateRoomMetadata", room, map[string]any{"room": room, "metadata": metadata}, nil)
}

func (l *liveKit) SendData(ctx context.Context, room, topic string, payload []byte, identities []string) error {
	req := map[string]any{"room": room, "data": base64.StdEncoding.EncodeToString(payload), "kind": "RELIABLE", "topic": topic}
	if len(identities) > 0 {
		req["destination_identities"] = identities
	}
	err := l.twirp(ctx, "SendData", room, req, nil)
	if err == errMediaNotFound {
		return nil // nobody is in that room
	}
	return err
}

// lkSources maps our source names to LiveKit's TrackSource enum names.
var lkSources = map[string]string{SourceCamera: "CAMERA", SourceMicrophone: "MICROPHONE", SourceScreenShare: "SCREEN_SHARE", SourceScreenShareAudio: "SCREEN_SHARE_AUDIO"}

// UpdatePermissions replaces identity's permission (LiveKit replaces the
// whole message, so every field is sent); revoked sources are unpublished.
func (l *liveKit) UpdatePermissions(ctx context.Context, room, identity string, sources []string) error {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		if n, ok := lkSources[s]; ok {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("livekit: an empty source list would allow every source")
	}
	return l.twirp(ctx, "UpdateParticipant", room, map[string]any{
		"room": room, "identity": identity,
		"permission": map[string]any{
			"can_subscribe": true, "can_publish": true, "can_publish_data": true,
			"can_publish_sources": names, "can_update_metadata": false,
		},
	}, nil)
}

// lkEgressInfo is the part of LiveKit's EgressInfo we read. Twirp answers
// with proto names, webhooks with camelCase; int64s arrive as strings and
// enums as names (numbers are accepted too).
type lkEgressInfo struct {
	EgressID    string       `json:"egress_id"`
	EgressID2   string       `json:"egressId"`
	RoomName    string       `json:"room_name"`
	RoomName2   string       `json:"roomName"`
	Status      flexEnum     `json:"status"`
	Error       string       `json:"error"`
	StartedAt   flexInt      `json:"started_at"`
	StartedAt2  flexInt      `json:"startedAt"`
	EndedAt     flexInt      `json:"ended_at"`
	EndedAt2    flexInt      `json:"endedAt"`
	FileResults []lkFileInfo `json:"file_results"`
	FileResult2 []lkFileInfo `json:"fileResults"`
}

type lkFileInfo struct {
	Filename string  `json:"filename"`
	Size     flexInt `json:"size"`
	Duration flexInt `json:"duration"` // nanoseconds
}

// egressStatus maps LiveKit's EgressStatus to RecordingInfo.Status.
func egressStatus(v string) string {
	switch v {
	case "EGRESS_STARTING", "0", "":
		return RecStarting
	case "EGRESS_ACTIVE", "1":
		return RecActive
	case "EGRESS_ENDING", "2":
		return RecEnding
	case "EGRESS_COMPLETE", "3", "EGRESS_LIMIT_REACHED", "6":
		return RecComplete
	}
	return RecFailed // EGRESS_FAILED, EGRESS_ABORTED
}

func (e lkEgressInfo) info() RecordingInfo {
	pick := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	ts := func(a, b flexInt) time.Time {
		n := int64(a)
		if n == 0 {
			n = int64(b)
		}
		if n == 0 {
			return time.Time{}
		}
		return time.Unix(0, n).UTC() // nanoseconds
	}
	out := RecordingInfo{ID: pick(e.EgressID, e.EgressID2), Room: pick(e.RoomName, e.RoomName2), Status: egressStatus(string(e.Status)),
		Error: e.Error, StartedAt: ts(e.StartedAt, e.StartedAt2), EndedAt: ts(e.EndedAt, e.EndedAt2)}
	files := e.FileResults
	if len(files) == 0 {
		files = e.FileResult2
	}
	for _, f := range files {
		out.SizeBytes += int64(f.Size)
		out.DurationMs += int64(f.Duration) / int64(time.Millisecond)
	}
	return out
}

// StartRecording starts a room-composite egress writing one MP4 to S3.
func (l *liveKit) StartRecording(ctx context.Context, room string, out RecordingOutput) (RecordingInfo, error) {
	layout := out.Layout
	if layout == "" {
		layout = "speaker"
	}
	s3 := map[string]any{"access_key": out.S3.AccessKey, "secret": out.S3.Secret, "region": out.S3.Region, "bucket": out.S3.Bucket,
		"force_path_style": out.S3.ForcePathStyle, "content_disposition": "inline"}
	if out.S3.Endpoint != "" {
		s3["endpoint"] = out.S3.Endpoint
	}
	var info lkEgressInfo
	err := l.rpc(ctx, "livekit.Egress", "StartRoomCompositeEgress", &videoGrant{RoomRecord: true, Room: room}, map[string]any{
		"room_name":    room,
		"layout":       layout,
		"file_outputs": []any{map[string]any{"file_type": "MP4", "filepath": out.Filepath, "disable_manifest": true, "s3": s3}},
	}, &info)
	if err != nil {
		return RecordingInfo{}, err
	}
	return info.info(), nil
}

func (l *liveKit) StopRecording(ctx context.Context, id string) (RecordingInfo, error) {
	var info lkEgressInfo
	if err := l.rpc(ctx, "livekit.Egress", "StopEgress", &videoGrant{RoomRecord: true}, map[string]any{"egress_id": id}, &info); err != nil {
		return RecordingInfo{}, err
	}
	return info.info(), nil
}

type lkTrack struct {
	SID    string `json:"sid"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Muted  bool   `json:"muted"`
}

type lkParticipant struct {
	Identity   string            `json:"identity"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
	Tracks     []lkTrack         `json:"tracks"`
}

func (l *liveKit) ListParticipants(ctx context.Context, room string) ([]MediaParticipant, error) {
	var out struct {
		Participants []lkParticipant `json:"participants"`
	}
	if err := l.twirp(ctx, "ListParticipants", room, map[string]any{"room": room}, &out); err != nil {
		if err == errMediaNotFound {
			return nil, nil
		}
		return nil, err
	}
	list := make([]MediaParticipant, 0, len(out.Participants))
	for _, p := range out.Participants {
		mp := MediaParticipant{Identity: p.Identity, Name: p.Name, Attributes: p.Attributes}
		for _, t := range p.Tracks {
			mp.Tracks = append(mp.Tracks, MediaTrack{SID: t.SID, Kind: strings.ToLower(t.Type), Source: strings.ToLower(t.Source), Muted: t.Muted})
		}
		list = append(list, mp)
	}
	return list, nil
}

func (l *liveKit) MuteTrack(ctx context.Context, room, identity, trackSID string) error {
	return l.twirp(ctx, "MutePublishedTrack", room, map[string]any{"room": room, "identity": identity, "track_sid": trackSID, "muted": true}, nil)
}

func (l *liveKit) RemoveParticipant(ctx context.Context, room, identity string) error {
	return l.twirp(ctx, "RemoveParticipant", room, map[string]any{"room": room, "identity": identity}, nil)
}

func (l *liveKit) UpdateParticipant(ctx context.Context, room, identity, metadata string, attributes map[string]string) error {
	req := map[string]any{"room": room, "identity": identity}
	if metadata != "" {
		req["metadata"] = metadata
	}
	if len(attributes) > 0 {
		req["attributes"] = attributes
	}
	return l.twirp(ctx, "UpdateParticipant", room, req, nil)
}

func (l *liveKit) EndRoom(ctx context.Context, room string) error {
	err := l.twirp(ctx, "DeleteRoom", room, map[string]any{"room": room}, nil)
	if err == errMediaNotFound {
		return nil
	}
	return err
}

func (l *liveKit) LiveRooms(ctx context.Context, rooms []string) (map[string]bool, error) {
	var out struct {
		Rooms []struct {
			Name string `json:"name"`
		} `json:"rooms"`
	}
	if err := l.twirp(ctx, "ListRooms", "", map[string]any{"names": rooms}, &out); err != nil {
		return nil, err
	}
	live := make(map[string]bool, len(out.Rooms))
	for _, r := range out.Rooms {
		live[r.Name] = true
	}
	return live, nil
}

// flexInt decodes protojson int64s, which arrive as strings ("1793479917").
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	*f = flexInt(n)
	return err
}

// flexEnum decodes a protojson enum given as its name or its number.
type flexEnum string

func (f *flexEnum) UnmarshalJSON(b []byte) error {
	*f = flexEnum(strings.Trim(string(b), `"`))
	return nil
}

// ParseWebhook verifies LiveKit's Authorization JWT (issuer = our API key,
// sha256 claim = base64 SHA-256 of the body) and decodes the event.
func (l *liveKit) ParseWebhook(r *http.Request, body []byte) (MediaEvent, error) {
	auth := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	var claims liveKitClaims
	if auth == "" || verifyHS256(l.secret, auth, time.Now(), &claims) != nil || claims.Issuer != l.key {
		return MediaEvent{}, errWebhookAuth
	}
	sum := sha256.Sum256(body)
	if !hmac.Equal([]byte(claims.Sha256), []byte(base64.StdEncoding.EncodeToString(sum[:]))) {
		return MediaEvent{}, errWebhookAuth
	}
	var ev struct {
		Event     string  `json:"event"`
		ID        string  `json:"id"`
		CreatedAt flexInt `json:"createdAt"`
		Room      *struct {
			SID  string `json:"sid"`
			Name string `json:"name"`
		} `json:"room"`
		Participant *struct {
			SID      string `json:"sid"`
			Identity string `json:"identity"`
			Name     string `json:"name"`
		} `json:"participant"`
		EgressInfo *lkEgressInfo `json:"egressInfo"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return MediaEvent{}, fmt.Errorf("livekit webhook: %w", err)
	}
	out := MediaEvent{ID: ev.ID, Kind: ev.Event, At: time.Now().UTC()}
	if ev.CreatedAt > 0 {
		out.At = time.Unix(int64(ev.CreatedAt), 0).UTC()
	}
	if ev.Room != nil {
		out.Room, out.RoomSID = ev.Room.Name, ev.Room.SID
	}
	if ev.Participant != nil {
		out.Identity, out.Name, out.ParticipantSID = ev.Participant.Identity, ev.Participant.Name, ev.Participant.SID
	}
	if ev.EgressInfo != nil {
		info := ev.EgressInfo.info()
		out.Recording = &info
		if out.Room == "" {
			out.Room = info.Room
		}
	}
	return out, nil
}
