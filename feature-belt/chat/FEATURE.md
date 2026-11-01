# Chat

Direct and group messaging with live delivery, on web and mobile.

## What it does

- Direct conversations (one per pair) and groups with a name, photo and owner/admin/member roles (up to `CHAT_GROUP_MAX_MEMBERS`); add, remove, leave, hand over ownership.
- Text, photos, video, audio and documents (private; signed links only members get), replies, any-emoji reactions, edits within `CHAT_EDIT_WINDOW` ("Edited"), delete for everyone ("Message deleted").
- Live delivery, typing indicator, read state ("Read" in a group means everyone else read it), unread counts per conversation and on the nav/tab badge, search across your conversations, jump to a message.
- Sends show at once and are idempotent: each carries a client id, so a retry never doubles a message; a failed send offers Retry or Delete.
- Mute (1 hour, 8 hours, 1 week, until turned back on), archive (back with the next message), delete a direct conversation for yourself, leave a group.
- Report messages and people, block (no direct messages across a block; a blocked person's messages are hidden in groups, live ones too). Staff open any conversation from a report, audited, and hide messages.

## Data

| Table | Purpose |
|---|---|
| `chat_conversations` | direct (ordered `user_low`/`user_high`, unique per pair) or group (`title`, `avatar_key`); `last_message_at` orders the inbox |
| `chat_members` | role, read pointer (`last_read_message_id` + `last_read_at`), `muted_until`, `archived_at`, `cleared_at` (delete for me), `left_at` |
| `chat_messages` | text or system event (`body` = event code, `subject_user_id`); `reply_to_id`, `client_id` (unique per sender), `edited_at`, `deleted_at` + `deleted_by` (sender, staff, account_deleted) |
| `chat_attachments` | storage key (purpose `chat_attachment`), kind, type, name, size, dimensions |
| `chat_reactions` | one row per message, person and emoji |
| `chat_deleted_objects` | keys of erased files; a worker deletes the object and stamps `deleted_at` (the key can't be reused) |

`migrations/1000_chat_init.sql` is the schema. It runs `CREATE EXTENSION IF NOT EXISTS pg_trgm` (trusted since Postgres 13).

## API

Under `/v1`, signed in; writes others see need an active account.

| Method | Path | Purpose |
|---|---|---|
| GET | `/chat/conversations?archived=&cursor=` | inbox, keyset `(last_message_at, id)` |
| POST | `/chat/conversations` | `{kind:"direct", user_id}` (200 existing / 201 new) or `{kind:"group", title, member_ids, avatar_key?}` |
| GET / PATCH / DELETE | `/chat/conversations/{id}` | detail with members and `hidden_user_ids` / group name and photo (owner, admin) / leave or delete for me |
| POST | `/chat/conversations/{id}/members` | `{user_ids}` (owner, admin) |
| PATCH / DELETE | `/chat/conversations/{id}/members/{user_id}` | `{role}` incl. `owner` transfer (owner) / remove, or leave when self |
| GET / POST | `/chat/conversations/{id}/messages` | `?before=\|after=\|around=<id>` → `{items, has_older, has_newer}` / send `{client_id, body, reply_to_id?, attachments?}` |
| POST | `/chat/conversations/{id}/read` · `/typing` | read pointer forward · typing signal (no write) |
| PUT/DELETE · POST/DELETE | `/chat/conversations/{id}/mute` · `/archive` | `{until}` or `{forever:true}` · archive |
| PATCH / DELETE | `/chat/messages/{id}` | edit (sender, window) / delete for everyone |
| POST / DELETE | `/chat/messages/{id}/reactions` | `{emoji}` / `?emoji=` |
| GET | `/chat/attachments/{id}` · `/chat/unread` · `/chat/search?q=` · `/chat/people?q=` | fresh signed link · totals · search · people picker |
| GET / DELETE | `/admin/chat/conversations/{id}?around=` · `/admin/chat/messages/{id}` | staff view (audited `chat.conversation.view`) · hide `{reason}` (audited `chat.message.remove`) |

Rate limits per person: messages `CHAT_MESSAGES_PER_MINUTE`, new conversations `CHAT_NEW_CONVERSATIONS_PER_DAY`, typing 40/min, reactions and deletes 60/min, edits and creates 30/min.

Realtime: `chat:<conversation id>` (members; revoked on removal, leave and account deletion) carries `chat.message`, `chat.message_updated`, `chat.read`, `chat.typing`, `chat.member`; each member's user topic gets `chat.inbox {conversation_id, reason, message?}`. Topic events aren't filtered per viewer: the shared handlers drop events from `hidden_user_ids`. Removals from the moderation queue publish nothing; clients see them on the next fetch.

Notifications: kind `chat.message` ("Messages", push on) for text messages (not muted, not blocked, not your own) and "<name> added you". Direct: title = sender, body = first 80 characters or "Sent a photo". Group: title = group, body = "<name>: …". The kind names the conversation (`AgentTarget`) for the inbox's agent scope.

Agent tools (read-only, as the caller): `chat_list_conversations`, `chat_search_messages`. Both leave out conversations the caller's agent scope hides (`chat.conversation` items set from the agents feature's ⋮ menu, else the chat level), like search.

Uploads: `POST /v1/uploads` purpose `chat_attachment` (private: photos, video, audio, PDF, txt, csv, zip, docx, xlsx, pptx; `CHAT_ATTACHMENT_MAX_MB`) or `chat_avatar` (public images, 5 MB).

## Screens

- Shared (`packages/shared/src/chat`): DTOs, `chatEndpoints`, hooks (`useChatInbox`, `useChatMessages` both directions, `useSendMessage` optimistic by `client_id`, edit/delete/react optimistic, `useMarkRead`, `useTypingSignal`/`useChatTypingUsers`, members and settings mutations, `useChatUnreadCount`, staff hooks), realtime cache handlers, copy helpers (`messagePreview`, `systemEventText`, times).
- Web: `/chat` split view ≥ 1024 px (inbox + thread), stacked below (thread at `/chat/<id>` with Back). Inbox/Archived, message search, new message or group (people picker), thread with day separators, grouped bubbles, quotes, files and lightbox, reactions, hover/focus actions (react, reply, edit, delete, report), Read/Sent, typing, jump to `?message=` with highlight, details sheet (photo, name, notifications, members and roles, add people, report/block, leave). Admin → Chat: open a conversation from an id or report link; staff view with hidden text and Remove. Nav item and Home tile with the unread count.
- Mobile: Chat tab with the unread badge (inbox, search, new); thread screen (inverted list, long-press actions sheet, quick reactions, composer above the keyboard, photos from the system picker); details screen; new message/group and add-people modal; search.
- Every view has skeleton, empty and error states.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `CHAT_MESSAGE_MAX_CHARS` | 4000 | longest message |
| `CHAT_GROUP_MAX_MEMBERS` | 50 | group size, owner included |
| `CHAT_EDIT_WINDOW` | 5m | edit window; 0 turns editing off (the UI offers Edit for 5 minutes) |
| `CHAT_NEW_CONVERSATIONS_PER_DAY` | 20 | new conversations per person per 24 hours |
| `CHAT_MESSAGES_PER_MINUTE` | 60 | messages per person per minute |
| `CHAT_ATTACHMENT_MAX_MB` | 25 | largest attachment (mirror `CHAT_ATTACHMENT_MAX_BYTES` in shared if changed) |

Invalid values fail the boot (`config.Problem`). No new Go or npm dependencies; requires `notifications` and `moderation`.

## Compliance

- Account deletion: the person's messages become tombstones (text and attachments erased; files go with their storage prefix), reactions removed, owned groups pass to the longest-serving admin or member, memberships removed, empty conversations deleted, live subscriptions revoked after commit. Direct partners see "Deleted user".
- Export: conversations (metadata), every message the person sent with 24-hour links to their files, their reactions.
- UGC: `moderation.Check` on messages, edits and group names; report from every message and person; block from direct conversations and member lists. Staff removal hides (members see "Message deleted") and keeps text and files for the moderation record until the sender's account is deleted; dismissing an auto-hidden report restores the message.
- Privacy policy: message text appears in push notifications. Mobile uses the system photo picker (no library permission). Offering the camera is a small stitch: the template's camera purpose string already covers photos the user chooses to share.

## Stitching

1. Decide who may message whom: `chat.SetContactPolicy(func(ctx, q, from, to) error)` (default: any active user, not blocked either way); the people picker searches names and exact emails, so replace `/chat/people` if your app has its own directory.
2. Decide whether people added to a group see earlier history (default yes).
3. Link to `POST /v1/chat/conversations {kind:"direct", user_id}` from profiles and other features, then open `/chat/<id>`.
4. Add the push-preview line to the privacy policy.
5. If you change `CHAT_ATTACHMENT_MAX_MB` or `CHAT_EDIT_WINDOW`, update `CHAT_ATTACHMENT_MAX_BYTES` and `EDIT_WINDOW_MS` in the clients.

## Extending

- `chat.SetContactPolicy` for messaging rules; `refreshChatAfterBlock(queryClient)` after any block UI outside chat.
- System events are codes (`created`, `member_added`, …) worded client-side by `systemEventText` with current names.
- Don't change: `client_id` uniqueness, `(created_at, id)` ordering, membership-scoped id checks, hide-vs-erase split.

## Provenance

Distilled from SFLuv chat v2 (`/Users/coding/SFLUV/app/backend`: migrations 1.67–1.76, `db/app_messages.go`, `db/app_chat_group.go`; mobile screens), with ASSEMBLY's reply-scope fix and DOGPATCH/THASSA message and typing patterns. Fixed: inbox paginated (SFLuv capped at 200), idempotent sends (SFLuv could double-send), reply targets rejected outside the conversation (ASSEMBLY leak), per-message rate limit, uuid keys, S3 instead of BYTEA, account deletion that removes chat data, moderation queue instead of the alerts-inbox email, a web client (SFLuv was mobile-only). Stripped: bulletin naming, payments, transaction/merchant/event attachments, contacts, wallet-tied push, beta gating.

Verified: backend HTTP and realtime flows, web flows with two users in Playwright (375/1440, light/dark), `pnpm typecheck`, `pnpm check`, web build, iOS export. Not verified on a device: mobile keyboard behaviour, long-press, inverted-list paging, photo picker.

## Changes

- 1.0.6: attachment links carry the file's name (`inline; filename=…`), so a download saves as "Minutes.pdf", not the storage key.
- 1.0.5: Post-commit side effects run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them and a panic is logged instead of crashing the process: notification fan-out (`chat.notify`) and the stored-file clean-up after a delete (`chat.delete_objects`). Search hits are titled with the sender and set `Context` ("Chat · <conversation>"; template `app.SearchResult.Context`).
- 1.0.4: the agent tools apply the caller's agent scope (a `chat.conversation` set to Off was listed with its last message and searchable before); search checks a page in one query; the notification kind carries `AgentTarget` (needs notifications ≥ 1.1.1).
- 1.0.3: Global search: `app.SearchProvider` (kind `chat.message`) over text messages in the caller's own conversations, the rules of `GET /chat/search`; agent calls also need the conversation readable at the caller's agent scope.
- 1.0.2: shared hook renamed to `useRemoveChatMember` (the generic name collided with another feature's export).
- 1.0.1: camera note updated for the template's generic purpose string.
- 1.0.0: web and mobile clients, shared hooks; document attachments; post-commit revocation on account deletion; `hidden_user_ids` for client-side block filtering.
