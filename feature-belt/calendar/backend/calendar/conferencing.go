package calendar

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Audience says who may join a meeting room the calendar asks for.
const (
	// AudienceInternal: signed-in members of ConferenceRequest.OrgID when it
	// is set, else only the host and ConferenceRequest.ParticipantIDs.
	AudienceInternal = "internal"
	// AudienceExternal: anyone with the link, including guests without an
	// account (external attendees and bookers).
	AudienceExternal = "external"
)

// ConferenceRequest describes the meeting a room is for. The provider
// enforces Audience when someone opens the link.
type ConferenceRequest struct {
	// EventID is the calendar event the room belongs to (for a repeating
	// event, an event of its series; for a booking, the host's event), so
	// the provider can link back to /calendar/event/<id>. Rooms aren't
	// shared between events, but more than one room may name the same
	// series: treat it as a link, not a unique key.
	EventID        uuid.UUID
	HostID         uuid.UUID   // the calendar owner (host of the room)
	Title          string      // event title
	StartsAt       time.Time   // UTC
	EndsAt         time.Time   // UTC
	Audience       string      // AudienceInternal | AudienceExternal
	OrgID          *uuid.UUID  // internal booking links: the link's org
	ParticipantIDs []uuid.UUID // platform users invited (attendees, a signed-in booker)
}

// Conference is a room the provider made.
type Conference struct {
	ID      string // the provider's id; passed back to Update and Cancel
	JoinURL string // absolute URL people open to join
}

// Conferencing creates meeting rooms for events ("Add video call") and for
// bookings whose location is a video call. The app's stitching wires an
// implementation with SetConferencing (for example the calls feature,
// whose rooms live at PUBLIC_WEB_URL/meet/<code>); with none set, video
// calls aren't offered. Calls are made outside database transactions and
// must be safe to retry.
type Conferencing interface {
	Create(ctx context.Context, req ConferenceRequest) (Conference, error)
	// Update changes the room's time, title or participants (reschedules).
	Update(ctx context.Context, id string, req ConferenceRequest) (Conference, error)
	// Cancel closes the room. Cancelling an unknown or closed room is not an error.
	Cancel(ctx context.Context, id string) error
}

var conferencing atomic.Pointer[Conferencing]

// SetConferencing wires the conferencing provider. Call it from the app's
// stitching (internal/api/feature_<name>.go init or a provider's New)
// before requests are served; nil turns video calls off.
func SetConferencing(c Conferencing) {
	if c == nil {
		conferencing.Store(nil)
		return
	}
	conferencing.Store(&c)
}

// conferencingProvider returns the wired provider or nil.
func conferencingProvider() Conferencing {
	if p := conferencing.Load(); p != nil {
		return *p
	}
	return nil
}

// VideoAvailable reports whether a conferencing provider is wired.
func VideoAvailable() bool { return conferencingProvider() != nil }
