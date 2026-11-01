package channels

// Agent scope: when an AI agent acts for the user (auth.AgentFrom), its reads
// are narrowed to the channels the user lets agents read. Without the marker
// or an installed AgentPolicy nothing changes.

import (
	"context"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

const (
	agentFeature = "channels"
	agentKind    = "channels.channel"
)

// agentReader reports whether the caller's agent may read a channel. The
// feature's max level is only an upper bound (an override can be lower), so
// the one shortcut is max < Read: nothing is readable. Otherwise every
// channel is checked, once per request.
type agentReader struct {
	ctx    context.Context
	s      *Service
	caller *auth.Identity
	fixed  *bool // same answer for every channel
	cache  map[uuid.UUID]bool
}

func (s *Service) agentReader(ctx context.Context, caller *auth.Identity) (*agentReader, error) {
	r := &agentReader{ctx: ctx, s: s, caller: caller, cache: map[uuid.UUID]bool{}}
	yes, no := true, false
	if _, ok := auth.AgentFrom(ctx); !ok {
		r.fixed = &yes
		return r, nil
	}
	p := app.FindAgentPolicy(s.d.Modules)
	if p == nil {
		r.fixed = &yes
		return r, nil
	}
	_, top, err := p.FeatureLevel(ctx, caller, agentFeature)
	if err != nil {
		return nil, err
	}
	if top < app.AgentRead {
		r.fixed = &no
	}
	return r, nil
}

func (r *agentReader) can(channelID uuid.UUID) (bool, error) {
	if r.fixed != nil {
		return *r.fixed, nil
	}
	if ok, seen := r.cache[channelID]; seen {
		return ok, nil
	}
	ok, err := app.AgentAllows(r.ctx, r.s.d.Modules, r.caller, agentFeature, agentKind, channelID.String(), app.AgentRead)
	if err != nil {
		return false, err
	}
	r.cache[channelID] = ok
	return ok, nil
}

// require answers errChannelNotFound when the agent may not read the channel.
func (r *agentReader) require(channelID uuid.UUID) error {
	ok, err := r.can(channelID)
	if err != nil {
		return err
	}
	if !ok {
		return errChannelNotFound
	}
	return nil
}
