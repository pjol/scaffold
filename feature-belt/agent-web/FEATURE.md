# Agent-readable web

## What it does

- **`/llms.txt`** ([llmstxt.org](https://llmstxt.org)): the app in one screen for agents: what it is, a link to the Markdown of every public page, the MCP endpoint and server card (when the `mcp` feature is installed), every agent tool by name, and how to reach the rest. **`/llms-full.txt`** adds each page in full.
- A **Markdown twin of every public page**: `<path>.md` (`/legal/terms.md`, `/index.md` for `/`), or the page's own URL with `Accept: text/markdown` ranked at least as high as `text/html`. Browsers keep getting HTML; paths without a twin pass through untouched, so signed-in screens are never answered here.
- **WebMCP** on signed-in pages: when the browser exposes `document.modelContext`, the page registers tools a browser agent calls on the open tab: `site_current_page`, `site_list_pages` (llms.txt), `site_read_page` (a twin), `site_open_page` (same-site navigation), and every **read-only** `app.ToolProvider` tool the person's AI agent scope allows, run as them. Nothing registered here changes data; write tools stay with `/mcp` and the page's own controls.
- No duplicate of the `mcp` feature: tools come from the same `app.ToolProvider`s, calls are marked `auth.Agent{Via: "webmcp"}` and gated by the installed `app.AgentPolicy` exactly like `/mcp` calls, and llms.txt points agents at `/mcp` for everything else.
- Web only. Mobile has no pages for agents to read.

## Data

None (no tables; the migration block 3100 is reserved).

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/agent-web/manifest` | public | `{api_url, mcp: {endpoint, server_card} or null, webmcp, tools: [{name, description (first sentence), read_only, feature}]}` for llms.txt |
| GET | `/v1/agent-web/tools` | user | Read tools for this person's browser agent: `{items: [{name, description, input_schema, feature}]}`; features their agent scope turns off are left out |
| POST | `/v1/agent-web/tools/{name}` | user, `AGENT_WEB_TOOL_CALLS_PER_MINUTE`, consent-exempt (reads) | `{arguments}` → `{status: "ok", result}` or `{status: "suggested", suggestion_id}`; 400 `invalid_input` (JSON Schema), 404 `unknown_tool` (no such read tool; write tools are never served), 403 with the agent policy's code, 504 `timeout` (15 s), or the tool's own error |

Web routes (Next, public): `/llms.txt`, `/llms-full.txt`, `/md/[[...path]]` (the twins; the proxy rewrites to it, so the visible URL stays the page's). Twins answer `text/markdown` with `Vary: Accept`, cached at the CDN for each page's `maxAge`; an unknown twin path is a Markdown 404.

## Screens

- None of its own. `web/src/features/agent-web/WebMcp.tsx` is a session mount (renders nothing) wired at `scaffold:session`.
- Default twins (`web/src/features/agent-web/pages.ts`): `/` (the app and its pages), `/legal/:doc` (Terms, Privacy, from the bundled documents), `/support` (support email from `/v1/config`), `/delete-account` (steps, what is deleted and kept).

## Config

| Variable | Default | Purpose |
|---|---|---|
| `AGENT_WEB_WEBMCP_ENABLED` | `true` | Offer read tools to browser agents; `false` leaves llms.txt and the twins |
| `AGENT_WEB_TOOL_CALLS_PER_MINUTE` | `60` | WebMCP tool calls per person per minute |

Web: no variables. Twins read the API at `NEXT_PUBLIC_API_URL` from the server, without credentials.

## Compliance

- Stores nothing. Twins render only what signed-out visitors can already see; WebMCP calls run as the signed-in person with their permissions and agent scope, read-only, rate limited.
- Tool results are marked `untrustedContentHint` (they can carry other people's text).

## Stitching

1. Rewrite `web/src/features/agent-web/site.ts`: a one-sentence summary, an `about` paragraph (who it's for, units, limits, what's demo data) and any conventions agents must know.
2. Give every public page a twin in `pages.ts` (each entry of `proxy.ts` `PUBLIC_PATHS` except sign-in): its pattern, title, one-line summary, `examples` for patterns with params, and a `render` that builds Markdown with `md.ts` from bundled content or the public API (`agentWebApi`). Features with public pages need theirs too (calendar booking pages, published docs, the mcp `/connect` page if you want it read).
3. If the app's data is public (the oracle's prices with `ORACLE_PUBLIC_READS`), consider a public page plus a twin for it: agents then read it without an account.
4. Write prescriptive descriptions on every read tool (`app.ToolProvider`): WebMCP agents see exactly those.
5. Projects made before 2026-11-16: add the `scaffold:imports` and `scaffold:proxy` anchors and the `PROXY_HANDLERS` loop to `web/proxy.ts` from the template before installing.

## Extending

- Twins: one entry per page in `pages.ts`; `match.ts` matches `:param` segments and ranks `Accept`; `server.ts` has `agentWebApi`, `agentWebMarkdown`, `agentWebText`, `agentWebOrigin`. Keep renderers free of credentials: they run for anyone.
- WebMCP tools beyond the app's read tools (prefilling a form for the person to submit, as MOX's `prepare_order` does) belong in the feature that owns the form, registered the same way (`document.modelContext.registerTool` with an abort signal), and never submit.
- Don't serve write tools over WebMCP: a browser agent changing data goes through `/mcp` with its own credential, or through the page where the person acts.

## Provenance

Distilled from MOX (`web/lib/md.ts`, `web/app/llms.txt`, `web/app/llms-full.txt`, `web/app/md/*`, `web/proxy.ts`, `web/components/live/WebMcp.tsx`). Generalised: per-page renderers became a registry of public-page twins; the hard-coded market tools became the app's own `app.ToolProvider` read tools served through `/v1/agent-web/tools` under the agent-scope gate; the proxy handler joins the template's `PROXY_HANDLERS` instead of replacing the proxy. MOX's order-ticket tools (`prepare_order`, `cancel_order`) stay with their product: WebMCP here never changes data.

## Changes

- 1.0.0: first version.
