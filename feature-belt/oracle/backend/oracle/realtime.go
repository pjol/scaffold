package oracle

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/realtime"
)

// Realtime: every signed-in user may follow prices (they are as public as
// the read routes). Topics:
//
//	oracle:<feed id>       oracle.price, oracle.observation, oracle.history for one feed (baskets also get their parts' observations)
//	oracle:feeds           oracle.prices (the feeds that changed this tick), oracle.feeds (catalog changed)
//	oracle:observations    oracle.observation for every feed
const (
	EventPrice       = "oracle.price"
	EventPrices      = "oracle.prices"
	EventObservation = "oracle.observation"
	EventHistory     = "oracle.history"
	EventFeeds       = "oracle.feeds"

	feedsTopic        = "oracle:feeds"
	observationsTopic = "oracle:observations"
)

func feedTopic(id uuid.UUID) string { return realtime.Topic("oracle", id) }

// AuthorizeTopic allows any signed-in user on oracle topics.
func (s *Service) AuthorizeTopic(_ context.Context, _ uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	if !strings.HasPrefix(topic, "oracle:") {
		return false, false
	}
	rest := strings.TrimPrefix(topic, "oracle:")
	if rest == "feeds" || rest == "observations" {
		return true, true
	}
	_, err := uuid.Parse(rest)
	return true, err == nil
}

type event struct {
	topic, typ string
	data       any
}

// emit queues an event; a full queue drops it (delivery is at-most-once and
// clients refetch on reconnect).
func (s *Service) emit(topic, typ string, data any) {
	select {
	case s.events <- event{topic, typ, data}:
	default:
	}
}

func (s *Service) emitter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.events:
			pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := s.d.Broker.Publish(pctx, ev.topic, ev.typ, ev.data); err != nil && ctx.Err() == nil {
				s.log.Debug("oracle: realtime publish failed", "topic", ev.topic, "err", err)
			}
			cancel()
		}
	}
}

// emitHistory tells clients a feed's history changed (and its baskets').
// It publishes directly, so it works on any instance.
func (s *Service) emitHistory(ctx context.Context, feedID uuid.UUID) {
	bg.Go(ctx, "oracle.history_event", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		ids := []uuid.UUID{feedID}
		rows, err := s.d.Pool.Query(ctx, `SELECT basket_id FROM oracle_basket_parts WHERE feed_id = $1`, feedID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if err := s.d.Broker.Publish(ctx, feedTopic(id), EventHistory, map[string]any{"feed_id": id}); err != nil {
				return err
			}
		}
		return nil
	})
}

// priceDTO is the wire form of a published price (realtime and REST).
type priceDTO struct {
	FeedID    uuid.UUID `json:"feed_id"`
	Key       string    `json:"key"`
	Price     *string   `json:"price"`
	Target    *string   `json:"target"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toPriceDTO(pp PricePoint) priceDTO {
	d := priceDTO{FeedID: pp.FeedID, Key: pp.Key, Status: pp.Status, UpdatedAt: pp.At.UTC()}
	if pp.Price > 0 {
		p := fmtPrice(pp.Price, pp.Decimals)
		d.Price = &p
	}
	if pp.Target > 0 {
		t := fmtPrice(pp.Target, pp.Decimals)
		d.Target = &t
	}
	return d
}
