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
			CanPublishSources:    []string{"camera", "microphone", "screen_share", "screen_share_audio"},
			CanUpdateOwnMetadata: ptr(false),
		},
	})
	return tok, exp, err
}

// twirp calls one RoomService method with a one-minute admin token.
func (l *liveKit) twirp(ctx context.Context, method, room string, req, resp any) error {
	now := time.Now()
	tok, err := signHS256(l.secret, liveKitClaims{
		registeredClaims: registeredClaims{Issuer: l.key, NotBefore: now.Add(-5 * time.Second).Unix(), ExpiresAt: now.Add(time.Minute).Unix()},
		Video:            &videoGrant{RoomAdmin: true, RoomCreate: true, RoomList: true, Room: room},
	})
	if err != nil {
		return err
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, l.apiURL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
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
	return l.twirp(ctx, "CreateRoom", room, map[string]any{
		"name":              room,
		"max_participants":  o.MaxParticipants,
		"empty_timeout":     int(o.EmptyTimeout.Seconds()),
		"departure_timeout": int(o.DepartureTimeout.Seconds()),
	}, nil)
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
	return out, nil
}
