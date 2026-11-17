package social

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// ActivityItem is one item as a reader sees it.
type ActivityItem struct {
	ID        uuid.UUID      `json:"id"`
	Kind      string         `json:"kind"`
	Icon      string         `json:"icon"`
	Actor     Ref            `json:"actor"`
	Subject   *Ref           `json:"subject"`
	Title     string         `json:"title"`
	Summary   string         `json:"summary"`
	Link      string         `json:"link"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}

type activityRow struct {
	id        uuid.UUID
	actor     uuid.UUID
	kind      string
	subject   *uuid.UUID
	data      []byte
	createdAt time.Time
}

type activityCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// fetchFn reads up to n rows older than c (newest first by created_at, id).
type fetchFn func(ctx context.Context, c *activityCursor, n int) ([]activityRow, error)

func collectActivity(rows pgx.Rows, err error) ([]activityRow, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (activityRow, error) {
		var a activityRow
		return a, r.Scan(&a.id, &a.actor, &a.kind, &a.subject, &a.data, &a.createdAt)
	})
}

func cursorArgs(c *activityCursor) (*time.Time, uuid.UUID) {
	if c == nil {
		return nil, uuid.Nil
	}
	return &c.T, c.ID
}

// actorFetch reads one actor's items down the actor index. The owner (self)
// sees every item at once; others only the sections they may see, once
// visible_after has passed.
func actorFetch(q db.Querier, actor uuid.UUID, self bool, sections []string) fetchFn {
	return func(ctx context.Context, c *activityCursor, n int) ([]activityRow, error) {
		t, id := cursorArgs(c)
		return collectActivity(q.Query(ctx, `
			SELECT id, actor_id, kind, subject_id, data, created_at FROM social_activity
			WHERE actor_id = $1
			  AND ($2::bool OR (visible_after <= now() AND section = ANY($3)))
			  AND ($4::timestamptz IS NULL OR (created_at, id) < ($4, $5))
			ORDER BY created_at DESC, id DESC LIMIT $6`, actor, self, sections, t, id, n))
	}
}

// feedFetch reads the newest items of many actors: each actor down its own
// index (at most n apiece, its visible sections filtered in SQL), merged.
// "actor_id = ANY(...) ORDER BY created_at" would read and sort every
// followee's whole history on each page. sections[i] is a comma list for
// actors[i].
func feedFetch(q db.Querier, actors []uuid.UUID, sections []string) fetchFn {
	return func(ctx context.Context, c *activityCursor, n int) ([]activityRow, error) {
		t, id := cursorArgs(c)
		return collectActivity(q.Query(ctx, `
			SELECT a.id, a.actor_id, a.kind, a.subject_id, a.data, a.created_at
			FROM unnest($1::uuid[], $2::text[]) AS f(actor, secs)
			CROSS JOIN LATERAL (
				SELECT x.id, x.actor_id, x.kind, x.subject_id, x.data, x.created_at FROM social_activity x
				WHERE x.actor_id = f.actor AND x.visible_after <= now()
				  AND x.section = ANY (string_to_array(f.secs, ','))
				  AND ($3::timestamptz IS NULL OR (x.created_at, x.id) < ($3, $4))
				ORDER BY x.created_at DESC, x.id DESC LIMIT $5) a
			ORDER BY a.created_at DESC, a.id DESC LIMIT $5`, actors, sections, t, id, n))
	}
}

// render turns rows into items: registered kinds only, without subjects a
// block separates from the viewer or whose account is gone.
func render(ctx context.Context, q db.Querier, rows []activityRow, viewer uuid.UUID, hidden map[uuid.UUID]bool) ([]ActivityItem, error) {
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		ids = append(ids, r.actor)
		if r.subject != nil {
			ids = append(ids, *r.subject)
		}
	}
	refs, err := Refs(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ActivityItem, 0, len(rows))
	for _, r := range rows {
		it, ok := renderOne(r, refs, hidden)
		if ok && it.Kind == KindFollowed && it.Subject != nil && it.Subject.UserID == viewer {
			it.Title = "Followed you"
		}
		if ok {
			out = append(out, it)
		}
	}
	return out, nil
}

func renderOne(r activityRow, refs map[uuid.UUID]Ref, hidden map[uuid.UUID]bool) (ActivityItem, bool) {
	k, ok := kind(r.kind)
	actor, okA := refs[r.actor]
	if !ok || !okA || actor.Handle == "" {
		return ActivityItem{}, false
	}
	it := ActivityItem{ID: r.id, Kind: r.kind, Icon: k.Icon, Actor: actor, CreatedAt: r.createdAt, Data: map[string]any{}}
	if r.subject != nil {
		sub, ok := refs[*r.subject]
		if !ok || sub.Handle == "" || hidden[*r.subject] {
			return ActivityItem{}, false
		}
		it.Subject = &sub
	}
	_ = json.Unmarshal(r.data, &it.Data)
	if it.Data == nil {
		it.Data = map[string]any{}
	}
	var out Rendered
	if k.Render != nil {
		var ok bool
		if out, ok = safeRender(k, it.Data, it.Subject); !ok {
			return ActivityItem{}, false
		}
	} else {
		out = defaultRender(it.Data)
		if out.Title == "" {
			out.Title = k.Label
		}
	}
	it.Title, it.Summary, it.Link = out.Title, out.Summary, out.Link
	return it, it.Title != ""
}

// safeRender runs a module's Render; a panic drops the item.
func safeRender(k Kind, data map[string]any, subject *Ref) (r Rendered, ok bool) {
	defer func() {
		if recover() != nil {
			r, ok = Rendered{}, false
		}
	}()
	return k.Render(data, subject)
}

func defaultRender(data map[string]any) Rendered {
	str := func(k string) string {
		v, _ := data[k].(string)
		return strings.TrimSpace(v)
	}
	return Rendered{Title: str("title"), Summary: str("summary"), Link: str("link")}
}

// renderFollowed is KindFollowed: "Followed Sam Rivera".
func renderFollowed(_ map[string]any, subject *Ref) (Rendered, bool) {
	if subject == nil || subject.Handle == "" {
		return Rendered{}, false
	}
	name := subject.DisplayName
	if strings.TrimSpace(name) == "" {
		name = "@" + subject.Handle
	}
	return Rendered{Title: "Followed " + name, Summary: "@" + subject.Handle, Link: "/u/" + subject.Handle}, true
}

// page collects up to limit visible items, fetching past rows that render
// to nothing (four rounds at most). next is nil once nothing older remains.
func page(ctx context.Context, q db.Querier, fetch fetchFn, c *activityCursor, limit int, viewer uuid.UUID, hidden map[uuid.UUID]bool) (httpx.Page[ActivityItem], error) {
	items := []ActivityItem{}
	for round := 0; round < 4; round++ {
		rows, err := fetch(ctx, c, limit+1)
		if err != nil {
			return httpx.Page[ActivityItem]{}, err
		}
		more := len(rows) > limit
		if more {
			rows = rows[:limit]
		}
		if len(rows) == 0 {
			return httpx.Page[ActivityItem]{Items: items}, nil
		}
		rendered, err := render(ctx, q, rows, viewer, hidden)
		if err != nil {
			return httpx.Page[ActivityItem]{}, err
		}
		byID := make(map[uuid.UUID]ActivityItem, len(rendered))
		for _, it := range rendered {
			byID[it.ID] = it
		}
		for i, r := range rows {
			c = &activityCursor{T: r.createdAt, ID: r.id}
			if it, ok := byID[r.id]; ok {
				items = append(items, it)
				if len(items) == limit {
					if more || i < len(rows)-1 {
						return httpx.Page[ActivityItem]{Items: items, NextCursor: httpx.EncodeCursor(c)}, nil
					}
					return httpx.Page[ActivityItem]{Items: items}, nil
				}
			}
		}
		if !more {
			return httpx.Page[ActivityItem]{Items: items}, nil
		}
	}
	// Four rounds of hidden rows: the client follows the cursor.
	return httpx.Page[ActivityItem]{Items: items, NextCursor: httpx.EncodeCursor(c)}, nil
}

func decodeActivityCursor(s string) (*activityCursor, error) {
	if s == "" {
		return nil, nil
	}
	var c activityCursor
	if err := httpx.DecodeCursor(s, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Feed is the following feed: items of the people viewer follows, newest
// first, each gated by its section as the actor set it for followers, held
// until visible_after, without anyone a block separates from the viewer.
func (s *Service) Feed(ctx context.Context, viewer uuid.UUID, cursor string, limit int) (httpx.Page[ActivityItem], error) {
	empty := httpx.Page[ActivityItem]{Items: []ActivityItem{}}
	c, err := decodeActivityCursor(cursor)
	if err != nil {
		return empty, err
	}
	blocked := repairFor(ctx, s.d.Pool, viewer)
	hidden := make(map[uuid.UUID]bool, len(blocked))
	for _, id := range blocked {
		hidden[id] = true
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT f.followee_id, p.privacy FROM social_follows f
		JOIN social_profiles p ON p.user_id = f.followee_id
		JOIN users u ON u.id = f.followee_id AND u.status IN ('active', 'suspended')
		WHERE f.follower_id = $1 AND f.followee_id <> ALL($2)`, viewer, append(blocked, uuid.Nil))
	if err != nil {
		return empty, err
	}
	var actors []uuid.UUID
	var secs []string
	for rows.Next() {
		var id uuid.UUID
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return empty, err
		}
		if v := visibleSections(parsePrivacy(raw), Viewer{Follower: true}); len(v) > 0 {
			actors = append(actors, id)
			secs = append(secs, strings.Join(v, ","))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return empty, err
	}
	if len(actors) == 0 {
		return empty, nil
	}
	return page(ctx, s.d.Pool, feedFetch(s.d.Pool, actors, secs), c, limit, viewer, hidden)
}

// ProfileActivity is owner's activity as viewer sees it.
func (s *Service) ProfileActivity(ctx context.Context, owner, viewer uuid.UUID, cursor string, limit int) (httpx.Page[ActivityItem], error) {
	empty := httpx.Page[ActivityItem]{Items: []ActivityItem{}}
	c, err := decodeActivityCursor(cursor)
	if err != nil {
		return empty, err
	}
	o, err := loadOwner(ctx, s.d.Pool, owner)
	if err != nil {
		return empty, err
	}
	v, err := viewerOf(ctx, s.d.Pool, owner, viewer)
	if err != nil {
		return empty, err
	}
	if v.Blocked || (!v.Self && !o.visible()) {
		return empty, nil
	}
	hidden, _, err := blockedSet(ctx, s.d.Pool, viewer)
	if err != nil {
		return empty, err
	}
	secs := visibleSections(o.privacy, v)
	if !v.Self && len(secs) == 0 {
		return empty, nil
	}
	if secs == nil {
		secs = []string{}
	}
	return page(ctx, s.d.Pool, actorFetch(s.d.Pool, owner, v.Self, secs), c, limit, viewer, hidden)
}

func activityLimit(r *http.Request) int {
	return min(httpx.ParsePage(r).Limit, 50)
}

func (s *Service) getFeed(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Feed(r.Context(), auth.MustFromContext(r.Context()).UserID, r.URL.Query().Get("cursor"), activityLimit(r))
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

func (s *Service) getProfileActivity(w http.ResponseWriter, r *http.Request) error {
	owner, err := s.target(r)
	if err != nil {
		return err
	}
	p, err := s.ProfileActivity(r.Context(), owner, auth.MustFromContext(r.Context()).UserID, r.URL.Query().Get("cursor"), activityLimit(r))
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}
