package notifications

// Agent scope: the inbox quotes every feature (message text, page and record
// titles), so an AI agent reading it for the person (auth.AgentFrom) sees a
// notification only when the person lets agents read what it is about.
// Otherwise the inbox would be a side door into a feature or an item they
// turned off for agents. Without the marker or an installed AgentPolicy
// nothing changes.

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
)

// agentScan bounds how many rows an agent's inbox or unread count looks
// through to find the ones it may read.
const agentScan = 500

var (
	targetKindRE = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	targetIDRE   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,100}$`)
)

// DataTarget is a Kind.AgentTarget for notifications whose data carries the
// item's id under key: DataTarget("channels.channel", "channel_id").
func DataTarget(kind, key string) func(map[string]any) (string, string) {
	return func(data map[string]any) (string, string) {
		id, _ := data[key].(string)
		return kind, id
	}
}

// agentCall reports whether ctx is an AI agent call with a policy to apply.
func (s *Service) agentCall(ctx context.Context) bool {
	_, ok := auth.AgentFrom(ctx)
	return ok && app.FindAgentPolicy(s.d.Modules) != nil
}

type agentRef struct{ kind, id string }

// agentTarget is the item a notification is about: its kind's AgentTarget,
// else data {"kind", "id"} (access shares). Zero when unknown or malformed.
func agentTarget(ctx context.Context, it Item) (ref agentRef) {
	if k, ok := lookupKind(it.Kind); ok && k.AgentTarget != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.ErrorContext(ctx, "notifications: agent target panicked", "kind", it.Kind, "panic", fmt.Sprint(r))
					ref = agentRef{}
				}
			}()
			ref.kind, ref.id = k.AgentTarget(it.Data)
		}()
	} else {
		ref.kind, _ = it.Data["kind"].(string)
		ref.id, _ = it.Data["id"].(string)
	}
	ref.kind, ref.id = strings.TrimSpace(ref.kind), strings.TrimSpace(ref.id)
	if !targetKindRE.MatchString(ref.kind) || !targetIDRE.MatchString(ref.id) {
		return agentRef{}
	}
	return ref
}

// agentFilter keeps the items an AI agent acting for caller may read. A
// notification about a known item needs that item readable at the caller's
// agent scope (item → ancestors → its feature → default); one from another
// feature's kind (access.shared about a page) also needs that feature
// readable. One about no known item needs its kind's feature (the id's
// prefix) readable as a whole. Other callers get items unchanged.
func (s *Service) agentFilter(ctx context.Context, caller *auth.Identity, items []Item) ([]Item, error) {
	if !s.agentCall(ctx) || len(items) == 0 {
		return items, nil
	}
	features := map[string]bool{}
	featureOK := func(f string) (bool, error) {
		if ok, seen := features[f]; seen {
			return ok, nil
		}
		ok, err := app.AgentAllows(ctx, s.d.Modules, caller, f, "", "", app.AgentRead)
		if err != nil {
			return false, err
		}
		features[f] = ok
		return ok, nil
	}
	targets := make([]agentRef, len(items))
	byKind := map[string][]string{}
	for i, it := range items {
		t := agentTarget(ctx, it)
		targets[i] = t
		if t.kind != "" {
			byKind[t.kind] = append(byKind[t.kind], t.id)
		}
	}
	readable := map[agentRef]bool{}
	for kind, ids := range byKind {
		m, err := app.AgentReadable(ctx, s.d.Modules, caller, kindGroup(kind), kind, ids)
		if err != nil {
			return nil, err
		}
		for id, ok := range m {
			readable[agentRef{kind, id}] = ok
		}
	}
	out := make([]Item, 0, len(items))
	for i, it := range items {
		f, t := kindGroup(it.Kind), targets[i]
		ok := true
		switch {
		case t.kind == "":
			var err error
			if ok, err = featureOK(f); err != nil {
				return nil, err
			}
		case !readable[t]:
			ok = false
		case kindGroup(t.kind) != f:
			var err error
			if ok, err = featureOK(f); err != nil {
				return nil, err
			}
		}
		if ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// agentInbox is inbox for an agent: newest first, only what it may read,
// at most limit items (it looks through up to agentScan rows).
func (s *Service) agentInbox(ctx context.Context, caller *auth.Identity, unreadOnly bool, limit int) ([]Item, error) {
	items, err := s.inbox(ctx, caller.UserID, unreadOnly, cursor{}, agentScan)
	if err != nil {
		return nil, err
	}
	if items, err = s.agentFilter(ctx, caller, items); err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// agentUnread is the unread count an agent may know: the unread rows it may
// read among the newest agentScan.
func (s *Service) agentUnread(ctx context.Context, caller *auth.Identity) (int, error) {
	items, err := s.agentInbox(ctx, caller, true, agentScan)
	return len(items), err
}

// itemsByID loads the caller's notifications among ids.
func (s *Service) itemsByID(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) ([]Item, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+itemCols+` FROM notifications WHERE user_id = $1 AND id = ANY($2)
		ORDER BY created_at DESC, id DESC`, userID, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

// markTargets resolves a mark-read request to the unread items it would
// change for this caller: for an agent, only ones it may read.
func (s *Service) markTargets(ctx context.Context, caller *auth.Identity, req markReadReq) ([]Item, error) {
	var (
		items []Item
		err   error
	)
	if req.All {
		items, err = s.inbox(ctx, caller.UserID, true, cursor{}, agentScan)
	} else {
		items, err = s.itemsByID(ctx, caller.UserID, req.IDs)
	}
	if err != nil {
		return nil, err
	}
	if items, err = s.agentFilter(ctx, caller, items); err != nil {
		return nil, err
	}
	unread := items[:0]
	for _, it := range items {
		if it.ReadAt == nil {
			unread = append(unread, it)
		}
	}
	return unread, nil
}
