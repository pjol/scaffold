# AI assistant

## What it does

- Users chat with an AI assistant on web and mobile. Replies stream in as they are written and keep running if the app closes or the connection drops; reopening the conversation resumes the stream. A reply can be stopped from any device.
- The assistant reads the user's own data through every installed feature's agent tools (`app.ToolProvider`), always as the signed-in user. Tools that change data are offered only with `ASSISTANT_ALLOW_WRITES=true`. Tool calls show as compact lines under the reply.
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
| GET | `/v1/admin/assistant/usage?days=30` | support | Daily totals across users |

The two `messages` routes mount through `app.StreamRoutes` (auth, user rate limit and consent gate, no request timeout); a gone client is detected from failed writes and 15 s heartbeats. Errors before a stream opens are JSON: 503 `assistant_unconfigured`, 403 `ai_consent_required` (`details.provider`, `provider_label`), 429 `quota_exceeded` (`kind`, `limit`, `used`, `resets_at`), 422 `content_rejected`, 409 `turn_in_progress`, 413 `message_too_long`.

Stream events: `message` `{message}` (user message, then the assistant snapshot on connect and the final message), `delta` `{message_id, text}`, `tool_start` `{message_id, call_id, name, input, started_at}`, `tool_end` `{…, status, summary, duration_ms}`, `error` `{message_id, code, message, retryable}`, `done` `{message_id, status, conversation}`. Moderation target: `assistant.message` (snapshot = reply + the prompt before it; removal hides the reply).

## Screens

- **Web** `/assistant` (list + new conversation; on phones the list), `/assistant/new`, `/assistant/<id>`: two panes at ≥ 1024 px, viewport-height thread with a sticky composer (Enter sends, Shift+Enter new line, Stop while streaming), tool activity lines, copy and report on each reply, retry on a failed reply, consent dialog before the first message, quota and mock banners, rename/delete menu. A new conversation is created on the first message and the URL moves with `history.replaceState`, so the stream isn't interrupted. `/settings/assistant`: provider, permission (allow / turn off), today's usage. `/admin/assistant`: daily table (7/30/90 days). Nav entry, Home tile, settings section and admin tab wire through the registry.
- **Mobile** Home row → `/assistant` (list, New conversation), `/assistant/[id]` (`new` starts one; inverted list, keyboard-aware composer, share and report per reply, consent dialog, quota banner), `/settings/assistant`. Streaming uses `fetch` from `expo/fetch`.
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
| `ASSISTANT_ALLOW_WRITES` | `false` | Offer tools with `ReadOnly=false` |
| `ASSISTANT_MAX_TOOL_ROUNDS` | `10` | Tool rounds before the model must answer |
| `ASSISTANT_CONTEXT_TOKENS` | `150000` | Compaction threshold |
| `ASSISTANT_DAILY_MESSAGES` / `ASSISTANT_DAILY_TOKENS` | `200` / `2000000` | Daily limits (0 = unlimited; admins unlimited) |
| `ASSISTANT_MAX_MESSAGE_CHARS` / `ASSISTANT_TURN_TIMEOUT` | `8000` / `5m` | Longest message / longest reply |

No new Go modules or npm packages (`expo/fetch` ships with Expo). Provider calls are plain HTTP + SSE.

## Compliance

- Consent per provider before any content leaves the server; Settings → Assistant shows and withdraws it. Name the provider in the privacy policy.
- Account deletion deletes conversations, messages, usage rows and consents (running replies are stopped). Export includes conversations with messages, usage and consents.
- Generative-AI UGC (Apple 1.2, Play): user text passes `moderation.Check`; every reply has Report, which feeds the moderation queue.
- Logs carry ids, token counts and latency, never prompts or message content.

## Stitching

1. Rewrite the product sections of `staticPrompt` (`backend/internal/assistant/prompt.go`); keep "Trust and safety".
2. Name the AI provider and what is sent to it in the privacy policy.
3. Decide `ASSISTANT_ALLOW_WRITES`.
4. Set daily limits; add prices for unlisted models in `pricing.go`.
5. Give the features the assistant should use an `app.ToolProvider` with prescriptive descriptions.
6. Mobile: the assistant is a Home row; if it is central to the app, add a tab instead (`mobile/app/(tabs)`).

## Extending

- **Another provider**: implement `Provider` (`provider.go`) over the neutral `Message`/`Part` types; keep provider-native blocks in `Part.Raw` tagged with provider and model.
- **Tools** belong in the owning feature's `Tools()`. Names containing delete, send, email, fetch_url or browse are refused at startup; inputs are validated against `InputSchema` before the handler runs; output is capped at ~12k tokens and wrapped as untrusted data.
- **Clients**: `useAssistantChat(id, { fetch })` owns streaming and writes events into the messages cache; `parseMarkdown` gives both apps the same safe tree.
- Don't move user text out of `<user_content>` fences, edit stored history in place (it breaks prompt caching and replayed thinking), or put per-request values in the static prompt.

## Provenance

Distilled from ChatMTG (`backend/internal/{llm,agent,chat}`, design §8): turn loop, guardrails (envelopes, fences, injection heuristic, Unicode hygiene, streaming canary scanner), progressive flush, cross-instance stop, stale sweep, titles, compaction. Changed on the way: provider-neutral transcript instead of Responses-API items; Anthropic Messages API added as the default (API version 2026-10-21, adaptive thinking, `block_binding: drop_block`, cache breakpoints); history replayed append-only and byte-identical (verified against a fake upstream) instead of reducing older turns every request; replay items stored as `json` because `jsonb` reordered keys; the canary lives in the database instead of a session secret; quotas count tokens and record cost; consent per provider; the client streaming parser decodes UTF-8 itself (Hermes has no TextDecoder).

**Unverified:** no live Anthropic or OpenAI call was made. The beta flags (`server-side-fallback-2026-07-01`, `thinking-binding-controls-2026-08-01`), the `fallbacks: "default"` parameter and the `signature_delta` event name come from documentation and memory and were tested only against a fake server. Run one real conversation with a key before launch.

## Changes

- 1.0.0: web and mobile screens, shared client; SSE routes moved to `app.StreamRoutes`; `config.Problem` for invalid provider settings.
- 0.1.0: backend.
