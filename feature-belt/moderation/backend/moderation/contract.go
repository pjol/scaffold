package moderation

// The cross-feature contract. Features that require moderation (chat, feed,
// assistant, …) import this package and use only what is in this file:
// IsBlocked/BlockedIDs to hide and refuse, Check before storing user text,
// and RegisterTarget so their content can be reported and removed.

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Target is a kind of reportable content owned by another feature. The
// owning feature registers it in its New with RegisterTarget.
type Target interface {
	// Snapshot returns the content as it is now. It is stored with the report,
	// so staff see what was reported even after an edit or delete. Return an
	// error wrapping pgx.ErrNoRows (or httpx.ErrNotFound) when id does not exist.
	Snapshot(ctx context.Context, id string) (Snapshot, error)
	// Remove takes the content down (soft delete, hide) inside the staff
	// member's transaction, so the removal and the audit row commit together.
	Remove(ctx context.Context, tx pgx.Tx, id string) error
}

// Restorer is optional. A Target that can undo Remove implements it, and
// dismissing a case whose content was auto-hidden by reports puts the content
// back. Without it, auto-hidden content stays removed after a dismissal.
type Restorer interface {
	Restore(ctx context.Context, tx pgx.Tx, id string) error
}

// Snapshot is reported content captured at report time.
type Snapshot struct {
	AuthorID  *uuid.UUID `json:"author_id"`
	Text      string     `json:"text"`
	MediaKeys []string   `json:"media_keys"`
	Link      string     `json:"link"` // app path to the content, e.g. /chat/<id>
}

// TargetUser is the target kind for reporting a person. It is built in.
const TargetUser = "user"

var (
	targetsMu sync.RWMutex
	targets   = map[string]Target{}
)

// RegisterTarget makes kind ("chat.message", "feed.post", …) reportable.
// Call it from the owning feature's New. Registering a kind twice replaces it.
func RegisterTarget(kind string, t Target) {
	if kind == "" || kind == TargetUser || t == nil {
		panic("moderation: RegisterTarget needs a non-empty kind other than \"user\" and a Target")
	}
	targetsMu.Lock()
	targets[kind] = t
	targetsMu.Unlock()
}

func target(kind string) (Target, bool) {
	targetsMu.RLock()
	defer targetsMu.RUnlock()
	t, ok := targets[kind]
	return t, ok
}

// TargetKinds lists the registered kinds plus "user", sorted.
func TargetKinds() []string {
	targetsMu.RLock()
	defer targetsMu.RUnlock()
	out := []string{TargetUser}
	for k := range targets {
		out = append(out, k)
	}
	sort.Strings(out[1:])
	return out
}

// IsBlocked reports whether either user has blocked the other. A block in
// either direction hides content and stops messages both ways.
func IsBlocked(ctx context.Context, q db.Querier, a, b uuid.UUID) (bool, error) {
	if a == b {
		return false, nil
	}
	var blocked bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM moderation_blocks WHERE blocker_id = $1 AND blocked_id = $2)
		    OR EXISTS (SELECT 1 FROM moderation_blocks WHERE blocker_id = $2 AND blocked_id = $1)`,
		a, b).Scan(&blocked)
	return blocked, err
}

// BlockedIDs returns every user whose content viewer must not see: people the
// viewer blocked and people who blocked the viewer. Use it as a query filter
// (`author_id <> ALL($n)`); an empty slice means nobody.
func BlockedIDs(ctx context.Context, q db.Querier, viewer uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT blocked_id FROM moderation_blocks WHERE blocker_id = $1
		UNION
		SELECT blocker_id FROM moderation_blocks WHERE blocked_id = $1`, viewer)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, err
}

// ErrContentRejected is what Check returns for text that fails the filter.
// Handlers return it as-is (422, code "content_rejected").
var ErrContentRejected = httpx.NewError(http.StatusUnprocessableEntity, "content_rejected",
	"That can't be posted because it breaks the community rules. Edit it and try again.")

// checker is the configured filter; New installs it. Before New runs (tests,
// tools) Check passes everything except what the default checker rejects.
var checker atomic.Pointer[filter]

// Check runs text through the blocked-terms list and, when configured, the
// moderation provider. It returns ErrContentRejected (a *httpx.Error, 422
// "content_rejected") when the text must not be posted. A provider outage
// fails open: the text passes and the error is logged.
func Check(ctx context.Context, text string) error {
	f := checker.Load()
	if f == nil {
		return nil
	}
	if f.matchTerm(text) {
		return ErrContentRejected
	}
	if f.provider == nil {
		return nil
	}
	flagged, err := f.provider.Flagged(ctx, text)
	if err != nil {
		slog.WarnContext(ctx, "moderation: provider check failed, allowing", "provider", f.provider.Name(), "err", err)
		return nil
	}
	if flagged {
		return ErrContentRejected
	}
	return nil
}
