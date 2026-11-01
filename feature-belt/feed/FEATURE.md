# Feed

## What it does

- People post text (up to 5000 characters), up to 10 attachments (photos, videos, documents) and a link; they edit and delete their own posts.
- Comments with one level of replies; authors edit and delete their own, post authors delete comments on their posts.
- One reaction per person per post or comment (like, love, haha, wow, sad), changeable; optimistic in the UI.
- @mentions suggest people while typing, are checked against the text by the server and notify the person.
- Staff post announcements; up to 3 active pinned announcements show above the feed until `pinned_until`.
- Unread badge from a per-user seen marker; "Show new posts" instead of shifting the list; blocked people's content is hidden both ways; posts and comments can be reported.

## Data

| Table | Purpose |
|---|---|
| `feed_posts` | posts and announcements: `kind`, `audience` (scope seam), body, link, `pinned_until`, `edited_at`, staff `removed_at`, generated `search` tsvector |
| `feed_media` | attachments per post, ordered; `kind` image/video/file, `name` for documents; `key` unique |
| `feed_comments` | comments and replies (`parent_id`, one level); `deleted_at` placeholder when replies exist; staff `removed_at` |
| `feed_post_reactions`, `feed_comment_reactions` | one row per person per target |
| `feed_mentions` | mentions in posts (`comment_id` null) and comments, UTF-16 offsets |
| `feed_seen` | when each person last opened the feed |
| `feed_media_trash` | storage keys to delete after a delete or media edit commits |

Migrations: `1100_feed_init.sql`, `1101_feed_attachments.sql` (documents).

## API

All under `/v1`. Lists are `{items, next_cursor}`, keyset on `(created_at, id)`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/feed/posts?cursor&limit&q&author_id` | user | feed, newest first; without q/author it leaves out the pinned announcements |
| GET | `/feed/pinned` | user | up to 3 active announcements |
| GET | `/feed/posts/{id}` | user | one post |
| POST | `/feed/posts` | active, 10/min | `{kind, audience, body, link_url, media:[{key,width,height,name}], mentions:[{user_id,start,length}], pinned_until}` |
| PATCH | `/feed/posts/{id}` | author, 30/min | `{body, link_url, media, mentions}`; media replaced when sent |
| DELETE | `/feed/posts/{id}` | author | with comments, reactions, files |
| GET / POST | `/feed/posts/{id}/comments` | user / active 30/min | flat list oldest first (`status` visible/deleted/removed/hidden) / `{body, parent_id, mentions}` |
| PATCH / DELETE | `/feed/comments/{id}` | author (delete: also post author) | edit / delete |
| PUT / DELETE | `/feed/posts/{id}/reaction`, `/feed/comments/{id}/reaction` | active, 120/min | set `{reaction}` / clear |
| GET / POST | `/feed/unread` / `/feed/seen` | user | `{unread_count ≤100, has_unread}` / mark seen |
| GET | `/feed/people?q=` | user, 60/min | 8 mention suggestions |
| GET | `/admin/feed/posts?q&author_id&kind&status`, `/admin/feed/posts/{id}` | support | every post, removed included |
| PATCH | `/admin/feed/posts/{id}` | support, audited | `{pinned_until}` (null unpins) |
| DELETE | `/admin/feed/posts/{id}`, `/admin/feed/comments/{id}` | support, audited | soft remove `{reason}` |
| POST | `/admin/feed/announcements` | support, audited | new announcement |

Uploads: purpose `feed_media` (jpeg, png, heic, webp, gif, mp4, mov; `FEED_MEDIA_MAX_MB`) and `feed_file` (PDF, Word, Excel, PowerPoint, text, CSV, zip; `FEED_FILE_MAX_MB`), both public-read under unguessable keys. Two purposes so each keeps its own types and size limit and the key prefix says how to render it.

Realtime: topic `feed:<uuid>` per audience (`feed:00000000-0000-0000-0000-000000000000` = "all"). Payloads carry ids and counts, clients refetch through the API (which applies blocks): `feed.post`, `feed.post_updated {post_id, deleted}`, `feed.comment {post_id, comment_id, comment_count, deleted}`, `feed.reaction {target_kind, target_id, post_id, reactions, reaction_count}`. Shared hooks apply them to the cache (`registerRealtimeHandler`) and refetch on reconnect.

Notifications: `feed.comment`, `feed.reply`, `feed.mention`, `feed.announcement` (push on), `feed.reaction` (push off); after commit, never to the actor or anyone with a block either way, one per person per comment. Moderation: `moderation.Check` on text; targets `feed.post`, `feed.comment` (snapshot, remove, restore). Agent tools: `feed_list_posts`, `feed_search_posts` (read-only; posts the caller's agent scope hides are left out). Notification kinds name the post (`AgentTarget`) for the inbox's agent scope.

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/feed` (nav, home tile, unread badge) | search (`?q=`), composer (mention suggestions, photo/video and document pickers, link), pinned announcements, post cards, "Show new posts", load more |
| Web | `/feed/posts/[id]` (notification links) | post, threaded comments with replies, reply/edit/delete, reactions, `?comment=` highlight |
| Web | `/admin/feed` | posts table (search, kind, status), remove with reason, pin/unpin, new announcement dialog |
| Mobile | Feed tab (unread dot) | pinned, posts, pull to refresh, "Show new posts", infinite scroll |
| Mobile | `/feed/posts/[id]`, `/feed/compose` (modal, `?id=` edits) | comments with a composer above the keyboard; system photo picker and document picker |

Post and comment menus: Copy link (web) / Share post (mobile: `EXPO_PUBLIC_WEB_URL` link when set, else the app link), Edit, Delete, Report (moderation `ReportDialog`), Block (moderation `BlockDialog`). Every view has skeleton, empty and error states. Photos open the lightbox; videos play inline on web and in the in-app browser on mobile; documents open in a new tab or the in-app browser.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `FEED_POST_MAX_CHARS` | 5000 | longest post |
| `FEED_COMMENT_MAX_CHARS` | 2000 | longest comment |
| `FEED_MEDIA_MAX_MB` | 50 | largest photo or video |
| `FEED_FILE_MAX_MB` | 25 | largest document |

Zero or less fails the boot. Mobile adds `expo-document-picker` (~57.0.3, native: rebuild the dev client). No new Go dependencies. Clients mirror the defaults in `FEED_LIMITS`; change both together.

## Compliance

- Account deletion removes the person's posts (and everything on them), comments, reactions, mentions of them and the seen marker; a top-level comment others replied to stays as a blank placeholder. Files go with the core storage cleanup.
- Export: posts (with attachment URLs), comments, reactions.
- UGC: text filter before posting, report post/comment/person, blocks hide content both ways, staff removal is audited and reversible by moderation.
- Privacy policy: post text appears in push notifications.

## Stitching

1. Audience: default one feed for every signed-in person. For org feeds implement `feed.Scope` (audience keys `org:<id>`) and register it with `feed.SetScope(func(d *app.Deps) feed.Scope {…})` from an init in `internal/api`; implement `PeopleSearcher` (and `AudienceNamer`, so search hits name their audience and org) too, and subscribe clients to `feedAudienceTopic` per audience instead of `FEED_ALL_TOPIC`.
2. Announcements: staff by default (feed composer API and the admin page).
3. Bucket policy: `feed_media/` and `feed_file/` public-read.
4. Privacy policy and store data-safety: user posts, photos, videos and documents.

## Extending

- Seams: `Scope`, `PeopleSearcher`, the shared hooks and `patchFeedPost`/`patchFeedComment` for custom cache updates.
- Keep audience keys `all` or `<kind>:<uuid>` (the uuid names the realtime topic).
- The reaction set is a CHECK constraint plus `FEED_REACTIONS`; widen both with a migration.

## Provenance

Distilled from ASSEMBLY's chapter feed (posts and announcements in one table, one comment level, one reaction per person, mentions, media, PDF attachments, editing, seen-state) and THASSA's mention offsets. Fixed: keyset cursor `(created_at, id)` (ASSEMBLY had no tie-breaker); pinned announcements bounded to 3 (ASSEMBLY loaded every unread one); posts and comments are reportable (ASSEMBLY could only report people); staff removal is a soft, audited hide; mentions are verified against the text; attachment keys are ownership-checked and unique; no per-row subqueries.

Verified: backend build/vet and curl of every route; web flows in Chromium at 375 and 1440, light and dark (compose with mention and attachments, comments and replies, reactions, report dialog, admin removal and announcements), console clean; mobile typecheck and `expo export --platform ios`. Not verified on a device: the mobile screens at runtime, modal presentation of `/feed/compose`, document picking.

## Changes

- 1.0.4: Post-commit side effects run through `bg.Go` (template `internal/bg`), so shutdown and `cmd/seed` wait for them and a panic is logged instead of crashing the process: `afterCommit` takes a task name (`feed.notify_comment`, `feed.delete_media`, …) and runs on `bg.Go`. Search hits set `Context` ("Feed", after the audience's name) and `OrgID` when the Scope implements the new optional `AudienceNamer`.
- 1.0.3: the agent tools drop posts the caller's agent scope hides (a `feed.post` set to Off was listed and searchable before); notification kinds carry `AgentTarget` (needs notifications ≥ 1.1.1).
- 1.0.2: Global search: `app.SearchProvider` (kind `feed.post`) over posts the caller's feed shows, the rules of `GET /feed/posts?q=`; agent calls also need the post readable at the caller's agent scope.
- 1.0.1: Share post on mobile, with a web link when `EXPO_PUBLIC_WEB_URL` is set (app link otherwise).
- 1.0.0: web and mobile UI, shared client, document attachments (`feed_file`, migration 1101), `config.Problem` for bad limits.
- 0.1.0: backend.
