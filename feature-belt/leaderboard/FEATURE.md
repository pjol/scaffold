# Leaderboard

## What it does

- Boards rank people by a score the app provides: profit and loss, points, streaks, fastest times (ascending boards). Ties share a rank.
- Each person sees the board, their own rank and score, and pages through the rest.
- People appear under a generated alias (`amber-otter-4821`) by default; in Settings they choose to show their name and photo, or to hide from every board.
- Boards come from the app (`leaderboard.RegisterBoard`, scheduled or pushed with `SetScore`) or from any installed module that implements `leaderboard.Source` (the exchange feature's profit and loss board).

## Data

| Table | Purpose |
|---|---|
| `leaderboard_profiles` | per person: `visibility` (alias / name / hidden) and a unique `alias` (adjective-noun-NNNN, 8.1 million combinations; after repeated collisions the suffix is taken from the person's id, so it never runs out) |
| `leaderboard_entries` | one score per person per board; `rank` computed by the refresh job (null when hidden or not rankable) |

A job (`leaderboard.refresh`, every 15 s, once across instances) reloads scheduled boards that are due (`Board.Refresh`, default 1 minute), gives new people a profile, and re-ranks every board. Banned and deleted accounts are never ranked.

## API

All under `/v1`, signed in.

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/leaderboard/boards` | user | boards `{id, title, format, currency?, ascending, ranked, updated_at}` |
| GET | `/leaderboard/boards/{board}?limit&cursor` | user | ranked entries `{rank, name, avatar_url, score, is_me}`, best first, keyset on `(rank, user)` |
| GET | `/leaderboard/boards/{board}/me` | user | the caller's `{rank, score, ranked, visibility}` |
| GET / PUT | `/leaderboard/profile` | user; PUT active, 30/h | `{visibility, alias, display_name}` / `{visibility}` (hiding takes effect at once) |

No realtime: clients refetch every 30 s, the pace scores change at. Agent tool: `leaderboard_get` (read-only: list boards, or one board's top entries and the caller's standing).

## Screens

| Surface | Route | Contents |
|---|---|---|
| Web | `/leaderboard` (nav, home tile) | board tabs (`?board`), the caller's rank card, ranked list with LoadMore |
| Web | `/settings/leaderboard` (Settings) | how you appear: alias, name and photo, or hidden |
| Mobile | `/leaderboard` (home entry), `/settings/leaderboard` (Settings) | the same |

## Config

| Variable | Default | |
|---|---|---|
| `LEADERBOARD_DEFAULT_VISIBILITY` | `alias` | how people appear until they choose: `alias`, `name` or `hidden` |

## Compliance

- Deletion removes the person's profile and scores; export returns them.
- Names and photos are shown only to signed-in people and only when the person chose to; the default is an alias.
- No UGC (aliases are generated from fixed word lists).

## Stitching

1. Decide the boards. From app code (an `init()` in `backend/internal/api/stitch_leaderboard.go`):
   ```go
   leaderboard.RegisterBoard(leaderboard.Board{ID: "points", Title: "Points", Format: leaderboard.FormatNumber,
   	Scores: func(ctx context.Context) (map[uuid.UUID]decimal.Decimal, error) { /* every ranked person */ }})
   ```
   or push-only (no `Scores`): call `leaderboard.SetScore(ctx, tx, "points", userID, value)` where the score changes. A module can implement `Source` (`LeaderboardBoard() (id, title, unit string)` and `LeaderboardScores(ctx)`) instead; `unit` is `""`, `"%"` or a currency code.
2. Decide the default visibility (`LEADERBOARD_DEFAULT_VISIBILITY`); `alias` keeps people anonymous until they opt in.
3. Place the entry: the nav item and home tile ship; move or drop them in the registries if the board belongs on another screen.

## Extending

- Seams: `Board`, `RegisterBoard`, `Source`, `SetScore`, `RemoveScore`; formats `number`, `money`, `percent` (`formatLeaderboardScore` in shared).
- Ranks are computed only by the refresh job; don't write `rank` elsewhere.

## Provenance

Generalised from MOX's PnL leaderboard (engine `Leaderboard` and the web leaderboard page): boards and their scores come from the app instead of the engine, people are scaffold users with an alias or their name by choice instead of public handles, ranks are stored and paged instead of recomputed per request.

## Changes

- 1.0.0: first version.
