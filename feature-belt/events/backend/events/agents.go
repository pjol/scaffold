package events

// Agent scope: when an AI agent acts for the user (auth.AgentFrom), its reads
// are narrowed to the events the user lets agents read (events.event items
// set from the agents feature's ⋮ menu, else the feature's level). Without
// the marker or an installed AgentPolicy nothing changes.

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notifications"
)

const (
	agentFeature = "events"
	agentKind    = "events.event"
)

// eventTarget is the event a notification is about, for the inbox's agent
// scope.
var eventTarget = notifications.DataTarget(agentKind, "event_id")

// getTarget names the event events_get reads, so a per-event agent scope
// applies before it runs.
func getTarget(input json.RawMessage) (kind, id string) {
	var in struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(input, &in) != nil {
		return agentKind, ""
	}
	if _, err := uuid.Parse(in.ID); err != nil {
		return agentKind, ""
	}
	return agentKind, in.ID
}

// agentAllows reports whether the caller's agent may read one event (always
// true for the person themselves).
func (s *Service) agentAllows(ctx context.Context, caller *auth.Identity, id uuid.UUID) (bool, error) {
	return app.AgentAllows(ctx, s.d.Modules, caller, agentFeature, agentKind, id.String(), app.AgentRead)
}

// agentEvents drops events the caller's agent may not read from a page (the
// cursor still continues after the last event fetched).
func (s *Service) agentEvents(ctx context.Context, caller *auth.Identity, page httpx.Page[Event]) (httpx.Page[Event], error) {
	if _, ok := auth.AgentFrom(ctx); !ok || len(page.Items) == 0 {
		return page, nil
	}
	ids := make([]string, len(page.Items))
	for i, e := range page.Items {
		ids[i] = e.ID.String()
	}
	ok, err := app.AgentReadable(ctx, s.d.Modules, caller, agentFeature, agentKind, ids)
	if err != nil {
		return page, err
	}
	items := make([]Event, 0, len(page.Items))
	for i, e := range page.Items {
		if ok[ids[i]] {
			items = append(items, e)
		}
	}
	page.Items = items
	return page, nil
}
