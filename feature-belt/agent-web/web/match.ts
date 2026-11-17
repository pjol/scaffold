/**
 * Which twin answers a path, and whether a request prefers Markdown. Pure (the proxy
 * and the /md route both use it).
 */
import { agentWebPages, type AgentWebPage } from "./pages";

/** Match a path against a pattern (`/legal/:doc`); params are decoded. */
export function matchAgentWebPath(pattern: string, path: string): Record<string, string> | null {
  const a = pattern.split("/").filter(Boolean);
  const b = path.split("/").filter(Boolean);
  if (a.length !== b.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < a.length; i++) {
    const seg = a[i] ?? "";
    const val = b[i] ?? "";
    if (seg.startsWith(":")) {
      try {
        params[seg.slice(1)] = decodeURIComponent(val);
      } catch {
        return null;
      }
    } else if (seg !== val) {
      return null;
    }
  }
  return params;
}

/** The twin for a path (trailing slashes ignored), with its params. */
export function findAgentWebPage(path: string): { page: AgentWebPage; params: Record<string, string> } | null {
  const p = path.replace(/\/+$/, "") || "/";
  for (const page of agentWebPages) {
    const params = matchAgentWebPath(page.path, p);
    if (params) return { page, params };
  }
  return null;
}

/** True when an Accept header ranks text/markdown at least as high as text/html. */
export function wantsMarkdown(accept: string | null): boolean {
  if (!accept || !accept.toLowerCase().includes("text/markdown")) return false;
  let md = 0;
  let html = 0;
  for (const part of accept.split(",")) {
    const [type = "", ...params] = part.trim().split(";");
    const qp = params.find((x) => x.trim().startsWith("q="));
    const q = qp ? Number(qp.trim().slice(2)) || 0 : 1;
    const t = type.trim().toLowerCase();
    if (t === "text/markdown") md = Math.max(md, q);
    else if (t === "text/html") html = Math.max(html, q);
  }
  return md > 0 && md >= html;
}
