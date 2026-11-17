"use client";

import { isApiError, useAgentWebEndpoints, useAgentWebTools } from "@scaffoldapp/shared";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef } from "react";

// WebMCP: tools a browser agent calls on the open page (document.modelContext,
// https://webmachinelearning.github.io/webmcp/). Mounted while signed in. Site tools
// read pages as Markdown and navigate; the app's read tools come from every feature's
// app.ToolProvider (the same tools /mcp serves) and run as the signed-in person through
// /v1/agent-web/tools. Nothing registered here changes data.

type Json = Record<string, unknown>;

interface WebTool {
  name: string;
  description: string;
  inputSchema: Json;
  annotations?: { readOnlyHint?: boolean; untrustedContentHint?: boolean };
  execute: (args: Json, ctx?: { signal?: AbortSignal }) => Promise<string>;
}

interface ModelContext {
  registerTool: (tool: WebTool, options?: { signal?: AbortSignal }) => unknown;
}

function modelContext(): ModelContext | null {
  if (typeof window === "undefined") return null;
  const mc =
    (document as unknown as { modelContext?: ModelContext }).modelContext ?? (navigator as unknown as { modelContext?: ModelContext }).modelContext;
  return mc && typeof mc.registerTool === "function" ? mc : null;
}

/** Objects are the contract; tolerate a JSON string from older agents. */
function argsOf(a: unknown): Json {
  if (typeof a === "string") {
    try {
      const v: unknown = JSON.parse(a);
      return v && typeof v === "object" ? (v as Json) : {};
    } catch {
      return {};
    }
  }
  return a && typeof a === "object" ? (a as Json) : {};
}

/** A same-origin app path, or null ("/x", never "//host" or a URL). */
function appPath(v: unknown, fallback: string): string | null {
  const p = typeof v === "string" && v.trim() ? v.trim() : fallback;
  if (!p.startsWith("/") || p.startsWith("//") || p.includes("\\")) return null;
  return p;
}

function register(mc: ModelContext, tool: WebTool, signal: AbortSignal) {
  try {
    const res = mc.registerTool(tool, { signal }) as { unregister?: () => void } | Promise<unknown> | undefined;
    // Early implementations returned a handle instead of honouring the signal.
    if (res && typeof (res as { unregister?: () => void }).unregister === "function") {
      signal.addEventListener("abort", () => (res as { unregister: () => void }).unregister());
    } else if (res && typeof (res as Promise<unknown>).catch === "function") {
      (res as Promise<unknown>).catch(() => {});
    }
  } catch (e) {
    if (process.env.NODE_ENV !== "production") console.warn("[webmcp] registerTool failed", tool.name, e);
  }
}

/** Session mount: registers WebMCP tools while signed in, when the browser supports it. */
export function AgentWebWebMcp() {
  const router = useRouter();
  const pathname = usePathname();
  const nav = useRef({ router, pathname });
  nav.current = { router, pathname };
  const supported = typeof window !== "undefined" && modelContext() !== null;
  const tools = useAgentWebTools({ enabled: supported });
  const endpoints = useAgentWebEndpoints();
  const items = tools.data?.items;

  useEffect(() => {
    const mc = modelContext();
    if (!mc) return undefined;
    const controller = new AbortController();
    const site: WebTool[] = [
      {
        name: "site_current_page",
        description: "The page open in this tab: its path, full URL and title.",
        inputSchema: { type: "object", properties: {} },
        annotations: { readOnlyHint: true },
        execute: async () => JSON.stringify({ path: nav.current.pathname, url: window.location.href, title: document.title }),
      },
      {
        name: "site_list_pages",
        description: "This site's llms.txt: what the app is, its public pages (each readable as Markdown), the MCP endpoint and the tools agents can use.",
        inputSchema: { type: "object", properties: {} },
        annotations: { readOnlyHint: true },
        execute: async (_args, ctx) => {
          const res = await fetch("/llms.txt", { signal: ctx?.signal ?? null });
          return res.ok ? res.text() : `Couldn't load llms.txt (HTTP ${res.status}).`;
        },
      },
      {
        name: "site_read_page",
        description: "Read a public page of this site as Markdown. path is an app path such as /legal/terms; omit it for the open page. Signed-in screens have no Markdown version: use the app's read tools for their data.",
        inputSchema: { type: "object", properties: { path: { type: "string", description: "App path starting with /" } } },
        annotations: { readOnlyHint: true, untrustedContentHint: true },
        execute: async (raw, ctx) => {
          const path = appPath(argsOf(raw)["path"], nav.current.pathname);
          if (!path) return "path must be an app path starting with /.";
          const res = await fetch(`${path === "/" ? "/index" : path.replace(/\/+$/, "")}.md`, { headers: { Accept: "text/markdown" }, signal: ctx?.signal ?? null });
          const type = res.headers.get("content-type") ?? "";
          if (!res.ok || !type.includes("markdown")) return `No Markdown version of ${path}. Call site_list_pages for the pages that have one.`;
          return res.text();
        },
      },
      {
        name: "site_open_page",
        description: "Open an app path in this tab (same site only), e.g. /settings. Navigation only: it never submits anything.",
        inputSchema: { type: "object", properties: { path: { type: "string", description: "App path starting with /" } }, required: ["path"] },
        annotations: { readOnlyHint: true },
        execute: async (raw) => {
          const path = appPath(argsOf(raw)["path"], "");
          if (!path) return "path must be an app path starting with /.";
          if (nav.current.pathname !== path) nav.current.router.push(path);
          return `Opened ${path}.`;
        },
      },
    ];
    const app: WebTool[] = (items ?? []).map((t) => ({
      name: t.name,
      description: t.description,
      inputSchema: t.input_schema,
      annotations: { readOnlyHint: true, untrustedContentHint: true },
      execute: async (raw) => {
        try {
          const r = await endpoints.call(t.name, argsOf(raw));
          if (r.status === "suggested") return `Sent to the person as a suggestion (${r.suggestion_id ?? "pending"}); nothing ran.`;
          return JSON.stringify(r.result ?? null);
        } catch (e) {
          return isApiError(e) ? `${e.code}: ${e.message}` : "internal: The tool call failed. Try again.";
        }
      },
    }));
    for (const tool of [...site, ...app]) register(mc, tool, controller.signal);
    return () => controller.abort();
  }, [items, endpoints]);

  return null;
}
