import { NextResponse, type NextRequest } from "next/server";
import { findAgentWebPage, wantsMarkdown } from "./match";

/** Where twins are served; the proxy rewrites to it, so the visible URL stays the page's. */
export const AGENT_WEB_MD_PREFIX = "/md";

/**
 * A `PROXY_HANDLERS` entry (web/proxy.ts): `<page>.md`, and any public page requested
 * with `Accept: text/markdown`, is rewritten to its Markdown twin. Paths without a twin
 * pass through untouched, so private pages are never answered here.
 */
export function agentWebProxy(req: NextRequest): NextResponse | null {
  const { pathname, search } = req.nextUrl;
  if (pathname === AGENT_WEB_MD_PREFIX || pathname.startsWith(`${AGENT_WEB_MD_PREFIX}/`)) return null;
  let path: string;
  if (pathname.endsWith(".md")) {
    path = pathname.slice(0, -3);
    if (path === "" || path === "/index") path = "/";
  } else if (wantsMarkdown(req.headers.get("accept"))) {
    path = pathname;
  } else {
    return null;
  }
  if (!findAgentWebPage(path)) return null;
  const target = `${AGENT_WEB_MD_PREFIX}${path === "/" ? "" : path}${search}`;
  return NextResponse.rewrite(new URL(target, req.url));
}
