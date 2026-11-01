package events

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/notify"
	"scaffoldapp/backend/internal/realtime"
)

// hostCheck loads an event and the caller's access; it fails with 404 when
// the caller can't see the event and 403 when they can but aren't a host
// (creatorOnly: the creator).
func (s *Service) hostCheck(ctx context.Context, q db.Querier, eid, caller uuid.UUID, creatorOnly bool) (title string, a access, err error) {
	var host uuid.UUID
	var visibility string
	if err := q.QueryRow(ctx, `SELECT host_id, title, visibility FROM events WHERE id = $1`, eid).Scan(&host, &title, &visibility); err != nil {
		if db.IsNoRows(err) {
			return "", a, errNotFound
		}
		return "", a, err
	}
	if a, err = loadAccess(ctx, q, eid, host, caller); err != nil {
		return "", a, err
	}
	switch {
	case !a.canView(visibility):
		return "", a, errNotFound
	case creatorOnly && !a.creator:
		return "", a, httpx.Forbidden("Only the event's creator can do that.")
	case !a.host():
		return "", a, httpx.Forbidden("Only hosts can do that.")
	}
	return title, a, nil
}

// ---- co-hosts --------------------------------------------------------------

type userReq struct {
	UserID uuid.UUID `json:"user_id"`
}

func (s *Service) addCoHostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req userReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	title, _, err := s.hostCheck(ctx, s.d.Pool, eid, caller.UserID, true)
	if err != nil {
		return err
	}
	if req.UserID == caller.UserID || req.UserID == uuid.Nil {
		return httpx.BadRequest("Pick someone else to co-host.")
	}
	var added bool
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM event_cohosts WHERE event_id = $1`, eid).Scan(&n); err != nil {
			return err
		}
		if n >= maxCoHosts {
			return httpx.Conflict("An event can have up to 10 co-hosts.")
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO event_cohosts (event_id, user_id, added_by)
			SELECT $1, id, $3 FROM users WHERE id = $2 AND status = 'active'
			ON CONFLICT (event_id, user_id) DO NOTHING`, eid, req.UserID, caller.UserID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM event_cohosts WHERE event_id = $1 AND user_id = $2)`,
				eid, req.UserID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return httpx.NotFound("That person isn't available.")
			}
		}
		added = tag.RowsAffected() > 0
		return nil
	})
	if err != nil {
		return err
	}
	if added {
		name := displayName(caller)
		s.afterCommit(ctx, "events.notify_cohost", func(ctx context.Context) {
			s.notifyAll(ctx, caller.UserID, []uuid.UUID{req.UserID}, notify.Notification{
				Kind: NotifyInvite, Title: name + " made you a co-host", Body: title,
				Link: eventLink(eid), Data: map[string]any{"event_id": eid},
			})
		})
	}
	e, _, err := s.loadEvent(ctx, eid, caller.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

// removeCoHostHandler: the creator removes a co-host, or a co-host steps down.
func (s *Service) removeCoHostHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	uid, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	if _, _, err := s.hostCheck(ctx, s.d.Pool, eid, caller.UserID, uid != caller.UserID); err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(ctx, `DELETE FROM event_cohosts WHERE event_id = $1 AND user_id = $2`, eid, uid)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That person isn't a co-host.")
	}
	_ = realtime.Revoke(ctx, s.d.Broker, uid, "events:"+eid.String())
	httpx.NoContent(w)
	return nil
}

// ---- invites ---------------------------------------------------------------

// Invitee is one invited user.
type Invitee struct {
	User      Person    `json:"user"`
	InvitedAt time.Time `json:"invited_at"`
}

func (s *Service) listInvitesHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, _, err := s.hostCheck(ctx, s.d.Pool, eid, caller.UserID, false); err != nil {
		return err
	}
	p := httpx.ParsePage(r)
	var c cursor
	if err := httpx.DecodeCursor(p.Cursor, &c); err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT u.id, u.display_name, u.avatar_url, i.created_at
		FROM event_invites i JOIN users u ON u.id = i.user_id
		WHERE i.event_id = $1 AND ($2::timestamptz IS NULL OR (i.created_at, i.user_id) > ($2, $3::uuid))
		ORDER BY i.created_at, i.user_id LIMIT $4`, eid, nullTime(c.T), c.ID, p.Limit+1)
	if err != nil {
		return err
	}
	list := []Invitee{}
	for rows.Next() {
		var in Invitee
		if err := rows.Scan(&in.User.ID, &in.User.DisplayName, &in.User.AvatarURL, &in.InvitedAt); err != nil {
			rows.Close()
			return err
		}
		list = append(list, in)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, httpx.NewPage(list, p.Limit, func(i Invitee) any { return cursor{i.InvitedAt, i.User.ID} }))
	return nil
}

type inviteReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

// inviteHandler invites users by id (any visibility; for invite-only events
// it is what grants access). New invitees are notified.
func (s *Service) inviteHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req inviteReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.UserIDs) == 0 || len(req.UserIDs) > maxInvitesCall {
		return httpx.BadRequest("Invite 1 to 100 people at a time.")
	}
	title, _, err := s.hostCheck(ctx, s.d.Pool, eid, caller.UserID, false)
	if err != nil {
		return err
	}
	rows, err := s.d.Pool.Query(ctx, `
		INSERT INTO event_invites (event_id, user_id, invited_by)
		SELECT $1, u.id, $3 FROM users u
		WHERE u.id = ANY($2) AND u.status = 'active' AND u.id <> $3
		ON CONFLICT (event_id, user_id) DO NOTHING
		RETURNING user_id`, eid, req.UserIDs, caller.UserID)
	if err != nil {
		return err
	}
	invited, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	name := displayName(caller)
	s.afterCommit(ctx, "events.notify_invited", func(ctx context.Context) {
		s.notifyAll(ctx, caller.UserID, invited, notify.Notification{
			Kind: NotifyInvite, Title: name + " invited you", Body: title,
			Link: eventLink(eid), Data: map[string]any{"event_id": eid},
		})
	})
	httpx.OK(w, map[string]any{"invited": len(invited)})
	return nil
}

// uninviteHandler withdraws an invite. On an invite-only event that also
// drops the person's RSVP (promoting the waitlist if they held a spot) and
// their realtime subscription.
func (s *Service) uninviteHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	ctx := r.Context()
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	uid, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	if _, _, err := s.hostCheck(ctx, s.d.Pool, eid, caller.UserID, false); err != nil {
		return err
	}
	var promoted []uuid.UUID
	var ev lockedEvent
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		if ev, err = lockEvent(ctx, tx, eid); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM event_invites WHERE event_id = $1 AND user_id = $2`, eid, uid)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpx.NotFound("That person isn't invited.")
		}
		if ev.visibility != VisibilityInvite || ev.host == uid {
			return nil
		}
		var prev string
		err = tx.QueryRow(ctx, `DELETE FROM event_rsvps WHERE event_id = $1 AND user_id = $2 RETURNING status`, eid, uid).Scan(&prev)
		if db.IsNoRows(err) {
			return nil
		}
		if err == nil && prev == RSVPGoing {
			promoted, err = promoteWaitlist(ctx, tx, eid)
		}
		return err
	})
	if err != nil {
		return err
	}
	_ = realtime.Revoke(ctx, s.d.Broker, uid, "events:"+eid.String())
	s.publishCounts(ctx, eid)
	s.afterCommit(ctx, "events.notify_promoted", func(ctx context.Context) { s.notifyPromoted(ctx, eid, ev.title, promoted) })
	httpx.NoContent(w)
	return nil
}

// ---- invite link -----------------------------------------------------------

// rotateInviteLinkHandler issues a new invite code (the old one stops
// working). Anyone signed in who opens the link joins the invite list.
func (s *Service) rotateInviteLinkHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, _, err := s.hostCheck(r.Context(), s.d.Pool, eid, caller.UserID, false); err != nil {
		return err
	}
	code := randomToken(12)
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE events SET invite_code = $2, updated_at = now() WHERE id = $1`, eid, code); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"code": code, "path": "/events/join?code=" + code})
	return nil
}

func (s *Service) disableInviteLinkHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	eid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, _, err := s.hostCheck(r.Context(), s.d.Pool, eid, caller.UserID, false); err != nil {
		return err
	}
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE events SET invite_code = NULL, updated_at = now() WHERE id = $1`, eid); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type joinReq struct {
	Code string `json:"code"`
}

func (s *Service) joinHandler(w http.ResponseWriter, r *http.Request) error {
	caller := auth.MustFromContext(r.Context())
	var req joinReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" || len(code) > 64 {
		return httpx.NotFound("That invite link doesn't work anymore.")
	}
	var eid uuid.UUID
	err := s.d.Pool.QueryRow(r.Context(), `
		WITH ev AS (SELECT id, host_id FROM events WHERE invite_code = $1 AND status = 'scheduled')
		INSERT INTO event_invites (event_id, user_id, invited_by)
		SELECT id, $2, host_id FROM ev
		ON CONFLICT (event_id, user_id) DO UPDATE SET event_id = EXCLUDED.event_id
		RETURNING event_id`, code, caller.UserID).Scan(&eid)
	if db.IsNoRows(err) {
		return httpx.NotFound("That invite link doesn't work anymore.")
	}
	if err != nil {
		return err
	}
	e, _, err := s.loadEvent(r.Context(), eid, caller.UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, e)
	return nil
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func displayName(id *auth.Identity) string {
	if strings.TrimSpace(id.DisplayName) == "" {
		return "Someone"
	}
	return id.DisplayName
}
