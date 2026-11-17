# Forum

## What it does

- Communities, each with a description, rules, moderators and a lock; staff create them, and the app makes one per object of its own with `forum.EnsureCommunity` (a product, a place, an event).
- Text posts (Markdown) and link posts; threaded comments up to 8 levels deep, long threads behind "N more replies"; edit and delete your own.
- Up and down votes on posts and comments, ranked hot, new or top (today, this week, month, year, all time); karma is the votes your posts and comments received.
- `@Name` mentions with suggestions; notifications for comments on your post, replies to your comment and mentions; live comment, vote and status updates on an open post.
- Every post, comment and edit goes through moderation screening: refused, labelled (off-topic posts rank lower), held (visible only to the author, with the reason, and staff until reviewed) or removed. Report, block (hides both ways), pin, lock and soft remove by moderators.

## Data

| Table | Purpose |
|---|---|
| `forum_communities` | slug, title, description, rules, locked; `ref_kind`/`ref_id` for app-made communities (unique) |
| `forum_moderators` | community moderators (pin, lock, remove in their community) |
| `forum_posts` | title, Markdown body, link and domain, labels, `mod_reasons`, status visible / held / removed / deleted, score, `hot`, comment count, pinned, locked, full-text `search` |
| `forum_comments` | post, parent, depth 0–8, body, labels, `mod_reasons`, status, score |
| `forum_votes` | one ±1 per person per post or comment, with the author who received it and whether it counts for karma |
| `forum_mentions` | who a post or comment mentions |

`hot = sign(s)·log10(max(|s|,1)) + (created_s − 1.7e9)/45000`, minus 2 when labelled `off_topic`; it is stored and updated on each vote (newer posts start higher, so nothing re-ranks with time).

## API

All under `/v1`, signed in; writes need an active account.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/forum/communities` | user | every community, most active first (`posts_7d`) |
| GET | `/forum/communities/{slug}` | user | community, moderators, pinned posts, `can_moderate` |
| GET | `/forum/posts` | user | `?community=&sort=hot\|new\|top&t=day\|week\|month\|year\|all&author_id=&q=`; keyset pages |
| POST | `/forum/posts` | user, 10/min + 5/h (2/h for accounts under 3 days) | `{community, title, body, url?, mentions[]}` → `{post, moderation}`; 422 `content_rejected`, 403 `posting_restricted` / `community_locked` |
| GET / PATCH / DELETE | `/forum/posts/{id}` | user; author for writes | read; edit (re-screened, held and removed posts can't be edited); delete (a placeholder stays) |
| POST | `/forum/posts/{id}/moderate` | staff or community moderator | `{action: pin\|unpin\|lock\|reopen\|remove\|restore, note}`; restore is staff only and refused while a moderation case is open; audited |
| GET | `/forum/posts/{id}/comments` | user | `?sort=top\|new\|old`: top-level comments with replies (50 per level, `more_replies`), keyset pages |
| POST | `/forum/posts/{id}/comments` | user, 20/min + 30/h (10/h new) | `{body, parent_id?, mentions[]}` → `{comment, moderation}`; 403 `post_locked` |
| GET | `/forum/comments/{id}/replies` | user | one comment's replies, oldest first, keyset pages |
| PATCH / DELETE | `/forum/comments/{id}` | author | edit (re-screened) / delete |
| POST | `/forum/comments/{id}/moderate` | staff or community moderator | `{action: remove\|restore}`; audited |
| PUT | `/forum/votes` | user, 120/min | `{target_kind, target_id, value: -1\|0\|1}` → `{score, viewer_vote}`; never your own, never across a block |
| GET | `/forum/people?q=` | user, 60/min | mention suggestions (blocked people left out) |
| GET | `/forum/karma?user_id=` | user | `{karma, karma_90d}` |
| GET / POST | `/admin/forum/communities` | support | list with moderators and counts / create; audited |
| PATCH | `/admin/forum/communities/{id}` | support | title, slug, description, rules, locked; audited |
| POST / DELETE | `/admin/forum/communities/{id}/moderators[/{user_id}]` | support | add by `{email}` / remove; audited |

Realtime on `forum:<post id>` (anyone who may open the post): `forum.comment` `{post_id, comment_id, parent_id, comment_count}`, `forum.vote` `{post_id, target_kind, target_id, score}`, `forum.status`, `forum.post_updated`; ids and scores only, clients refetch. Notifications: `forum.comment`, `forum.reply`, `forum.mention` (push on), never across a block, sent when the item becomes visible (on approval for held ones). Moderation targets `forum.post`, `forum.comment` (snapshot, idempotent remove, restore, after-commit `Changed`). Global search: visible posts by title and body. Agent tools (read-only): `forum_list_communities`, `forum_list_posts`, `forum_get_post` (with its comment tree). Agents don't post or comment: a forum post is public speech in the person's name that others read, vote on and reply to.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Forum | `/forum` (nav, home tile, ⌘K "New forum post"): Hot / New / Top (period in the URL), posts across communities, community list | `forum` (Home row): New post, community chips, sort, posts (pull to refresh, endless) |
| Community | `/forum/c/[slug]`: description in ⓘ, New post (hidden when locked), pinned, sorted posts, rules, moderators | `forum/c/[slug]`: the same; rules and moderators behind a button |
| Post | `/forum/posts/[id]` (`?comment=` scrolls to one): held or removed banner with the reasons, votes, Markdown body, ⋮ (copy link, edit, delete, report, block; pin, lock, remove, restore for moderators), comment composer with @ suggestions, sortable tree with collapse and "more replies" | `forum/posts/[id]`: the same, actions in a sheet |
| Compose | `/forum/submit?community=`: community, Text / Link, title, link, Markdown text | `forum/submit` (modal; `?edit=` edits) |
| Admin → Forum | `/admin/forum`: communities table, create / edit dialog with moderators by email | none |

Every async view has skeleton, empty and error states. Shared hooks in `packages/shared/src/forum` (`useForumPosts`, `useForumPost`, `useForumComments`, `useForumVote` (optimistic), `useCreateForumPost`, …); text helpers `forumMentionQuery`, `forumMentionIds`, `forumAge`.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `FORUM_POST_MAX_CHARS` | `10000` | longest post text |
| `FORUM_COMMENT_MAX_CHARS` | `5000` | longest comment |
| `FORUM_VOTE_MIN_AGE` | `0s` | account age before voting (24h for a public forum) |
| `FORUM_KARMA_MIN_AGE` | `168h` | younger voters move scores, not karma |

Screening, strikes and the decision model are configured on moderation (its FEATURE.md).

## Compliance

- UGC duties: screening before posting (moderation rules, optional classifier and decision model), report post and comment, block both ways, staff queue for reports and held items, posting restrictions after repeated removals.
- Deletion: the person's posts and comments become "[deleted]" placeholders with text, link and labels cleared (replies by others stay); their votes are withdrawn from scores and deleted; mentions of them and their moderator seats are deleted. Votes they received stay (other people's).
- Export: their posts, comments, votes and karma.
- Post titles and comment excerpts appear in push notifications; say so in the privacy policy.

## Stitching

1. Decide the communities: Admin → Forum, or `forum.EnsureCommunity(ctx, tx, forum.CommunitySpec{RefKind: "product", RefID: id, Title, Description})` where your objects are created; link to `/forum/c/<slug>` from the object's page (or embed `ForumPostList` with `useForumPosts({community})`). Write each description: screening reads it to judge off-topic.
2. Name community moderators; decide who on staff works Admin → Moderation (held posts and reports).
3. Choose `FORUM_VOTE_MIN_AGE` and `FORUM_KARMA_MIN_AGE`; tune moderation's screening settings.
4. Place the entry (nav, home tile and ⌘K ship on web; a Home row on mobile).
5. Optional labels: implement `forum.Labeler` and call `forum.SetLabeler` from an init in `backend/internal/api/stitch_forum.go`; name the labels in `FORUM_LABELS`.
6. Privacy policy and store forms: posts, comments, votes, mentions; text in push notifications.

## Extending

- Go: `forum.EnsureCommunity`, `forum.Karma(ctx, q, userID, since)` (the reputation feature's community seam), `forum.SetLabeler`, `forum.Hot`, `forum.PostTopic`.
- Web `@/features/forum`: `ForumPostList`, `ForumPostRow`, `ForumSortBar`, `ForumMarkdown`, `ForumVoteButtons`, `ForumBadges`; mobile the same minus the sort bar.
- Keep posts and comments going through `moderation.Screen` + `moderation.Record`; don't show held items to anyone but their author and staff, and don't add a forum-side approve (the queue owns it).

## Provenance

Distilled from MOX's `backend/internal/forum` (types incl. Hot, posts, comments, tree, votes, communities, target) and its moderation hooks, on branch `worktree-wf_70d3fcd7-22d-1`; the screens follow MOX's layout (feed, community, post with tree, submit) built on the belt's feed patterns, not ported from MOX's components. Generalised: scaffold users and blocks instead of handles and the social graph, staff and community moderators from scaffold roles and a forum table, communities from staff or `EnsureCommunity` instead of games, markets, decks and events, UUID/timestamptz schema, keyset pages everywhere, votes carrying their recipient so karma needs no nightly job. Left out: market tagging, autocards, position disclosures and the trade watcher (`forum.SetLabeler` is the generic seam), simulated accounts, appeals, follower activity, batched score frames (one `forum.vote` event per vote). Fixed on the way: held items read as removed to others; a forum-side restore can't bypass an open moderation case; held comments don't count in `comment_count`; notifications held back for held items go out once on approval; a reply rendered alone keeps its parent (a regression test covers it).

Verified on a generated project (forum, chat, feed): `go build`, `go vet`, `go test ./internal/forum/... ./internal/moderation/...`, `pnpm typecheck`, `pnpm check`, `scaffold doctor`; on the booted backend with curl: seed, hot and new lists, comment tree with block placeholders, create (visible, held, removed third copy), mention notification, votes (own and blocked refused), replies, community moderator pin and refusals, locked community, admin community CRUD and moderators, the queue's approve and remove with strike, posting restriction, export, account deletion. Not verified: `next build`, `expo export`, Playwright, devices, realtime delivery in a browser, global search and agent tools through the search, mcp and assistant features (not installed in the test project).

## Changes

- 1.0.0: first version.
