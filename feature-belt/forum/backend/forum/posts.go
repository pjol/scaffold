package forum

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

const postCols = `p.id, p.community_id, c.slug, c.title, p.author_id, u.display_name, u.avatar_url, p.title, p.body,
	p.url, p.domain, p.labels, p.mod_reasons, p.status, p.score, p.hot, p.comment_count, p.pinned, p.locked,
	p.created_at, p.edited_at`

const postFrom = ` FROM forum_posts p JOIN forum_communities c ON c.id = p.community_id JOIN users u ON u.id = p.author_id `

type postRow struct {
	ID           uuid.UUID
	Community    CommunityRef
	AuthorID     uuid.UUID
	AuthorName   string
	AuthorAvatar *string
	Title, Body  string
	URL, Domain  *string
	Labels       []string
	ModReasons   []string
	Status       string
	Score        int
	Hot          float64
	CommentCount int
	Pinned       bool
	Locked       bool
	CreatedAt    time.Time
	EditedAt     *time.Time
}

func scanPost(row pgx.Row) (postRow, error) {
	var p postRow
	err := row.Scan(&p.ID, &p.Community.ID, &p.Community.Slug, &p.Community.Title, &p.AuthorID, &p.AuthorName, &p.AuthorAvatar,
		&p.Title, &p.Body, &p.URL, &p.Domain, &p.Labels, &p.ModReasons, &p.Status, &p.Score, &p.Hot, &p.CommentCount,
		&p.Pinned, &p.Locked, &p.CreatedAt, &p.EditedAt)
	return p, err
}

func scanPosts(rows pgx.Rows) ([]postRow, error) {
	defer rows.Close()
	out := []postRow{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) loadPost(ctx context.Context, q db.Querier, id uuid.UUID, lock bool) (postRow, error) {
	sql := `SELECT ` + postCols + postFrom + ` WHERE p.id = $1`
	if lock {
		sql += ` FOR UPDATE OF p`
	}
	p, err := scanPost(q.QueryRow(ctx, sql, id))
	if db.IsNoRows(err) {
		return p, httpx.NotFound("That post doesn't exist.")
	}
	return p, err
}

func postPath(id uuid.UUID) string { return "/forum/posts/" + id.String() }

// summary renders a post for a viewer: deleted posts lose author and text.
func summary(p postRow, vote int) PostSummary {
	out := PostSummary{ID: p.ID, Community: p.Community, Title: p.Title, Kind: "text", URL: p.URL, Domain: p.Domain,
		Excerpt: Excerpt(p.Body), Score: p.Score, CommentCount: p.CommentCount, ViewerVote: vote,
		Labels: nonNilStrings(p.Labels), Status: p.Status, Pinned: p.Pinned, Locked: p.Locked, CreatedAt: p.CreatedAt, EditedAt: p.EditedAt}
	if p.URL != nil {
		out.Kind = "link"
	}
	if p.Status == StatusDeleted {
		out.Title, out.Excerpt, out.URL, out.Domain, out.Labels = "[deleted]", "", nil, nil, []string{}
		return out
	}
	out.Author = &Author{ID: p.AuthorID, DisplayName: p.AuthorName, AvatarURL: p.AuthorAvatar}
	return out
}

// summaries renders a page of posts with the viewer's votes.
func (s *Service) summaries(ctx context.Context, v viewer, rows []postRow) ([]PostSummary, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, p := range rows {
		ids = append(ids, p.ID)
	}
	votes, err := s.viewerVotes(ctx, v.id, "post", ids)
	if err != nil {
		return nil, err
	}
	out := make([]PostSummary, 0, len(rows))
	for _, p := range rows {
		out = append(out, summary(p, votes[p.ID]))
	}
	return out, nil
}

// ---- list ----

type postCursor struct {
	H  float64   `json:"h,omitempty"`
	S  int       `json:"s,omitempty"`
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

type listQuery struct {
	community *uuid.UUID
	sort      string // hot | new | top
	t         string // top period: day | week | month | year | all
	cursor    string
	limit     int
	authorID  *uuid.UUID
	search    string
}

// listPosts is one keyset page of visible posts (plus, on the author's own
// list, their held and removed ones).
func (s *Service) listPosts(ctx context.Context, v viewer, q listQuery) (httpx.Page[PostSummary], error) {
	var c postCursor
	if err := httpx.DecodeCursor(q.cursor, &c); err != nil {
		return httpx.Page[PostSummary]{}, err
	}
	hasCursor := q.cursor != ""
	args := []any{v.blocked}
	arg := func(x any) string {
		args = append(args, x)
		return fmt.Sprintf("$%d", len(args))
	}
	where := []string{"NOT (p.author_id = ANY($1))"}
	if q.authorID != nil && *q.authorID == v.id {
		where = append(where, "p.status <> 'deleted'")
	} else {
		where = append(where, "p.status = 'visible'")
	}
	if q.community != nil {
		where = append(where, "p.community_id = "+arg(*q.community))
	}
	if q.authorID != nil {
		where = append(where, "p.author_id = "+arg(*q.authorID))
	}
	if tsq := searchQuery(q.search); tsq != "" {
		where = append(where, "p.search @@ to_tsquery('simple', "+arg(tsq)+")")
	}
	var order string
	switch q.sort {
	case "new":
		order = "p.created_at DESC, p.id DESC"
		if hasCursor {
			where = append(where, "(p.created_at, p.id) < ("+arg(c.T)+", "+arg(c.ID)+"::uuid)")
		}
	case "top":
		since, ok := sinceFor(q.t)
		if !ok {
			return httpx.Page[PostSummary]{}, httpx.BadRequest("t must be day, week, month, year or all.")
		}
		if !since.IsZero() {
			where = append(where, "p.created_at > "+arg(since))
		}
		order = "p.score DESC, p.created_at DESC, p.id DESC"
		if hasCursor {
			where = append(where, "(p.score, p.created_at, p.id) < ("+arg(c.S)+"::int, "+arg(c.T)+"::timestamptz, "+arg(c.ID)+"::uuid)")
		}
	case "", "hot":
		order = "p.hot DESC, p.id DESC"
		if hasCursor {
			where = append(where, "(p.hot, p.id) < ("+arg(c.H)+"::float8, "+arg(c.ID)+"::uuid)")
		}
	default:
		return httpx.Page[PostSummary]{}, httpx.BadRequest("sort must be hot, new or top.")
	}
	limit := q.limit
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT `+postCols+postFrom+` WHERE `+strings.Join(where, " AND ")+
		` ORDER BY `+order+` LIMIT `+arg(limit+1), args...)
	if err != nil {
		return httpx.Page[PostSummary]{}, err
	}
	list, err := scanPosts(rows)
	if err != nil {
		return httpx.Page[PostSummary]{}, err
	}
	page := httpx.NewPage(list, limit, func(p postRow) any { return postCursor{H: p.Hot, S: p.Score, T: p.CreatedAt, ID: p.ID} })
	items, err := s.summaries(ctx, v, page.Items)
	if err != nil {
		return httpx.Page[PostSummary]{}, err
	}
	return httpx.Page[PostSummary]{Items: items, NextCursor: page.NextCursor}, nil
}

// listPostsHandler: GET /v1/forum/posts?community=<slug>&sort=hot|new|top&t=&author_id=&q=&cursor=&limit=
func (s *Service) listPostsHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	v, err := s.viewerOf(ctx, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	qs := r.URL.Query()
	p := httpx.ParsePage(r)
	q := listQuery{sort: qs.Get("sort"), t: qs.Get("t"), cursor: p.Cursor, limit: p.Limit, search: qs.Get("q")}
	if slug := qs.Get("community"); slug != "" {
		c, err := s.communityBySlug(ctx, s.d.Pool, slug)
		if err != nil {
			return err
		}
		q.community = &c.ID
	}
	if a := qs.Get("author_id"); a != "" {
		id, err := uuid.Parse(a)
		if err != nil {
			return httpx.BadRequest("Invalid author_id.")
		}
		q.authorID = &id
	}
	page, err := s.listPosts(ctx, v, q)
	if err != nil {
		return err
	}
	httpx.OK(w, page)
	return nil
}

// ---- read one ----

func (s *Service) postView(ctx context.Context, v viewer, p postRow) (Post, error) {
	votes, err := s.viewerVotes(ctx, v.id, "post", []uuid.UUID{p.ID})
	if err != nil {
		return Post{}, err
	}
	out := Post{PostSummary: summary(p, votes[p.ID]), Body: p.Body, Mentions: []Author{}, ModerationReasons: []string{}}
	if p.Status == StatusDeleted {
		out.Body = ""
	} else if out.Mentions, err = s.mentionsOf(ctx, "post", p.ID); err != nil {
		return Post{}, err
	}
	if (p.Status == StatusHeld || p.Status == StatusRemoved) && (v.id == p.AuthorID || v.staff) {
		out.ModerationReasons = nonNilStrings(p.ModReasons)
	}
	if out.CanModerate, err = s.canModerate(ctx, v, p.Community.ID); err != nil {
		return Post{}, err
	}
	return out, nil
}

func (s *Service) getPostHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, auth.MustFromContext(ctx))
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if !v.canSeePost(p) {
		return httpx.NotFound("That post doesn't exist.")
	}
	out, err := s.postView(ctx, v, p)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// ---- create ----

type createPostReq struct {
	Community string      `json:"community"` // slug
	Title     string      `json:"title"`
	Body      string      `json:"body"`
	URL       string      `json:"url"`
	Mentions  []uuid.UUID `json:"mentions"`
}

func validTitle(t string) (string, error) {
	t = strings.TrimSpace(t)
	if n := utf8.RuneCountInString(t); n < TitleMin || n > TitleMax {
		return "", httpx.BadRequest(fmt.Sprintf("The title needs %d to %d characters.", TitleMin, TitleMax))
	}
	return t, nil
}

func validURL(raw string) (*string, *string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil, nil
	}
	if len(raw) > URLMax {
		return nil, nil, httpx.BadRequest(fmt.Sprintf("The link is too long (%d characters at most).", URLMax))
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, httpx.BadRequest("The link must start with http:// or https://.")
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	return &raw, &host, nil
}

// checkRate counts the author's recent items in the database, so the limit
// holds across instances: new accounts get less.
func (s *Service) checkRate(ctx context.Context, table string, author uuid.UUID, perHour, perHourNew int, noun string) error {
	var (
		n       int
		created time.Time
	)
	if err := s.d.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+table+` WHERE author_id = $1 AND created_at > now() - interval '1 hour'),
		(SELECT created_at FROM users WHERE id = $1)`, author).Scan(&n, &created); err != nil {
		return err
	}
	limit := perHour
	if time.Since(created) < newAccount {
		limit = perHourNew
	}
	if n >= limit {
		return rateLimited(fmt.Sprintf("You can write %d %s an hour. Try again later.", limit, noun))
	}
	return nil
}

func (s *Service) placeWords(c CommunityRef, description string) string {
	if description == "" {
		return "the forum community " + c.Title
	}
	return "the forum community " + c.Title + ": " + description
}

// createPostHandler: POST /v1/forum/posts. Screened; held posts are stored
// and visible to their author with the reasons.
func (s *Service) createPostHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	var req createPostReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, err := validTitle(req.Title)
	if err != nil {
		return err
	}
	body := strings.TrimSpace(req.Body)
	if utf8.RuneCountInString(body) > s.cfg.postMax {
		return httpx.BadRequest(fmt.Sprintf("Keep the post under %d characters.", s.cfg.postMax))
	}
	link, domain, err := validURL(req.URL)
	if err != nil {
		return err
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	comm, err := s.communityBySlug(ctx, s.d.Pool, req.Community)
	if err != nil {
		return err
	}
	mod, err := s.canModerate(ctx, v, comm.ID)
	if err != nil {
		return err
	}
	if comm.Locked && !mod {
		return errCommunityLocked
	}
	if err := moderation.CheckPosting(ctx, s.d.Pool, caller.UserID); err != nil { // the clearer refusal first
		return err
	}
	if err := s.checkRate(ctx, "forum_posts", caller.UserID, 5, 2, "posts"); err != nil {
		return err
	}
	mentions, err := s.validMentions(ctx, v, title+"\n"+body, req.Mentions)
	if err != nil {
		return err
	}
	k, err := Karma(ctx, s.d.Pool, caller.UserID, time.Time{})
	if err != nil {
		return err
	}
	id := uuid.New()
	ref := CommunityRef{ID: comm.ID, Slug: comm.Slug, Title: comm.Title}
	urlText := ""
	if link != nil {
		urlText = *link
	}
	dec, err := moderation.Screen(ctx, s.d.Pool, moderation.Item{Kind: TargetPost, ID: id.String(), AuthorID: caller.UserID,
		Title: title, Body: body, URL: urlText, Place: s.placeWords(ref, comm.Description), Link: postPath(id), Karma: &k})
	if err != nil {
		return err
	}
	labels := mergeLabels(dec.Labels, s.appLabels(ctx, LabelInput{Kind: "post", AuthorID: caller.UserID, Community: comm, Title: title, Body: body, URL: urlText}))
	status := dec.Status()
	now := time.Now()
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO forum_posts (id, community_id, author_id, title, body, url, domain, labels, mod_reasons, status, hot, notified, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			id, comm.ID, caller.UserID, title, body, link, domain, labels, dec.Reasons(), status,
			Hot(0, now, hasLabel(labels, moderation.LabelOffTopic)), status == StatusVisible, now); err != nil {
			return err
		}
		if err := saveMentions(ctx, tx, "post", id, mentions); err != nil {
			return err
		}
		return moderation.Record(ctx, tx, dec)
	})
	if err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if status == StatusVisible {
		s.afterVisiblePost(ctx, p, mentions)
	}
	out, err := s.postView(ctx, v, p)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusCreated, PostResult{Post: out, Moderation: dec.Outcome()})
	return nil
}

// ---- edit / delete ----

type editPostReq struct {
	Title    *string      `json:"title"`
	Body     *string      `json:"body"`
	Mentions *[]uuid.UUID `json:"mentions"`
}

// editableStatus refuses edits to held and removed items: a held item's text
// is what its case shows staff, so an approve never publishes unreviewed text.
func editableStatus(status, noun string) error {
	switch status {
	case StatusRemoved:
		return httpx.NewError(http.StatusForbidden, "not_editable", "A removed "+noun+" can't be edited.")
	case StatusHeld:
		return httpx.NewError(http.StatusForbidden, "not_editable", "This "+noun+" is waiting for review and can't be edited until then. You can delete it.")
	case StatusDeleted:
		return httpx.NotFound("That " + noun + " doesn't exist.")
	}
	return nil
}

// editPostHandler: PATCH /v1/forum/posts/{id}. Re-screened; the status never
// drops on edit.
func (s *Service) editPostHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req editPostReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	if p.AuthorID != caller.UserID {
		return httpx.NotFound("That post doesn't exist.")
	}
	if err := editableStatus(p.Status, "post"); err != nil {
		return err
	}
	title, body := p.Title, p.Body
	if req.Title != nil {
		if title, err = validTitle(*req.Title); err != nil {
			return err
		}
	}
	if req.Body != nil {
		body = strings.TrimSpace(*req.Body)
		if utf8.RuneCountInString(body) > s.cfg.postMax {
			return httpx.BadRequest(fmt.Sprintf("Keep the post under %d characters.", s.cfg.postMax))
		}
	}
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	var mentions []uuid.UUID
	if req.Mentions != nil {
		if mentions, err = s.validMentions(ctx, v, title+"\n"+body, *req.Mentions); err != nil {
			return err
		}
	}
	comm, err := s.communityByID(ctx, s.d.Pool, p.Community.ID)
	if err != nil {
		return err
	}
	k, err := Karma(ctx, s.d.Pool, caller.UserID, time.Time{})
	if err != nil {
		return err
	}
	urlText := ""
	if p.URL != nil {
		urlText = *p.URL
	}
	dec, err := moderation.Screen(ctx, s.d.Pool, moderation.Item{Kind: TargetPost, ID: id.String(), AuthorID: caller.UserID,
		Title: title, Body: body, URL: urlText, Place: s.placeWords(p.Community, comm.Description), Link: postPath(id), Karma: &k, Edit: true})
	if err != nil {
		return err
	}
	var before string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		cur, err := s.loadPost(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err := editableStatus(cur.Status, "post"); err != nil {
			return err
		}
		before = cur.Status
		status := cur.Status
		if dec.Status() != StatusVisible {
			status = dec.Status()
		}
		labels := mergeLabels(cur.Labels, dec.Labels)
		if _, err := tx.Exec(ctx, `UPDATE forum_posts SET title = $2, body = $3, labels = $4, status = $5, mod_reasons = $6,
			hot = $7, edited_at = now() WHERE id = $1`,
			id, title, body, labels, status, dec.Reasons(), Hot(cur.Score, cur.CreatedAt, hasLabel(labels, moderation.LabelOffTopic))); err != nil {
			return err
		}
		if req.Mentions != nil {
			if err := saveMentions(ctx, tx, "post", id, mentions); err != nil {
				return err
			}
		}
		return moderation.Record(ctx, tx, dec)
	})
	if err != nil {
		return err
	}
	if p, err = s.loadPost(ctx, s.d.Pool, id, false); err != nil {
		return err
	}
	if p.Status != before {
		s.publish(ctx, id, EventStatus, map[string]any{"post_id": id, "target_kind": "post", "target_id": id, "status": p.Status})
	} else {
		s.publish(ctx, id, EventPostUpdated, map[string]any{"post_id": id})
	}
	out, err := s.postView(ctx, v, p)
	if err != nil {
		return err
	}
	httpx.OK(w, PostResult{Post: out, Moderation: dec.Outcome()})
	return nil
}

// deletePostHandler: DELETE /v1/forum/posts/{id}: the author's post becomes
// a "[deleted]" placeholder; its comments stay.
func (s *Service) deletePostHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `UPDATE forum_posts SET status = 'deleted', removed_at = now(), pinned = false
		WHERE id = $1 AND author_id = $2 AND status <> 'deleted'`, id, caller.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That post doesn't exist.")
	}
	s.publish(ctx, id, EventStatus, map[string]any{"post_id": id, "target_kind": "post", "target_id": id, "status": StatusDeleted})
	httpx.NoContent(w)
	return nil
}

// ---- moderator actions ----

type moderateReq struct {
	Action string `json:"action"` // pin | unpin | lock | reopen | remove | restore (comments: remove | restore)
	Note   string `json:"note"`
}

// moderatePostHandler: POST /v1/forum/posts/{id}/moderate. Staff and the
// community's moderators pin, lock and remove; only staff restore, and never
// past an open moderation case (approve it from the queue). Audited.
func (s *Service) moderatePostHandler(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req moderateReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	req.Note = clipRunes(strings.TrimSpace(req.Note), 1000)
	v, err := s.viewerOf(ctx, caller)
	if err != nil {
		return err
	}
	var status string
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		p, err := s.loadPost(ctx, tx, id, true)
		if err != nil {
			return err
		}
		mod, err := s.canModerate(ctx, v, p.Community.ID)
		if err != nil {
			return err
		}
		if !mod {
			return httpx.Forbidden("Only moderators of this community can do that.")
		}
		status = p.Status
		switch req.Action {
		case "pin", "unpin":
			if p.Status != StatusVisible {
				return httpx.Conflict("Only visible posts can be pinned.")
			}
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET pinned = $2 WHERE id = $1`, id, req.Action == "pin")
		case "lock", "reopen":
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET locked = $2 WHERE id = $1`, id, req.Action == "lock")
		case "remove":
			if p.Status != StatusVisible {
				return httpx.Conflict("This post isn't visible.")
			}
			status = StatusRemoved
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET status = 'removed', removed_at = now(), pinned = false WHERE id = $1`, id)
		case "restore":
			if !v.staff {
				return httpx.Forbidden("Only staff can restore a post.")
			}
			if err := noOpenCase(ctx, tx, TargetPost, id); err != nil {
				return err
			}
			if p.Status != StatusRemoved {
				return httpx.Conflict("This post isn't removed.")
			}
			status = StatusVisible
			_, err = tx.Exec(ctx, `UPDATE forum_posts SET status = 'visible', removed_at = NULL WHERE id = $1`, id)
		default:
			return httpx.BadRequest("action must be pin, unpin, lock, reopen, remove or restore.")
		}
		if err != nil {
			return err
		}
		return audit.Log(ctx, tx, caller.UserID, "forum.post."+req.Action, "forum_post", id.String(),
			map[string]any{"community_id": p.Community.ID, "author_id": p.AuthorID, "note": req.Note})
	})
	if err != nil {
		return err
	}
	if req.Action == "remove" || req.Action == "restore" {
		s.publish(ctx, id, EventStatus, map[string]any{"post_id": id, "target_kind": "post", "target_id": id, "status": status})
	} else {
		s.publish(ctx, id, EventPostUpdated, map[string]any{"post_id": id})
	}
	p, err := s.loadPost(ctx, s.d.Pool, id, false)
	if err != nil {
		return err
	}
	out, err := s.postView(ctx, v, p)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// noOpenCase refuses a forum-side restore while the moderation queue holds
// the item: approving it there keeps the case, the audit and the author's
// notice together.
func noOpenCase(ctx context.Context, q db.Querier, kind string, id uuid.UUID) error {
	var open bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM moderation_cases WHERE target_kind = $1 AND target_id = $2 AND status = 'open')`,
		kind, id.String()).Scan(&open); err != nil {
		return err
	}
	if open {
		return httpx.Conflict("This is in the moderation queue. Approve or remove it from Admin → Moderation.")
	}
	return nil
}
