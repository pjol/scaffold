/**
 * Server-side helpers for the agent-readable routes: reading the API without a session
 * and answering with Markdown or plain text. Route handlers and page renderers only.
 */
import type { ClientConfig } from "@scaffoldapp/shared";
import type { AgentWebManifest } from "@scaffoldapp/shared";
import { API_URL } from "@/lib/env";

/**
 * GET an API path (`/v1/...`) without credentials and parse JSON; null on any failure,
 * so a twin can say what's unavailable instead of failing. Responses are cached for
 * `revalidate` seconds.
 */
export async function agentWebApi<T>(path: string, revalidate = 30): Promise<T | null> {
  if (!API_URL) return null;
  try {
    const res = await fetch(`${API_URL}${path}`, { headers: { Accept: "application/json" }, next: { revalidate }, signal: AbortSignal.timeout(5000) });
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

/** `GET /v1/config`: app name and support email. */
export const agentWebConfig = () => agentWebApi<ClientConfig>("/v1/config", 300);

/** `GET /v1/agent-web/manifest`: tools and the MCP endpoint, for /llms.txt. */
export const agentWebManifest = () => agentWebApi<AgentWebManifest>("/v1/agent-web/manifest", 300);

function cache(maxAge: number): string {
  return `public, max-age=0, s-maxage=${maxAge}, stale-while-revalidate=${maxAge * 4}`;
}

/** A Markdown response; `Vary: Accept` because the same URL also serves HTML. */
export function agentWebMarkdown(body: string, init: { status?: number; maxAge?: number } = {}): Response {
  return new Response(body, {
    status: init.status ?? 200,
    headers: {
      "Content-Type": "text/markdown; charset=utf-8",
      "Cache-Control": cache(init.maxAge ?? 60),
      Vary: "Accept",
      "X-Robots-Tag": "noindex",
    },
  });
}

/** A plain-text response (/llms.txt). */
export function agentWebText(body: string, maxAge = 300): Response {
  return new Response(body, { headers: { "Content-Type": "text/plain; charset=utf-8", "Cache-Control": cache(maxAge) } });
}

/** The site's origin as the visitor reached it (links in Markdown are absolute). */
export function agentWebOrigin(req: Request): string {
  const url = new URL(req.url);
  const host = req.headers.get("x-forwarded-host") ?? req.headers.get("host") ?? url.host;
  const proto = req.headers.get("x-forwarded-proto") ?? url.protocol.replace(/:$/, "");
  return `${proto.split(",")[0]?.trim() || "https"}://${host.split(",")[0]?.trim() || url.host}`;
}
