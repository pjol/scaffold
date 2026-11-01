import { orgsNoun, orgsNounTitle } from "../orgs/noun";
import type { CallsAccess, CallsDeniedReason, CallsHistoryItem, CallsParticipantMeta, CallsPerson, CallsRole } from "./types";

/** `abc-defg-hij` from a code in any case, with or without dashes, or a join link. Null if it isn't one. */
export function parseCallsCode(input: string): string | null {
  let s = input.trim().toLowerCase();
  const slash = s.lastIndexOf("/");
  if (slash >= 0) s = s.slice(slash + 1);
  s = s.split("?")[0]?.split("#")[0]?.replace(/-/g, "") ?? "";
  if (!/^[a-z]{10}$/.test(s)) return null;
  return `${s.slice(0, 3)}-${s.slice(3, 7)}-${s.slice(7)}`;
}

/** "45 s", "34 min", "1 h 5 min". */
export function formatCallsDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  const rest = m % 60;
  return rest ? `${h} h ${rest} min` : `${h} h`;
}

/** "0:42", "12:05", "1:02:09": a running call timer. */
export function formatCallsElapsed(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = String(s % 60).padStart(2, "0");
  return h ? `${h}:${String(m).padStart(2, "0")}:${sec}` : `${m}:${sec}`;
}

/** Names for a list of people, the viewer left out: "Jordan", "Jordan and Priya", "Jordan and 3 others". */
export function callsPeopleNames(people: CallsPerson[], meId?: string | null): string {
  const names = people.filter((p) => !meId || p.user?.id !== meId).map((p) => p.name);
  if (names.length === 0) return "Just you";
  if (names.length === 1) return names[0] ?? "";
  if (names.length === 2) return `${names[0]} and ${names[1]}`;
  return `${names[0]} and ${names.length - 1} others`;
}

/** A history row's title: the meeting title, or "Call with …". */
export function callsHistoryTitle(item: CallsHistoryItem, meId?: string | null): string {
  if (item.meeting.title) return item.meeting.title;
  if (item.type === "missed" && item.caller) return `Call from ${item.caller.display_name}`;
  const others = item.participants.filter((p) => p.user?.id !== meId);
  return others.length ? `Call with ${callsPeopleNames(others, meId)}` : "Call";
}

// Getters: the org noun (`configureOrgsNoun`) is read when a label renders, not at import.
export const CALLS_ACCESS_LABELS: Record<CallsAccess, string> = {
  get org() {
    return `${orgsNounTitle()} only`;
  },
  invited: "Invited people",
  open: "Anyone with the link",
};

/** One-line explanations for the access picker (tooltips). */
export const CALLS_ACCESS_HELP: Record<CallsAccess, string> = {
  get org() {
    return `Members of the ${orgsNoun()} and invited people join. Nobody else can.`;
  },
  invited: "Invited people join. Others with an account can ask to join.",
  get open() {
    return `Invited people and ${orgsNoun()} members join. Anyone else with the link, guests too, asks to join while the lobby is on.`;
  },
};

export const CALLS_ROLE_LABELS: Record<CallsRole, string> = {
  host: "Host",
  cohost: "Co-host",
  invitee: "Invited",
  member: "Member",
  guest: "Guest",
};

export function callsDeniedMessage(reason: CallsDeniedReason | null | undefined): string {
  switch (reason) {
    case "org_only":
      return `Only members of this meeting's ${orgsNoun()} can join.`;
    case "invited_only":
      return "Only people invited to this meeting can join.";
    case "locked":
      return "The host locked this meeting.";
    case "removed":
      return "A host removed you from this call.";
    case "canceled":
      return "This meeting was canceled.";
    case "sign_in":
      return "Sign in to join this meeting.";
    default:
      return "You can't join this meeting.";
  }
}

/** The user id in a `u:<id>` identity (null for guests). */
export function callsIdentityUserId(identity: string): string | null {
  return identity.startsWith("u:") ? identity.slice(2) : null;
}

export function isCallsGuestIdentity(identity: string): boolean {
  return identity.startsWith("g:");
}

/** A participant's server-owned metadata (role, avatar). */
export function parseCallsParticipantMeta(metadata: string | undefined | null): CallsParticipantMeta {
  if (!metadata) return { role: "member" };
  try {
    const v = JSON.parse(metadata) as Partial<CallsParticipantMeta>;
    return { role: v.role ?? "member", ...(v.user_id ? { user_id: v.user_id } : {}), ...(v.avatar_url !== undefined ? { avatar_url: v.avatar_url } : {}) };
  } catch {
    return { role: "member" };
  }
}

export function isCallsHostRole(role: CallsRole | undefined): boolean {
  return role === "host" || role === "cohost";
}

/** When a participant raised their hand (ms), from the server-set `hand` attribute; null when lowered. */
export function callsHandRaisedAt(attributes: Readonly<Record<string, string>> | undefined): number | null {
  const v = attributes?.["hand"];
  if (!v) return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : null;
}

/** Reactions anyone in a call can send (data channel, not stored). */
export const CALLS_REACTIONS = ["👍", "❤️", "😂", "🎉", "👏", "😮"] as const;
export type CallsReaction = (typeof CALLS_REACTIONS)[number];
/** Data-channel topic for reactions. */
export const CALLS_REACTION_TOPIC = "calls.reaction";

/** One byte: the reaction's index (no text codecs, which Hermes lacks in part). */
export function encodeCallsReaction(emoji: CallsReaction): Uint8Array {
  return new Uint8Array([CALLS_REACTIONS.indexOf(emoji)]);
}

export function decodeCallsReaction(payload: Uint8Array): CallsReaction | null {
  if (payload.length !== 1) return null;
  return CALLS_REACTIONS[payload[0] ?? -1] ?? null;
}

/** The device's IANA time zone ("America/Los_Angeles"), or undefined when the runtime can't tell. */
export function callsDeviceZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}

// ───────────────────────────────────────────────────────── time zone seam ──

let appZone: string | null = null;
const zoneListeners = new Set<() => void>();

/**
 * Sets the zone Calls shows times in and sends with new meetings (an IANA name, e.g. the
 * person's calendar zone); null or an unknown name falls back to the device's. Call it from
 * the app's stitching whenever the zone becomes known or changes. Times read in a zone other
 * than the device's are labelled ("Today, 10:00 PM PST").
 */
export function setCallsTimeZone(zone: string | null | undefined): void {
  const next = zone && validCallsZone(zone) ? zone : null;
  if (next === appZone) return;
  appZone = next;
  for (const l of zoneListeners) l();
}

/** The zone Calls words times in: the app's (`setCallsTimeZone`), else the device's. */
export function callsTimeZone(): string | undefined {
  return appZone ?? callsDeviceZone();
}

/** Subscribe to `setCallsTimeZone` changes (for `useCallsTimeZone`). */
export function subscribeCallsTimeZone(listener: () => void): () => void {
  zoneListeners.add(listener);
  return () => zoneListeners.delete(listener);
}

function validCallsZone(timeZone: string): boolean {
  try {
    new Intl.DateTimeFormat("en-US", { timeZone });
    return true;
  } catch {
    return false;
  }
}

function zoneOption(timeZone: string | undefined): { timeZone?: string } {
  return timeZone && validCallsZone(timeZone) ? { timeZone } : {};
}

interface CallsWall {
  y: number;
  mo: number;
  d: number;
  h: number;
  mi: number;
}

const wallFormats = new Map<string, Intl.DateTimeFormat>();

function wallIn(ms: number, timeZone: string | undefined): CallsWall {
  const out: CallsWall = { y: 0, mo: 0, d: 0, h: 0, mi: 0 };
  const key = zoneOption(timeZone).timeZone ?? "";
  let f = wallFormats.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat("en-US", { ...(key ? { timeZone: key } : {}), hourCycle: "h23", year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric" });
    wallFormats.set(key, f);
  }
  for (const p of f.formatToParts(new Date(ms))) {
    const v = Number(p.value);
    if (p.type === "year") out.y = v;
    else if (p.type === "month") out.mo = v;
    else if (p.type === "day") out.d = v;
    else if (p.type === "hour") out.h = v === 24 ? 0 : v;
    else if (p.type === "minute") out.mi = v;
  }
  return out;
}

const wallUTC = (w: CallsWall) => Date.UTC(w.y, w.mo - 1, w.d, w.h, w.mi);

/** " PST" when `timeZone`'s clock differs from the device's at `ms`, else "". */
function zoneSuffix(ms: number, timeZone: string | undefined): string {
  if (!timeZone || !validCallsZone(timeZone)) return "";
  if (wallUTC(wallIn(ms, timeZone)) === wallUTC(wallIn(ms, undefined))) return "";
  try {
    const name = new Intl.DateTimeFormat(undefined, { timeZone, timeZoneName: "short" }).formatToParts(new Date(ms)).find((p) => p.type === "timeZoneName")?.value;
    return ` ${name || timeZone}`;
  } catch {
    return ` ${timeZone}`;
  }
}

/** "PST" for `timeZone` now when it differs from the device's, else null (labels for time fields). */
export function callsZoneLabel(timeZone: string | undefined = callsTimeZone(), at = Date.now()): string | null {
  return zoneSuffix(at, timeZone).trim() || null;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** The value for a `datetime-local` field from an ISO time, as a wall time in `timeZone` (default: Calls' zone). */
export function toCallsLocalInput(iso: string | null | undefined, timeZone: string | undefined = callsTimeZone()): string {
  const ms = iso ? Date.parse(iso) : Number.NaN;
  if (Number.isNaN(ms)) return "";
  const w = wallIn(ms, timeZone);
  return `${w.y}-${pad(w.mo)}-${pad(w.d)}T${pad(w.h)}:${pad(w.mi)}`;
}

/** An ISO time from a `datetime-local` value read as a wall time in `timeZone` (default: Calls' zone). */
export function fromCallsLocalInput(value: string, timeZone: string | undefined = callsTimeZone()): string | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value);
  if (!m) return null;
  const guess = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]), Number(m[4]), Number(m[5]));
  if (Number.isNaN(guess)) return null;
  // The zone's offset near the guess, applied twice so daylight-saving edges land right.
  const offset = (ms: number) => wallUTC(wallIn(ms, timeZone)) - (ms - (ms % 60_000));
  const first = guess - offset(guess);
  return new Date(guess - offset(first)).toISOString();
}

/** "3:00 PM" in `timeZone` (default: Calls' zone), with the zone named when it isn't the device's. */
export function formatCallsTime(iso: string, timeZone: string | undefined = callsTimeZone()): string {
  const ms = Date.parse(iso);
  return new Date(ms).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit", ...zoneOption(timeZone) }) + zoneSuffix(ms, timeZone);
}

/**
 * "Today, 3:00 PM", "Tomorrow, 9:30 AM", "Thu, Nov 6, 5:00 PM" in `timeZone` (default: Calls'
 * zone, see `setCallsTimeZone`), with the zone named when it isn't the device's.
 */
export function formatCallsWhen(iso: string, timeZone: string | undefined = callsTimeZone(), now = new Date()): string {
  const ms = Date.parse(iso);
  const d = new Date(ms);
  const tz = zoneOption(timeZone);
  const time = formatCallsTime(iso, timeZone);
  const w = wallIn(ms, timeZone);
  const n = wallIn(now.getTime(), timeZone);
  const diff = Math.round((Date.UTC(w.y, w.mo - 1, w.d) - Date.UTC(n.y, n.mo - 1, n.d)) / 86_400_000);
  if (diff === 0) return `Today, ${time}`;
  if (diff === 1) return `Tomorrow, ${time}`;
  if (diff === -1) return `Yesterday, ${time}`;
  const date = d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short", ...tz, ...(w.y !== n.y ? { year: "numeric" } : {}) });
  return `${date}, ${time}`;
}
