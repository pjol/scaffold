package social

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Blocks and the graph. Moderation owns blocks and has no block hook, so
// social keeps the graph consistent itself, three ways:
//
//  1. Follow refuses when a block exists either way (moderation.IsBlocked).
//  2. Every read that shows edges filters by moderation.BlockedIDs of the
//     person the edges belong to, and repairs on the way: edges between that
//     person and anyone a block separates them from are deleted (with their
//     "followed" activity) and the counters recounted. Lists, profiles, the
//     feed and follows all go through repair, so whoever looks sees a
//     consistent graph at once.
//  3. The social.blocks job repairs a slice of profiles every minute (each
//     profile once a day), for people nobody looks at.

// repair deletes the follows between user and each of blocked (both
// directions) and their "followed" activity, then recounts. It returns how
// many follows went.
func repair(ctx context.Context, q db.Querier, user uuid.UUID, blocked []uuid.UUID) (int64, error) {
	if len(blocked) == 0 {
		return 0, nil
	}
	rows, err := q.Query(ctx, `
		DELETE FROM social_follows
		WHERE (follower_id = $1 AND followee_id = ANY($2)) OR (followee_id = $1 AND follower_id = ANY($2))
		RETURNING follower_id, followee_id`, user, blocked)
	if err != nil {
		return 0, err
	}
	touched := []uuid.UUID{user}
	var n int64
	for rows.Next() {
		var a, b uuid.UUID
		if err := rows.Scan(&a, &b); err != nil {
			rows.Close()
			return n, err
		}
		touched = append(touched, a, b)
		n++
	}
	rows.Close()
	if err := rows.Err(); err != nil || n == 0 {
		return n, err
	}
	if _, err := q.Exec(ctx, `
		DELETE FROM social_activity WHERE kind = $1
		  AND ((actor_id = $2 AND subject_id = ANY($3)) OR (subject_id = $2 AND actor_id = ANY($3)))`,
		KindFollowed, user, blocked); err != nil {
		return n, err
	}
	return n, recount(ctx, q, uniqueIDs(touched))
}

// repairFor fetches user's blocks and repairs. Best-effort on read paths:
// a failure is logged and the read goes on (it filters anyway).
func repairFor(ctx context.Context, q db.Querier, user uuid.UUID) []uuid.UUID {
	ids, err := moderation.BlockedIDs(ctx, q, user)
	if err != nil {
		slog.WarnContext(ctx, "social: blocked ids", "err", err)
		return []uuid.UUID{}
	}
	if _, err := repair(ctx, q, user, ids); err != nil {
		slog.WarnContext(ctx, "social: repair after blocks", "err", err)
	}
	return ids
}

// recount sets the follower and following counters of ids from the edges.
func recount(ctx context.Context, q db.Querier, ids []uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE social_profiles p SET
		  followers = (SELECT count(*) FROM social_follows f WHERE f.followee_id = p.user_id),
		  following = (SELECT count(*) FROM social_follows f WHERE f.follower_id = p.user_id),
		  updated_at = now()
		WHERE p.user_id = ANY($1)`, ids)
	return err
}

// sweepBlocked repairs the slice of profiles due this minute (by a hash of
// the user id, so each comes up once every 1440 runs) that have any follows.
func sweepBlocked(ctx context.Context, pool db.Querier, now time.Time) (int64, error) {
	slot := now.UTC().Hour()*60 + now.UTC().Minute()
	rows, err := pool.Query(ctx, `
		SELECT user_id FROM social_profiles
		WHERE (followers > 0 OR following > 0) AND mod(abs(hashtext(user_id::text)), 1440) = $1`, slot)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, err
	}
	var total int64
	for _, id := range ids {
		blocked, err := moderation.BlockedIDs(ctx, pool, id)
		if err != nil {
			return total, err
		}
		n, err := repair(ctx, pool, id, blocked)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// FollowState is the answer to a follow or unfollow.
type FollowState struct {
	UserID    uuid.UUID `json:"user_id"`
	Following bool      `json:"following"`
	Followers int       `json:"followers"`
}

// follow makes follower follow followee. Idempotent; counters move in the
// same transaction; created says whether the edge is new.
func (s *Service) follow(ctx context.Context, follower, followee uuid.UUID) (created bool, err error) {
	if follower == followee {
		return false, ErrSelfFollow
	}
	blocked, err := moderation.IsBlocked(ctx, s.d.Pool, follower, followee)
	if err != nil {
		return false, err
	}
	if blocked {
		return false, ErrBlocked
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM users WHERE id = $1`, followee).Scan(&status); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != "active" && status != "suspended" {
			return ErrNotFound
		}
		if err := ensureProfiles(ctx, tx, []uuid.UUID{follower, followee}); err != nil {
			return err
		}
		// Lock the follower's row: the cap holds under concurrent follows.
		var following int
		if err := tx.QueryRow(ctx, `SELECT following FROM social_profiles WHERE user_id = $1 FOR UPDATE`, follower).Scan(&following); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO social_follows (follower_id, followee_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, follower, followee)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if following >= s.maxFollowing {
			return errFollowLimit(s.maxFollowing)
		}
		if _, err := tx.Exec(ctx, `UPDATE social_profiles SET following = following + 1, updated_at = now() WHERE user_id = $1`, follower); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE social_profiles SET followers = followers + 1, updated_at = now() WHERE user_id = $1`, followee); err != nil {
			return err
		}
		created = true
		return Record(ctx, tx, Activity{ActorID: follower, Kind: KindFollowed, RefID: followee.String(), SubjectID: followee})
	})
	if err != nil || !created {
		return created, err
	}
	s.notifyFollow(ctx, follower, followee)
	return true, nil
}

// unfollow removes the edge (idempotent) and its activity item.
func unfollow(ctx context.Context, q db.Querier, follower, followee uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM social_follows WHERE follower_id = $1 AND followee_id = $2`, follower, followee)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE social_profiles SET following = GREATEST(following - 1, 0), updated_at = now() WHERE user_id = $1`, follower); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE social_profiles SET followers = GREATEST(followers - 1, 0), updated_at = now() WHERE user_id = $1`, followee); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `DELETE FROM social_activity WHERE actor_id = $1 AND kind = $2 AND ref_id = $3`, follower, KindFollowed, followee.String())
	return err
}

func (s *Service) followState(ctx context.Context, me, other uuid.UUID) (FollowState, error) {
	st := FollowState{UserID: other}
	err := s.d.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM social_follows WHERE follower_id = $1 AND followee_id = $2),
		       COALESCE((SELECT followers FROM social_profiles WHERE user_id = $2), 0)`, me, other).Scan(&st.Following, &st.Followers)
	return st, err
}

// putFollow: PUT /v1/social/follows/{user_id}.
func (s *Service) putFollow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	other, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	if _, err := s.follow(ctx, me, other); err != nil {
		return err
	}
	st, err := s.followState(ctx, me, other)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

// deleteFollow: DELETE /v1/social/follows/{user_id}.
func (s *Service) deleteFollow(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	other, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	if err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error { return unfollow(ctx, tx, me, other) }); err != nil {
		return err
	}
	st, err := s.followState(ctx, me, other)
	if err != nil {
		return err
	}
	httpx.OK(w, st)
	return nil
}

// ---- people lists ----------------------------------------------------------

// Person is a row in followers/following lists, search and suggestions.
type Person struct {
	Ref
	Followers   int        `json:"followers"`
	IsFollowing bool       `json:"is_following"`      // the caller follows them
	FollowsYou  bool       `json:"follows_you"`       // they follow the caller
	Since       *time.Time `json:"since,omitempty"`   // lists: when the follow began
	Mutuals     *int       `json:"mutuals,omitempty"` // suggestions: people you follow who follow them
}

const personCols = `u.id, p.handle, u.display_name, u.avatar_url, p.followers,
	EXISTS (SELECT 1 FROM social_follows x WHERE x.follower_id = $1 AND x.followee_id = u.id),
	EXISTS (SELECT 1 FROM social_follows x WHERE x.follower_id = u.id AND x.followee_id = $1)`

func scanPerson(row pgx.CollectableRow) (Person, error) {
	var p Person
	err := row.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.AvatarURL, &p.Followers, &p.IsFollowing, &p.FollowsYou)
	return p, err
}

type edgeCursor struct {
	T  time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

// listEdges pages owner's followers (dir "followers") or following, newest
// first, as viewer sees them: gated by the owner's connections section,
// without anyone a block separates from the owner or the viewer.
func (s *Service) listEdges(ctx context.Context, owner, viewer uuid.UUID, dir string, pp httpx.PageParams) (httpx.Page[Person], error) {
	empty := httpx.Page[Person]{Items: []Person{}}
	ok, err := CanSee(ctx, s.d.Pool, owner, viewer, SectionConnections)
	if err != nil || !ok {
		return empty, err
	}
	var c edgeCursor
	if err := httpx.DecodeCursor(pp.Cursor, &c); err != nil {
		return empty, err
	}
	hide := append(repairFor(ctx, s.d.Pool, owner), uuid.Nil)
	if viewer != owner {
		_, mine, err := blockedSet(ctx, s.d.Pool, viewer)
		if err != nil {
			return empty, err
		}
		hide = append(hide, mine...)
	}
	self, other := "f.followee_id", "f.follower_id"
	if dir == "following" {
		self, other = "f.follower_id", "f.followee_id"
	}
	var before *time.Time
	if !c.T.IsZero() {
		before = &c.T
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT `+personCols+`, f.created_at
		FROM social_follows f
		JOIN users u ON u.id = `+other+` AND u.status IN ('active', 'suspended')
		JOIN social_profiles p ON p.user_id = u.id
		WHERE `+self+` = $2 AND `+other+` <> ALL($3)
		  AND ($4::timestamptz IS NULL OR (f.created_at, `+other+`) < ($4, $5))
		ORDER BY f.created_at DESC, `+other+` DESC LIMIT $6`,
		viewer, owner, hide, before, c.ID, pp.Limit+1)
	if err != nil {
		return empty, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Person, error) {
		var p Person
		var since time.Time
		err := row.Scan(&p.UserID, &p.Handle, &p.DisplayName, &p.AvatarURL, &p.Followers, &p.IsFollowing, &p.FollowsYou, &since)
		p.Since = &since
		return p, err
	})
	if err != nil {
		return empty, err
	}
	return httpx.NewPage(items, pp.Limit, func(p Person) any { return edgeCursor{T: *p.Since, ID: p.UserID} }), nil
}

func (s *Service) edgesHandler(dir string) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		owner, err := s.target(r)
		if err != nil {
			return err
		}
		page, err := s.listEdges(r.Context(), owner, auth.MustFromContext(r.Context()).UserID, dir, httpx.ParsePage(r))
		if err != nil {
			return err
		}
		httpx.OK(w, page)
		return nil
	}
}

func (s *Service) getFollowers(w http.ResponseWriter, r *http.Request) error {
	return s.edgesHandler("followers")(w, r)
}

func (s *Service) getFollowing(w http.ResponseWriter, r *http.Request) error {
	return s.edgesHandler("following")(w, r)
}
