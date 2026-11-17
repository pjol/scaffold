# Channels

A Slack-style workspace per organization: channels, forums, announcement channels, direct messages and threads, on web and mobile.

## What it does

- Each org gets a workspace: public channels anyone in the org can browse and join, private channels by invitation, announcement channels (only admins post; replies optional), direct and group messages (up to 9 people, or a note to yourself; a group DM can become a private channel). `CHANNELS_DEFAULT_CHANNELS` (#general) are created on the first visit and every member joins them once.
- Chat channels are a timeline; forum channels list titled posts with tags. Threads everywhere ("also send to channel", a Threads view, follow/unfollow). Mentions: @person, @here, @channel (permission-gated), @group (when wired), plus notification keywords.
- Messages: a safe markdown subset (formatting toolbar and shortcuts, code blocks highlighted), files of any type (picked, pasted or dropped, uploaded with progress; previewed with the file preview kit), polls, custom emoji and skin tones, reactions, pins, edits (↑), deletes, forwards, link previews (outside links fetched once server-side and stored; links to the app become cards checked against each viewer), bookmarks above the timeline.
- Your own tools: server-kept drafts, Drafts & sent (drafts, scheduled messages, sent), send later, reminders on messages and `/remind me …`, Later with reminders, Activity (mentions, reactions to you, replies in threads you follow), All unread, mark unread, custom sidebar sections with sort and collapse, starred, profile cards (title, local time, status, teams), slash commands with autocomplete, keyboard shortcuts.
- Search with `in:#channel from:@x with:@x to:me has:file|link|pin|reaction is:thread|saved before: after: on: during:` and filter chips. Per-channel notifications and mute; Do Not Disturb holds pushes app-wide.
- Admin: org roles decide who creates, archives, edits, @channels, posts in announcement channels, adds emoji, manages link previews and retention (workspace, per-channel override; audited) and incoming webhooks (audited). Staff open a channel from a report and hide messages.

## Data

| Table | Purpose |
|---|---|
| `channels` | org, kind (public, private, dm, group_dm), layout (chat, forum), `name`, topic, description, `forum_tags`, `is_default`, `dm_key`, archived; `posting` (everyone, admins), `replies_open`, `retention_days` override |
| `channel_members` | role (admin = manages the channel), read pointer, `notify`, `muted`, `starred_at`, `hidden_at` (closed DM), `section_id` |
| `channel_messages` | text, system or bot (apps, incoming webhooks: `webhook_id`); threads (`thread_root_id`, `also_in_channel`); forum `title`/`tags`; counters; pins; `client_id`; `forward_of` + `is_forward`; `links_hidden`; tombstones |
| `channel_mentions`, `channel_reactions` (unicode or `:custom:`), `channel_attachments`, `channel_saved`, `channel_thread_follows`, `channel_autojoins` | as named |
| `channel_drafts` · `channel_scheduled` · `channel_reminders` | one draft per person/conversation/thread · messages to send later (failed ones keep the reason) · reminders (about a message, which also goes to Later, or text) |
| `channel_link_previews` · `channel_message_links` | cached Open Graph/oEmbed metadata with stored thumbnail and icon keys · a message's links |
| `channel_emoji` · `channel_polls`, `channel_poll_options`, `channel_poll_votes` | org emoji and aliases · polls (votes kept for anonymous polls, voters never returned) |
| `channel_sidebar_sections` · `channel_user_prefs` | your sections · per person per org: sort, collapsed sections, keywords, skin tone, Activity read time, profile title, time zone |
| `channel_bookmarks` · `channel_webhooks` · `channel_workspace_settings` | links above a channel · incoming webhooks (token hash only) · link previews switch, retention |
| `channel_deleted_objects` | storage keys queued by triggers (attachments, emoji, replaced previews); a worker deletes the objects |

Migrations: `2200_channels_init.sql`, `2201_channels_depth.sql`.

## API

Under `/v1`, signed in (agent credentials are refused by core auth). Org routes use `orgs.Require("member")`; channel and message routes load the caller's access first (org member, and channel member unless public) and answer 404 otherwise. Writes others see need an active account.

| Method | Path | Purpose |
|---|---|---|
| GET | `/channels/config` · `/channels/unread` | limits · badge totals |
| GET | `/channels/orgs/{orgID}/workspace` | sidebar, permissions, counts (Threads, Later, Activity, drafts, scheduled), prefs, sections, settings |
| GET | `…/orgs/{orgID}/browse` · `people` · `people/{userID}` · `groups` · `threads` · `later` · `search?tz=` · `activity?filter=` · `unreads` · `drafts` · `sent` · `scheduled` · `reminders?state=` · `emoji` · `settings` | lists and the profile card |
| POST | `…/orgs/{orgID}/channels` · `/dms` · `/reminders` · `/sections` · `/emoji` · `/activity/read` | create channel, open DM, reminder (`message_id`/`text`/`command`, `at`/`when`, `tz`), section, emoji (`key` or `alias_of`), mark Activity seen |
| PUT / PATCH | `…/orgs/{orgID}/prefs` · `…/orgs/{orgID}/settings` | your prefs · link previews and retention (ActManageWorkspace, audited) |
| GET / PATCH | `/channels/channels/{id}` | detail with `permissions` (post, reply, …) / name, topic, description, tags, private, default, `posting`, `replies_open`, `retention_days` (audited) |
| POST / DELETE | `/channels/channels/{id}/archive` · `/join` · `/membership` · `/members[/{userID}]` · `/convert` (group DM → private) | membership |
| PUT · POST | `…/prefs` (incl. `section_id`) · `…/read` · `…/unread {message_id}` · `…/typing` | your state |
| GET / POST | `…/messages` (send: `{client_id, body, thread_root_id?, also_send_to_channel?, title?, tags?, attachments?[{key, file_name, content_type, …}]}`) · `…/polls` · `…/scheduled` (`send_at` or `when`) | timeline, polls, send later |
| GET / PUT / DELETE | `…/draft?thread_root_id=` | your draft (empty body deletes) |
| GET / POST | `…/bookmarks` · `…/webhooks` (ActManageIntegrations; URL shown once) | bookmarks, webhooks |
| GET | `…/topics` · `…/pins` · `…/files` · POST `…/call` | forum posts, pins, files, call seam |
| GET / PATCH / DELETE | `/channels/messages/{id}` · `…/thread` | message (also how clients load a forward's original), thread / edit / delete |
| POST / PUT / DELETE | `…/reactions` · `…/pin` · `…/later` · `…/follow` · `…/thread/read` · `…/forward` · `…/vote` · `…/poll/close` · `…/previews` | message actions |
| PATCH / DELETE | `/channels/scheduled/{id}` (+ POST `…/send`) · `/channels/reminders/{id}` · `/channels/sections/{id}` · `/channels/bookmarks/{id}` · `/channels/emoji/{id}` · `/channels/webhooks/{id}` | edit, reschedule, done, remove |
| GET | `/channels/card?url=` · `/channels/attachments/{id}` | the viewer's card for an app link (404: none or not theirs) · fresh file link |
| POST | `/v1/channels/hooks/{token}` (public) | incoming webhook `{text}`: 30/min per hook, filtered, no @channel/@here, agent credentials refused, path redacted |
| GET / DELETE | `/admin/channels/channels/{id}` · `/admin/channels/messages/{id}` | staff view (audited) · hide (audited) |

Rate limits per person on every write (messages `CHANNELS_MESSAGES_PER_MINUTE`, drafts 120/min, polls 20/min, emoji 30/h, forwards at the message rate…).

Realtime: `channels:<id>` carries `channels.message`, `channels.message_updated` (previews arriving, votes; `poll.my_votes` is null in live events), `channels.typing`, `channels.channel` (`updated | members | pins | bookmarks | retention`); each member's user topic gets `channels.sidebar` (`message | added | removed | updated | read | later | prefs | emoji | draft | scheduled | activity`) and followers `channels.thread`. Leaving the org revokes topics at once (`orgs.OnMemberRemoved`), with a 15-minute backstop.

Jobs: `channels.send_scheduled` and `channels.fire_reminders` (30 s), `channels.retention` (hourly), `channels.prune_previews` (6 h), `channels.delete_objects` (1 min), `channels.prune_departed` (15 min).

Link previews: outside links (up to 3 per message) are fetched after the send through `safehttp` (private addresses refused except in development, 8 s, redirects ≤ 5, HTML head ≤ 768 KB, images ≤ 5 MB and ≤ 40 MP), Open Graph/Twitter/HTML metadata with oEmbed fallback, cached 24 h per URL; the image (≤ 600 px JPEG) and icon (64 px PNG) are stored, so readers load them from this app's storage. A workspace switch turns outside previews off.

Notifications: `channels.mention`, `channels.message`, `channels.thread`, `channels.keyword`, `channels.reminder`, `channels.scheduled` (a scheduled message that failed). Mute and "nothing" send none. The `presence.dnd` delivery filter holds push and email app-wide.

Agent tools (as the caller, same checks, narrowed by agent scope): `channels_list_mine`, `channels_read_history`, `channels_read_thread`, `channels_search_messages` (all operators, `tz`), and writes `channels_post_message`, `channels_schedule_message`, `channels_create_poll` (Target = the channel) and `channels_set_reminder` (the caller's own; feature-level target), each with an exact `Preview`. `app.SearchProvider` returns channels and messages under the same checks.

## Screens

- Shared (`packages/shared/src/channels`): endpoints, hooks (hooks.ts + depth.ts), realtime handlers, `parseChannelsMarkdown` (custom emoji, escapes), slash-command registry (`registerChannelsSlashCommand`, 15 built-ins), copy and time helpers.
- Web `/channels`: sidebar (Activity, All unread, Threads, Later, Drafts & sent, Browse; starred, your sections (drag in, ⋮ rename/order/remove), channels, DMs; sort; collapse remembered), channel (header with announcement mark, bookmarks bar, timeline with previews, polls, forwards, file previews, profile cards from names), composer (toolbar, `@ : /` autocomplete, drop and paste, progress, send later, poll), thread pane, details (posting, retention, webhooks), Activity, All unread, Drafts & sent, Later (reminders), Search (filters), preferences (title, keywords, emoji, workspace settings), shortcuts sheet (⌥↑/↓, ⌥⇧↑/↓, ⌘⇧A, ⌘⇧T, ⌘⇧M, Esc, ⌘/).
- Mobile: the same views (Activity, All unread, Drafts & sent, person and forward screens), bookmarks row, long-press actions (remind, mark unread, forward, remove previews), composer with photos and files (`expo-document-picker`), slash commands, send later, polls; custom times typed as phrases.
- Every view has skeleton, empty and error states.

## Config

| Variable | Default | Meaning |
|---|---|---|
| `CHANNELS_MESSAGE_MAX_CHARS` | 8000 | longest message |
| `CHANNELS_EDIT_WINDOW` | 0 | edit window after sending; 0 = no limit |
| `CHANNELS_MESSAGES_PER_MINUTE` | 60 | messages per person per minute (agent posts included) |
| `CHANNELS_ATTACHMENT_MAX_MB` | 50 | largest file |
| `CHANNELS_DEFAULT_CHANNELS` | general | channels a new workspace starts with |
| `CHANNELS_MENTION_ALL_MAX_MEMBERS` | 50 | above this (and in default channels) @channel/@here is for owners and admins |

Requires orgs, presence, notifications (≥ 1.1.0) and moderation; core `safehttp`; mobile `expo-document-picker` (rebuild the binary).

## Compliance

- Deletion: the person's messages become tombstones (text, files, previews, polls gone); their reactions, votes, mentions, saved items, follows, drafts, scheduled messages (uploads deleted), reminders, prefs, sections and channel memberships go; emoji, bookmarks and webhooks they created stay with the org (creator cleared). Export: memberships, prefs, sections, sent messages with 24-hour file links, reactions, poll votes, saved items, drafts, scheduled messages, reminders.
- UGC: `moderation.Check` on messages, polls, titles, names, topics, emoji names, bookmarks, profile titles and webhook posts; report from every message; blocks hide both ways. Unknown file types are served as downloads only (`application/octet-stream`).
- Privacy policy: message text appears in push notifications; link previews are fetched by the server, not readers.

## Stitching

1. Permissions: `channels.SetPermissionCheck(fn)` (access matrix), falling back to `channels.DefaultPermission(req)`; register the 1.2 actions `channels.add_emoji`, `channels.manage_workspace`, `channels.manage_integrations`, `channels.post_announcement`.
2. Groups: `channels.SetGroupResolver(r)`; add `GroupsOf` for teams on profile cards.
3. Calls: `channels.SetCallStarter(fn)` (also `/call`).
4. App cards and bookmarks: `channels.RegisterUnfurler("/docs/", fn)` per feature (viewer access checked inside; nil = invisible).
5. Zones: `channels.SetUserZone(fn)` (e.g. the calendar's zone).
6. Slash commands: `registerChannelsSlashCommand` in the web and mobile stitch files.
7. Announcements: `channels.PostBotMessage`, `channels.DefaultChannel`. Presence visibility: `presence.SharedGroupPolicy("org_members", "org_id", "user_id")`.
8. Name and place the section; add the privacy lines above.

## Extending

- Mention tokens: `<@uuid>`, `<@here>`, `<@channel>`, `<@group:id|handle>`; custom emoji `:name:`.
- Out of scope: channels shared between orgs (every access check is org-bound; needs federation), DND schedules (presence owns DND), outgoing webhooks and bot users, a native date picker on mobile (times are typed phrases the server reads), admin bulk export.
- Don't change: access through `loadAccess`, `client_id` uniqueness, `(created_at, id)` ordering, the delete triggers, hide-vs-erase, previews only through `safehttp`.

## Provenance

Built on `feature-belt/chat`'s mechanics, adapted to org workspaces with Slack's channel, thread, mention and sidebar model; 1.2 adds Slack's weekly-use depth.

Verified (1.2): Go unit tests (time phrases incl. DST, `/remind` parsing, search operators, scheduling bounds, keyword matching, link extraction, Open Graph/oEmbed fixtures, thumbnails, preview fetches refused on private and metadata addresses through `safehttp`); typecheck and checks on a generated app; dev-up runs listed in the round-3 report.

## Changes

- 1.2.0: drafts on the server and Drafts & sent; send later; reminders (messages, Later, `/remind`); link previews through `safehttp` with stored thumbnails and the `RegisterUnfurler` seam for app cards; Activity, All unread, mark unread; slash commands (registry, autocomplete); polls; custom emoji, frequent emoji, skin tones; search operators and filters; forwards; sidebar sections, sort and collapse; bookmarks; announcement channels; keywords; shortcuts; profile cards; group DM → private channel; incoming webhooks; retention; files of any type with upload progress and drag-drop, previewed with the core kit; highlighted code blocks; agent tools to schedule, remind and poll. Migration 2201; mobile needs `expo-document-picker`.
- 1.1.1: errors and agent tool descriptions name organizations with `orgs.Noun()`; attachment links carry the file's name.
- 1.1.0: leaving an org revokes topics at once (`orgs.OnMemberRemoved`); fan-out through `bg.Go`; search sets `OrgID` and `Context`.
- 1.0.1: notification kinds carry `AgentTarget`.
- 1.0.0: first release.
