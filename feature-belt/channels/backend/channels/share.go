package channels

// Forwarding a message, and turning a group DM into a private channel.
//
// A forward is a new message pointing at the original; each viewer loads the
// original through GET /channels/messages/{id}, so nobody sees it unless they
// may read it. Forwarding is allowed only where that holds for the target's
// members at the time: from a public channel to any conversation in the same
// org the sender can post in; from a private channel or DM only into a
// conversation whose members are all members of the source.

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/orgs"
)

type forwardReq struct {
	ChannelID    uuid.UUID  `json:"channel_id"`
	ThreadRootID *uuid.UUID `json:"thread_root_id"`
	Comment      string     `json:"comment"`
	ClientID     string     `json:"client_id"`
}

var errForwardNotAllowed = httpx.NewError(http.StatusForbidden, "forward_not_allowed",
	"Messages from private conversations can only go where everyone is already in that conversation.")

// POST /channels/messages/{id}/forward {channel_id, comment?, thread_root_id?} — 201 with the new message.
func (s *Service) forward(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req forwardReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	original, err := s.forwardable(ctx, me, id, req.ChannelID)
	if err != nil {
		return err
	}
	msg, created, err := s.post(ctx, me, req.ChannelID, sendReq{ClientID: req.ClientID, Body: req.Comment, ThreadRootID: req.ThreadRootID, forwardOf: &original})
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.JSON(w, status, msg)
	return nil
}

// forwardable checks me may forward message id into target and returns the
// message the forward shows (a forward of a forward shows the original).
func (s *Service) forwardable(ctx context.Context, me, id, target uuid.UUID) (uuid.UUID, error) {
	src, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return uuid.Nil, err
	}
	if src.isForward {
		var orig *uuid.UUID
		if err := s.d.Pool.QueryRow(ctx, `SELECT forward_of FROM channel_messages WHERE id = $1`, id).Scan(&orig); err != nil {
			return uuid.Nil, err
		}
		if orig == nil {
			return uuid.Nil, httpx.Conflict("The original message is gone.")
		}
		if src, err = s.loadMessage(ctx, s.d.Pool, *orig, me); err != nil {
			return uuid.Nil, httpx.Conflict("You can't open the original message.")
		}
	}
	if src.Kind == msgSystem || src.Deleted {
		return uuid.Nil, httpx.Conflict("This message can't be forwarded.")
	}
	if src.SenderID != nil {
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, me, *src.SenderID); err != nil || blocked {
			if err == nil {
				err = errMessageNotFound
			}
			return uuid.Nil, err
		}
	}
	dst, err := s.loadAccess(ctx, s.d.Pool, target, me)
	if err != nil {
		return uuid.Nil, err
	}
	if dst.OrgID != src.OrgID {
		return uuid.Nil, errChannelNotFound
	}
	if err := dst.requireWritable(); err != nil {
		return uuid.Nil, err
	}
	if src.access.Kind != kindPublic && src.ChannelID != target {
		var outsider bool
		if err := s.d.Pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM channel_members t WHERE t.channel_id = $1
			               AND NOT EXISTS (SELECT 1 FROM channel_members x WHERE x.channel_id = $2 AND x.user_id = t.user_id))`,
			target, src.ChannelID).Scan(&outsider); err != nil {
			return uuid.Nil, err
		}
		if outsider || dst.Kind == kindPublic {
			return uuid.Nil, errForwardNotAllowed
		}
	}
	return src.MessageID, nil
}

// ---- group DM → private channel --------------------------------------------------------

type convertReq struct {
	Name string `json:"name"`
}

// POST /channels/channels/{id}/convert {name} — a group DM becomes a private
// channel with the same people and history; the caller becomes its admin.
func (s *Service) convertGroupDM(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req convertReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	if err := checkText(ctx, name); err != nil {
		return err
	}
	a, err := s.loadAccess(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if err := a.requireWritable(); err != nil {
		return err
	}
	if a.Kind != kindGroupDM {
		return httpx.BadRequest("Only group messages can become channels.")
	}
	ok, err := allowed(ctx, s.d.Pool, PermissionRequest{Action: ActCreatePrivate, OrgID: a.OrgID, UserID: me, OrgRole: a.OrgRole})
	if err != nil {
		return err
	}
	if !ok {
		return httpx.Forbidden("You can't create private channels in this " + orgs.Noun() + ".")
	}
	var ev uuid.UUID
	err = db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE channels SET kind = 'private', name = $2, dm_key = NULL, updated_at = now() WHERE id = $1 AND kind = 'group_dm'`, id, name)
		if db.IsUniqueViolation(err, "channels_name_uidx") {
			return httpx.Conflict("#" + name + " is taken. Choose another name.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_members SET role = 'admin', hidden_at = NULL, updated_at = now() WHERE channel_id = $1 AND user_id = $2`, id, me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE channel_members SET hidden_at = NULL, updated_at = now() WHERE channel_id = $1`, id); err != nil {
			return err
		}
		ev, err = insertSystem(ctx, tx, id, me, "converted", nil)
		return err
	})
	if err != nil {
		return err
	}
	s.announce(ctx, id, ev)
	s.publish(ctx, id, EventChannel, ChannelEvent{ChannelID: id, Reason: "updated"})
	s.sidebarAll(ctx, id, "updated")
	return s.respondDetail(w, r, me, id)
}
