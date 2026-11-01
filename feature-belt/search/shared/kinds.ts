import type { SearchGroup, SearchResult } from "./types";

/**
 * Display names for result kinds, plural ("Messages"). Every kind the belt's features send
 * is listed; an app adds or renames with `registerSearchKindLabel` at module load. Unknown
 * kinds fall back to their last segment, capitalised and made plural.
 */
const labels: Record<string, string> = {
  "access.team": "Teams",
  "calendar.booking_link": "Booking links",
  "calendar.event": "Events",
  "calls.meeting": "Meetings",
  "channels.channel": "Channels",
  "channels.message": "Messages",
  "chat.conversation": "Conversations",
  "chat.message": "Messages",
  "crm.company": "Companies",
  "crm.contact": "Contacts",
  "crm.deal": "Deals",
  "docs.page": "Pages",
  "events.event": "Events",
  "feed.post": "Posts",
  "files.item": "Files",
  "orgs.org": "Organizations",
  "places.place": "Places",
  "repos.file": "Code",
  "repos.repository": "Repositories",
};

export function registerSearchKindLabel(kind: string, label: string): void {
  labels[kind] = label;
}

/** English plural of a lower-case noun phrase's last word: company → companies, branch → branches. */
function plural(words: string): string {
  if (/[^aeiou]y$/.test(words)) return `${words.slice(0, -1)}ies`;
  if (/(s|x|z|ch|sh)$/.test(words)) return `${words}es`;
  return `${words}s`;
}

export function searchKindLabel(kind: string): string {
  const known = labels[kind];
  if (known) return known;
  const last = kind.split(".").pop() ?? kind;
  const words = last.replace(/[_-]+/g, " ").trim().toLowerCase();
  if (!words) return "Results";
  const p = plural(words);
  return p.charAt(0).toUpperCase() + p.slice(1);
}

/** Groups hits by kind, groups ordered by their best hit (results arrive best first). */
export function groupSearchResults(items: readonly SearchResult[]): SearchGroup[] {
  const groups = new Map<string, SearchGroup>();
  for (const item of items) {
    let g = groups.get(item.kind);
    if (!g) {
      g = { kind: item.kind, label: searchKindLabel(item.kind), items: [] };
      groups.set(item.kind, g);
    }
    g.items.push(item);
  }
  return [...groups.values()];
}

/** Recent searches, newest first, without duplicates (case-insensitive), at most `max`. */
export function pushRecentSearch(list: readonly string[], query: string, max = 8): string[] {
  const q = query.trim().replace(/\s+/g, " ");
  if (!q) return [...list];
  return [q, ...list.filter((x) => x.toLowerCase() !== q.toLowerCase())].slice(0, max);
}

const providerLabels: Record<string, string> = {
  access: "Teams",
  calendar: "Calendar",
  calls: "Calls",
  channels: "Channels",
  chat: "Chat",
  crm: "CRM",
  docs: "Docs",
  events: "Events",
  feed: "Feed",
  files: "Files",
  mcp: "Connected apps",
  orgs: "Organizations",
  places: "Places",
  repos: "Repositories",
};

/** Names a feature (a provider's module) for people; register an app's own at module load. */
export function registerSearchProviderLabel(name: string, label: string): void {
  providerLabels[name] = label;
}

export function searchProviderLabel(name: string): string {
  return providerLabels[name] ?? name.charAt(0).toUpperCase() + name.slice(1);
}

/** The features that didn't answer, named for people, for a "results may be incomplete" notice. */
export function unansweredSearchProviders(providers: readonly { name: string; status: string }[]): string[] {
  return providers.filter((p) => p.status !== "ok").map((p) => searchProviderLabel(p.name));
}

/**
 * Where a hit lives, for its row: `context` without the org's name when the search is
 * scoped to that org (the org is already the scope), else all of it. "" when unknown.
 */
export function searchResultContext(item: SearchResult, scopedOrgId?: string | null): string {
  const ctx = item.context?.trim() ?? "";
  if (!ctx || !scopedOrgId || !item.org_id || item.org_id.toLowerCase() !== scopedOrgId.toLowerCase()) return ctx;
  const at = ctx.indexOf(" · ");
  return at < 0 ? "" : ctx.slice(at + 3);
}
