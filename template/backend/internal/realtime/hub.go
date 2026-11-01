package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Authorizer decides whether a user may subscribe to a topic other than their
// own user:<id> topic and admin:live (both handled by the hub itself).
// api.Build composes it from every module's app.TopicAuthorizer.
type Authorizer func(ctx context.Context, userID uuid.UUID, isAdmin bool, topic string) bool

// Connection limits.
const (
	maxTopicsPerConn = 20
	maxTopicLen      = 128
	maxMessageBytes  = 1024
	sendBuffer       = 64
	writeWait        = 10 * time.Second
	pongWait         = 60 * time.Second
	pingPeriod       = 25 * time.Second // < pongWait, and under typical 60 s LB idle timeouts
	authorizeTimeout = 5 * time.Second
	// A client may send at most clientMsgBurst messages per clientMsgWindow.
	clientMsgBurst  = 30
	clientMsgWindow = 10 * time.Second
	// maxConnsPerUser caps one user's sockets on this instance;
	// further upgrades get 429.
	maxConnsPerUser = 8
)

// Hub owns this instance's WebSocket connections and fans broker events out to
// the connections subscribed to each event's topic.
type Hub struct {
	broker    Broker
	authorize Authorizer
	origins   []string
	upgrader  websocket.Upgrader

	mu        sync.RWMutex
	topics    map[string]map[*conn]struct{}
	conns     map[*conn]struct{}
	userConns map[uuid.UUID]int
}

// HubOption configures a Hub.
type HubOption func(*Hub)

// WithAllowedOrigins sets the browser origins allowed to open a socket
// (normally cfg.CORSOrigins; "*" allows any). Requests without an Origin
// header (native apps) and same-origin requests are always allowed.
func WithAllowedOrigins(origins []string) HubOption {
	return func(h *Hub) { h.origins = append([]string(nil), origins...) }
}

func NewHub(b Broker, authorize Authorizer, opts ...HubOption) *Hub {
	h := &Hub{
		broker:    b,
		authorize: authorize,
		topics:    map[string]map[*conn]struct{}{},
		conns:     map[*conn]struct{}{},
		userConns: map[uuid.UUID]int{},
	}
	for _, o := range opts {
		o(h)
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 4096,
		CheckOrigin:     h.checkOrigin,
	}
	return h
}

// Run subscribes to the broker and fans events out until ctx is done, then
// closes every connection so clients reconnect to another instance.
func (h *Hub) Run(ctx context.Context) {
	h.broker.Subscribe(ctx, h.dispatch)
	<-ctx.Done()
	h.mu.Lock()
	all := make([]*conn, 0, len(h.conns))
	for c := range h.conns {
		all = append(all, c)
	}
	h.mu.Unlock()
	for _, c := range all {
		c.close(websocket.CloseGoingAway, "server shutting down")
	}
}

// ServeWS upgrades an authenticated request (auth.FromContext) to a WebSocket
// and auto-subscribes it to the caller's user:{id} topic.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		httpx.Fail(w, r, httpx.ErrUnauthenticated)
		return
	}
	h.mu.RLock()
	open := h.userConns[id.UserID]
	h.mu.RUnlock()
	if open >= maxConnsPerUser {
		httpx.Fail(w, r, errTooManyConns)
		return
	}
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // the upgrader already wrote the HTTP error
	}
	c := &conn{
		hub:     h,
		ws:      ws,
		userID:  id.UserID,
		support: id.IsSupport(),
		send:    make(chan []byte, sendBuffer),
		done:    make(chan struct{}),
		topics:  map[string]struct{}{},
	}
	h.register(c)
	go c.writePump()
	c.readPump(context.WithoutCancel(r.Context()))
}

func (h *Hub) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // native apps don't send Origin
	}
	for _, o := range h.origins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	u, err := url.Parse(origin)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

var errTooManyConns = httpx.NewError(http.StatusTooManyRequests, "rate_limited",
	"Too many open connections. Close another tab or device and try again.")

func (h *Hub) register(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = struct{}{}
	h.userConns[c.userID]++
	h.addTopicLocked(c, UserTopic(c.userID))
}

func (h *Hub) remove(c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.conns[c]; !ok {
		return
	}
	delete(h.conns, c)
	if h.userConns[c.userID]--; h.userConns[c.userID] <= 0 {
		delete(h.userConns, c.userID)
	}
	for t := range c.topics {
		h.removeTopicLocked(c, t)
	}
}

func (h *Hub) addTopicLocked(c *conn, topic string) {
	subs := h.topics[topic]
	if subs == nil {
		subs = map[*conn]struct{}{}
		h.topics[topic] = subs
	}
	subs[c] = struct{}{}
	c.topics[topic] = struct{}{}
}

func (h *Hub) removeTopicLocked(c *conn, topic string) {
	delete(c.topics, topic)
	if subs := h.topics[topic]; subs != nil {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}
}

// subscribe adds topic to c if the caller may see it and c is under its topic cap.
func (h *Hub) subscribe(ctx context.Context, c *conn, topic string) {
	if topic == "" || len(topic) > maxTopicLen {
		return
	}
	h.mu.RLock()
	_, already := c.topics[topic]
	full := len(c.topics) >= maxTopicsPerConn
	h.mu.RUnlock()
	if already || full || !h.allowed(ctx, c, topic) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, open := h.conns[c]; open && len(c.topics) < maxTopicsPerConn {
		h.addTopicLocked(c, topic)
	}
}

func (h *Hub) allowed(ctx context.Context, c *conn, topic string) bool {
	switch {
	case topic == UserTopic(c.userID):
		return true
	case strings.HasPrefix(topic, "user:"), topic == ControlTopic:
		return false
	case topic == AdminLiveTopic:
		return c.support
	case h.authorize == nil:
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, authorizeTimeout)
	defer cancel()
	return h.authorize(ctx, c.userID, c.support, topic)
}

func (h *Hub) unsubscribe(c *conn, topic string) {
	if topic == UserTopic(c.userID) {
		return // the personal topic is permanent for the connection's lifetime
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := c.topics[topic]; ok {
		h.removeTopicLocked(c, topic)
	}
}

// dispatch delivers ev to local subscribers. It never blocks: a connection
// whose buffer is full is dropped (the client reconnects and refetches).
// Control messages are handled here and never reach clients.
func (h *Hub) dispatch(ev Event) {
	if ev.Topic == ControlTopic {
		h.control(ev)
		return
	}
	h.mu.RLock()
	subs := h.topics[ev.Topic]
	if len(subs) == 0 {
		h.mu.RUnlock()
		return
	}
	targets := make([]*conn, 0, len(subs))
	for c := range subs {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	frame, err := json.Marshal(ev)
	if err != nil {
		slog.Warn("realtime: marshal event", "type", ev.Type, "err", err)
		return
	}
	for _, c := range targets {
		c.enqueue(frame)
	}
}

// control handles a ControlTopic message.
func (h *Hub) control(ev Event) {
	if ev.Type != EventRevoke {
		return
	}
	var m revokeMessage
	if err := json.Unmarshal(ev.Data, &m); err != nil || m.Topic == "" || m.Topic == UserTopic(m.UserID) {
		return
	}
	h.mu.RLock()
	var targets []*conn
	for c := range h.topics[m.Topic] {
		if m.UserID == uuid.Nil || c.userID == m.UserID { // no user id: every subscriber (RevokeTopic)
			targets = append(targets, c)
		}
	}
	h.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	// Re-check access off the broker goroutine (it queries the database): the
	// user may have regained access (e.g. re-accepted) since the revoke.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), authorizeTimeout)
		defer cancel()
		for _, c := range targets {
			if h.allowed(ctx, c, m.Topic) {
				continue
			}
			h.mu.Lock()
			if _, ok := c.topics[m.Topic]; ok {
				h.removeTopicLocked(c, m.Topic)
			}
			h.mu.Unlock()
		}
	}()
}

type clientMessage struct {
	Type  string `json:"type"`
	Topic string `json:"topic"`
}

// conn is one WebSocket client. Its topics map is guarded by hub.mu.
type conn struct {
	hub     *Hub
	ws      *websocket.Conn
	userID  uuid.UUID
	support bool
	send    chan []byte
	done    chan struct{}
	once    sync.Once
	topics  map[string]struct{}
}

func (c *conn) enqueue(frame []byte) {
	select {
	case c.send <- frame:
	case <-c.done:
	default:
		c.close(websocket.CloseTryAgainLater, "slow consumer")
	}
}

// close is idempotent and safe from any goroutine.
func (c *conn) close(code int, reason string) {
	c.once.Do(func() {
		close(c.done)
		_ = c.ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = c.ws.Close()
	})
}

func (c *conn) readPump(ctx context.Context) {
	defer func() {
		c.hub.remove(c)
		c.close(websocket.CloseNormalClosure, "")
	}()
	c.ws.SetReadLimit(maxMessageBytes)
	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { return c.ws.SetReadDeadline(time.Now().Add(pongWait)) })

	windowStart, count := time.Now(), 0
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if now := time.Now(); now.Sub(windowStart) > clientMsgWindow {
			windowStart, count = now, 0
		}
		if count++; count > clientMsgBurst {
			c.close(websocket.ClosePolicyViolation, "too many messages")
			return
		}
		var msg clientMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "ping":
			if frame, err := json.Marshal(Event{Type: "pong", Data: json.RawMessage(`{}`), TS: time.Now().UTC()}); err == nil {
				c.enqueue(frame)
			}
		case "subscribe":
			c.hub.subscribe(ctx, c, msg.Topic)
		case "unsubscribe":
			c.hub.unsubscribe(c, msg.Topic)
		}
	}
}

func (c *conn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case frame := <-c.send:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.TextMessage, frame); err != nil {
				c.close(websocket.CloseGoingAway, "")
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close(websocket.CloseGoingAway, "")
				return
			}
		case <-c.done:
			return
		}
	}
}
