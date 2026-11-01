package calls

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Host controls act on the live room through the SFU's server API. The
// room only knows identities; the checks here use the meeting's roles.

// liveMeetingAsHost loads {id} for a host or co-host and requires the SFU
// and a live session.
func (s *Service) liveMeetingAsHost(r *http.Request, me *auth.Identity) (*meetingRow, access, error) {
	if err := s.requireMedia(); err != nil {
		return nil, access{}, err
	}
	m, a, err := s.meetingAsHost(r, me)
	if err != nil {
		return nil, access{}, err
	}
	if m.LiveSession == nil {
		return nil, access{}, errNotLive
	}
	return m, a, nil
}

// micTracks are a participant's unmuted microphone tracks.
func micTracks(p MediaParticipant) []string {
	var out []string
	for _, t := range p.Tracks {
		if t.Muted || t.Kind != "audio" || t.Source == "screen_share_audio" {
			continue
		}
		out = append(out, t.SID)
	}
	return out
}

// mute: POST /calls/meetings/{id}/mute {identity} or {all:true} → {muted}.
// "All" skips hosts, co-hosts and the caller. Hosts can't unmute anyone.
func (s *Service) mute(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req struct {
		Identity string `json:"identity"`
		All      bool   `json:"all"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Identity == "" && !req.All {
		return httpx.BadRequest("Choose who to mute.")
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	room := m.ID.String()
	people, err := s.media.ListParticipants(dctx, room)
	if err != nil {
		return s.mediaErr(ctx, err)
	}
	skip := map[string]bool{userIdentity(me.UserID): true}
	if req.All {
		hosts, err := hostIDs(ctx, s.d.Pool, m)
		if err != nil {
			return err
		}
		for _, id := range hosts {
			skip[userIdentity(id)] = true
		}
	}
	muted, found := 0, false
	for _, p := range people {
		if req.All && skip[p.Identity] || !req.All && p.Identity != req.Identity {
			continue
		}
		found = true
		for _, sid := range micTracks(p) {
			if err := s.media.MuteTrack(dctx, room, p.Identity, sid); err != nil && err != errMediaNotFound {
				return s.mediaErr(ctx, err)
			}
			muted++
		}
	}
	if !req.All && !found {
		return httpx.NotFound("That person isn't in the call.")
	}
	httpx.OK(w, map[string]int{"muted": muted})
	return nil
}

// remove: POST /calls/meetings/{id}/remove {identity}. The person can't
// rejoin this session; a guest's admission ends. Co-hosts can't remove the
// host or other co-hosts.
func (s *Service) remove(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, a, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	var req struct {
		Identity string `json:"identity"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	guest, id, ok := parseIdentity(req.Identity)
	if !ok {
		return httpx.BadRequest("Choose who to remove.")
	}
	if !guest {
		if id == me.UserID {
			return httpx.BadRequest("Leave the call instead.")
		}
		if id == m.HostID {
			return httpx.Forbidden("The host can't be removed.")
		}
		var role string
		_ = s.d.Pool.QueryRow(ctx, `SELECT role FROM calls_invitees WHERE meeting_id = $1 AND user_id = $2`, m.ID, id).Scan(&role)
		if role == RoleCohost && a.Mode != ModeHost {
			return httpx.Forbidden("Only the host can remove a co-host.")
		}
	}
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO calls_removals (session_id, identity, removed_by) VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING`, *m.LiveSession, req.Identity, me.UserID); err != nil {
			return err
		}
		if guest {
			_, err := tx.Exec(ctx, `UPDATE calls_lobby SET status = 'removed', updated_at = now() WHERE id = $1 AND meeting_id = $2`, id, m.ID)
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE calls_lobby SET status = 'removed', updated_at = now()
			WHERE meeting_id = $1 AND user_id = $2 AND status = 'admitted'`, m.ID, id)
		return err
	})
	if err != nil {
		return err
	}
	if !guest {
		s.revokeMeeting(ctx, m.ID, id)
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	if err := s.media.RemoveParticipant(dctx, m.ID.String(), req.Identity); err != nil && err != errMediaNotFound {
		return s.mediaErr(ctx, err)
	}
	httpx.NoContent(w)
	return nil
}

// end: POST /calls/meetings/{id}/end. Everyone is disconnected; the link
// keeps working for a later session.
func (s *Service) end(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	m, _, err := s.liveMeetingAsHost(r, me)
	if err != nil {
		return err
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	if err := s.media.EndRoom(dctx, m.ID.String()); err != nil {
		return s.mediaErr(ctx, err)
	}
	if err := s.closeSession(dctx, *m.LiveSession, nowUTC(), "host_ended", &me.UserID); err != nil {
		return err
	}
	s.publish(ctx, meetingTopic(m.ID), EventMeetingUpdated, map[string]any{"id": m.ID, "ended": true})
	httpx.NoContent(w)
	return nil
}

// hand: POST /calls/meetings/{id}/hand {raised} for yourself, or
// {identity, raised:false} for a host lowering someone's hand.
func (s *Service) hand(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx)
	if err := s.requireMedia(); err != nil {
		return err
	}
	m, err := s.meetingParam(r)
	if err != nil {
		return err
	}
	var req struct {
		Raised   bool   `json:"raised"`
		Identity string `json:"identity"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	a, err := s.accessFor(ctx, s.d.Pool, m, me.UserID)
	if err != nil {
		return err
	}
	if !a.canJoin() {
		return errMeetingNotFound
	}
	target := userIdentity(me.UserID)
	if req.Identity != "" && req.Identity != target {
		if !a.isHost() {
			return errNotHost
		}
		if req.Raised {
			return httpx.BadRequest("You can only lower someone else's hand.")
		}
		target = req.Identity
	}
	if m.LiveSession == nil {
		return errNotLive
	}
	if err := s.setHand(ctx, m, target, req.Raised); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// setHand sets or clears the "hand" attribute (raised-at, unix ms).
func (s *Service) setHand(ctx context.Context, m *meetingRow, identity string, raised bool) error {
	v := ""
	if raised {
		v = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	dctx, cancel := detached(ctx)
	defer cancel()
	err := s.media.UpdateParticipant(dctx, m.ID.String(), identity, "", map[string]string{"hand": v})
	if err == errMediaNotFound {
		return httpx.NotFound("That person isn't in the call.")
	}
	if err != nil {
		return s.mediaErr(ctx, err)
	}
	return nil
}

func (s *Service) mediaErr(ctx context.Context, err error) error {
	slog.ErrorContext(ctx, "calls: media server call failed", "err", err)
	return errMediaUnavailable
}

// closeSession ends a session in its own transaction, then ends the
// meeting's rings (see closeSessionTx).
func (s *Service) closeSession(ctx context.Context, sessionID uuid.UUID, at time.Time, reason string, by *uuid.UUID) error {
	var meetingID uuid.UUID
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var err error
		meetingID, err = closeSessionTx(ctx, tx, sessionID, at, reason, by)
		return err
	})
	if err != nil || meetingID == uuid.Nil {
		return err
	}
	return s.missRings(ctx, meetingID)
}
