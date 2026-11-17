package baskets

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/realtime"
)

// Realtime: baskets:<basket id> carries baskets.basket (a Summary, is_mine
// always false) when a basket's status, name, entries or visibility change.
// Any signed-in person may follow an existing basket.
const EventBasket = "baskets.basket"

func basketTopic(id uuid.UUID) string { return realtime.Topic("baskets", id) }

// AuthorizeTopic allows baskets:<id> for any signed-in person when the basket exists.
func (s *Service) AuthorizeTopic(ctx context.Context, _ uuid.UUID, _ bool, topic string) (handled, allowed bool) {
	rest, ok := strings.CutPrefix(topic, "baskets:")
	if !ok {
		return false, false
	}
	id, err := uuid.Parse(rest)
	if err != nil {
		return true, false
	}
	var exists bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM baskets WHERE id = $1)`, id).Scan(&exists); err != nil {
		return true, false
	}
	return true, exists
}
