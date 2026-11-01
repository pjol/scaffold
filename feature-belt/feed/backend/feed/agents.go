package feed

// Agent scope: when an AI agent acts for the user (auth.AgentFrom), its reads
// are narrowed to the posts the user lets agents read (feed.post items set
// from the agents feature's ⋮ menu, else the feed's level). Without the
// marker or an installed AgentPolicy nothing changes.

import (
	"context"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notifications"
)

const (
	agentFeature = "feed"
	agentKind    = "feed.post"
)

// postAgentTarget is the post a feed notification is about, for the inbox's
// agent scope.
var postAgentTarget = notifications.DataTarget(agentKind, "post_id")

// agentPosts drops posts the caller's agent may not read from a page (the
// cursor still continues after the last post fetched).
func (s *Service) agentPosts(ctx context.Context, caller *auth.Identity, page httpx.Page[Post]) (httpx.Page[Post], error) {
	if _, ok := auth.AgentFrom(ctx); !ok || len(page.Items) == 0 {
		return page, nil
	}
	ids := make([]string, len(page.Items))
	for i, p := range page.Items {
		ids[i] = p.ID.String()
	}
	ok, err := app.AgentReadable(ctx, s.d.Modules, caller, agentFeature, agentKind, ids)
	if err != nil {
		return page, err
	}
	items := make([]Post, 0, len(page.Items))
	for i, p := range page.Items {
		if ok[ids[i]] {
			items = append(items, p)
		}
	}
	page.Items = items
	return page, nil
}
