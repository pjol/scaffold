# Social

## What it does

- Everyone gets a public profile at `/u/<handle>`: display name and photo (core), a handle, a bio, up to 5 links, the joined date, follower and following counts, "Follows you", and an Activity tab.
- Handles are unique and case-insensitive (3–20 of a–z, 0–9 and `_`, starting with a letter), generated from the name on first use, and changeable once per cooldown. Reserved words and lookalikes of staff names ("supp0rt") are refused; the old handle redirects, and nobody else can take it, for a while.
- Follow and unfollow anyone (idempotent, up to 2,000), browse followers and following, get a grouped notification when someone follows you (at most one per follower per hour).
- Choose who sees each part of your profile (Everyone, Followers, Only me): Activity and Followers-and-following ship; modules add their own parts.
- The Following feed shows what the people you follow did, newest first, as each of them chose to share it; items can be held back until a time. Modules record activity; social ships "Followed …".
- Find people by name or @handle; suggestions are people followed by people you follow, then the most followed. A block either way (moderation) hides people everywhere here and removes follows between them.

## Data

| Table | Purpose |
|---|---|
| `social_profiles` | one per person: lowercase `handle` (unique, prefix-indexed), `bio`, `links` (JSON), `privacy` (section → visibility; missing = default), `followers`/`following` counters, `handle_changed_at` |
| `social_handle_history` | handles given up: who they redirect to and until when; `user_id` NULL = held for nobody (deleted or moderated account) |
| `social_follows` | follower → followee, indexed both ways by time for keyset lists |
| `social_follow_notices` | last follow notification per pair (the hourly limit) |
| `social_activity` | actor, kind, section, `ref_id` (unique per actor and kind when set), optional `subject_id`, `data`, `visible_after` |

Jobs (once across instances): `social.profiles` (every minute: profiles for new accounts, 500 per run), `social.blocks` (every minute: repairs the follows of 1/1440 of profiles, so each daily), `social.retention` (hourly: activity older than `SOCIAL_ACTIVITY_RETENTION`, expired handle holds, old notice stamps).

## API

All under `/v1`, signed in; writes behind the consent gate. `{handle}` takes an old handle (resolves to the current one) or `me`.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/social/me` | user | handle, bio, links, privacy rows `{key, label, default, visibility}`, `handle_change_at`, limits |
| PATCH | `/social/me` | active, 30/h | `{bio?, links?, privacy?: {section: visibility}}`; text goes through `moderation.Check` |
| PUT | `/social/me/handle` | active, 10/h | `{handle}`; 409 `handle_taken`, 422 `handle_invalid`/`handle_reserved`/`content_rejected`, 429 `handle_cooldown` (`details.available_at`) |
| GET | `/social/handles/{handle}` | 60/min | `{available, code?, message?}` for the settings form |
| GET | `/social/profiles/{handle}` | user | profile; `restricted` when a block separates you (name and photo only); `sections` = what you may see |
| GET | `/social/profiles/{handle}/followers`, `/following` | user | `Page<Person>` newest first, gated by the owner's connections section |
| GET | `/social/profiles/{handle}/activity` | user | `Page<ActivityItem>`; the owner sees held items too |
| PUT / DELETE | `/social/follows/{user_id}` | active, 100/h (+1,000/day to follow) | idempotent → `{user_id, following, followers}`; 403 `blocked`, 422 `self_follow`/`follow_limit` |
| GET | `/social/feed` | user | `Page<ActivityItem>` of people you follow |
| GET | `/social/people?q=` | 60/min | ≤ 50 people by handle or name prefix (2+ characters) |
| GET | `/social/suggestions` | user | ≤ 30 people to follow, with `mutuals` |

No realtime topics: the feed refetches every minute and a `social.follow` notification event refreshes your counts. Notification kind `social.follow` ("New followers", push on, grouped per person followed). Report kind `social.profile` (handle, bio, links); staff removal clears bio and links and replaces the handle. Global search: people (`social.person`). Agent tools (read-only): `social_get_profile`, `social_list_feed`, `social_search_people`. No follow tool: a follow is seen by others and notifies the person, so it stays their own act (as moderation keeps blocking).

## Screens

| Where | Web | Mobile |
|---|---|---|
| Feed | `/following` (nav "Following"): activity rows, Load more; empty: Find people + 5 suggestions | Following tab, pull to refresh, endless scroll |
| People | `/people` (Home tile): search (`?q=`), suggestions | `people` (Home row): the same |
| Profile | `/u/[handle]` (account menu "Your profile" → `/u/me`): header, bio, links, joined, Follow or Edit profile, ⋮ (copy link, report, block), tabs Activity · Followers · Following (`?tab=`); old handles replace the URL | `u/[handle]`: the same, segmented tabs, ⋮ sheet |
| Settings | `/settings/public-profile`: handle with live availability, bio with counter, links, who sees what (one select per section) | `settings/public-profile` (+ "Your profile" row): the same, segmented controls |

Every list has skeleton, empty and error states. Exports for other features: web and mobile `@/features/social` (`FollowButton`, `PersonRow`, `ActivityRow`, `socialName`); shared hooks `useSocialProfile`, `useSocialFollow` (optimistic everywhere the person shows), `useSocialFeed`, `useSocialPeople`, `useSocialSuggestions`, `useSocialMe`, `useUpdateSocialProfile`, `useChangeSocialHandle`, `useSocialHandleCheck`, `socialProfilePath`.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `SOCIAL_HANDLE_COOLDOWN` | `168h` | shortest time between handle changes (0 = any time) |
| `SOCIAL_HANDLE_REDIRECT` | `720h` | how long an old handle redirects and stays unclaimable |
| `SOCIAL_RESERVED_HANDLES` | empty | extra reserved handles (comma-separated) |
| `SOCIAL_MAX_FOLLOWING` | `2000` | most people one person follows (1–100,000) |
| `SOCIAL_BIO_MAX_CHARS` | `280` | longest bio |
| `SOCIAL_ACTIVITY_RETENTION` | `2160h` | activity kept (≥ 24h) |

Invalid values fail the boot through `config.Problem`. No npm or Go dependencies beyond moderation's `golang.org/x/text`.

## Compliance

- Deletion: profile, follows both ways (counters recounted), notices and activity (theirs and items about them) are deleted. The handle and old handles are held unlinked for `SOCIAL_HANDLE_REDIRECT` so nobody takes one over and inherits old links, then freed. Export: profile, handles still redirecting, following and followers by handle, newest 5,000 activity items.
- UGC: bios, link labels and handles go through `moderation.Check`; profiles are reportable (`social.profile`); blocks apply everywhere. Links open with `rel="noopener noreferrer nofollow ugc"`.
- Profiles are visible to signed-in people only; privacy settings cover activity and lists, not the name, handle, bio, links or counts (the settings screen says so).

## Stitching

1. Record activity: `social.RegisterKind(social.Kind{ID: "feed.post", Label: "Posts", Section: "posts", Icon: "newspaper", Render: …})` and `social.RegisterSection("posts", "Posts", social.VisibilityPublic)` in the module's `New`; `social.Record(ctx, tx, social.Activity{ActorID, Kind, RefID, Data, VisibleAfter})` where it happens; `social.RemoveActivity(ctx, q, kind, refID)` on delete. Modules that don't require social do it from `backend/internal/api/stitch_social.go`.
2. Show people by handle: `social.Refs(ctx, q, ids)` server-side; link names and avatars to `/u/<handle>`; reuse `FollowButton`/`PersonRow`.
3. Reserve brand words (`SOCIAL_RESERVED_HANDLES`); pick the cooldown and redirect window.
4. Place entries: web nav Following, Home tile People, account menu Your profile, Settings → Public profile; mobile Following tab, Home row People, Settings rows.
5. Signed-out profiles: add a `PublicRoutes` route that calls `social.BuildProfile(ctx, q, owner, uuid.Nil)` (public sections only) and a page under `(public)` plus its proxy public path.
6. Privacy policy and store forms: public profile, follows, activity shown to followers; follow pushes go through notifications.

## Extending

```go
social.Refs(ctx, q, ids)                         // map[uuid.UUID]social.Ref{user_id, handle, display_name, avatar_url}
social.UserIDByHandle(ctx, q, "@sam")            // current or redirecting holder; 404 *httpx.Error
social.RegisterSection(key, label, default)      // a privacy row in Settings
social.CanSee(ctx, q, owner, viewer, section)    // owner always; never across a block; uuid.Nil = anonymous
social.RegisterKind(social.Kind{…}); social.Record(ctx, q, social.Activity{…}); social.RemoveActivity(ctx, q, kind, ref)
social.BuildProfile(ctx, q, owner, viewer)       // the profile as viewer sees it
```

`Kind.Render(data, subject)` returns title, summary and link (nil: data `title`/`summary`/`link`); `Activity.SubjectID` names another person (shown with their current handle; hidden across blocks). `Data` is shown to everyone who may see the item: no private fields. Don't write `social_follows` or the counters outside `graph.go`; blocks stay moderation's.

Blocks and the graph: moderation has no block hook, so `IsBlocked` refuses follows, every read filters by `BlockedIDs` and repairs (deletes the pair's follows and "followed" items, recounts) for the person whose edges it shows, and the `social.blocks` job repairs everyone else within a day. A `moderation.OnBlock` hook would make the sweep unnecessary.

## Provenance

Distilled from MOX's social layer (`backend/internal/social`: profiles, graph, privacy, activity, refs, retention, limits; the `platform/social.go` contracts; `web/components/social` and the profile settings screen). Generalised: no badges, reputation, positions, decks, copy trading, Privy identity, simulated accounts or kill switches; MOX's fixed privacy sections became `RegisterSection`, its fixed activity kinds `RegisterKind` with server-side rendering; the in-memory UserRef cache became an indexed query (multi-instance safe); blocks moved to moderation (MOX had its own table); profiles are scaffold users with lazily generated handles. Kept: per-actor lateral feed reads, refetch past hidden rows, the privacy matrix, idempotent follows with counters in the transaction and the 2,000 cap, hourly follow-notice grouping, lookalike name folding (exact for short names, so "stuff" passes where MOX refused it). Added: handle cooldown and redirect window with held handles, follow cleanup after blocks, reportable profiles, suggestions, global search, keyset follower lists, export and deletion.

Verified: Go build, vet, unit tests and database tests (handles, redirects, cooldown, follows, caps, blocks and repair, the sweep, feed privacy and holds, deletion, every route over HTTP) against Postgres; the backend booted with the seed and routes exercised with dev tokens; web and mobile typecheck, `pnpm check`, doctor. Not verified: `next build`, `expo export`, Playwright at 375/1440 in light and dark, and the mobile screens on a device.

## Changes

- 1.0.0: first version.
