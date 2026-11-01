package chat

// Agent scope: when an AI agent acts for the user (auth.AgentFrom), its reads
// are narrowed to the conversations the user lets agents read (the agents
// feature's ⋮ menu sets chat.conversation items). Without the marker or an
// installed AgentPolicy nothing changes.

import (
	"context"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

const (
	agentFeature = "chat"
	agentKind    = "chat.conversation"
)

// agentReadable reports which conversations the caller's agent may read:
// all of them for the person themselves.
func (s *Service) agentReadable(ctx context.Context, caller *auth.Identity, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = id.String()
	}
	m, err := app.AgentReadable(ctx, s.d.Modules, caller, agentFeature, agentKind, strs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for i, id := range ids {
		out[id] = m[strs[i]]
	}
	return out, nil
}

// agentHits drops search hits in conversations the caller's agent may not read.
func (s *Service) agentHits(ctx context.Context, caller *auth.Identity, hits []SearchHit) ([]SearchHit, error) {
	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.Conversation.ID
	}
	ok, err := s.agentReadable(ctx, caller, ids)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		if ok[h.Conversation.ID] {
			out = append(out, h)
		}
	}
	return out, nil
}
