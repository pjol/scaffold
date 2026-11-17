# Moderation

## What it does

- Anyone signed in reports a person or any registered content (chat message, post, comment, AI reply) with a reason (spam, harassment, hate, sexual, violence, self_harm, illegal, other) and a note; the content is captured at report time, reports on one target collect into one case, and the dialog can block the author in the same step.
- Block anyone: their content disappears for both people and messages stop in both directions.
- Chat-style text is checked before posting (`Check`: blocked terms that ignore case, accents and swaps like `h3ll0`, plus optional OpenAI). Posts and comments are screened (`Screen`): rules-v1 refuse, label, hold or remove them, then an optional decision model can only escalate. Held items stay visible to their author (with the reason) and staff until reviewed.
- Staff work one queue of reported and screened cases, oldest first against a 24-hour target: approve or dismiss, remove, warn, suspend, ban, resolve. Screening's decisions (rules hit, model answers) show on the case. Reporters hear only that their report was reviewed.
- A staff removal or ban is a strike; strikes count 90 days and stop posting for a while at set counts (3 → 24 h, 5 → 7 days). Settings → Safety shows strikes, held and removed content, blocked people and reports.
- Optional auto-hide once N different people report something, until staff decide.

## Data

| Table | Purpose |
|---|---|
| `moderation_blocks` | blocker → blocked, PK on the pair, reverse index for either-direction checks |
| `moderation_cases` | one per target while open: snapshot, `source` report or auto (screening), `categories`, distinct reporters, hidden state, staff action and note |
| `moderation_reports` | one per reporter per case: reason, note, the snapshot that reporter saw |
| `moderation_decisions` | every screening of an item (refusals included): rules version, rules and final action, hits `{rule, action, category, detail, reason}`, labels, model, status, answers, distrusted, latency, cost, text hash (repeat rules) |
| `moderation_strikes` | per person: case, action (remove, ban), category, staff, revoked |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| POST | `/v1/moderation/reports` | user, 20/h, consent-exempt | `{target_kind, target_id, reason, note}` (`"user"` reports a person); 201 new, 200 updated |
| GET | `/v1/moderation/reports` | user | your reports, outcome open, actioned or closed |
| GET / POST | `/v1/moderation/blocks` | user; POST 60/h, consent-exempt | people you blocked / `{user_id}` (idempotent) |
| GET | `/v1/moderation/blocks/ids` | user | `{user_ids}` you blocked (never who blocked you) |
| DELETE | `/v1/moderation/blocks/{user_id}` | user | unblock |
| GET | `/v1/moderation/content` | user | your content screening held or staff removed: state in_review / removed / approved, user-safe reasons |
| GET | `/v1/moderation/standing` | user | strikes counting now, `restricted_until`, the ladder |
| GET | `/v1/admin/moderation/summary` | support | open, over SLA, held by screening, closing times, `decision_model` health |
| GET | `/v1/admin/moderation/cases` | support | `?status=open\|resolved\|dismissed\|closed&kind=&author_id=` |
| GET | `/v1/admin/moderation/cases/{id}` | support | case, reports, screening `decisions`, author history with strikes |
| POST | `/v1/admin/moderation/cases/{id}/actions` | support | `{action, note, remove_content, notify_reporters}`; audited |
| POST | `/v1/admin/moderation/decision-model/reset` | admin | turn a model that stopped itself back on; audited |

Actions: `dismiss` (Approve on held content: the Target's `Restore`), `remove` (`Target.Remove`; held content too), `warn`, `suspend`, `ban` (only admins on staff, nobody on themselves; on held content they need `remove_content`), `resolve` (not for held content). Removals and bans add a strike. Audit: `moderation.case.<action>`, `user.suspend`, `user.ban`.

Realtime: `moderation.case_opened` / `case_closed` on `admin:live`; `moderation.blocks_changed` on the blocker's `user:` topic. Notifications `moderation.outcome` (reporters) and `moderation.notice` (authors: removed, approved, warned, suspended, posting paused). Agent tools (read-only): `moderation_list_blocks`, `moderation_list_reports`, `moderation_my_standing`, `moderation_queue_summary` (staff). Agents never block.

### Screening

Rules-v1 (`rules.go`), on title, body and link, with facts from moderation's own data (account age, staff role, repeats by text hash, holds and strikes in 90 days) and the feature's `Item.Karma`: `blocked_term` refuses (422 `content_rejected`); `unsafe_link` removes IP-literal and numeric hosts, denylisted domains, punycode or Unicode look-alikes and visual copies of the app's domains; `near_brand_link`, `link_shortener`, `new_account_links` (under 3 days and karma under 10), `many_links` (over 3), `credential_ask`, `wallet_address`, `off_platform_contact` (under 7 days), `repeat_accounts` (3 or more other accounts in 24 h), `mention_flood` (over 10), `staff_impersonation` ("I'm from <app> support" by non-staff) and the optional classifier hold; `repeat_self` (third copy in 24 h) removes. Order: allow < label < hold < remove.

Decision model (optional, `MODERATION_DECISION_URL`): one request in the decisions format with a state of short strings (the place's description, item kind, links described, account age, karma, history, repetition and rule flags in words) where user text appears only in sanitised, clipped, ⟦delimited⟧ `title_quoted`, `text_quoted`, `parent_quoted`; yes/no questions spam, scam, harassment, manipulation, on_topic, addresses_moderator. Thresholds: spam hold 0.70 / remove 0.92, scam 0.60 / 0.85, harassment 0.65 / 0.90, manipulation hold 0.75 (never removes), on_topic ≤ 0.15 labels `off_topic`, addresses_moderator ≥ 0.60 holds and marks the other answers distrusted. Final = max(rules, model). The client spaces requests (`MAX_RPS`, gives up when no slot frees within half the timeout), times out, stops for the UTC day at the cost cap, pauses on 429/529 for Retry-After, and turns off on 401/402/403 until reset or restart; every failure keeps the rules' decision and is recorded as the decision's `model_status`.

## Screens

| Where | Web | Mobile |
|---|---|---|
| Settings → Safety | `/settings/safety`: strikes (and when posting resumes), held and removed content with reasons, blocked people, your reports | `settings/safety`: the same |
| Admin → Moderation | `/admin/moderation`: queue health with held count and decision-model health (Turn back on for admins), Open/Closed; `/admin/moderation/[id]`: content, Screening (hits, answers, model, latency), reports, author history and strikes, Approve/Remove for held content | none |
| Anywhere | `ReportDialog`, `ReportButton`, `BlockDialog`, `BlockButton` | same names |

## Config

| Variable | Default | Purpose |
|---|---|---|
| `MODERATION_BLOCKED_TERMS` | empty | comma-separated terms refused before posting |
| `MODERATION_PROVIDER` / `OPENAI_API_KEY` | `none` | `openai` adds a classifier: `Check` refuses what it flags, `Screen` holds it |
| `MODERATION_AUTOHIDE_REPORTS` | `0` | distinct reporters that hide content; 0 = off |
| `MODERATION_NOTIFY_REPORTER` | `true` | tell reporters their report was reviewed |
| `MODERATION_LINK_DENYLIST` | empty | domains whose links are removed |
| `MODERATION_BRAND_DOMAINS` | empty | the app's other domains (PUBLIC_WEB_URL's is added); look-alikes are unsafe |
| `MODERATION_STRIKE_LIMITS` | `3:24h,5:168h` | strikes:pause steps; `off` disables |
| `MODERATION_DECISION_URL`, `_API_KEY`, `_MODEL` | empty | decision model; off when the URL is unset (a URL without a key fails the boot) |
| `MODERATION_DECISION_MAX_RPS`, `_TIMEOUT`, `_DAILY_USD` | `5`, `3s`, `5` | request rate, per-request limit (posting waits for it), daily cost cap per instance (0 = none) |

## Compliance

- Apple 1.2 / Play UGC: filter and screen before posting, report content and people, block both ways, a staffed queue with a 24-hour target, AI output reportable.
- Deletion: their reports stay without `reporter_id`; cases about their content keep their shape with text scrubbed and open ones close; blocks and strikes are deleted; screening decisions stay without author, text hash or matched detail.
- Export: reports filed, blocks, strikes, and the screening decisions that labelled, held or removed their content.
- The decision model and OpenAI see post text: name the service in the privacy policy before turning either on.

## Stitching

1. Write the community rules and link them from the Terms; review the reason labels in `shared/types.ts`.
2. Choose `MODERATION_AUTOHIDE_REPORTS`, `MODERATION_STRIKE_LIMITS`, `MODERATION_BRAND_DOMAINS` and `MODERATION_LINK_DENYLIST`; fill `MODERATION_BLOCKED_TERMS`.
3. Name who works the queue (held items and reports) and how they hear of new cases (`admin:live`).
4. Optionally set the decision model (`MODERATION_DECISION_URL`, `_API_KEY`, `_MODEL`) and watch its health and cost in Admin → Moderation.
5. Add report and block entry points wherever no feature covers them (profiles, avatars, member lists).

## Extending

```go
moderation.IsBlocked(ctx, q, a, b) / BlockedIDs(ctx, q, viewer) // contract.go, unchanged
moderation.Check(ctx, text)                                     // chat-style yes/no
moderation.RegisterTarget("forum.post", t)                      // Snapshot + idempotent Remove (+ Restorer, Changer)
d, err := moderation.Screen(ctx, pool, moderation.Item{Kind, ID, AuthorID, Title, Body, URL, Parent, Place, Link, Karma, Edit})
// store the item with d.Status() and d.Labels, then in the same transaction:
moderation.Record(ctx, tx, d)                                   // decision row + auto case when held or removed
moderation.Strikes(ctx, q, userID, since) / RestrictedUntil / CheckPosting(ctx, q, userID)
moderation.UseDecider(d)                                        // another decision model; nil = rules only
```

Generate the item id before `Screen`; show the author `d.Outcome()`; an edit never lowers a held or removed status. `Changer.Changed(ctx, id, restored)` runs after staff remove or approve (realtime, held-back notifications). Don't change `contract.go`. UI components as before (web `onOpenChange`, mobile `onClose` sheets).

## Provenance

- 1.0: DOGPATCH reports and admin moderation (dedupe, DB-counted rate limit, suspend/ban rules), ASSEMBLY blocks and reports (fixed: reports never read, blocks not enforced everywhere), SFLuv bulletin auto-hide.
- 1.1: MOX `backend/internal/moderation` (rules-v1, links, sanitize, screen, service, cases) and `backend/internal/jev` on branch `worktree-wf_70d3fcd7-22d-1`. Generalised: brand lists from the app's domains instead of MOX's, staff from `admin_role`, karma from the feature (`Item.Karma`), questions without markets, positions or disclosures, the client speaks the decisions format to any URL. Changed: the model runs inline under its timeout instead of MOX's post-commit queue, so the author sees the final state at once and a Target needs no hold method; strikes come from staff removals only. Left out: appeals (authors contact support), the sweeper that turned unreviewed automated removals into strikes, community-scoped staff roles.
- Verified: unit tests (rules table, thresholds, combine never lowers, state quoting against injection, the HTTP client's cap, 429, 401/402/403, timeout), a generated project's build, typecheck and doctor, and the flows on a booted backend (screen, hold, approve, remove with strike, posting restriction, content and standing, export, deletion). Not verified: a real decision-model endpoint, `next build`, `expo export`, Playwright and devices.

## Changes

- 1.1.0: screening for UGC features (`Screen`, `Record`: rules-v1 and an optional decision model that only escalates, every decision recorded), held items in the queue as auto cases with Approve, strikes and posting restrictions (`Strikes`, `RestrictedUntil`, `CheckPosting`), Settings → Safety strikes and held content, case page screening details, decision-model health and reset. Migration `0201_moderation_screening.sql`. `Check` and `contract.go` unchanged.
- 1.0.1: the `moderation_block_user` agent tool is gone (no preview or rate limit, and a block changes who may reach the person, which AI agents never change).
- 1.0.0: first release (backend, shared hooks, web and mobile UI).
