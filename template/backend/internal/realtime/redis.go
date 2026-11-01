package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is the production Broker. Every API instance publishes envelopes to
// one Redis pub/sub channel and subscribes to it; each instance's Hub then
// delivers only to the topics its local connections hold.
//
// One channel carries every event and each instance's Hub drops events with
// no local subscribers after one map lookup. A single Redis primary handles
// 100k+ msg/s; past that, shard by topic hash onto N channels.
type Redis struct {
	client *redis.Client
}

const (
	redisChannel        = "scaffoldapp:realtime"
	redisPublishTimeout = 2 * time.Second
	redisChannelBuffer  = 4096
)

// NewRedis returns a Redis-backed broker (REDIS_URL, e.g. redis://:pw@host:6379/0).
func NewRedis(ctx context.Context, url string) (Broker, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	client := redis.NewClient(opts)
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Redis{client: client}, nil
}

// Publish sends the envelope to every instance. It is bounded by a short
// timeout so a Redis hiccup can't stall the request that triggered it.
func (r *Redis) Publish(ctx context.Context, topic, eventType string, data any) error {
	ev, err := NewEvent(topic, eventType, data)
	if err != nil {
		return err
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), redisPublishTimeout)
	defer cancel()
	return r.client.Publish(ctx, redisChannel, b).Err()
}

// Subscribe delivers every envelope to fn until ctx is done. go-redis
// reconnects and resubscribes on its own after network errors; events
// published while disconnected are lost (clients refetch on reconnect).
func (r *Redis) Subscribe(ctx context.Context, fn func(Event)) {
	ps := r.client.Subscribe(ctx, redisChannel)
	// Wait for the subscription confirmation so events published right after
	// Subscribe returns are not missed.
	if _, err := ps.Receive(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("realtime: redis subscribe not yet confirmed; will retry in background", "err", err)
	}
	ch := ps.Channel(redis.WithChannelSize(redisChannelBuffer))
	go func() {
		defer ps.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var ev Event
				if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
					slog.Warn("realtime: bad envelope on redis channel", "err", err)
					continue
				}
				fn(ev)
			}
		}
	}()
}

func (r *Redis) Close() error { return r.client.Close() }
