package calendar

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Per-person caches of connected calendars, in memory only (each backend
// instance keeps its own; nothing is shared between people). A calendar's
// events are brought up to date incrementally at most every syncEvery
// (Google syncToken, Graph delta link); calendar lists, busy time and live
// windows outside the synced horizon are kept for a short while. Entries
// nobody used for cacheIdle are dropped.

const (
	syncEvery    = 30 * time.Second
	calsTTL      = 2 * time.Minute
	busyTTL      = time.Minute
	liveTTL      = 30 * time.Second
	cacheIdle    = 30 * time.Minute
	cacheMaxKeys = 5000
)

// extCalendar is a platform calendar as both adapters describe it.
type extCalendar struct {
	ID, Name, AccessRole, Timezone string
	Primary, CanWrite, Hidden      bool
	Meeting                        string // native meeting link on its events: ProviderGoogleMeet, ProviderTeams or ""
	platform, connID, account      string
}

// extAttendee is a guest on a platform event (our response vocabulary).
type extAttendee struct {
	Email, Name, Response string
	Organizer, Self       bool
	Optional, Resource    bool
}

// extEvent is a platform event (or one instance of a series) as both
// adapters describe it. Times are UTC; all-day events also carry dates.
type extEvent struct {
	ID, CalendarID, SeriesID     string
	Title, Location, Description string
	Start, End                   time.Time
	AllDay                       bool
	StartDate, EndDate           string // all-day: first and last (inclusive) day
	Timezone                     string // IANA, when known
	Status                       string // confirmed | tentative | cancelled
	ShowAs                       string // busy | free
	Organizer                    extAttendee
	OrganizerSelf                bool
	Attendees                    []extAttendee
	MyResponse                   string // needs_action | accepted | tentative | declined | "" (organizer)
	ConferenceURL                string
	ConferenceProvider           string // google_meet | teams | zoom | app | ""
	WebURL                       string
	Recurrence                   []string // series masters: RRULE/EXDATE lines
	Recurring                    bool
	Updated                      time.Time
	Stamp                        string // etag / change key
	CanEdit                      bool
	Private                      bool
}

func (e extEvent) overlaps(from, to time.Time) bool { return e.End.After(from) && e.Start.Before(to) }

// extWrite is a create or change sent to a platform. Nil fields stay.
type extWrite struct {
	Title, Description, Location *string
	TimeSet                      bool
	Start, End                   time.Time
	AllDay                       bool
	StartDate, EndDate           string // all-day: first and last (inclusive) day
	Timezone                     string
	ShowAs                       *string
	Attendees                    *[]extAttendee
	RuleSet                      bool
	Rule                         *Rule // nil with RuleSet: stop repeating
	Meeting                      string // ProviderGoogleMeet | ProviderTeams to add natively; "remove"
	JoinURL                      string // a meeting made elsewhere, written into location and description
	JoinLabel                    string // "Zoom", "Forum call"
	SendUpdates                  bool
}

// calAdapter is one platform's calendar API, built per request from the
// person's signing client and the connection's endpoints.
type calAdapter interface {
	calendars(ctx context.Context) ([]extCalendar, error)
	// events returns cal's events and instances overlapping [from, to),
	// bringing st up to date first (incrementally when it can).
	events(ctx context.Context, cal extCalendar, st *calState, from, to time.Time) ([]extEvent, error)
	event(ctx context.Context, calID, eventID string) (extEvent, error)
	create(ctx context.Context, calID string, w extWrite) (extEvent, error)
	update(ctx context.Context, calID, eventID string, w extWrite) (extEvent, error)
	remove(ctx context.Context, calID, eventID string, sendUpdates bool) error
	respond(ctx context.Context, calID, eventID, response string) (extEvent, error)
	freeBusy(ctx context.Context, cals []extCalendar, from, to time.Time) ([]busySpan, error)
}

// calState is one calendar's synced events for one person.
type calState struct {
	mu   sync.Mutex
	used time.Time

	// Google: singles, series masters and exceptions since the horizon, the
	// sync token that continues them, and expanded instances per series.
	token     string
	items     map[string]extEvent
	synced    time.Time
	instances map[string]instEntry

	// Graph: delta-synced months of the default calendar.
	buckets map[string]*graphBucket

	// Live windows (outside the horizon; Graph calendars without delta).
	views map[string]viewEntry
}

type instEntry struct {
	at     time.Time
	stamp  string
	events []extEvent
}

type graphBucket struct {
	delta  string
	items  map[string]extEvent
	synced time.Time
}

type viewEntry struct {
	at     time.Time
	events []extEvent
}

// reset forgets everything (a stale sync token).
func (st *calState) reset() {
	st.token, st.items, st.synced, st.instances, st.buckets, st.views = "", nil, time.Time{}, nil, nil, nil
}

// view returns a fresh live window from the cache.
func (st *calState) view(key string) ([]extEvent, bool) {
	if v, ok := st.views[key]; ok && time.Since(v.at) < liveTTL {
		return v.events, true
	}
	return nil, false
}

func (st *calState) putView(key string, events []extEvent) {
	if st.views == nil {
		st.views = map[string]viewEntry{}
	}
	if len(st.views) > 50 {
		st.views = map[string]viewEntry{}
	}
	st.views[key] = viewEntry{at: time.Now(), events: events}
}

type stateKey struct {
	user      uuid.UUID
	conn, cal string
}

type calsKey struct {
	user uuid.UUID
	conn string
}

type calsEntry struct {
	at   time.Time
	list []extCalendar
}

type busyKey struct {
	user     uuid.UUID
	from, to int64
}

type busyEntry struct {
	at    time.Time
	spans []busySpan
}

type connCache struct {
	mu     sync.Mutex
	states map[stateKey]*calState
	cals   map[calsKey]calsEntry
	busy   map[busyKey]busyEntry
	swept  time.Time
}

var cache = &connCache{states: map[stateKey]*calState{}, cals: map[calsKey]calsEntry{}, busy: map[busyKey]busyEntry{}}

// state returns (creating) the cache of one person's calendar.
func (c *connCache) state(user uuid.UUID, conn, cal string) *calState {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked()
	k := stateKey{user, conn, cal}
	st := c.states[k]
	if st == nil {
		st = &calState{}
		c.states[k] = st
	}
	st.used = time.Now()
	return st
}

func (c *connCache) calendars(user uuid.UUID, conn string) ([]extCalendar, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cals[calsKey{user, conn}]
	if !ok || time.Since(e.at) > calsTTL {
		return nil, false
	}
	return e.list, true
}

func (c *connCache) putCalendars(user uuid.UUID, conn string, list []extCalendar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cals[calsKey{user, conn}] = calsEntry{at: time.Now(), list: list}
}

func (c *connCache) busyFor(user uuid.UUID, from, to time.Time) ([]busySpan, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.busy[busyKey{user, from.Unix(), to.Unix()}]
	if !ok || time.Since(e.at) > busyTTL {
		return nil, false
	}
	return e.spans, true
}

func (c *connCache) putBusy(user uuid.UUID, from, to time.Time, spans []busySpan) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.busy) > cacheMaxKeys {
		c.busy = map[busyKey]busyEntry{}
	}
	c.busy[busyKey{user, from.Unix(), to.Unix()}] = busyEntry{at: time.Now(), spans: spans}
}

// forgetBusy drops a person's cached busy time (a calendar's busy choice or
// one of their events changed).
func (c *connCache) forgetBusy(user uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.busy {
		if k.user == user {
			delete(c.busy, k)
		}
	}
}

// stale makes the next read of one calendar sync with the platform (after a
// change made here) and drops the person's cached busy time.
func (c *connCache) stale(user uuid.UUID, conn, cal string) {
	c.mu.Lock()
	st := c.states[stateKey{user, conn, cal}]
	c.mu.Unlock()
	if st != nil {
		st.mu.Lock()
		st.synced = time.Time{} // series instances stay: the sync drops those it touches
		for _, b := range st.buckets {
			b.synced = time.Time{}
		}
		st.views = nil
		st.mu.Unlock()
	}
	c.forgetBusy(user)
}

// forgetUser drops everything cached for a person (account deletion).
func (c *connCache) forgetUser(user uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.states {
		if k.user == user {
			delete(c.states, k)
		}
	}
	for k := range c.cals {
		if k.user == user {
			delete(c.cals, k)
		}
	}
	for k := range c.busy {
		if k.user == user {
			delete(c.busy, k)
		}
	}
}

func (c *connCache) sweepLocked() {
	if time.Since(c.swept) < time.Minute && len(c.states) < cacheMaxKeys {
		return
	}
	c.swept = time.Now()
	for k, st := range c.states {
		if time.Since(st.used) > cacheIdle || len(c.states) > cacheMaxKeys {
			delete(c.states, k)
		}
	}
	for k, e := range c.cals {
		if time.Since(e.at) > calsTTL {
			delete(c.cals, k)
		}
	}
	for k, e := range c.busy {
		if time.Since(e.at) > busyTTL {
			delete(c.busy, k)
		}
	}
}
