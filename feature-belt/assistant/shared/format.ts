import type { AssistantConversation, AssistantMessage, AssistantUsage } from "./types";

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

/** Plain text of a message (copy, accessibility). */
export function assistantMessageText(m: Pick<AssistantMessage, "parts">): string {
  return m.parts.map((p) => p.text).join("");
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
