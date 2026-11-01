# AI agent scope

## What it does

- People decide how far AI agents acting for them may go: apps connected over MCP (their OAuth grant or API key, the `mcp` feature) and the in-app assistant (`assistant`, with outside services from `connectors`). Levels: **Off** (agents can't see it), **Read** (the default), **Suggest** (each change waits for the person), **Edit** (changes run).
- A default, then per feature, per external service (`connectors:<id>`) and per item. An item's level passes down the access tree (a folder's Off hides everything inside); the nearest setting wins: item → nearest ancestor → feature → default. "Turn off all agent access" stops every agent at once and keeps the settings.
- An agent never exceeds its person: the scope only narrows what they can do, and agents never get sharing, permission, membership or credential changes at any level.
- At Suggest, a write becomes a suggestion: a platform notification ("Claude suggests: Edit “Q3 plan”") and a live toast with Review and Accept. The review shows a readable before/after diff, the target and the agent. Accept re-runs the tool as the person (their access now decides); a changed target asks before accepting; failures (tool gone, item deleted, access lost) are shown and kept. Accept or reject everything from one agent.
- The ⋮ menu on every feature and item has "AI agent access ▸ Default (Read) · Off · Read · Suggest · Edit", showing where the default comes from ("from folder Design"). Settings → AI agents has the rest; staff can turn a person's agent access off (audited).

## Data

| Table | Purpose |
|---|---|
| `agents_settings` | Per person: default level, own off switch, staff off switch (who, when). No row = read, on |
| `agents_feature_scopes` | Level per feature or `connectors:<id>`, with the service's name when set |
| `agents_resource_scopes` | Level per item (kind, id); title and app path for kinds outside access |
| `agents_clients` | Agents seen per person (via, client key, name, last seen, last suggestion push) |
| `agents_suggestions` | Deferred tool call (tool, feature, source, input, target), preview (summary, before, after, link), agent, status `pending · accepting · accepted · rejected · failed · expired`, result or error, expiry |

SQL: `agents_resolve(user, feature, kind, id, skip_self, ignore_pause)` (level and where it comes from), `agents_level(…)`, and `access_agent_cap(user, kind, id)` replacing access's stub (off → 0, read/suggest → view, edit → edit), so `access_level(…, via_agent)` and `access_visible(…, via_agent)` narrow lists and search.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET / PUT | `/v1/agents/settings` | user (PUT: active, 300/h) | Default, off switch, features with tool counts, counts, agents seen, installed agent features; `{default_level?, paused?}` |
| GET | `/v1/agents/services` | user | Connected external services (dynamic tools) and settings kept for disconnected ones |
| GET | `/v1/agents/scope?r=feature,kind,id` | user | 1–50 scopes: level, own setting, inherited level and source, the person's own access level |
| PUT | `/v1/agents/scope` | active, 300/h | `{feature, kind?, id?, level\|null, title?, link?}`; access items must be viewable (404 otherwise); 2,000 items max |
| GET / DELETE | `/v1/agents/scopes[/{id}]` | user (DELETE: active) | Items with their own setting (keyset), with live titles; remove one |
| GET | `/v1/agents/summary` | user | `{pending, paused}` for badges |
| GET | `/v1/agents/suggestions?status=&via=&client_key=` | user | Own suggestions, newest first (keyset) |
| GET | `/v1/agents/suggestions/{id}` | owner | One suggestion |
| POST | `/v1/agents/suggestions/{id}/accept` | owner, active, 600/h | `{force?}`; 409 `target_changed` (details: the target now), `suggestion_expired`, `suggestion_decided` |
| POST | `/v1/agents/suggestions/{id}/reject` | owner, active, 600/h | Discard |
| POST | `/v1/agents/suggestions/bulk` | active, 60/h | `{action: accept\|reject, via, client_key}`: up to 50 from one agent |
| GET | `/v1/admin/agents/stats`, `/v1/admin/agents/users/{id}` | support | Counts; one person's switches, counts and agents (no contents) |
| PUT | `/v1/admin/agents/users/{id}/paused` | admin, 120/h | `{paused, reason?}`, audited `agents.pause` / `agents.resume` |

Every route refuses agent calls (403 `agent_forbidden`). Realtime on `user:<id>`: `agents.suggestion` / `agents.suggestion_updated` `{suggestion}`, `agents.scopes_changed`. Notification kind `agents.suggestion` (push on; a burst from one agent pushes once per 2 minutes through a notifications delivery filter), link `/agents/suggestions/<id>`.

**Policy** (`app.AgentPolicy`, called by mcp and assistant): `FeatureLevel` (feature level and the highest item level on the kinds it works with: its own `<feature>.*` kinds plus other features' kinds its tools target, so Edit on one drive item lists a repos feature's write tools), `ResourceLevel`, `ResourceLevels` (a page in one query), `GateTool`. Reads run at Read or above (the target's level, else the feature's highest; spanning tools filter rows themselves; a target id is canonicalised, and a write naming an item by an id that isn't one is refused, `invalid_input`); writes run at Edit, become a suggestion at Suggest, are refused below (`agent_scope_read_only`, `agent_scope_off`). Refused at any level (`agent_forbidden`): writes of `access`, `mcp`, `connectors` (its own tools) and `agents`, and write tools whose names carry share, grant, revoke, permission, role, invite, member, owner, credential, token, secret, password, api key, oauth or visibility. Suggestions are refused for items the person can only view, over `AGENTS_MAX_PENDING` waiting or `AGENTS_SUGGESTIONS_PER_HOUR` (`suggestion_limit`).

**Agent tools** (read-only; none changes a scope): `agents_my_scopes` (levels; items at Off are only counted), `agents_get_suggestion`, `agents_list_suggestions` (the calling agent's own: status, summary, target, the error, and the id an accepted call returned, never its output, which ran as the person).

## Screens

- **Web** `/settings/agents`: off banner, Suggestions row (count), Default (segmented, ⓘ explains every level), Features and Connected services (level picker each, ⓘ tool counts), items with their own setting (link, level, remove), agents seen with links to Connected apps / API keys / Assistant / Connections when those exist, Turn off (confirm). `/agents/suggestions`: Waiting (grouped by agent, Accept all / Reject all), Accepted, Rejected, Failed tabs. `/agents/suggestions/<id>`: facts, outcome banner, diff or input, Open, Accept / Reject, changed-target review. `/admin/agents`: counts, person lookup, audited off switch. ⋮ provider; session mount: live toast (Review, Accept) that takes over the notifications toast, keeps the count warm.
- **Mobile** Settings → AI agents (Agent access, Suggestions with badge): the same settings with level sheets; `/agents/suggestions` and `/agents/suggestions/[id]`; ⋮ sheet provider. The notifications feature's foreground toast opens a suggestion.
- Every async view has skeleton, empty and error states.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `AGENTS_SUGGESTION_TTL` | `168h` | Waiting suggestions expire after this (1h–2160h) |
| `AGENTS_MAX_PENDING` | `50` | Waiting suggestions per person |
| `AGENTS_SUGGESTIONS_PER_HOUR` | `120` | Suggestions per person per hour |
| `AGENTS_SUGGESTION_RETENTION` | `2160h` | Decided suggestions are deleted after this (≥ 24h) |

## Compliance

- Account deletion deletes settings, overrides, agents seen and suggestions (with their inputs). Export includes all of them.
- Suggestions hold what an agent proposed to change (input, before/after text) until decided and for the retention after: name it in the privacy policy.
- Staff pauses go to the audit log; people's own changes don't.

## Stitching

1. Put a ⋮ `ResourceMenu` on every feature section and item screen (feature; kind + id for an item; `connectors:<id>` for a service). The AI agent access row appears there by itself.
2. Give every write tool a `Target` and a `Preview` (summary, before/after). A `Target` returns its kind even when the input has no id: agents call it once with `{}` to learn which kinds a feature acts on; declare kinds it can't report with `agents.SetFeatureKinds(feature, kinds...)` (repos targets `repos.repository`, which sits under a drive item: `SetFeatureKinds("repos", "files.item")` lets a folder's Edit list its write tools). Without a Preview the review shows the tool name and input. The forbidden-word rule reads tool names only: a generic write ("update field") must refuse arguments that change access itself (owner, team, parent, visibility), as `crm_update_field` does.
3. Features whose rows aren't access resources drop rows from agent reads with `app.AgentReadable` / `app.AgentAllows` (only `FeatureLevel` max < Read may skip the per-row check). Access-backed queries pass `access.ViaAgent(ctx)` to `access_level` / `access_visible`.
4. Turn on `ASSISTANT_ALLOW_WRITES` for Suggest and Edit to matter in the assistant; MCP connections need the write scope. Outside-service writes still ask in the assistant even at Edit.
5. Name modules that don't read well capitalised with `agents.SetFeatureLabel` and forbid writes the word rule misses with `agents.ForbidTool`. Settings list content features only: `mcp`, `moderation`, `notifications` and `search` are hidden (they keep the default level; a person's own setting on one lists it again). Adjust with `agents.HideFeature` / `agents.ShowFeature`.
6. Choose the TTL, pending cap and hourly limit; mention suggestions in the privacy policy.

## Extending

- Scopes are per person, not per org: item settings follow the item's org naturally; feature settings apply in every org. Per-org feature levels would need an `org_id` column and a resolution step.
- A scope follows the access tree whether or not an item inherits sharing; kinds outside access get direct overrides only. A feature's ⋮ menu may set items of the kinds its tools target (a repository that is a drive item).
- Cross-kind tools: an item's own level (or an ancestor's) applies whichever feature acts on it. A feature-level setting gates that feature's tools, but access's SQL cap (`access_agent_cap`) knows only the item's kind, so it applies the kind's own feature level: "Repositories: Edit" lets the repos tools run, while the drive item's access check still uses the Files level. Give the repository its own Edit (⋮ menu) or set Files too.
- Accept compares the preview's before text to detect a changed target; tools whose Preview has no before text can't detect it.
- Out of scope: per-agent levels (all agents of a person share the scope), scheduled access windows, editing a suggestion before accepting.

## Provenance

New for the scaffold belt (no earlier project had agent scopes). Built on the access feature's agent seam, the mcp and assistant gate integration, and the notifications delivery filter. Verified with a real MCP API key, the assistant's mock provider, folder inheritance on access resources, search, and Playwright on web.

## Changes

- 1.1.0: Settings → AI agents lists content features only: infrastructure modules (`mcp`, `moderation`, `notifications`, `search`) are hidden and follow the default like any feature without its own setting; `agents.HideFeature` / `agents.ShowFeature` adjust the list. `agents_get_suggestion` and `agents_list_suggestions` no longer return an accepted call's output (computed as the person, it could name things past the agent's scope): status, summary, target, error and the returned item's id. The seeded suggestion seeds its tool's feature first, so its preview reads in the person's time zone.
- 1.0.2: `GateTool` canonicalises a target's UUID (upper case, braces, `urn:uuid:`), so respelling an id can't step around the item's own setting, and refuses a write whose target id isn't one (`invalid_input`) instead of falling back to the feature's level. Stitching notes for cross-kind features and generic update tools.
- 1.0.1: `FeatureLevel` counts item overrides on every kind a feature's tools target (learned from `Target` with an empty input, plus `SetFeatureKinds`), so Edit on one repository that is a drive item lists the repos write tools; a feature's ⋮ menu accepts those kinds.
- 1.0.0: first version.
