package docs

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/notify"
)

// NotifyUpdate tells followers a page they follow was edited. Edits are
// batched: one notification per saved version (history.go takes one after
// 2 idle minutes, or every DOCS_SNAPSHOT_INTERVAL while someone keeps
// editing), at most once an hour per follower and page.
const NotifyUpdate = "docs.page_update"

const followNotifyGap = time.Hour

// autoFollow makes userID follow the page unless they unfollowed it before
// (the creator on create, editors when a version is saved).
func autoFollow(ctx context.Context, q db.Querier, userID, pageID uuid.UUID) error {
	_, err := q.Exec(ctx, `INSERT INTO docs_follows (user_id, page_id) VALUES ($1, $2) ON CONFLICT (user_id, page_id) DO NOTHING`, userID, pageID)
	return err
}

// follow / unfollow: PUT / DELETE /docs/pages/{id}/follow (view). Unfollowing
// is remembered, so editing the page again doesn't follow it again.
func (s *Service) follow(w http.ResponseWriter, r *http.Request) error {
	return s.setFollow(w, r, true)
}

func (s *Service) unfollow(w http.ResponseWriter, r *http.Request) error {
	return s.setFollow(w, r, false)
}

func (s *Service) setFollow(w http.ResponseWriter, r *http.Request, on bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := access.Require(ctx, s.d.Pool, me, Kind, id, access.View); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `
		INSERT INTO docs_follows (user_id, page_id, following) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, page_id) DO UPDATE SET following = EXCLUDED.following, updated_at = now()`, me, id, on); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"following": on})
	return nil
}

// followers returns the people following a page who can still open it
// (never agent-capped: notifications carry their own agent target).
func (s *Service) followers(ctx context.Context, pageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT f.user_id FROM docs_follows f JOIN users u ON u.id = f.user_id AND u.status = 'active'
		WHERE f.page_id = $1 AND f.following AND access_level(f.user_id, 'docs.page', $1) >= 1
		ORDER BY f.created_at LIMIT 500`, pageID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// notifyEdits runs after a version is saved: its editors start following the
// page, and the other followers hear who edited it (batched, see
// NotifyUpdate). People blocked by or blocking an editor aren't told.
func (s *Service) notifyEdits(ctx context.Context, pageID uuid.UUID, editors []uuid.UUID, summary string) {
	if len(editors) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, e := range editors {
		if err := autoFollow(ctx, s.d.Pool, e, pageID); err != nil {
			slogWarn(ctx, "docs: auto-follow", err)
		}
	}
	title, err := pageTitle(ctx, s.d.Pool, pageID)
	if err != nil {
		return
	}
	names := make([]string, 0, 3)
	for _, e := range editors {
		if len(names) == 2 {
			break
		}
		names = append(names, s.displayName(ctx, e))
	}
	who := strings.Join(names, " and ")
	if len(editors) > 2 {
		who = names[0] + ", " + names[1] + " and others"
	}
	people, err := s.followers(ctx, pageID)
	if err != nil {
		slogWarn(ctx, "docs: followers", err)
		return
	}
	isEditor := map[uuid.UUID]bool{}
	for _, e := range editors {
		isEditor[e] = true
	}
	for _, u := range people {
		if isEditor[u] {
			continue
		}
		blocked := false
		for _, e := range editors {
			if b, _ := moderation.IsBlocked(ctx, s.d.Pool, e, u); b {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		// Claim the slot first, so two instances never both notify.
		tag, err := s.d.Pool.Exec(ctx, `UPDATE docs_follows SET notified_at = now()
			WHERE user_id = $1 AND page_id = $2 AND following AND (notified_at IS NULL OR notified_at < now() - $3::interval)`,
			u, pageID, fmt.Sprintf("%d seconds", int(followNotifyGap.Seconds())))
		if err != nil || tag.RowsAffected() == 0 {
			continue
		}
		s.send(ctx, notify.Notification{
			UserID: u, Kind: NotifyUpdate, Title: who + " edited " + title, Body: summary,
			Link: "/docs/" + pageID.String(), Data: map[string]any{"page_id": pageID},
		})
	}
}

// notifyFollowersOfComment tells followers about a new comment (people the
// comment flow already told are in done).
func (s *Service) notifyFollowersOfComment(ctx context.Context, actor uuid.UUID, p *Page, thread uuid.UUID, body []Span, done map[uuid.UUID]bool) {
	people, err := s.followers(ctx, p.ID)
	if err != nil {
		return
	}
	name := s.displayName(ctx, actor)
	for _, u := range people {
		if done[u] {
			continue
		}
		if blocked, _ := moderation.IsBlocked(ctx, s.d.Pool, actor, u); blocked {
			continue
		}
		done[u] = true
		s.send(ctx, notify.Notification{UserID: u, Kind: NotifyComment, Title: name + " commented on " + displayTitle(p.Title),
			Body: snippet(plainOf(body), 140), Link: "/docs/" + p.ID.String() + "?comment=" + thread.String(),
			Data: map[string]any{"page_id": p.ID, "thread_id": thread}})
	}
}
