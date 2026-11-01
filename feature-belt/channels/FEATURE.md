# Channels

A Slack-style workspace per organization: channels, forums, direct messages and threads, on web and mobile.

## What it does

- Each org gets a workspace: public channels anyone in the org can browse and join, private channels by invitation, direct and group messages (up to 9 people, or a note to yourself). `CHANNELS_DEFAULT_CHANNELS` (#general) are created on the first visit and every member joins them once (leaving sticks).
- Chat channels are a running timeline; forum channels list titled posts (with the forum's tags) by latest activity, each with its own thread. Threads everywhere: reply counts and repliers on the post, "also send to channel", a Threads view with unread replies, follow/unfollow.
- Mentions: @person, @here (members online now), @channel (permission-gated), @group (when the app wires groups). The sidebar bolds unread channels and badges mentions (every DM message counts as one), like Slack.
- Messages: a safe markdown subset (bold, italic, strike, code, code blocks, quotes, lists, links, emoji shortcodes) rendered without HTML; files (private, signed links); edits ("edited"), deletes, reactions, pins, Later (saved items, open/done), message links, jump to message, typing, presence dots and status in headers and member lists.
- Per-channel notifications (default, all, mentions, nothing) and mute; Do Not Disturb holds pushes app-wide. Search with `in:#channel`, `from:@name`, `from:me`, `has:file`, `is:thread`.
- Org roles decide who creates, archives, edits and uses @channel (`DefaultPermission`, replaceable). Report and block like chat; staff open a channel from a report (audited) and hide messages.

## Data

| Table | Purpose |
|---|---|
| `channels` | org, kind (public, private, dm, group_dm), layout (chat, forum), unique `name`, topic, description, `forum_tags`, `is_default`, `dm_key` (one DM per set of people per org), archived; cascades with the org |
| `channel_members` | role (admin = manages the channel), read pointer, `notify`, `muted`, `starred_at`, `hidden_at` (closed DM) |
| `channel_autojoins` | default channels each person was joined to once |
| `channel_messages` | text, system event or bot message; `thread_root_id` + `also_in_channel`; forum `title`/`tags`; `mention_channel`; root counters (`reply_count`, `last_reply_at`, `activity_at`); pins; `client_id` (idempotent sends); tombstones (`deleted_by` sender, moderator, staff, account_deleted) |
| `channel_mentions` | resolved mentions per member (user, here, group) for counts and notifications |
| `channel_reactions`, `channel_attachments`, `channel_saved`, `channel_thread_follows` | reactions, files, Later, followed threads with their read pointer |
| `channel_deleted_objects` | storage keys of deleted attachments (a trigger queues every delete, cascades included); a worker removes the objects |

`migrations/2200_channels_init.sql` is the schema (`pg_trgm` for search).

## API

Under `/v1`, signed in. Org routes use `orgs.Require("member")`; every channel and message route loads the caller's access first (org member, and channel member unless public) and answers 404 otherwise. Writes others see need an active account.

| Method | Path | Purpose |
|---|---|---|
| GET | `/channels/config` · `/channels/unread` | limits and wired integrations · badge totals per org |
| GET | `/channels/orgs/{orgID}/workspace` | sidebar, permissions, Threads and Later counts (joins default channels) |
| GET | `/channels/orgs/{orgID}/browse` · `people` · `groups` · `threads` · `later` · `search` | channel browser, member picker and @ autocomplete, @groups, Threads view, Later, message search |
| POST | `/channels/orgs/{orgID}/channels` · `/dms` | create a channel (20/h) · open a DM or group DM (200 existing / 201 new) |
| GET / PATCH | `/channels/channels/{id}` | detail with `permissions`, `hidden_user_ids` / name, topic (any member), description, tags, make private, default |
| POST / DELETE | `/channels/channels/{id}/archive` · `/join` · `/membership` | archive, unarchive · join a public channel · leave or close a DM |
| GET / POST / DELETE | `/channels/channels/{id}/members[/{userID}]` | members by name / add / remove |
| PUT · POST · POST | `…/prefs` · `…/read` · `…/typing` | notify, muted, starred · read pointer forward · typing |
| GET / POST | `/channels/channels/{id}/messages` | timeline `?before=\|after=\|around=` / send `{client_id, body, thread_root_id?, also_send_to_channel?, title?, tags?, attachments?}` |
| GET | `…/topics?tag=` · `…/pins` · `…/files` | forum posts by activity · pinned · files |
| POST | `/channels/channels/{id}/call` | `{path}` from the app's CallStarter (404 `calls_unavailable` when unwired) |
| GET | `/channels/messages/{id}` · `…/thread` | message and its channel (links) · root + replies, `following`, `last_read` |
| PATCH / DELETE | `/channels/messages/{id}` | edit (sender) / delete (sender, or a channel or org admin) |
| POST / DELETE / PUT | `…/reactions` · `…/pin` · `…/later` · `…/follow` · `…/thread/read` | react, pin, save for later `{done}`, follow, thread read pointer |
| GET | `/channels/attachments/{id}` | fresh signed link |
| GET / DELETE | `/admin/channels/channels/{id}?around=&thread=` · `/admin/channels/messages/{id}` | staff view (audited `channels.channel.view`) · hide `{reason}` (audited `channels.message.remove`) |

Rate limits per person: messages `CHANNELS_MESSAGES_PER_MINUTE`, typing 40/min, reactions and deletes 60/min, edits and settings 30/min, calls 10/min, search 60/min.

Realtime: `channels:<id>` (members; org members for public channels) carries `channels.message`, `channels.message_updated` (thread roots' counts too), `channels.typing`, `channels.channel`; each member's user topic gets `channels.sidebar {org_id, channel_id, reason, message?, mention}`, followers get `channels.thread`. Removal from a private channel revokes the topic. People who leave the org (removed, left, org deleted, account deleted) lose REST access at once and, right after the commit (`orgs.OnMemberRemoved`), their memberships, follows and live subscriptions: every channel they were in and the org's public channels (newest 2,000). `channels.prune_departed` (every 15 minutes) is the backstop for a hook that never ran.

Notifications (registered kinds): `channels.mention` (mentions and DMs), `channels.message` (channels set to all), `channels.thread` (followed threads). Mute and "nothing" send none. Channels registers the `presence.dnd` delivery filter: Do Not Disturb holds push and email app-wide, the inbox still gets them.

Agent tools (as the caller, same access checks, narrowed by the user's agent scope): `channels_list_mine`, `channels_read_history`, `channels_read_thread`, `channels_search_messages`, and `channels_post_message` (write; `Target` = the channel, `Preview` = "Post in #name" with the text). `app.SearchProvider` returns channels and messages under the same checks. Its notification kinds name the channel (`AgentTarget`), so an agent reading the inbox skips mentions and messages from channels it may not read.

## Screens

- Shared (`packages/shared/src/channels`): `channelsEndpoints`, hooks (`useChannelsWorkspace`, `useChannelsMessages` and `useChannelsThread` in both directions, `useChannelsTopics`, `useChannelsSend` optimistic by `client_id`, optimistic edit/delete/react/pin/save, `useChannelsMarkRead`, typing, prefs, members, DMs, Threads, Later, search, staff), realtime cache handlers, `parseChannelsMarkdown` with mention encode/decode, emoji shortcodes, copy helpers.
- Web: `/channels` (nav entry, Home tile, mention badge): sidebar (Threads, Later, Browse, starred, channels, direct messages; bold unread, mention badges; new channel and new message dialogs; ⋮ section menu), channel view (header with topic, members, Start call when wired, ⋮ `ResourceMenu`), timeline with day separators and a "New" divider, composer (formatting toolbar, @ and : autocomplete, files, Enter / Shift+Enter, ↑ edits your last message), thread pane on the right (over the channel below 1280 px), forum list with tag filters and New post, details sheet (about and settings, members with presence and status, pinned, files), Threads, Later, Search, Browse; stacks with back buttons below 1024 px. Admin → Channels: open from an id or a report link; staff view with Remove.
- Mobile: Channels tab (badge) with sections; channel screen (inverted list, long-press actions, join bar), thread screen, forum list and new post, composer with photos and @ / : suggestions, details, new DM, search, browse, Threads, Later.
- Every view has skeleton, empty and error states.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `CHANNELS_MESSAGE_MAX_CHARS` | 8000 | longest message |
| `CHANNELS_EDIT_WINDOW` | 0 | edit window after sending; 0 = no limit |
| `CHANNELS_MESSAGES_PER_MINUTE` | 60 | messages per person per minute (agent posts included) |
| `CHANNELS_ATTACHMENT_MAX_MB` | 50 | largest file (clients read it from the workspace config) |
| `CHANNELS_DEFAULT_CHANNELS` | general | channels a new workspace starts with; everyone joins them once |
| `CHANNELS_MENTION_ALL_MAX_MEMBERS` | 50 | above this (and in default channels) @channel/@here is for owners and admins |

Invalid values fail the boot (`config.Problem`). Requires orgs, presence, notifications (≥ 1.1.0, for the delivery filter) and moderation; no new dependencies.

## Compliance

- Deletion: the person's messages become tombstones (text, titles, files gone); their reactions, mentions, saved items, follows and channel memberships go; DM memberships stay so the others see "Deleted user"; DMs left without anyone are deleted. Export: memberships and preferences, sent messages with 24-hour file links, reactions, saved items.
- UGC: `moderation.Check` on messages, titles, names, topics, descriptions and tags; report from every message; blocks hide messages both ways and refuse DMs. Staff removal hides and keeps the content for the record; dismissing an auto-hidden report restores it.
- Privacy policy: message text appears in push notifications.

## Stitching

1. Permissions: `channels.SetPermissionCheck(func(ctx, q, req channels.PermissionRequest) (bool, error))` to use the access feature, falling back to `channels.DefaultPermission(req)`.
2. Groups: `channels.SetGroupResolver(r)` (`SearchGroups`, `GroupMembers`), e.g. access teams.
3. Calls: `channels.SetCallStarter(func(ctx, caller, target channels.CallTarget) (path string, err error))`.
4. Announcements: `channels.PostBotMessage(ctx, channelID, channels.BotMessage{BotName, Text, ThreadRootID})`; `channels.DefaultChannel(ctx, orgID)` finds #general.
5. Presence: `presence.SetVisibilityPolicy(presence.SharedGroupPolicy("org_members", "org_id", "user_id"))` so presence follows org membership.
6. Do Not Disturb covers every feature's pushes through the filter channels registers; delete that block in `channels.New` to limit it.
7. Name and place the section (nav entry and mobile tab); the workspace follows `useActiveOrg`, and people in no org get a link to Settings → Organizations.
8. Add the push-preview line to the privacy policy.

## Extending

```go
channels.SetPermissionCheck(fn) // actions: ActCreatePublic, ActCreatePrivate, ActArchive, ActEdit, ActRemoveMember, ActMentionAll, ActSetDefault
channels.SetGroupResolver(r)    // SearchGroups(ctx, orgID, viewerID, q, limit), GroupMembers(ctx, orgID, groupID)
channels.SetCallStarter(fn)     // func(ctx, *auth.Identity, channels.CallTarget) (string, error)
id, err := channels.PostBotMessage(ctx, channelID, channels.BotMessage{BotName: "Calendar", Text: "…"})
```

- Mention tokens: `<@uuid>`, `<@here>`, `<@channel>`, `<@group:id|handle>`; clients show names from `message.mentions`.
- Out of scope: scheduled messages and reminders on Later items, custom emoji, link unfurls, channels shared between orgs, announcement-only posting, DND schedules, a document picker on mobile (photos only).
- Don't change: access through `loadAccess`, `client_id` uniqueness, `(created_at, id)` ordering, the attachment delete trigger, hide-vs-erase.

## Provenance

Built for the forum belt on `feature-belt/chat`'s proven mechanics (idempotent sends, read pointers, keyset windows in both directions, reactions, attachments, tombstones, typing, staff view, deletion), adapted to org workspaces with Slack's channel, thread, mention and sidebar model.

Verified on dev-up with three dev identities in Playwright (live delivery, typing, threads and reply counts, reactions, mention badges, forum posts, new channel and DM, attachments, edit with ↑, delete, pin, Later; 375 and 1440 px, light and dark, clean console); REST and WebSocket negatives (private channel, non-org member, removal revokes); agent tools and search under a feature-Read / channel-Off policy; the seams (groups, calls, permission hook, bot message); DND holding push; `expo export`. Not driven on a device: mobile keyboard, long press, photo picker.

## Changes

- 1.1.1: errors and agent tool descriptions name organizations with `orgs.Noun()` (orgs ≥ 1.3.0). Attachment links carry the file's name (`inline; filename=…`), so a download saves as "Minutes.pdf", not the storage key.
- 1.1.0: leaving an org revokes the person's channel topics right after the commit through `orgs.OnMemberRemoved` (orgs ≥ 1.1.2), public channels they read without joining included; the one-minute prune is now a 15-minute backstop. Post-commit notification fan-out and stored-file clean-up run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them. The call button reads “Start call” in channels and DMs alike. “Organization” in copy comes from the orgs noun helpers (`orgsNoun`, `orgsNounA`, `orgsNounTitle`). Search sets `OrgID` and `Context` (the org, then `#channel` or the DM's people for a message; message hits are titled with the sender or the forum post's title) and keeps to the org a search is scoped to (`app.SearchOrg`).
- 1.0.1: notification kinds carry `AgentTarget` (the channel, needs notifications ≥ 1.1.1), so an agent reading the person's inbox no longer sees mentions, DMs and thread replies from channels their agent scope turns off.
- 1.0.0: first release (backend, shared hooks, web and mobile UI).
