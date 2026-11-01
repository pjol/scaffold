package orgs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/bg"
)

// Membership hooks. SQL stops a removed person at once, but features that
// hand out something for membership or a role (realtime subscriptions above
// all) need to hear about it: an admin removed them, they left, the org was
// deleted (once per member), their account was deleted, or their role
// changed. Hooks run after the change commits, off the request (bg.Go).

// MemberRemovedFunc learns that userID no longer belongs to orgID. It runs in
// the background with a context that ends after memberRemovedTimeout; it is
// best-effort (log failures; a panic is recovered and logged).
type MemberRemovedFunc func(ctx context.Context, orgID, userID uuid.UUID)

const memberRemovedTimeout = 30 * time.Second

var (
	memberRemovedMu    sync.RWMutex
	memberRemovedHooks []MemberRemovedFunc

	// departedOnDeletion keeps the orgs a user being deleted belonged to,
	// from DeleteAccount (inside the deletion transaction) to AccountDeleted
	// (after it commits). user id → []uuid.UUID
	departedOnDeletion sync.Map
)

// OnMemberRemoved registers fn. Call it from the registering feature's New;
// hooks are read when a removal happens, so construction order never matters.
func OnMemberRemoved(fn MemberRemovedFunc) {
	if fn == nil {
		panic("orgs: OnMemberRemoved needs a function")
	}
	memberRemovedMu.Lock()
	memberRemovedHooks = append(memberRemovedHooks, fn)
	memberRemovedMu.Unlock()
}

// fireMemberRemoved runs every hook for each user leaving orgID, in the
// background. Call it only after the removal committed.
func fireMemberRemoved(ctx context.Context, orgID uuid.UUID, users ...uuid.UUID) {
	memberRemovedMu.RLock()
	hooks := append([]MemberRemovedFunc(nil), memberRemovedHooks...)
	memberRemovedMu.RUnlock()
	if len(hooks) == 0 || len(users) == 0 {
		return
	}
	bg.Go(ctx, "orgs.member_removed", func(base context.Context) error {
		for _, u := range users {
			for _, fn := range hooks {
				runHook(base, "member-removed", orgID, u, func(ctx context.Context) { fn(ctx, orgID, u) })
			}
		}
		return nil
	})
}

// RoleChangedFunc learns that userID's role in orgID changed from one role to
// another ("owner", "admin", "member"): an admin changed it, or ownership was
// transferred (twice: the new owner and the one stepping down). Leaving an
// org is OnMemberRemoved's. Same rules as MemberRemovedFunc: background,
// bounded context, best-effort.
type RoleChangedFunc func(ctx context.Context, orgID, userID uuid.UUID, from, to string)

var (
	roleChangedMu    sync.RWMutex
	roleChangedHooks []RoleChangedFunc
)

// OnRoleChanged registers fn. Call it from the registering feature's New.
// The access feature uses it: a permission held by a role can be lost with it.
func OnRoleChanged(fn RoleChangedFunc) {
	if fn == nil {
		panic("orgs: OnRoleChanged needs a function")
	}
	roleChangedMu.Lock()
	roleChangedHooks = append(roleChangedHooks, fn)
	roleChangedMu.Unlock()
}

// roleChange is one person's role change, for fireRoleChanged.
type roleChange struct {
	userID   uuid.UUID
	from, to string
}

// fireRoleChanged runs every hook for each change, in the background. Call it
// only after the change committed.
func fireRoleChanged(ctx context.Context, orgID uuid.UUID, changes ...roleChange) {
	roleChangedMu.RLock()
	hooks := append([]RoleChangedFunc(nil), roleChangedHooks...)
	roleChangedMu.RUnlock()
	if len(hooks) == 0 || len(changes) == 0 {
		return
	}
	bg.Go(ctx, "orgs.role_changed", func(base context.Context) error {
		for _, c := range changes {
			for _, fn := range hooks {
				runHook(base, "role-changed", orgID, c.userID, func(ctx context.Context) { fn(ctx, orgID, c.userID, c.from, c.to) })
			}
		}
		return nil
	})
}

// runHook runs one hook call with its own timeout, logging a panic.
func runHook(base context.Context, what string, orgID, userID uuid.UUID, call func(ctx context.Context)) {
	ctx, cancel := context.WithTimeout(base, memberRemovedTimeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "orgs: "+what+" hook panicked", "org_id", orgID, "user_id", userID, "panic", p)
		}
	}()
	call(ctx)
}

// AccountDeleted fires the member-removed hooks for every org the deleted
// user belonged to (collected by DeleteAccount).
func (s *Service) AccountDeleted(ctx context.Context, userID uuid.UUID) error {
	v, ok := departedOnDeletion.LoadAndDelete(userID)
	if !ok {
		return nil
	}
	list, _ := v.([]uuid.UUID)
	for _, orgID := range list {
		fireMemberRemoved(ctx, orgID, userID)
	}
	return nil
}
