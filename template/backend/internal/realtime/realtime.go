// Package realtime fans server events out to WebSocket clients across every API
// instance. Domain code only ever calls Broker.Publish; the WebSocket hub
// (hub.go) subscribes to the broker and delivers to local connections.
//
// Implementations: Memory (single instance / dev) and Redis (production,
// REDIS_URL). Delivery is best-effort and at-most-once — clients refetch the
// resource on reconnect, so a dropped event is never a correctness problem.
package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event is the wire envelope: {"type","topic","data","ts"}.
type Event struct {
	Type  string          `json:"type"`
	Topic string          `json:"topic"`
	Data  json.RawMessage `json:"data"`
	TS    time.Time       `json:"ts"`
}

// Topics are "<kind>:<id>". The hub owns user:<id> (every connection is
// subscribed to its own) and admin:live (support and admin); every other
// prefix belongs to the module whose app.TopicAuthorizer handles it, e.g.
// chat:<conversation id>.
func UserTopic(id uuid.UUID) string { return "user:" + id.String() }

// Topic builds a module topic: Topic("chat", id) = "chat:<id>".
func Topic(kind string, id uuid.UUID) string { return kind + ":" + id.String() }

const AdminLiveTopic = "admin:live"

// ControlTopic carries broker-wide control messages between hubs. It is never
// delivered to clients (and no client may subscribe to it).
const ControlTopic = "_hub:control"

// EventRevoke (on ControlTopic) asks every hub to re-check one user's access
// to a topic and drop the subscription if it is gone (a member removed from a
// conversation stops receiving it).
const EventRevoke = "hub.revoke"

// revokeMessage is the EventRevoke payload.
type revokeMessage struct {
	UserID uuid.UUID `json:"user_id"`
	Topic  string    `json:"topic"`
}

// Revoke publishes a revoke for userID's subscription to topic on every
// instance. Best-effort like every publish.
func Revoke(ctx context.Context, b Broker, userID uuid.UUID, topic string) error {
	return b.Publish(ctx, ControlTopic, EventRevoke, revokeMessage{UserID: userID, Topic: topic})
}

// RevokeTopic asks every instance to re-check every subscriber of topic and
// drop those who lost access, for changes that affect an unknown set of people
// (a shared item made private). It is a revoke with no user id.
func RevokeTopic(ctx context.Context, b Broker, topic string) error {
	return b.Publish(ctx, ControlTopic, EventRevoke, revokeMessage{Topic: topic})
}

// EventNotification is published by notify.Realtime on the user topic.
// Modules define their own event types next to their topics ("chat.message").
const EventNotification = "notification"

// Broker publishes events and lets the hub subscribe to everything.
type Broker interface {
	// Publish marshals data and sends it to topic. Never blocks on slow consumers.
	Publish(ctx context.Context, topic, eventType string, data any) error
	// Subscribe delivers every published event (all topics) until ctx is done.
	// The hub is the only subscriber; it filters by local connections' topics.
	Subscribe(ctx context.Context, fn func(Event))
	Close() error
}

// NewEvent builds an envelope.
func NewEvent(topic, eventType string, data any) (Event, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: eventType, Topic: topic, Data: b, TS: time.Now().UTC()}, nil
}

// Memory is an in-process broker for a single instance.
type Memory struct {
	mu   sync.RWMutex
	subs map[int]func(Event)
	next int
}

func NewMemory() *Memory { return &Memory{subs: map[int]func(Event){}} }

func (m *Memory) Publish(_ context.Context, topic, eventType string, data any) error {
	ev, err := NewEvent(topic, eventType, data)
	if err != nil {
		return err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, fn := range m.subs {
		fn(ev)
	}
	return nil
}

func (m *Memory) Subscribe(ctx context.Context, fn func(Event)) {
	m.mu.Lock()
	id := m.next
	m.next++
	m.subs[id] = fn
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.subs, id)
		m.mu.Unlock()
	}()
}

func (m *Memory) Close() error { return nil }
