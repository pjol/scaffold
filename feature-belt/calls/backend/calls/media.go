package calls

import (
	"context"
	"errors"
	"net/http"
	"time"

	"scaffoldapp/backend/internal/httpx"
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

// RoomController is the optional part of Media the room's shared state
// needs: server-owned room metadata every participant reads (recording,
// spotlight, the host's rules, breakout rooms), server-sent data messages
// (poll and question updates, broadcasts) and per-participant publish
// permissions (presenting, unmuting). LiveKit implements it; without it
// those controls answer 501.
type RoomController interface {
	UpdateRoomMetadata(ctx context.Context, room, metadata string) error
	// SendData sends a reliable data message on topic to identities (all when empty).
	SendData(ctx context.Context, room, topic string, payload []byte, identities []string) error
	// UpdatePermissions sets which sources (Source* constants) identity may publish.
	UpdatePermissions(ctx context.Context, room, identity string, sources []string) error
}

// Recorder is the optional part of Media that records a room into one
// video file in object storage (LiveKit Egress, room composite).
type Recorder interface {
	StartRecording(ctx context.Context, room string, out RecordingOutput) (RecordingInfo, error)
	StopRecording(ctx context.Context, recordingID string) (RecordingInfo, error)
}

// RecordingOutput is where and how a recording is written.
type RecordingOutput struct {
	Filepath string // object key
	Layout   string // provider layout ("speaker")
	S3       S3Target
}

// S3Target is the bucket a recorder uploads to.
type S3Target struct {
	AccessKey, Secret, Region, Endpoint, Bucket string
	ForcePathStyle                              bool
}

// RecordingInfo is the provider's view of a recording.
type RecordingInfo struct {
	ID         string // the provider's id (LiveKit egress id)
	Room       string
	Status     string // RecStarting | RecActive | RecEnding | RecComplete | RecFailed
	Error      string
	SizeBytes  int64
	DurationMs int64
	StartedAt  time.Time
	EndedAt    time.Time
}

// Normalised recording states (RecordingInfo.Status).
const (
	RecStarting = "starting"
	RecActive   = "active"
	RecEnding   = "ending"
	RecComplete = "complete"
	RecFailed   = "failed"
)

// Track sources a participant may publish (Grant.Sources, UpdatePermissions).
const (
	SourceCamera           = "camera"
	SourceMicrophone       = "microphone"
	SourceScreenShare      = "screen_share"
	SourceScreenShareAudio = "screen_share_audio"
)

var allSources = []string{SourceCamera, SourceMicrophone, SourceScreenShare, SourceScreenShareAudio}

// Grant is what one join credential allows.
type Grant struct {
	Room       string
	Identity   string
	Name       string
	Metadata   string            // server-owned JSON the clients read (role, avatar)
	Attributes map[string]string // initial attributes
	Sources    []string          // what they may publish (nil = every source)
	TTL        time.Duration
}

// RoomOptions apply when a room is created.
type RoomOptions struct {
	MaxParticipants  int
	EmptyTimeout     time.Duration // close after this long with nobody ever joining
	DepartureTimeout time.Duration // close this long after the last person leaves
	Metadata         string        // initial room metadata
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
	EventEgressStarted     = "egress_started"
	EventEgressUpdated     = "egress_updated"
	EventEgressEnded       = "egress_ended"
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
	Recording      *RecordingInfo // egress_* events
}

// errNoRoomControl is returned when the media server can't do room state.
var errNoRoomControl = httpx.NewError(http.StatusNotImplemented, "calls_unsupported", "This call server can't do that.")

// errMediaNotFound is returned when the room or participant isn't running.
var errMediaNotFound = errors.New("calls: not found at the media server")

// errWebhookAuth is returned for webhooks that fail authentication.
var errWebhookAuth = errors.New("calls: webhook signature invalid")
