package channels

// Mentions live in the message body as tokens the clients insert from
// autocomplete and render with current names:
//
//	<@3fa85f64-5717-4562-b3fc-2c963f66afa6>   a person
//	<@here>                                   members online right now
//	<@channel>                                every member
//	<@group:<id>|<handle>>                    a group from the app's GroupResolver
//
// Only channel members are mentioned (a mention of anyone else is plain text
// to them); @here and @channel need ActMentionAll.

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/presence"
)

var (
	userTokenRe    = regexp.MustCompile(`<@([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})>`)
	specialTokenRe = regexp.MustCompile(`<@(here|channel)>`)
	groupTokenRe   = regexp.MustCompile(`<@group:([A-Za-z0-9_.:-]{1,64})\|([A-Za-z0-9_.-]{1,64})>`)
	groupIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	handlePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// mentionedUserIDs returns the people a body's tokens name, once each, in order.
func mentionedUserIDs(body string) []uuid.UUID {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, m := range userTokenRe.FindAllStringSubmatch(body, 50) {
		if id, err := uuid.Parse(m[1]); err == nil && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// mentions is what a message mentions, resolved to channel members.
type mentions struct {
	Channel bool                 // @channel
	Users   map[uuid.UUID]string // member → user | here | group
}

func (m mentions) includes(userID uuid.UUID) bool {
	_, ok := m.Users[userID]
	return m.Channel || ok
}

var errMentionAll = httpx.NewError(403, "mention_not_allowed", "You can't use @channel or @here in this channel. Mention people by name instead.")

// resolveMentions reads the tokens in body and resolves them to members of
// the channel (never the sender, never someone with a block either way with
// the sender). It refuses @channel/@here without ActMentionAll.
func (s *Service) resolveMentions(ctx context.Context, q db.Querier, a *access, sender *uuid.UUID, body string) (mentions, error) {
	out := mentions{Users: map[uuid.UUID]string{}}
	specials := map[string]bool{}
	for _, m := range specialTokenRe.FindAllStringSubmatch(body, 10) {
		specials[m[1]] = true
	}
	userIDs := mentionedUserIDs(body)
	groupTokens := groupTokenRe.FindAllStringSubmatch(body, 10)
	if len(specials) == 0 && len(userIDs) == 0 && len(groupTokens) == 0 {
		return out, nil
	}
	if (specials["here"] || specials["channel"]) && sender != nil {
		ok, err := s.can(ctx, q, a, ActMentionAll)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, errMentionAll
		}
	}
	rows, err := q.Query(ctx, `SELECT user_id FROM channel_members WHERE channel_id = $1`, a.ChannelID)
	if err != nil {
		return out, err
	}
	memberList, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return out, err
	}
	members := uuidSet(memberList)
	var blocked map[uuid.UUID]bool
	if sender != nil {
		ids, err := moderation.BlockedIDs(ctx, q, *sender)
		if err != nil {
			return out, err
		}
		blocked = uuidSet(ids)
		delete(members, *sender)
	}
	for id := range blocked {
		delete(members, id)
	}
	out.Channel = specials["channel"]
	if specials["here"] && !out.Channel {
		viewer := a.UserID
		if sender != nil {
			viewer = *sender
		}
		ids := make([]uuid.UUID, 0, len(members))
		for id := range members {
			ids = append(ids, id)
		}
		online, err := presence.Lookup(ctx, q, viewer, ids)
		if err != nil {
			return out, err
		}
		for id, p := range online {
			if p.Status == presence.StatusOnline {
				out.Users[id] = "here"
			}
		}
	}
	if res := currentGroupResolver(); res != nil {
		for _, g := range groupTokens {
			ids, err := res.GroupMembers(ctx, a.OrgID, g[1])
			if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, httpx.ErrNotFound) {
				continue // an unknown group is plain text
			}
			if err != nil {
				return out, err
			}
			for _, id := range ids {
				if members[id] {
					out.Users[id] = "group"
				}
			}
		}
	}
	for _, id := range userIDs {
		if members[id] {
			out.Users[id] = "user"
		}
	}
	return out, nil
}

// saveMentions records resolved mentions for a message (in the caller's tx).
func saveMentions(ctx context.Context, tx pgx.Tx, msgID uuid.UUID, m mentions) error {
	if _, err := tx.Exec(ctx, `DELETE FROM channel_mentions WHERE message_id = $1`, msgID); err != nil {
		return err
	}
	if len(m.Users) > 0 {
		ids, kinds := make([]uuid.UUID, 0, len(m.Users)), make([]string, 0, len(m.Users))
		for id, k := range m.Users {
			ids, kinds = append(ids, id), append(kinds, k)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO channel_mentions (message_id, user_id, kind) SELECT $1, u, k FROM unnest($2::uuid[], $3::text[]) AS t(u, k)
			ON CONFLICT DO NOTHING`, msgID, ids, kinds); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE channel_messages SET mention_channel = $2 WHERE id = $1`, msgID, m.Channel)
	return err
}
