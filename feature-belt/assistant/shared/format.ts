import type { AssistantContextKind, AssistantContextRef, AssistantContextRequest, AssistantConversation, AssistantMessage, AssistantUsage } from "./types";

/** Where the user reviews a suggestion their AI agent settings made of a change (the agents feature). */
export function assistantSuggestionHref(suggestionId: string): string {
  return `/agents/suggestions/${encodeURIComponent(suggestionId)}`;
}

/** Pretty JSON of a tool input for approval (the complete input, as sent). */
export function assistantToolInputText(input: unknown): string {
  if (input === undefined || input === null) return "{}";
  try {
    return JSON.stringify(input, null, 2);
  } catch {
    return String(input);
  }
}

/** "assistant_get_usage" → "Get usage", "linear__create_issue" → "Create issue": the module or service prefix dropped, sentence case. */
export function assistantToolLabel(name: string): string {
  const words = name.split(/[_-]+/).filter(Boolean);
  const rest = words.length > 1 ? words.slice(1) : words;
  const s = rest.join(" ");
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : name;
}

export function assistantConversationTitle(c: Pick<AssistantConversation, "title"> | null | undefined): string {
  return c?.title.trim() || "New conversation";
}

/** Plain text of a message (copy, accessibility); attached resources aren't text. */
export function assistantMessageText(m: Pick<AssistantMessage, "parts">): string {
  return m.parts
    .filter((p) => p.type !== "context")
    .map((p) => p.text)
    .join("");
}

/** The resource a message attached, if any. */
export function assistantMessageContext(m: Pick<AssistantMessage, "parts">): AssistantContextRef | null {
  return m.parts.find((p) => p.type === "context")?.context ?? null;
}

/** Compact token count: 950, 12.3k, 1.2M. */
export function formatTokenCount(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return `${(n / 1000).toFixed(n < 10_000 ? 1 : 0).replace(/\.0$/, "")}k`;
  return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, "")}M`;
}

/** Which daily limit is used up, if any (admins are unlimited). */
export function assistantLimitReached(u: AssistantUsage | null | undefined): "messages" | "tokens" | null {
  if (!u || u.unlimited) return null;
  if (u.message_limit > 0 && u.messages >= u.message_limit) return "messages";
  if (u.token_limit > 0 && u.tokens >= u.token_limit) return "tokens";
  return null;
}

/** User-facing line for a stop reason that changes how a reply reads. */
export function assistantStopNote(m: Pick<AssistantMessage, "status" | "stop_reason">): string | null {
  if (m.status === "cancelled") return "Stopped";
  if (m.stop_reason === "blocked") return "Reply withheld";
  return null;
}

/** In-context help: what a ⋮ menu attaches and how the assistant opens with it. */
export interface AssistantContextLaunch {
  context: AssistantContextRequest;
  /** `summarise` sends the summary prompt at once; `ask` attaches the resource and waits for a question. */
  mode: "summarise" | "ask";
  /** The message `summarise` sends. */
  prompt?: string;
  /** Kind label ("channel") for the chip. */
  label?: string;
}

/** The Summarise message for a kind and scope, with the title filled in ("this channel" without one). */
export function assistantContextPrompt(kind: AssistantContextKind, scopeId: string | undefined, title: string | undefined): string {
  const scope = kind.scopes.find((s) => s.id === scopeId) ?? kind.scopes[0];
  const t = title?.trim() || `this ${kind.label}`;
  return (scope?.prompt ?? kind.prompt).replace("{title}", t);
}

/** Query string that opens the assistant with a resource (`/assistant/new?…`; mobile routes take the same). */
export function assistantContextQuery(l: AssistantContextLaunch): string {
  const pairs: [string, string | undefined][] = [
    ["ctx_kind", l.context.kind],
    ["ctx_id", l.context.id],
    ["ctx_scope", l.context.scope],
    ["ctx_title", l.context.title?.slice(0, 120)],
    ["ctx_label", l.label],
    ["mode", l.mode],
    ["q", l.prompt?.slice(0, 2000)],
  ];
  return pairs
    .filter((p): p is [string, string] => !!p[1])
    .map(([k, v]) => `${k}=${encodeURIComponent(v)}`)
    .join("&");
}

/** Reads `assistantContextQuery` back (from search params); null without a resource. A bare `q` is an "ask" with no resource. */
export function parseAssistantContextQuery(get: (key: string) => string | null | undefined): { launch: AssistantContextLaunch | null; question: string | null } {
  const kind = get("ctx_kind") ?? "";
  const id = get("ctx_id") ?? "";
  const q = get("q")?.trim() || null;
  if (!/^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$/.test(kind) || !/^[A-Za-z0-9_.:-]{1,128}$/.test(id)) return { launch: null, question: q };
  const scope = get("ctx_scope") || undefined;
  const title = get("ctx_title") || undefined;
  const label = get("ctx_label") || undefined;
  const mode = get("mode") === "summarise" ? "summarise" : "ask";
  return {
    launch: { context: { kind, id, ...(scope ? { scope } : {}), ...(title ? { title } : {}) }, mode, ...(q ? { prompt: q } : {}), ...(label ? { label } : {}) },
    question: mode === "ask" ? q : null,
  };
}

/** "Summarise · #general (Unread messages)" for the chip on a sent message or the composer. */
export function assistantContextChipText(c: Pick<AssistantContextRef, "label" | "title" | "scope_label">): string {
  const label = c.label ? c.label.charAt(0).toUpperCase() + c.label.slice(1) : "";
  return [label, c.title].filter(Boolean).join(": ") + (c.scope_label ? ` · ${c.scope_label}` : "");
}
