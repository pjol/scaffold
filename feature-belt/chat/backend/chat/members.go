package chat

// Group membership rules, enforced here and nowhere else:
//   - Exactly one owner. The owner can't be removed or demoted, and leaves only
//     after handing the group over (or as its last member, which deletes it).
//   - The owner promotes members to admin, demotes admins, and transfers
//     ownership (the old owner becomes an admin in the same transaction).
//   - The owner and admins add and remove members and change the name and
//     picture. An admin can't remove another admin.
//   - Leaving or being removed stamps left_at; the row keeps the person's
//     messages attributed, and being added again restores the history.
//   - Nobody can add a person they have a block with, either way.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/realtime"
)

// MemberEvent is published as chat.member.
type MemberEvent struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	Action         string    `json:"action"` // added | removed | left | role
	UserID         uuid.UUID `json:"user_id"`
	Role           string    `json:"role"`
	ActorID        uuid.UUID `json:"actor_id"`
}

var errNotInGroup = httpx.NotFound("That person isn't in this group.")

type addMembersReq struct {
	UserIDs []uuid.UUID `json:"user_ids"`
}

// POST /chat/conversations/{id}/members — owner or admin.
func (s *Service) addMembers(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req addMembersReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ids := dedupe(req.UserIDs, me)
	if len(ids) == 0 {
		return httpx.BadRequest("Choose at least 1 person.")
	}
	var head convHead
	var events, added []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		mb, err := loadMember(r.Context(), tx, convID, me, true)
		if err != nil {
			return err
		}
		if mb.Kind != kindGroup {
			return errNotGroup
		}
		if !mb.canManage() {
			return errNotGroupAdmin
		}
		head = mb.head()
		var count int
		if err := tx.QueryRow(r.Context(), `
			SELECT count(*) FROM chat_members WHERE conversation_id = $1 AND left_at IS NULL`, convID).Scan(&count); err != nil {
			return err
		}
		for _, uid := range ids {
			var active bool
			if err := tx.QueryRow(r.Context(), `
				SELECT EXISTS (SELECT 1 FROM chat_members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL)`,
				convID, uid).Scan(&active); err != nil {
				return err
			}
			if active {
				continue
			}
			if err := checkReachable(r.Context(), tx, me, uid); err != nil {
				return err
			}
			if count+1 > s.cfg.groupMax {
				return httpx.BadRequest(fmt.Sprintf("Groups can have up to %d members.", s.cfg.groupMax))
			}
			// New and returning members start with everything so far read.
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO chat_members (conversation_id, user_id, role, added_by, last_read_message_id, last_read_at)
				SELECT $1, $2, 'member', $3, lm.id, lm.created_at
				FROM (SELECT 1) one
				LEFT JOIN LATERAL (
				    SELECT id, created_at FROM chat_messages WHERE conversation_id = $1
				    ORDER BY created_at DESC, id DESC LIMIT 1
				) lm ON true
				ON CONFLICT (conversation_id, user_id) DO UPDATE SET
				  left_at = NULL, archived_at = NULL, role = 'member', added_by = EXCLUDED.added_by,
				  joined_at = now(), last_read_message_id = EXCLUDED.last_read_message_id,
				  last_read_at = EXCLUDED.last_read_at, updated_at = now()`, convID, uid, me); err != nil {
				return err
			}
			count++
			ev, _, err := insertSystem(r.Context(), tx, convID, me, "member_added", &uid)
			if err != nil {
				return err
			}
			events, added = append(events, ev), append(added, uid)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.announce(r.Context(), head, events...)
	for _, uid := range added {
		s.publish(r.Context(), convID, EventMember, MemberEvent{ConversationID: convID, Action: "added", UserID: uid, Role: roleMember, ActorID: me})
		s.inboxEvent(r.Context(), uid, convID, "added", nil)
	}
	d, err := s.detail(r.Context(), me, convID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// DELETE /chat/conversations/{id}/members/{user_id} — remove someone, or
// leave when user_id is the caller.
func (s *Service) removeMember(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	target, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	if err := s.removeFromGroup(r.Context(), me, convID, target); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

// removeFromGroup removes target from a group on actor's behalf (actor ==
// target is leaving). The last member leaving deletes the group.
func (s *Service) removeFromGroup(ctx context.Context, actor, convID, target uuid.UUID) error {
	var head convHead
	var ev uuid.UUID
	deleted := false
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		mb, err := loadMember(ctx, tx, convID, actor, true)
		if err != nil {
			return err
		}
		if mb.Kind != kindGroup {
			return errNotGroup
		}
		head = mb.head()
		var targetRole string
		err = tx.QueryRow(ctx, `
			SELECT role FROM chat_members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL`,
			convID, target).Scan(&targetRole)
		if db.IsNoRows(err) {
			return errNotInGroup
		}
		if err != nil {
			return err
		}
		if actor == target {
			if targetRole == roleOwner {
				var others int
				if err := tx.QueryRow(ctx, `
					SELECT count(*) FROM chat_members WHERE conversation_id = $1 AND user_id <> $2 AND left_at IS NULL`,
					convID, actor).Scan(&others); err != nil {
					return err
				}
				if others > 0 {
					return httpx.Conflict("Make someone else the owner before you leave.")
				}
				deleted = true
				return deleteConversations(ctx, tx, []uuid.UUID{convID})
			}
		} else {
			switch {
			case mb.Role == roleOwner:
			case mb.Role == roleAdmin && targetRole == roleMember:
			case mb.Role == roleAdmin:
				return httpx.Forbidden("Admins can only remove members.")
			default:
				return errNotGroupAdmin
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE chat_members SET left_at = now(), role = 'member', updated_at = now()
			WHERE conversation_id = $1 AND user_id = $2`, convID, target); err != nil {
			return err
		}
		event := "member_removed"
		if actor == target {
			event = "member_left"
		}
		ev, _, err = insertSystem(ctx, tx, convID, actor, event, &target)
		return err
	})
	if err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	if err := realtime.Revoke(ctx, s.d.Broker, target, realtime.Topic(topicKind, convID)); err != nil {
		slog.WarnContext(ctx, "chat: revoke topic", "conversation_id", convID, "err", err)
	}
	s.inboxEvent(ctx, target, convID, "removed", nil)
	if deleted {
		s.kickObjectCleanup(ctx)
		return nil
	}
	action := "removed"
	if actor == target {
		action = "left"
	}
	s.publish(ctx, convID, EventMember, MemberEvent{ConversationID: convID, Action: action, UserID: target, Role: roleMember, ActorID: actor})
	s.announce(ctx, head, ev)
	return nil
}

type roleReq struct {
	Role string `json:"role"` // owner (transfer) | admin | member
}

// PATCH /chat/conversations/{id}/members/{user_id} — owner only.
func (s *Service) setMemberRole(w http.ResponseWriter, r *http.Request) error {
	me := auth.MustFromContext(r.Context()).UserID
	convID, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	target, err := httpx.URLParamUUID(r, "user_id")
	if err != nil {
		return err
	}
	var req roleReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	switch req.Role {
	case roleOwner, roleAdmin, roleMember:
	default:
		return httpx.BadRequest("Role must be owner, admin or member.")
	}
	var head convHead
	var events []uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		mb, err := loadMember(r.Context(), tx, convID, me, true)
		if err != nil {
			return err
		}
		if mb.Kind != kindGroup {
			return errNotGroup
		}
		if mb.Role != roleOwner {
			return httpx.Forbidden("Only the group's owner can change roles.")
		}
		if target == me {
			return httpx.BadRequest("Make someone else the owner instead.")
		}
		head = mb.head()
		var current string
		err = tx.QueryRow(r.Context(), `
			SELECT role FROM chat_members WHERE conversation_id = $1 AND user_id = $2 AND left_at IS NULL`,
			convID, target).Scan(&current)
		if db.IsNoRows(err) {
			return errNotInGroup
		}
		if err != nil || current == req.Role {
			return err
		}
		if req.Role == roleOwner {
			if _, err := tx.Exec(r.Context(), `
				UPDATE chat_members SET role = 'admin', updated_at = now() WHERE conversation_id = $1 AND user_id = $2`,
				convID, me); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE chat_members SET role = $3, updated_at = now() WHERE conversation_id = $1 AND user_id = $2`,
			convID, target, req.Role); err != nil {
			return err
		}
		if req.Role == roleOwner {
			ev, _, err := insertSystem(r.Context(), tx, convID, me, "owner_changed", &target)
			if err != nil {
				return err
			}
			events = append(events, ev)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(r.Context(), convID, EventMember, MemberEvent{ConversationID: convID, Action: "role", UserID: target, Role: req.Role, ActorID: me})
	if req.Role == roleOwner {
		s.publish(r.Context(), convID, EventMember, MemberEvent{ConversationID: convID, Action: "role", UserID: me, Role: roleAdmin, ActorID: me})
	}
	s.announce(r.Context(), head, events...)
	d, err := s.detail(r.Context(), me, convID)
	if err != nil {
		return err
	}
	httpx.OK(w, d)
	return nil
}

// deleteConversations deletes conversations outright (rows cascade) and
// queues their stored files.
func deleteConversations(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_deleted_objects (storage_key)
		SELECT a.storage_key FROM chat_attachments a JOIN chat_messages m ON m.id = a.message_id
		WHERE m.conversation_id = ANY($1)
		UNION
		SELECT avatar_key FROM chat_conversations WHERE id = ANY($1) AND avatar_key IS NOT NULL
		ON CONFLICT (storage_key) DO NOTHING`, ids); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM chat_conversations WHERE id = ANY($1)`, ids)
	return err
}
