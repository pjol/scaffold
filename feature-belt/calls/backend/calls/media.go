package calls

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Media is the SFU behind calls. LiveKit (livekit.go) is the shipped
// implementation; another SFU implements these operations and is installed
// with SetMedia from stitching code. Room names are meeting ids; identities
// are "u:<user id>" for people and "g:<lobby entry id>" for guests.
type Media interface {
	// Name is the provider's name for logs ("livekit").
	Name() string
	// ClientURL is the URL clients connect to (wss://… for LiveKit).
	ClientURL() string
	// Token mints a short-lived credential that lets one identity join one room.
	Token(g Grant) (token string, expiresAt time.Time, err error)
	// EnsureRoom creates the room if it isn't running (idempotent).
	EnsureRoom(ctx context.Context, room string, opts RoomOptions) error
	// ListParticipants lists who is in a running room (empty when it isn't).
	ListParticipants(ctx context.Context, room string) ([]MediaParticipant, error)
	// MuteTrack mutes one published track (hosts can mute, never unmute).
	MuteTrack(ctx context.Context, room, identity, trackSID string) error
	// RemoveParticipant disconnects identity from room.
	RemoveParticipant(ctx context.Context, room, identity string) error
	// UpdateParticipant sets server-owned metadata and attributes (an
	// attribute set to "" is deleted). Empty metadata leaves it unchanged.
	UpdateParticipant(ctx context.Context, room, identity, metadata string, attributes map[string]string) error
	// EndRoom disconnects everyone and closes the room.
	EndRoom(ctx context.Context, room string) error
	// LiveRooms reports which of rooms are running.
	LiveRooms(ctx context.Context, rooms []string) (map[string]bool, error)
	// ParseWebhook authenticates an SFU webhook and normalises it.
	ParseWebhook(r *http.Request, body []byte) (MediaEvent, error)
}

// Grant is what one join credential allows.
type Grant struct {
	Room       string
	Identity   string
	Name       string
	Metadata   string            // server-owned JSON the clients read (role, avatar)
	Attributes map[string]string // initial attributes
	TTL        time.Duration
}

// RoomOptions apply when a room is created.
type RoomOptions struct {
	MaxParticipants  int
	EmptyTimeout     time.Duration // close after this long with nobody ever joining
	DepartureTimeout time.Duration // close this long after the last person leaves
}

// MediaParticipant is one person in a running room.
type MediaParticipant struct {
	Identity   string
	Name       string
	Attributes map[string]string
	Tracks     []MediaTrack
}

// MediaTrack is one published track.
type MediaTrack struct {
	SID    string
	Kind   string // audio | video
	Source string // microphone | camera | screen_share | screen_share_audio | unknown
	Muted  bool
}

// Webhook event kinds the module records.
const (
	EventRoomStarted       = "room_started"
	EventRoomFinished      = "room_finished"
	EventParticipantJoined = "participant_joined"
	EventParticipantLeft   = "participant_left"
)

// MediaEvent is a normalised SFU webhook.
type MediaEvent struct {
	ID             string // unique per event (deduplication)
	Kind           string // one of the Event* constants, or the provider's name for others
	Room           string // room name (a meeting id)
	RoomSID        string // the provider's id for this room instance
	Identity       string
	Name           string
	ParticipantSID string
	At             time.Time
}

// errMediaNotFound is returned when the room or participant isn't running.
var errMediaNotFound = errors.New("calls: not found at the media server")

// errWebhookAuth is returned for webhooks that fail authentication.
var errWebhookAuth = errors.New("calls: webhook signature invalid")
