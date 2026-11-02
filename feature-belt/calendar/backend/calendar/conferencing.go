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
	// Provider is the ConferenceOption.ID the person picked ("" or
	// ProviderApp = the app's own rooms); ConnectionID the host's connected
	// account for it (Zoom, Teams). Providers the wired Conferencing doesn't
	// offer are refused before Create is called.
	Provider     string
	ConnectionID string
	// GuestEmails are external attendees' addresses, for providers that
	// invite or admit by email. The app's own rooms ignore it.
	GuestEmails []string
}

// Conference is a room the provider made.
type Conference struct {
	ID      string // the provider's id; passed back to Update and Cancel
	JoinURL string // absolute URL people open to join
	// Provider is the option that made it ("" = ProviderApp).
	Provider string
}

// Meeting providers. ProviderApp is the wired Conferencing's own rooms (the
// default); the others are platforms. Google Meet links come from Google
// Calendar itself (an event written to a connected Google calendar); Teams
// from Outlook (an event written to a connected Outlook calendar) or from
// the wired Conferencing (a Graph online meeting); Zoom only from the wired
// Conferencing.
const (
	ProviderApp        = "app"
	ProviderGoogleMeet = "google_meet"
	ProviderTeams      = "teams"
	ProviderZoom       = "zoom"
)

// ConferenceOption is one meeting provider a person can pick for "Video
// call", for one target calendar.
type ConferenceOption struct {
	ID           string `json:"id"`                      // ProviderApp, ProviderZoom, …
	Label        string `json:"label"`                   // "Forum call", "Zoom"
	Platform     string `json:"platform,omitempty"`      // connections registry id (google, microsoft, zoom); "" for the app's own
	ConnectionID string `json:"connection_id,omitempty"` // the account it uses
	Account      string `json:"account,omitempty"`
	Status       string `json:"status,omitempty"` // active | needs_reauth | blocked
	FixURL       string `json:"fix_url,omitempty"`
	// Native options are made by the target calendar's platform (Meet on a
	// Google event, Teams on an Outlook event), not by Conferencing.
	Native bool `json:"native,omitempty"`
}

// ConferenceOptions is implemented by a Conferencing that offers more than
// its own rooms (Zoom, Teams through the calls feature): the options open to
// userID in orgID (uuid.Nil = no workspace), its own rooms first. Without
// it the only option is ProviderApp.
type ConferenceOptions interface {
	ConferenceOptions(ctx context.Context, userID, orgID uuid.UUID) ([]ConferenceOption, error)
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

// seamOptions are the wired Conferencing's options for userID (its own rooms
// first). An error from the provider leaves only its own rooms.
func seamOptions(ctx context.Context, userID, orgID uuid.UUID) []ConferenceOption {
	c := conferencingProvider()
	if c == nil {
		return nil
	}
	if p, ok := c.(ConferenceOptions); ok {
		if list, err := p.ConferenceOptions(ctx, userID, orgID); err == nil && len(list) > 0 {
			return list
		}
	}
	return []ConferenceOption{{ID: ProviderApp, Label: "Video call"}}
}

// seamOption finds one of the wired Conferencing's options.
func seamOption(ctx context.Context, userID, orgID uuid.UUID, id, connectionID string) (ConferenceOption, bool) {
	if id == "" {
		id = ProviderApp
	}
	for _, o := range seamOptions(ctx, userID, orgID) {
		if o.ID == id && (connectionID == "" || o.ConnectionID == "" || o.ConnectionID == connectionID) {
			return o, true
		}
	}
	return ConferenceOption{}, false
}
