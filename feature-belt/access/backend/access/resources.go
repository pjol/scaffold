package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
	"scaffoldapp/backend/internal/realtime"
)

// Errors the resource functions return for programming mistakes (they are
// plain errors, so a handler that returns them answers 500).
var (
	ErrUnknownKind   = errors.New("access: kind not registered")
	ErrNotRegistered = errors.New("access: resource not registered")
	ErrCycle         = errors.New("access: a resource can't be moved under itself")
)

// NewResource describes a resource a feature just created.
type NewResource struct {
	Kind string
	ID   uuid.UUID
	// OrgID is the org it belongs to. uuid.Nil makes it personal: only the
	// owner and people granted directly can reach it.
	OrgID uuid.UUID
	// OwnerID has implicit manage. uuid.Nil: nobody (org admins manage it).
	OwnerID uuid.UUID
	// Parent: inherit the parent's access (a page under a page, a file in a
	// folder). It must be registered and in the same org.
	Parent *Ref
	// Restricted: with a Parent, don't inherit (only the owner and this
	// resource's own grants count).
	Restricted bool
	// General gives everyone in the org this level (View, Comment or Edit).
	// None (the default) keeps it to the owner, grants and what it inherits.
	General Level
}

// CreateResource registers a resource. Call it in the transaction that
// inserts the feature's row. It doesn't check the creator's rights on the
// parent: Require(…, Edit) on the parent first. Registering an existing
// (kind, id) again is a no-op, so seeds can re-run.
func CreateResource(ctx context.Context, q db.Querier, r NewResource) error {
	k, ok := KindOf(r.Kind)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, r.Kind)
	}
	if r.ID == uuid.Nil {
		return errors.New("access: CreateResource needs an id")
	}
	org := nullUUID(r.OrgID)
	if r.General != None {
		if org == nil || r.General > Edit || !k.allows(r.General) {
			return fmt.Errorf("access: general level %s isn't valid for %s", r.General, r.Kind)
		}
	}
	var parentKind *string
	var parentID *uuid.UUID
	if r.Parent != nil {
		if err := sameOrg(ctx, q, *r.Parent, org); err != nil {
			return err
		}
		parentKind, parentID = &r.Parent.Kind, &r.Parent.ID
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO access_resources (kind, id, org_id, owner_id, parent_kind, parent_id, inherits, admins_manage)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (kind, id) DO NOTHING`,
		r.Kind, r.ID, org, nullUUID(r.OwnerID), parentKind, parentID, !(r.Parent != nil && r.Restricted), k.AdminsManage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 && r.General != None {
		_, err = q.Exec(ctx, `
			INSERT INTO access_grants (resource_kind, resource_id, subject_type, subject_id, level, granted_by)
			VALUES ($1, $2, 'org', $3, $4, $5)`, r.Kind, r.ID, org, int(r.General), nullUUID(r.OwnerID))
	}
	return err
}

// MoveResource gives a resource a new parent (nil: top level), in the
// feature's transaction. The parent must be in the same org and not the
// resource itself or one of its descendants. Its restricted flag is kept.
// After your transaction commits, call Changed(ctx, ref, nil) so features
// drop realtime subscriptions people lost.
func MoveResource(ctx context.Context, q db.Querier, ref Ref, parent *Ref) error {
	var org *uuid.UUID
	err := q.QueryRow(ctx, `SELECT org_id FROM access_resources WHERE kind = $1 AND id = $2 FOR UPDATE`, ref.Kind, ref.ID).Scan(&org)
	if db.IsNoRows(err) {
		return fmt.Errorf("%w: %s", ErrNotRegistered, ref)
	}
	if err != nil {
		return err
	}
	if parent == nil {
		_, err = q.Exec(ctx, `UPDATE access_resources SET parent_kind = NULL, parent_id = NULL, updated_at = now()
			WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID)
		return err
	}
	if err := sameOrg(ctx, q, *parent, org); err != nil {
		return err
	}
	// Walk up from the new parent; meeting ref means the move makes a cycle.
	var cycle bool
	if err := q.QueryRow(ctx, `
		WITH RECURSIVE up (kind, id, depth) AS (
			SELECT $1::text, $2::uuid, 0
			UNION ALL
			SELECT r.parent_kind, r.parent_id, up.depth + 1
			FROM up JOIN access_resources r ON r.kind = up.kind AND r.id = up.id
			WHERE r.parent_id IS NOT NULL AND up.depth < 256
		)
		SELECT EXISTS (SELECT 1 FROM up WHERE kind = $3 AND id = $4)`,
		parent.Kind, parent.ID, ref.Kind, ref.ID).Scan(&cycle); err != nil {
		return err
	}
	if cycle {
		return ErrCycle
	}
	_, err = q.Exec(ctx, `UPDATE access_resources SET parent_kind = $3, parent_id = $4, updated_at = now()
		WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID, parent.Kind, parent.ID)
	return err
}

// DeleteResource removes a resource with its grants and public links, and
// the registry rows of everything under it (delete or move the feature's
// child rows in the same transaction: unregistered rows are unreachable).
func DeleteResource(ctx context.Context, q db.Querier, ref Ref) error {
	_, err := q.Exec(ctx, `DELETE FROM access_resources WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID)
	return err
}

// SetOwner changes a resource's owner (uuid.Nil: no owner) without the HTTP
// route's checks, for a feature's own flows (claiming an import). The
// previous owner keeps nothing implicit.
func SetOwner(ctx context.Context, q db.Querier, ref Ref, ownerID uuid.UUID) error {
	tag, err := q.Exec(ctx, `UPDATE access_resources SET owner_id = $3, updated_at = now() WHERE kind = $1 AND id = $2`,
		ref.Kind, ref.ID, nullUUID(ownerID))
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotRegistered, ref)
	}
	return err
}

// SetOwnerTree is SetOwner for ref and every resource of the same kind under
// it (children, their children, …, restricted ones included), in the
// feature's transaction: a folder moving into another person's drive, or a
// deleted account's work handed to the org (uuid.Nil: org owners and admins
// manage it). Like SetOwner it skips the HTTP route's checks, previous owners
// keep nothing implicit, and nobody is notified. Resources of other kinds
// under the tree keep their owner. After your transaction commits, call
// Changed(ctx, ref, nil): the previous owner may have lost access.
func SetOwnerTree(ctx context.Context, q db.Querier, ref Ref, ownerID uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		WITH RECURSIVE tree (id) AS (
			SELECT id FROM access_resources WHERE kind = $1 AND id = $2
			UNION
			SELECT c.id FROM tree t JOIN access_resources c ON c.parent_kind = $1 AND c.parent_id = t.id AND c.kind = $1
		)
		UPDATE access_resources r SET owner_id = $3, updated_at = now()
		FROM tree WHERE r.kind = $1 AND r.id = tree.id`, ref.Kind, ref.ID, nullUUID(ownerID))
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotRegistered, ref)
	}
	return err
}

func sameOrg(ctx context.Context, q db.Querier, parent Ref, org *uuid.UUID) error {
	var parentOrg *uuid.UUID
	err := q.QueryRow(ctx, `SELECT org_id FROM access_resources WHERE kind = $1 AND id = $2`, parent.Kind, parent.ID).Scan(&parentOrg)
	if db.IsNoRows(err) {
		return fmt.Errorf("%w: parent %s", ErrNotRegistered, parent)
	}
	if err != nil {
		return err
	}
	if !equalUUIDPtr(parentOrg, org) {
		return fmt.Errorf("access: parent %s is in another org", parent)
	}
	return nil
}

// LevelOf returns userID's level on a resource: None when it isn't
// registered, isn't shared with them, they left its org, or its kind has a
// Permission they lack there. When ctx is an AI agent call (auth.WithAgent),
// the level is capped by the user's agent scope (access_agent_cap) and never
// exceeds Edit.
func LevelOf(ctx context.Context, q db.Querier, userID uuid.UUID, kind string, id uuid.UUID) (Level, error) {
	return levelOf(ctx, q, userID, kind, id, ViaAgent(ctx))
}

func levelOf(ctx context.Context, q db.Querier, userID uuid.UUID, kind string, id uuid.UUID, viaAgent bool) (Level, error) {
	var l int
	k, _ := KindOf(kind)
	if k.Permission == "" {
		err := q.QueryRow(ctx, `SELECT access_level($1, $2, $3, $4)`, userID, kind, id, viaAgent).Scan(&l)
		return Level(l), err
	}
	// The kind's org permission gates the level (personal resources have no
	// org to hold it in); an unregistered key lets nobody in.
	p, ok := PermissionOf(k.Permission)
	if !ok {
		return None, nil
	}
	err := q.QueryRow(ctx, `
		SELECT CASE WHEN r.org_id IS NULL OR access_permission_allowed(r.org_id, $1, $5, $6)
		            THEN access_level($1, $2, $3, $4) ELSE 0 END
		FROM access_resources r WHERE r.kind = $2 AND r.id = $3`,
		userID, kind, id, viaAgent, k.Permission, p.DefaultRoles).Scan(&l)
	if db.IsNoRows(err) {
		return None, nil
	}
	return Level(l), err
}

// ViaAgent reports whether ctx is an AI agent acting for the user (the mcp
// and assistant features mark tool calls with auth.WithAgent). Pass it as
// the fourth argument of access_level / third of access_visible in list
// queries: WHERE access_level($1, 'docs.page', p.id, $2) >= 1.
func ViaAgent(ctx context.Context) bool {
	_, ok := auth.AgentFrom(ctx)
	return ok
}

// ErrAgentForbidden answers agent calls to sharing, grant, link, team and
// permission changes: agents never change who has access.
var ErrAgentForbidden = httpx.NewError(http.StatusForbidden, "agent_forbidden", "AI agents can't change sharing or permissions. Do it yourself in the app.")

// refuseAgents guards access's own write routes (agents normally reach the
// app only through tools, which are read-only here; this is the backstop).
func refuseAgents(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ViaAgent(r.Context()) {
			httpx.Fail(w, r, ErrAgentForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Require checks userID has at least min on a resource and returns their
// level. Without view it answers 404 (existence never leaks); with view but
// less than min, 403 access_level_required.
func Require(ctx context.Context, q db.Querier, userID uuid.UUID, kind string, id uuid.UUID, min Level) (Level, error) {
	l, err := LevelOf(ctx, q, userID, kind, id)
	if err != nil {
		return None, err
	}
	if l < View {
		return None, notFound(kind)
	}
	if l < min {
		e := httpx.NewError(http.StatusForbidden, "access_level_required",
			fmt.Sprintf("You can %s this %s but not %s it. Ask someone who manages it.", verb(l), label(kind), verb(min)))
		e.Details = map[string]any{"level": l.String(), "required": min.String()}
		return l, e
	}
	return l, nil
}

func notFound(kind string) *httpx.Error {
	l := label(kind)
	return httpx.NotFound(strings.ToUpper(l[:1]) + l[1:] + " not found.")
}

func label(kind string) string {
	if k, ok := KindOf(kind); ok {
		return k.Label
	}
	return "item"
}

// verb is the level as a verb phrase: "view", "comment on", "edit", "manage".
func verb(l Level) string {
	if l == Comment {
		return "comment on"
	}
	return l.String()
}

// Resource is a registry row.
type Resource struct {
	Ref
	OrgID    *uuid.UUID `json:"org_id"`
	OwnerID  *uuid.UUID `json:"owner_id"`
	Parent   *Ref       `json:"parent"`
	Inherits bool       `json:"inherits"`
}

// Lookup reads a resource's registry row (ErrNotRegistered when missing).
// It checks nothing: call Require first when the id came from a client.
func Lookup(ctx context.Context, q db.Querier, ref Ref) (*Resource, error) {
	r := Resource{Ref: ref}
	var pk *string
	var pid *uuid.UUID
	err := q.QueryRow(ctx, `SELECT org_id, owner_id, parent_kind, parent_id, inherits FROM access_resources
		WHERE kind = $1 AND id = $2`, ref.Kind, ref.ID).Scan(&r.OrgID, &r.OwnerID, &pk, &pid, &r.Inherits)
	if db.IsNoRows(err) {
		return nil, fmt.Errorf("%w: %s", ErrNotRegistered, ref)
	}
	if err != nil {
		return nil, err
	}
	if pk != nil && pid != nil {
		r.Parent = &Ref{Kind: *pk, ID: *pid}
	}
	return &r, nil
}

// ---- change hooks ------------------------------------------------------------

// ChangeFunc learns that access to ref may have shrunk for users (nil: for
// anyone). Features re-check their realtime subscriptions:
// realtime.Revoke(ctx, b, user, topic) per user, or realtime.RevokeTopic
// when users is nil.
type ChangeFunc func(ctx context.Context, ref Ref, users []uuid.UUID)

var (
	changeMu    sync.RWMutex
	changeHooks []ChangeFunc
)

// OnChange registers fn (call it from the feature's New). It runs after every
// sharing change commits — grants, general access, inheritance, ownership,
// team membership, team deletion — once for the changed resource and once for
// each resource inheriting from it (up to 1,000), and after Changed.
func OnChange(fn ChangeFunc) {
	changeMu.Lock()
	changeHooks = append(changeHooks, fn)
	changeMu.Unlock()
}

// Changed reports a change a feature made itself (MoveResource, a bulk
// import) after its transaction commits. Best-effort, in the background.
func Changed(ctx context.Context, ref Ref, users []uuid.UUID) {
	fireChanged(ctx, []Ref{ref}, users)
}

// fireChanged expands refs to their inheriting descendants and runs the hooks
// and access's own realtime revokes off the request.
func fireChanged(ctx context.Context, refs []Ref, users []uuid.UUID) {
	if len(refs) == 0 {
		return
	}
	bg.Go(ctx, "access.changed", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		all, err := expand(ctx, refs)
		if err != nil {
			slog.WarnContext(ctx, "access: expand change", "err", err)
			all = refs
		}
		runChanged(ctx, all, users)
		return nil
	})
}

// runChanged revokes access's resource topics for users (nil: re-check every
// subscriber) and runs the OnChange hooks, for each ref as given.
func runChanged(ctx context.Context, all []Ref, users []uuid.UUID) {
	changeMu.RLock()
	hooks := append([]ChangeFunc(nil), changeHooks...)
	changeMu.RUnlock()
	b, _ := broker.Load().(brokerBox)
	for _, ref := range all {
		if ctx.Err() != nil {
			slog.WarnContext(ctx, "access: change hooks stopped at the deadline", "ref", ref.String())
			return
		}
		if b.Broker != nil {
			topic := resourceTopic(ref.ID)
			if users == nil {
				_ = realtime.RevokeTopic(ctx, b.Broker, topic)
			} else {
				for _, u := range users {
					_ = realtime.Revoke(ctx, b.Broker, u, topic)
				}
			}
		}
		for _, fn := range hooks {
			func() {
				defer func() {
					if p := recover(); p != nil {
						slog.ErrorContext(ctx, "access: change hook panicked", "ref", ref.String(), "panic", p)
					}
				}()
				fn(ctx, ref, users)
			}()
		}
	}
}

// memberRemovedCap bounds the resources whose topics are revoked when someone
// leaves an org (most recently updated first: the ones open in clients).
const memberRemovedCap = 5000

// memberRemoved is access's orgs.OnMemberRemoved hook (removed, left, org
// deleted, account deleted; after the commit). Every org resource needs
// membership, so userID loses the org's team and permission topic
// (access:<org>) and each resource's topics: access's own and, through the
// OnChange hooks with users = [userID], every feature's. The hub keys
// subscriptions by exact topic, so the revokes go per resource, newest first,
// up to memberRemovedCap. A deleted org's resources are gone with it; only its
// org topic is revoked.
func memberRemoved(ctx context.Context, orgID, userID uuid.UUID) {
	if b, _ := broker.Load().(brokerBox); b.Broker != nil {
		if err := realtime.Revoke(ctx, b.Broker, userID, orgTopic(orgID)); err != nil {
			slog.WarnContext(ctx, "access: realtime revoke failed", "org_id", orgID, "err", err)
		}
	}
	p := pool.Load()
	if p == nil {
		return
	}
	rows, err := p.Query(ctx, `SELECT kind, id FROM access_resources WHERE org_id = $1
		ORDER BY updated_at DESC, kind, id LIMIT $2`, orgID, memberRemovedCap)
	if err != nil {
		slog.WarnContext(ctx, "access: list resources for a leaver", "org_id", orgID, "err", err)
		return
	}
	refs, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Ref])
	if err != nil {
		slog.WarnContext(ctx, "access: list resources for a leaver", "org_id", orgID, "err", err)
		return
	}
	if len(refs) == memberRemovedCap {
		slog.WarnContext(ctx, "access: leaver's topics revoked for the newest resources only", "org_id", orgID, "cap", memberRemovedCap)
	}
	runChanged(ctx, refs, []uuid.UUID{userID})
}

// expand returns refs plus every resource inheriting from them (bounded).
func expand(ctx context.Context, refs []Ref) ([]Ref, error) {
	p := pool.Load()
	if p == nil {
		return refs, nil
	}
	kindsArr := make([]string, len(refs))
	ids := make([]uuid.UUID, len(refs))
	for i, r := range refs {
		kindsArr[i], ids[i] = r.Kind, r.ID
	}
	rows, err := p.Query(ctx, `
		WITH RECURSIVE tree (kind, id, depth) AS (
			SELECT k, i, 0 FROM unnest($1::text[], $2::uuid[]) AS s (k, i)
			UNION
			SELECT c.kind, c.id, t.depth + 1
			FROM tree t JOIN access_resources c ON c.parent_kind = t.kind AND c.parent_id = t.id
			WHERE c.inherits AND t.depth < $3
		)
		SELECT DISTINCT kind, id FROM tree LIMIT $4`, kindsArr, ids, maxDepth-1, expandCap)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Ref])
	if err == nil && len(out) == expandCap {
		slog.WarnContext(ctx, "access: change touched more resources than the hook cap", "cap", expandCap)
	}
	return out, err
}

// ---- small helpers -----------------------------------------------------------

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func equalUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// orgRole is the caller's role in the resource's org ("" for personal
// resources or non-members).
func orgRole(ctx context.Context, q db.Querier, org *uuid.UUID, userID uuid.UUID) (string, error) {
	if org == nil {
		return "", nil
	}
	return orgs.RoleOf(ctx, q, *org, userID)
}
