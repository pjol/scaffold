/** In-call room state: the server's room metadata, its signals, captions over the data channel, and labels. Portable (no DOM APIs). */

import { orgsNoun } from "../orgs/noun";
import type { CallsRecordingStatus, CallsRecordsAccess, CallsRoomMeta, CallsSignal } from "./types";

/** Data-channel topic of server-sent signals (polls, questions, broadcasts, "please unmute"). */
export const CALLS_SIGNAL_TOPIC = "calls.signal";
/** Data-channel topic of captions: each speaker's browser sends its own lines. */
export const CALLS_CAPTION_TOPIC = "calls.caption";

const EMPTY_META: CallsRoomMeta = {
  v: 1,
  meeting_id: "",
  recording: null,
  transcribing: false,
  captions: false,
  transcripts: false,
  can_record: false,
  spotlight: "",
  rules: { screen_share: true, chat: true, reactions: true, unmute: true, mute_on_entry: false, save_chat: false },
  breakout: null,
};

/** The room's server-owned metadata, with defaults for anything missing (an older server, no metadata yet). */
export function parseCallsRoomMeta(metadata: string | undefined | null): CallsRoomMeta {
  if (!metadata) return EMPTY_META;
  try {
    const v = JSON.parse(metadata) as Partial<CallsRoomMeta>;
    return { ...EMPTY_META, ...v, rules: { ...EMPTY_META.rules, ...(v.rules ?? {}) } };
  } catch {
    return EMPTY_META;
  }
}

/** UTF-8 without TextEncoder/TextDecoder, which Hermes lacks in part. */
export function encodeCallsUtf8(text: string): Uint8Array<ArrayBuffer> {
  const out: number[] = [];
  for (const ch of text) {
    let c = ch.codePointAt(0) ?? 0;
    if (c < 0x80) out.push(c);
    else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
    else if (c < 0x10000) out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    else {
      c = Math.min(c, 0x10ffff);
      out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
    }
  }
  return new Uint8Array(out);
}

export function decodeCallsUtf8(bytes: Uint8Array): string {
  let s = "";
  for (let i = 0; i < bytes.length; ) {
    const b = bytes[i++] ?? 0;
    let c = b;
    if (b >= 0xf0) c = ((b & 7) << 18) | (((bytes[i++] ?? 0) & 63) << 12) | (((bytes[i++] ?? 0) & 63) << 6) | ((bytes[i++] ?? 0) & 63);
    else if (b >= 0xe0) c = ((b & 15) << 12) | (((bytes[i++] ?? 0) & 63) << 6) | ((bytes[i++] ?? 0) & 63);
    else if (b >= 0xc0) c = ((b & 31) << 6) | ((bytes[i++] ?? 0) & 63);
    s += String.fromCodePoint(c);
  }
  return s;
}

/** A server signal from its data payload (null when it isn't one). */
export function decodeCallsSignal(payload: Uint8Array): CallsSignal | null {
  try {
    const v = JSON.parse(decodeCallsUtf8(payload)) as CallsSignal;
    return typeof v?.type === "string" ? v : null;
  } catch {
    return null;
  }
}

/** One caption update from a speaker: `id` numbers their utterances; `final` ends one. */
export interface CallsCaption {
  id: number;
  text: string;
  final: boolean;
}

export function encodeCallsCaption(c: CallsCaption): Uint8Array<ArrayBuffer> {
  return encodeCallsUtf8(JSON.stringify({ i: c.id, t: c.text.slice(0, 500), f: c.final }));
}

export function decodeCallsCaption(payload: Uint8Array): CallsCaption | null {
  try {
    const v = JSON.parse(decodeCallsUtf8(payload)) as { i?: unknown; t?: unknown; f?: unknown };
    if (typeof v.i !== "number" || typeof v.t !== "string") return null;
    return { id: v.i, text: v.t.slice(0, 500), final: v.f === true };
  } catch {
    return null;
  }
}

// A getter: the org noun (`configureOrgsNoun`) is read when the label renders.
export const CALLS_RECORDS_ACCESS_LABELS: Record<CallsRecordsAccess, string> = {
  participants: "People in the call",
  invited: "People in the call and invitees",
  get org() {
    return `Everyone in the ${orgsNoun()}`;
  },
};

export const CALLS_RECORDING_STATUS_LABELS: Record<CallsRecordingStatus, string> = {
  starting: "Starting",
  recording: "Recording",
  processing: "Processing",
  ready: "Ready",
  failed: "Failed",
};

/** "12.4 MB" */
export function formatCallsBytes(n: number | null | undefined): string {
  if (!n || n < 0) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u++;
  }
  return `${v >= 10 || u === 0 ? Math.round(v) : v.toFixed(1)} ${units[u]}`;
}

/** Seconds left until `iso` (never negative); null without a time. */
export function callsSecondsLeft(iso: string | null | undefined, now = Date.now()): number | null {
  if (!iso) return null;
  return Math.max(0, Math.round((Date.parse(iso) - now) / 1000));
}

/** "4:05" for a countdown. */
export function formatCallsCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}
