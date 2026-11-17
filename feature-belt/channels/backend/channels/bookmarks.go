package channels

// Bookmarks: links pinned above a channel's timeline. Links to the app's own
// resources (docs pages, files, records) resolve through the unfurler seam
// for each viewer: someone who can't open the resource sees "No access" and
// neither its title nor its link.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/safehttp"
)

const maxBookmarks = 50

// Bookmark is a link in a channel's bookmarks bar, as the viewer sees it.
type Bookmark struct {
	ID         uuid.UUID `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`  // "" when the viewer has no access
	Kind       string    `json:"kind"` // link | internal
	Card       *Card     `json:"card"` // internal links the viewer can open
	Accessible bool      `json:"accessible"`
	Position   int       `json:"position"`
	CreatedBy  *UserRef  `json:"created_by"`
	CanEdit    bool      `json:"can_edit"`
	CreatedAt  time.Time `json:"created_at"`
}

type bookmarkReq struct {
	Title    *string `json:"title"`
	URL      *string `json:"url"`
	Position *int    `json:"position"`
}

// cleanBookmarkURL accepts an app path or an http(s) URL the server could
// reach (no private hosts outside development).
func (s *Service) cleanBookmarkURL(raw string) (string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || utf8.RuneCountInString(raw) > 2000 {
		return "", false, httpx.BadRequest("Add a link up to 2000 characters.")
	}
	if u, ok := s.internalURL(raw); ok {
		return u.String(), true, nil
	}
	u, err := safehttp.CheckURL(raw, s.devFetch, s.devFetch)
	if err != nil {
		return "", false, httpx.BadRequest("Use an https link to a public site, or a link to a page in this app.")
	}
	return u.String(), false, nil
}

// GET /channels/channels/{id}/bookmarks
func (s *Service) listBookmarks(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, caller.UserID)
	if err != nil {
		return err
	}
	list, err := s.bookmarks(ctx, caller, a, nil)
	if err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": list})
	return nil
}

func (s *Service) bookmarks(ctx context.Context, caller *auth.Identity, a *access, only *uuid.UUID) ([]Bookmark, error) {
	canEditAny, err := s.can(ctx, s.d.Pool, a, ActEdit)
	if err != nil {
		return nil, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT b.id, b.title, b.url, b.position, b.created_at, b.created_by, u.display_name, u.avatar_url, u.status
		FROM channel_bookmarks b LEFT JOIN users u ON u.id = b.created_by
		WHERE b.channel_id = $1 AND ($2::uuid IS NULL OR b.id = $2)
		ORDER BY b.position, b.created_at, b.id`, a.ChannelID, only)
	if err != nil {
		return nil, err
	}
	type row struct {
		Bookmark
		by                   *uuid.UUID
		name, avatar, status *string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.ID, &x.Title, &x.URL, &x.Position, &x.CreatedAt, &x.by, &x.name, &x.avatar, &x.status)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Bookmark, 0, len(list))
	for _, x := range list {
		b := x.Bookmark
		b.Kind, b.Accessible = "link", true
		if x.by != nil {
			u := userRef(*x.by, deref(x.name), x.avatar, deref(x.status))
			b.CreatedBy = &u
		}
		b.CanEdit = a.Member && !a.Archived && (canEditAny || (x.by != nil && *x.by == caller.UserID))
		if _, internal := s.internalURL(b.URL); internal {
			b.Kind = "internal"
			card, err := s.cardFor(ctx, caller, b.URL)
			if err != nil {
				return nil, err
			}
			if card == nil {
				b.Accessible, b.Title, b.URL = false, "No access", ""
			} else {
				b.Card = card
			}
		}
		out = append(out, b)
	}
	return out, nil
}

// POST /channels/channels/{id}/bookmarks {url, title?} — 201. An app link
// must be one the caller can open.
func (s *Service) createBookmark(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req bookmarkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, caller.UserID)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	link, internal, err := s.cleanBookmarkURL(deref(req.URL))
	if err != nil {
		return err
	}
	title := strings.Join(strings.Fields(deref(req.Title)), " ")
	if internal {
		card, err := s.cardFor(ctx, caller, link)
		if err != nil {
			return err
		}
		if card == nil {
			return httpx.NotFound("That page doesn't exist or you can't open it.")
		}
		if title == "" {
			title = card.Title
		}
	}
	if title == "" {
		if u, err := url.Parse(link); err == nil {
			title = strings.TrimPrefix(u.Hostname(), "www.")
		}
	}
	title = truncate(title, 120)
	if title == "" {
		return httpx.BadRequest("Give the bookmark a title.")
	}
	if err := checkText(ctx, title); err != nil {
		return err
	}
	var bid uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n, next int
		if err := tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(position) + 1, 0) FROM channel_bookmarks WHERE channel_id = $1`, id).Scan(&n, &next); err != nil {
			return err
		}
		if n >= maxBookmarks {
			return httpx.BadRequest("A channel can have up to 50 bookmarks.")
		}
		return tx.QueryRow(ctx, `INSERT INTO channel_bookmarks (channel_id, title, url, position, created_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			id, title, link, next, caller.UserID).Scan(&bid)
	})
	if err != nil {
		return err
	}
	s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "bookmarks"})
	list, err := s.bookmarks(ctx, caller, a, &bid)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.JSON(w, http.StatusCreated, list[0])
	return nil
}

// bookmarkAccess loads a bookmark the caller may change (its creator, or
// whoever may edit the channel).
func (s *Service) bookmarkAccess(ctx context.Context, me, id uuid.UUID) (*access, error) {
	var channelID uuid.UUID
	var by *uuid.UUID
	err := s.d.Pool.QueryRow(ctx, `SELECT channel_id, created_by FROM channel_bookmarks WHERE id = $1`, id).Scan(&channelID, &by)
	notFound := httpx.NotFound("Bookmark not found.")
	if db.IsNoRows(err) {
		return nil, notFound
	}
	if err != nil {
		return nil, err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, me)
	if err != nil {
		return nil, notFound
	}
	if err := a.requireWritable(); err != nil {
		return nil, err
	}
	if by == nil || *by != me {
		if err := s.require(ctx, s.d.Pool, a, ActEdit, "Only the person who added it and the channel's admins can change a bookmark."); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// PATCH /channels/bookmarks/{id} {title?, url?, position?}
func (s *Service) updateBookmark(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req bookmarkReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.bookmarkAccess(ctx, caller.UserID, id)
	if err != nil {
		return err
	}
	var title, link *string
	if req.Title != nil {
		t := truncate(strings.Join(strings.Fields(*req.Title), " "), 120)
		if t == "" {
			return httpx.BadRequest("Give the bookmark a title.")
		}
		if err := checkText(ctx, t); err != nil {
			return err
		}
		title = &t
	}
	if req.URL != nil {
		l, internal, err := s.cleanBookmarkURL(*req.URL)
		if err != nil {
			return err
		}
		if internal {
			if card, err := s.cardFor(ctx, caller, l); err != nil || card == nil {
				if err == nil {
					err = httpx.NotFound("That page doesn't exist or you can't open it.")
				}
				return err
			}
		}
		link = &l
	}
	if req.Position != nil && (*req.Position < 0 || *req.Position > 1000) {
		return httpx.BadRequest("position is 0 to 1000.")
	}
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE channel_bookmarks SET title = COALESCE($2, title), url = COALESCE($3, url), position = COALESCE($4, position), updated_at = now()
		WHERE id = $1`, id, title, link, req.Position); err != nil {
		return err
	}
	s.publish(ctx, a.ChannelID, EventChannel, ChannelEvent{ChannelID: a.ChannelID, Reason: "bookmarks"})
	list, err := s.bookmarks(ctx, caller, a, &id)
	if err != nil || len(list) == 0 {
		return err
	}
	httpx.OK(w, list[0])
	return nil
}

// DELETE /channels/bookmarks/{id}
func (s *Service) deleteBookmark(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	a, err := s.bookmarkAccess(ctx, me, id)
	if err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(ctx, `DELETE FROM channel_bookmarks WHERE id = $1`, id); err != nil {
		return err
	}
	s.publish(ctx, a.ChannelID, EventChannel, ChannelEvent{ChannelID: a.ChannelID, Reason: "bookmarks"})
	httpx.NoContent(w)
	return nil
}
