package payments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/webhooks"
)

const maxWebhookBytes = 1 << 20

// webhook handles POST /webhooks/stripe: verify the signature (refused when
// no secret is set), skip replays, re-fetch the object from the provider,
// then apply it and record the event in one transaction (webhooks.Once). A
// failure answers 500 so the provider retries; nothing is half-recorded.
func (s *Service) webhook(w http.ResponseWriter, r *http.Request) {
	if s.gw == nil {
		httpx.Fail(w, r, errUnconfigured)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		httpx.Fail(w, r, httpx.BadRequest("Payload too large."))
		return
	}
	ev, err := s.gw.ParseWebhook(body, r.Header)
	switch {
	case errors.Is(err, ErrWebhookUnconfigured):
		slog.ErrorContext(r.Context(), "payments: webhook refused: STRIPE_WEBHOOK_SECRET is not set")
		httpx.Fail(w, r, httpx.NewError(http.StatusServiceUnavailable, "webhook_unconfigured", "Webhook verification isn't set up."))
		return
	case errors.Is(err, ErrBadSignature):
		slog.WarnContext(r.Context(), "payments: webhook signature rejected")
		httpx.Fail(w, r, httpx.NewError(http.StatusBadRequest, "bad_signature", "Signature check failed."))
		return
	case err != nil:
		httpx.Fail(w, r, httpx.BadRequest("Malformed event."))
		return
	}
	if ev.Type == "" {
		// An event type the module has no opinion on: acknowledge it, or the
		// provider retries it for days.
		httpx.OK(w, map[string]bool{"received": true})
		return
	}
	ctx := context.WithoutCancel(r.Context())
	var seen bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_events WHERE id = $1)`,
		s.gw.Name()+":"+ev.ID).Scan(&seen); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if seen {
		httpx.OK(w, map[string]bool{"received": true})
		return
	}
	snap, err := s.fetch(ctx, ev)
	if errors.Is(err, ErrNotFound) {
		slog.InfoContext(ctx, "payments: webhook object no longer exists at the provider", "event", ev.ID, "type", ev.RawType)
		httpx.OK(w, map[string]bool{"received": true})
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "payments: webhook re-fetch failed", "event", ev.ID, "type", ev.RawType, "err", err)
		httpx.Fail(w, r, err)
		return
	}
	var fx effects
	dup, err := webhooks.Once(ctx, s.d.Pool, s.gw.Name(), ev.ID, func(tx pgx.Tx) error {
		return s.apply(ctx, tx, ev, snap, &fx)
	})
	if err != nil {
		slog.ErrorContext(ctx, "payments: webhook handling failed", "event", ev.ID, "type", ev.RawType, "err", err)
		httpx.Fail(w, r, err)
		return
	}
	if !dup {
		s.flush(ctx, &fx)
	}
	httpx.OK(w, map[string]bool{"received": true})
}
