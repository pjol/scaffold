package feed

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
)

const postCols = `p.id, p.kind, p.audience, p.author_id, u.display_name, u.avatar_url,
	p.body, p.link_url, p.pinned_until, p.edited_at, p.created_at, p.removed_at`

func scanPosts(rows pgx.Rows) ([]Post, error) {
	defer rows.Close()
	out := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Kind, &p.Audience, &p.Author.ID, &p.Author.DisplayName, &p.Author.AvatarURL,
			&p.Body, &p.LinkURL, &p.PinnedUntil, &p.EditedAt, &p.CreatedAt, &p.RemovedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// pinnedSQL selects the active announcements shown above the feed: at most
// maxPinned, newest first. $1 audiences, $2 blocked ids.
const pinnedSQL = `
	SELECT p2.id FROM feed_posts p2
	WHERE p2.kind = 'announcement' AND p2.pinned_until > now() AND p2.removed_at IS NULL
	  AND p2.audience = ANY($1) AND NOT (p2.author_id = ANY($2))
	ORDER BY p2.created_at DESC, p2.id DESC LIMIT 3`

type listFilter struct {
	limit    int
	cursor   string
	q        string
	authorID *uuid.UUID
}

// listPosts is the viewer's feed, newest first, keyset on (created_at, id).
// The unfiltered feed leaves out the pinned announcements (GET /feed/pinned
// shows them on top); search and author filters include everything.
func (s *Service) listPosts(ctx context.Context, v viewer, f listFilter) (httpx.Page[Post], error) {
	var c cursor
	if err := httpx.DecodeCursor(f.cursor, &c); err != nil {
		return httpx.Page[Post]{}, err
	}
	tsq := searchQuery(f.q)
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+postCols+`
		FROM feed_posts p JOIN users u ON u.id = p.author_id
		WHERE p.removed_at IS NULL
		  AND p.audience = ANY($1)
		  AND NOT (p.author_id = ANY($2))
		  AND ($3::timestamptz IS NULL OR (p.created_at, p.id) < ($3, $4::uuid))
		  AND ($5::uuid IS NULL OR p.author_id = $5)
		  AND ($6::text = '' OR p.search @@ to_tsquery('simple', $6))
		  AND ($6::text <> '' OR $5::uuid IS NOT NULL OR p.id NOT IN (`+pinnedSQL+`))
		ORDER BY p.created_at DESC, p.id DESC LIMIT $7`,
		v.audiences, v.blocked, nullTime(c.T), c.ID, f.authorID, tsq, f.limit+1)
	if err != nil {
		return httpx.Page[Post]{}, err
	}
	posts, err := scanPosts(rows)
	if err != nil {
		return httpx.Page[Post]{}, err
	}
	page := httpx.NewPage(posts, f.limit, func(p Post) any { return cursor{p.CreatedAt, p.ID} })
	if err := s.hydratePosts(ctx, v.id, page.Items); err != nil {
		return httpx.Page[Post]{}, err
	}
	return page, nil
}

func (s *Service) listPostsHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	v, err := s.viewerFor(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	f := listFilter{limit: p.Limit, cursor: p.Cursor, q: strings.TrimSpace(r.URL.Query().Get("q"))}
	if a := r.URL.Query().Get("author_id"); a != "" {
		aid, err := uuid.Parse(a)
		if err != nil {
			return httpx.BadRequest("Invalid author_id.")
		}
		f.authorID = &aid
	}
	page, err := s.listPosts(r.Context(), v, f)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

func (s *Service) pinnedHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	v, err := s.viewerFor(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(r.Context(), `
		SELECT `+postCols+`
		FROM feed_posts p JOIN users u ON u.id = p.author_id
		WHERE p.id IN (`+pinnedSQL+`)
		ORDER BY p.created_at DESC, p.id DESC`, v.audiences, v.blocked)
	if err != nil {
		return err
	}
	posts, err := scanPosts(rows)
	if err != nil {
		return err
	}
	if err := s.hydratePosts(r.Context(), v.id, posts); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": posts})
	return nil
}

// loadPost returns one hydrated post. visibleTo nil skips the audience and
// block checks (staff views); removed posts are returned only then.
func (s *Service) loadPost(ctx context.Context, id uuid.UUID, viewerID uuid.UUID, visibleTo *viewer) (Post, error) {
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+postCols+` FROM feed_posts p JOIN users u ON u.id = p.author_id WHERE p.id = $1`, id)
	if err != nil {
		return Post{}, err
	}
	posts, err := scanPosts(rows)
	if err != nil {
		return Post{}, err
	}
	if len(posts) == 0 {
		return Post{}, httpx.NotFound("That post isn't available.")
	}
	p := posts[0]
	if visibleTo != nil {
		if p.RemovedAt != nil || !visibleTo.reads(p.Audience) || visibleTo.blocks(p.Author.ID) {
			return Post{}, httpx.NotFound("That post isn't available.")
		}
		p.RemovedAt = nil
	}
	if err := s.hydratePosts(ctx, viewerID, posts); err != nil {
		return Post{}, err
	}
	return posts[0], nil
}

func (s *Service) getPostHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerFor(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	p, err := s.loadPost(r.Context(), pid, id.UserID, &v)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// postRef is the minimum a write needs to know about a post.
type postRef struct {
	id, authorID uuid.UUID
	audience     string
}

// visiblePost loads a post the viewer may interact with (not removed, in
// their audiences, no block with its author), else 404.
func (s *Service) visiblePost(ctx context.Context, q db.Querier, v viewer, id uuid.UUID) (postRef, error) {
	ref := postRef{id: id}
	err := q.QueryRow(ctx, `SELECT author_id, audience FROM feed_posts WHERE id = $1 AND removed_at IS NULL`, id).
		Scan(&ref.authorID, &ref.audience)
	if db.IsNoRows(err) || (err == nil && (!v.reads(ref.audience) || v.blocks(ref.authorID))) {
		return postRef{}, httpx.NotFound("That post isn't available.")
	}
	return ref, err
}

// ---- create ----------------------------------------------------------------

type postReq struct {
	Kind        string         `json:"kind"`     // post (default) | announcement (staff)
	Audience    string         `json:"audience"` // default "all"
	Body        string         `json:"body"`
	LinkURL     *string        `json:"link_url"`
	Media       []mediaInput   `json:"media"`
	Mentions    []mentionInput `json:"mentions"`
	PinnedUntil *time.Time     `json:"pinned_until"` // announcements only
}

func (s *Service) createPostHandler(w http.ResponseWriter, r *http.Request) error {
	var req postReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := s.createPost(r.Context(), auth.MustFromContext(r.Context()), req)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, p)
	return nil
}

func (s *Service) createPost(ctx context.Context, caller *auth.Identity, req postReq) (Post, error) {
	if req.Kind == "" {
		req.Kind = KindPost
	}
	switch req.Kind {
	case KindPost:
		if req.PinnedUntil != nil {
			return Post{}, httpx.BadRequest("Only announcements can be pinned.")
		}
	case KindAnnouncement:
		if !caller.IsSupport() {
			return Post{}, httpx.Forbidden("Only staff can post announcements.")
		}
		if err := validPin(req.PinnedUntil); err != nil {
			return Post{}, err
		}
	default:
		return Post{}, httpx.BadRequest("Kind must be post or announcement.")
	}
	if req.Audience == "" {
		req.Audience = AudienceAll
	}
	if ok, err := s.scope.CanPost(ctx, caller, req.Audience); err != nil {
		return Post{}, err
	} else if !ok {
		return Post{}, httpx.Forbidden("You can't post there.")
	}
	if err := checkText(ctx, req.Body, s.cfg.postMax, "Your post"); err != nil {
		return Post{}, err
	}
	link, err := normalizeLink(req.LinkURL)
	if err != nil {
		return Post{}, err
	}
	media, err := validateMedia(req.Media, caller.UserID, nil)
	if err != nil {
		return Post{}, err
	}
	if strings.TrimSpace(req.Body) == "" && len(media) == 0 && link == nil {
		return Post{}, httpx.BadRequest("Write something or attach a file.")
	}
	mentions, err := s.resolveMentions(ctx, caller.UserID, req.Body, req.Mentions)
	if err != nil {
		return Post{}, err
	}

	var postID uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO feed_posts (author_id, kind, audience, body, link_url, pinned_until)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			caller.UserID, req.Kind, req.Audience, req.Body, link, req.PinnedUntil).Scan(&postID); err != nil {
			return err
		}
		if err := insertMedia(ctx, tx, postID, media); err != nil {
			return err
		}
		if err := insertMentions(ctx, tx, postID, nil, mentions); err != nil {
			return err
		}
		if req.Kind == KindAnnouncement {
			return audit.Log(ctx, tx, caller.UserID, "feed.announcement.create", "feed_post", postID.String(),
				map[string]any{"audience": req.Audience, "pinned_until": req.PinnedUntil})
		}
		return nil
	})
	if err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, postID, caller.UserID, nil)
	if err != nil {
		return Post{}, err
	}
	p.RemovedAt = nil
	s.publish(ctx, p.Audience, EventPost, map[string]any{
		"post_id": p.ID, "author_id": p.Author.ID, "kind": p.Kind, "audience": p.Audience, "created_at": p.CreatedAt,
	})
	author := actorName(caller.DisplayName)
	link2 := "/feed/posts/" + postID.String()
	data := map[string]any{"post_id": postID}
	s.afterCommit(ctx, "feed.notify_post", func(ctx context.Context) {
		notified := map[uuid.UUID]bool{}
		for _, m := range mentions {
			notified[m.userID] = true
			s.notifyUser(ctx, caller.UserID, notify.Notification{
				UserID: m.userID, Kind: NotifyMention, Title: author + " mentioned you",
				Body: preview(req.Body), Link: link2, Data: data,
			})
		}
		if req.Kind == KindAnnouncement {
			body := preview(req.Body)
			if body == "" {
				body = "Open the feed to read it."
			}
			s.fanOut(ctx, caller.UserID, req.Audience, notified, notify.Notification{
				Kind: NotifyAnnouncement, Title: "New announcement", Body: body, Link: link2, Data: data,
			})
		}
	})
	return p, nil
}

func validPin(t *time.Time) error {
	if t == nil {
		return nil
	}
	now := time.Now()
	if !t.After(now) {
		return httpx.BadRequest("Pick a pin end time in the future.")
	}
	if t.After(now.Add(maxPinDuration)) {
		return httpx.BadRequest("Pins can last up to 90 days.")
	}
	return nil
}

func insertMedia(ctx context.Context, tx pgx.Tx, postID uuid.UUID, media []mediaItem) error {
	for i, m := range media {
		_, err := tx.Exec(ctx, `
			INSERT INTO feed_media (post_id, position, key, kind, content_type, width, height, name)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, postID, i, m.key, m.kind, m.contentType, m.width, m.height, m.name)
		if db.IsUniqueViolation(err, "feed_media_key_key") {
			return httpx.Conflict("That file is already attached to another post. Upload it again.")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func insertMentions(ctx context.Context, tx pgx.Tx, postID uuid.UUID, commentID *uuid.UUID, mentions []mentionRow) error {
	for _, m := range mentions {
		if _, err := tx.Exec(ctx, `
			INSERT INTO feed_mentions (post_id, comment_id, user_id, start_idx, length)
			VALUES ($1, $2, $3, $4, $5)`, postID, commentID, m.userID, m.start, m.length); err != nil {
			return err
		}
	}
	return nil
}

// ---- edit ------------------------------------------------------------------

type updatePostReq struct {
	Body     *string        `json:"body"`     // mentions are replaced whenever body is sent
	LinkURL  *string        `json:"link_url"` // "" clears
	Media    *[]mediaInput  `json:"media"`    // replaces the whole list when sent
	Mentions []mentionInput `json:"mentions"`
}

// updatePostHandler edits the caller's own post. Kind, audience and the pin
// window are fixed after creation (staff change pins in the admin API).
func (s *Service) updatePostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req updatePostReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	var (
		trashed       []string
		newMentions   []mentionRow
		audience, txt string
	)
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var link *string
		if err := tx.QueryRow(ctx, `
			SELECT body, link_url, audience FROM feed_posts
			WHERE id = $1 AND author_id = $2 AND removed_at IS NULL FOR UPDATE`, pid, caller.UserID).
			Scan(&txt, &link, &audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("That post isn't available.")
			}
			return err
		}
		if req.Body != nil && *req.Body != txt {
			if err := checkText(ctx, *req.Body, s.cfg.postMax, "Your post"); err != nil {
				return err
			}
			txt = *req.Body
		}
		if req.LinkURL != nil {
			l, err := normalizeLink(req.LinkURL)
			if err != nil {
				return err
			}
			link = l
		}
		old := map[string]*string{}
		var oldKeys []string
		rows, err := tx.Query(ctx, `SELECT key, name FROM feed_media WHERE post_id = $1`, pid)
		if err != nil {
			return err
		}
		for rows.Next() {
			var k string
			var n *string
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return err
			}
			old[k] = n
			oldKeys = append(oldKeys, k)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		mediaCount := len(oldKeys)
		if req.Media != nil {
			media, err := validateMedia(*req.Media, caller.UserID, old)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM feed_media WHERE post_id = $1`, pid); err != nil {
				return err
			}
			if err := insertMedia(ctx, tx, pid, media); err != nil {
				return err
			}
			keep := map[string]bool{}
			for _, m := range media {
				keep[m.key] = true
			}
			for _, k := range oldKeys {
				if !keep[k] {
					trashed = append(trashed, k)
				}
			}
			mediaCount = len(media)
		}
		if strings.TrimSpace(txt) == "" && mediaCount == 0 && link == nil {
			return httpx.BadRequest("Write something or attach a file.")
		}
		if req.Body != nil {
			mentions, err := s.resolveMentions(ctx, caller.UserID, txt, req.Mentions)
			if err != nil {
				return err
			}
			rows, err := tx.Query(ctx, `
				DELETE FROM feed_mentions WHERE post_id = $1 AND comment_id IS NULL RETURNING user_id`, pid)
			if err != nil {
				return err
			}
			before, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			if err != nil {
				return err
			}
			if err := insertMentions(ctx, tx, pid, nil, mentions); err != nil {
				return err
			}
			had := map[uuid.UUID]bool{}
			for _, u := range before {
				had[u] = true
			}
			for _, m := range mentions {
				if !had[m.userID] {
					newMentions = append(newMentions, m)
				}
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE feed_posts SET body = $2, link_url = $3, edited_at = now(), updated_at = now() WHERE id = $1`,
			pid, txt, link); err != nil {
			return err
		}
		return trash(ctx, tx, trashed)
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, trashed)
	v, err := s.viewerFor(ctx, caller.UserID)
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, pid, caller.UserID, &v)
	if err != nil {
		return err
	}
	s.publish(ctx, audience, EventPostUpdated, map[string]any{"post_id": pid, "deleted": false})
	author := actorName(caller.DisplayName)
	s.afterCommit(ctx, "feed.notify_mentions", func(ctx context.Context) {
		for _, m := range newMentions {
			s.notifyUser(ctx, caller.UserID, notify.Notification{
				UserID: m.userID, Kind: NotifyMention, Title: author + " mentioned you",
				Body: preview(txt), Link: "/feed/posts/" + pid.String(), Data: map[string]any{"post_id": pid},
			})
		}
	})
	httpx.OK(w, p)
	return nil
}

// ---- delete ----------------------------------------------------------------

func (s *Service) deletePostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	pid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var keys []string
	var audience string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT audience FROM feed_posts WHERE id = $1 AND author_id = $2 AND removed_at IS NULL FOR UPDATE`,
			pid, caller.UserID).Scan(&audience); err != nil {
			if db.IsNoRows(err) {
				return httpx.NotFound("That post isn't available.")
			}
			return err
		}
		rows, err := tx.Query(ctx, `SELECT key FROM feed_media WHERE post_id = $1`, pid)
		if err != nil {
			return err
		}
		if keys, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		// Comments, reactions, mentions and media rows cascade.
		if _, err := tx.Exec(ctx, `DELETE FROM feed_posts WHERE id = $1`, pid); err != nil {
			return err
		}
		return trash(ctx, tx, keys)
	})
	if err != nil {
		return err
	}
	s.deleteObjects(ctx, keys)
	s.publish(ctx, audience, EventPostUpdated, map[string]any{"post_id": pid, "deleted": true})
	httpx.NoContent(w)
	return nil
}

// ---- trash -----------------------------------------------------------------

// trash queues storage keys for deletion inside tx.
func trash(ctx context.Context, tx pgx.Tx, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO feed_media_trash (key) SELECT unnest($1::text[]) ON CONFLICT (key) DO NOTHING`, keys)
	return err
}

// deleteObjects tries to remove trashed objects right away; the worker
// retries whatever fails.
func (s *Service) deleteObjects(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	s.afterCommit(ctx, "feed.delete_media", func(ctx context.Context) {
		if err := s.emptyTrashKeys(ctx, keys); err != nil {
			slog.WarnContext(ctx, "feed: media cleanup will retry", "err", err)
		}
	})
}

func (s *Service) emptyTrash(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `SELECT key FROM feed_media_trash ORDER BY created_at LIMIT 200`)
	if err != nil {
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	return s.emptyTrashKeys(ctx, keys)
}

func (s *Service) emptyTrashKeys(ctx context.Context, keys []string) error {
	var lastErr error
	for _, k := range keys {
		// A key re-attached to a live post since it was trashed stays.
		var live bool
		if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM feed_media WHERE key = $1)`, k).Scan(&live); err != nil {
			return err
		}
		if !live {
			if err := s.d.Storage.Delete(ctx, k); err != nil {
				lastErr = err
				continue
			}
		}
		if _, err := s.d.Pool.Exec(ctx, `DELETE FROM feed_media_trash WHERE key = $1`, k); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ---- unread + seen ---------------------------------------------------------

func (s *Service) unreadHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	v, err := s.viewerFor(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	var n int
	err = s.d.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM (
		  SELECT 1 FROM feed_posts p
		  WHERE p.removed_at IS NULL AND p.audience = ANY($1) AND p.author_id <> $2
		    AND NOT (p.author_id = ANY($3))
		    AND p.created_at > COALESCE((SELECT seen_at FROM feed_seen WHERE user_id = $2),
		                                (SELECT created_at FROM users WHERE id = $2))
		  LIMIT 100) x`, v.audiences, id.UserID, v.blocked).Scan(&n)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"unread_count": n, "has_unread": n > 0})
	return nil
}

func (s *Service) seenHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if _, err := s.d.Pool.Exec(r.Context(), `
		INSERT INTO feed_seen (user_id, seen_at) VALUES ($1, now())
		ON CONFLICT (user_id) DO UPDATE SET seen_at = GREATEST(feed_seen.seen_at, EXCLUDED.seen_at)`, id.UserID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// ---- people (mention suggestions) ------------------------------------------

// PeopleSearcher is an optional Scope extension: when the configured Scope
// implements it, mention suggestions come from it (members of an org, say)
// instead of every active account.
type PeopleSearcher interface {
	SearchPeople(ctx context.Context, viewer uuid.UUID, q string, limit int) ([]Author, error)
}

func (s *Service) peopleHandler(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(r.URL.Query().Get("q")), "@"))
	if q == "" || len([]rune(q)) > 60 {
		httpx.OK(w, map[string]any{"items": []Author{}})
		return nil
	}
	v, err := s.viewerFor(r.Context(), id.UserID)
	if err != nil {
		return err
	}
	var people []Author
	if ps, ok := s.scope.(PeopleSearcher); ok {
		found, err := ps.SearchPeople(r.Context(), id.UserID, q, 16)
		if err != nil {
			return err
		}
		for _, a := range found {
			if a.ID != id.UserID && !v.blocks(a.ID) && len(people) < 8 {
				people = append(people, a)
			}
		}
	} else {
		rows, err := s.d.Pool.Query(r.Context(), `
			SELECT id, display_name, avatar_url FROM users
			WHERE status = 'active' AND display_name <> '' AND id <> $1 AND NOT (id = ANY($2))
			  AND (display_name ILIKE $3 || '%' OR display_name ILIKE '% ' || $3 || '%')
			ORDER BY display_name, id LIMIT 8`, id.UserID, v.blocked, likePrefix(q))
		if err != nil {
			return err
		}
		if people, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Author]); err != nil {
			return err
		}
	}
	if people == nil {
		people = []Author{}
	}
	httpx.OK(w, map[string]any{"items": people})
	return nil
}
