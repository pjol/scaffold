# AI assistant

## What it does

- Users chat with an AI assistant on web and mobile. Replies stream in as they are written and keep running if the app closes or the connection drops; reopening the conversation resumes the stream. A reply can be stopped from any device.
- The assistant reads the user's own data through every installed feature's agent tools (`app.ToolProvider`), always as the signed-in user. Tools that change data are offered only with `ASSISTANT_ALLOW_WRITES=true`. Tool calls show as compact lines under the reply.
- Each turn it also gets the user's tools from outside services they connected (`app.DynamicToolProvider`, the `connectors` feature), labelled with the service in the UI. Their output is framed as third-party content, and every call that changes something in a service waits for the user to **Approve** or **Decline** it, with the complete input shown.
- With the `agents` feature installed, the user's AI agent scope applies to every tool: hidden features, read-only features, and writes turned into suggestions ("Suggested — waiting for your review", linking to the review).
- Before the first message the user allows sending their messages to the named AI provider (Apple 5.1.2(i)). The choice is stored and can be turned off in Settings → Assistant.
- Daily per-user limits on messages and tokens, with the reset time shown when reached. Usage and an estimated cost are recorded per reply; admins see daily totals.
- Users rename, delete and report replies (moderation queue). Replies render as Markdown that can't inject HTML.
- Provider-neutral: Anthropic Messages API (default) or OpenAI Responses API; a scripted mock in development only, labelled in the UI.

## Data

| Table | Purpose |
|---|---|
| `assistant_conversations` | Owner, title, compaction summary and the point it covers, latest prompt size |
| `assistant_messages` | Display `parts`, tool `activity`, replay `items` (neutral transcript with raw provider blocks, stored as `json` so bytes survive), status, usage, estimated cost, stop/removal marks |
| `assistant_usage_daily` | Per user per UTC day: messages, token counts, cost estimate |
| `assistant_consents` | User × provider consent with acceptance time |
| `assistant_settings` | Deployment values shared by every instance (the prompt canary) |
| `assistant_tool_decisions` | The user's approve/decline of an external write (message × call id); the instance running the turn waits on it |

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/assistant/status` | user | Provider, model, mock flag, consent, today's usage and limits |
| POST / DELETE | `/v1/assistant/consent` | user (POST: active) | `{provider}` must name the current provider / withdraw every consent |
| GET | `/v1/assistant/conversations?q=&cursor=` | user | Page, most recently active first (keyset on `last_message_at, id`) |
| POST | `/v1/assistant/conversations` | user, active, 30/min | `{title?}` → 201 |
| GET / PATCH / DELETE | `/v1/assistant/conversations/{id}` | owner (PATCH: active) | Read (`running` while a reply streams), rename `{title}`, delete with messages |
| GET | `/v1/assistant/conversations/{id}/messages?cursor=` | owner, stream group | Page, newest first. `Accept: text/event-stream` resumes the running reply |
| POST | `/v1/assistant/conversations/{id}/messages` | owner, active, 20/min, stream group | `{text}` → `text/event-stream` |
| POST | `/v1/assistant/conversations/{id}/stop` | owner | Stop the running reply on whichever instance runs it |
| POST | `/v1/assistant/conversations/{id}/tool-calls/{call_id}` | owner, active, 60/min | `{approved}` for a call awaiting approval in the running reply → 204; 404 `tool_call_not_found` when nothing waits |
| GET | `/v1/admin/assistant/usage?days=30` | support | Daily totals across users |

The two `messages` routes mount through `app.StreamRoutes` (auth, user rate limit and consent gate, no request timeout); a gone client is detected from failed writes and 15 s heartbeats. Errors before a stream opens are JSON: 503 `assistant_unconfigured`, 403 `ai_consent_required` (`details.provider`, `provider_label`), 429 `quota_exceeded` (`kind`, `limit`, `used`, `resets_at`), 422 `content_rejected`, 409 `turn_in_progress`, 413 `message_too_long`.

Stream events: `message` `{message}` (user message, then the assistant snapshot on connect and the final message), `delta` `{message_id, text}`, `tool_start` `{message_id, call_id, name, source?, input, started_at}`, `tool_approval` `{message_id, call_id, name, source, input, expires_at}`, `tool_decision` `{message_id, call_id, approved}`, `tool_end` `{…, status (ok|error|denied|suggested), summary, duration_ms, suggestion_id?}`, `error` `{message_id, code, message, retryable}`, `done` `{message_id, status, conversation}`. Moderation target: `assistant.message` (snapshot = reply + the prompt before it; removal hides the reply).

## Screens

- **Web** `/assistant` (list + new conversation; on phones the list), `/assistant/new`, `/assistant/<id>`: two panes at ≥ 1024 px, viewport-height thread with a sticky composer (Enter sends, Shift+Enter new line, Stop while streaming), tool activity lines (service badge, approval card with Approve/Decline, suggestion link), copy and report on each reply, retry on a failed reply, consent dialog before the first message, quota and mock banners, rename/delete menu. A new conversation is created on the first message and the URL moves with `history.replaceState`, so the stream isn't interrupted. `/settings/assistant`: provider, permission (allow / turn off), today's usage. `/admin/assistant`: daily table (7/30/90 days). Nav entry, Home tile, settings section and admin tab wire through the registry.
- **Mobile** Home row → `/assistant` (list, New conversation), `/assistant/[id]` (`new` starts one; inverted list, keyboard-aware composer, share and report per reply, approval cards, consent dialog, quota banner), `/settings/assistant`. Streaming uses `fetch` from `expo/fetch`.
- Every view has skeleton, empty and error states; the not-configured state replaces the thread.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `ASSISTANT_PROVIDER` | `anthropic` (dev-up: `mock`) | `anthropic`, `openai`, `mock`; `mock` outside development fails the boot (`config.Problem`) |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | — | Provider key; none → 503 `assistant_unconfigured` |
| `ANTHROPIC_BASE_URL` / `OPENAI_BASE_URL` | provider URL | Proxy override |
| `ASSISTANT_MODEL` / `ASSISTANT_TITLE_MODEL` | `claude-sonnet-5-5` / `claude-haiku-5-5` (OpenAI: `gpt-6-sol` / `gpt-6-luna`) | Chat model / titles and summaries |
| `ASSISTANT_EFFORT` | `low` | Reasoning effort |
| `ASSISTANT_ANTHROPIC_FALLBACK` | `true` | Server-side refusal fallback (`fallbacks: "default"`) |
| `ASSISTANT_ALLOW_WRITES` | `false` | Offer the app's own tools with `ReadOnly=false` (a ceiling: agent scopes only narrow it). External tools follow the user's per-connection switch and approval instead |
| `ASSISTANT_MAX_TOOL_ROUNDS` | `10` | Tool rounds before the model must answer |
| `ASSISTANT_CONTEXT_TOKENS` | `150000` | Compaction threshold |
| `ASSISTANT_DAILY_MESSAGES` / `ASSISTANT_DAILY_TOKENS` | `200` / `2000000` | Daily limits (0 = unlimited; admins unlimited) |
| `ASSISTANT_MAX_MESSAGE_CHARS` / `ASSISTANT_TURN_TIMEOUT` | `8000` / `5m` | Longest message / longest reply |

No new Go modules or npm packages (`expo/fetch` ships with Expo). Provider calls are plain HTTP + SSE.

## Compliance

- Consent per provider before any content leaves the server; Settings → Assistant shows and withdraws it. Name the provider in the privacy policy.
- Account deletion deletes conversations, messages, tool decisions, usage rows and consents (running replies are stopped). Export includes conversations with messages (tool lines name the service and outcome), usage and consents.
- Results from connected services are sent to the AI provider when the assistant uses them; the provider consent covers it. Say so in the privacy policy.
- Generative-AI UGC (Apple 1.2, Play): user text passes `moderation.Check`; every reply has Report, which feeds the moderation queue.
- Logs carry ids, token counts and latency, never prompts or message content.

## Stitching

1. Rewrite the product sections of `staticPrompt` (`backend/internal/assistant/prompt.go`); keep "Trust and safety".
2. Name the AI provider and what is sent to it in the privacy policy.
3. Decide `ASSISTANT_ALLOW_WRITES`. With the `agents` feature, turn it on and let each user's scope decide (their default is read only).
4. Set daily limits; add prices for unlisted models in `pricing.go`.
5. Give the features the assistant should use an `app.ToolProvider` with prescriptive descriptions.
6. Mobile: the assistant is a Home row; if it is central to the app, add a tab instead (`mobile/app/(tabs)`).

## Extending

- **Another provider**: implement `Provider` (`provider.go`) over the neutral `Message`/`Part` types; keep provider-native blocks in `Part.Raw` tagged with provider and model.
- **Tools** belong in the owning feature's `Tools()`. Names containing delete, send, email, fetch_url or browse are refused at startup; inputs are validated against `InputSchema` before the handler runs; output is capped at ~12k tokens and wrapped as untrusted data.
- **Per-caller tools** come from `app.DynamicToolProvider.ToolsFor` each turn (12 s budget, 128 at most). Set `Tool.Source` (the service name; it makes the tool external: no name rule, third-party framing of results and errors, approval for writes) and `Tool.Feature` (the agent-scope key; default the module name). A description that reads like instructions is withheld. Their input schemas are untrusted: validation stops after 20,000 schema nodes (`maxSchemaSteps`), so self-referencing `anyOf`/`oneOf` can't pin a CPU.
- **Agent scope**: with an `app.AgentPolicy` module, each turn hides tools by `FeatureLevel` (off: all; below suggest: writes) and every call runs `GateTool` under `auth.WithAgent(Via "assistant")`: run, refuse (the code becomes the tool error), or suggested (the model is told it waits for review; the UI links `/agents/suggestions/<id>`). External writes that may run still ask for approval.
- **Clients**: `useAssistantChat(id, { fetch })` owns streaming and writes events into the messages cache; `parseMarkdown` gives both apps the same safe tree.
- Don't move user text out of `<user_content>` fences, edit stored history in place (it breaks prompt caching and replayed thinking), or put per-request values in the static prompt.

## Provenance

Distilled from ChatMTG (`backend/internal/{llm,agent,chat}`, design §8): turn loop, guardrails (envelopes, fences, injection heuristic, Unicode hygiene, streaming canary scanner), progressive flush, cross-instance stop, stale sweep, titles, compaction. Changed on the way: provider-neutral transcript instead of Responses-API items; Anthropic Messages API added as the default (API version 2026-10-21, adaptive thinking, `block_binding: drop_block`, cache breakpoints); history replayed append-only and byte-identical (verified against a fake upstream) instead of reducing older turns every request; replay items stored as `json` because `jsonb` reordered keys; the canary lives in the database instead of a session secret; quotas count tokens and record cost; consent per provider; the client streaming parser decodes UTF-8 itself (Hermes has no TextDecoder).

**Unverified:** no live Anthropic or OpenAI call was made. The beta flags (`server-side-fallback-2026-07-01`, `thinking-binding-controls-2026-08-01`), the `fallbacks: "default"` parameter and the `signature_delta` event name come from documentation and memory and were tested only against a fake server. Run one real conversation with a key before launch.

## Changes

- 1.1.2: gofmt only; no behaviour change.
- 1.1.1: security review: input validation is bounded by a step budget (a ~100-byte remote schema with self-referencing `oneOf` meant ~4 billion node visits, still running after 10 s in a test, before approval and deaf to the turn's context); an external tool's error text gets the same third-party framing and injection scan as its results.
- 1.1.0: tools from every `DynamicToolProvider` merged per turn (connected services), labelled with their source, framed as third-party content, with an approval step (Approve/Decline, `assistant_tool_decisions`, `tool_approval`/`tool_decision` events) for external writes; AI agent scope (`app.AgentPolicy`) filters tools per turn and gates every call (suggestions link to their review); the mock calls any offered tool with `call <name> {json}`.
- 1.0.0: web and mobile screens, shared client; SSE routes moved to `app.StreamRoutes`; `config.Problem` for invalid provider settings.
- 0.1.0: backend.
