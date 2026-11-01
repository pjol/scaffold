import type { ChatAttachmentKind, ChatMessage, ChatReadMark } from "./types";

/** Order of messages in a thread: `(created_at, id)`, like the server. */
export function compareMessages(a: Pick<ChatMessage, "created_at" | "id">, b: Pick<ChatMessage, "created_at" | "id">): number {
  const ta = Date.parse(a.created_at);
  const tb = Date.parse(b.created_at);
  if (ta !== tb) return ta < tb ? -1 : 1;
  if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/** Whether `mark` (e.g. `others_read`) covers `message`: everyone else has read it. */
export function isReadBy(mark: ChatReadMark | null | undefined, message: Pick<ChatMessage, "created_at" | "id">): boolean {
  return !!mark && compareMessages(message, { created_at: mark.at, id: mark.message_id }) <= 0;
}

/** Name for a person in a sentence: "You" for the caller. */
function who(user: { id: string; display_name: string } | null, meId: string | undefined, capital = true): string {
  if (!user) return capital ? "Someone" : "someone";
  if (user.id === meId) return capital ? "You" : "you";
  return user.display_name;
}

/** A system message in words, with current names: "Sam added Jordan". */
export function systemEventText(m: Pick<ChatMessage, "event" | "sender" | "subject">, meId?: string): string {
  const actor = who(m.sender, meId);
  const subject = who(m.subject, meId, false);
  switch (m.event) {
    case "created":
      return `${actor} created the group`;
    case "member_added":
      return `${actor} added ${subject}`;
    case "member_removed":
      return `${actor} removed ${subject}`;
    case "member_left":
      return `${who(m.subject, meId)} left`;
    case "title_changed":
      return `${actor} renamed the group`;
    case "avatar_changed":
      return `${actor} changed the group photo`;
    case "owner_changed":
      return `${actor} made ${subject} the owner`;
    default:
      return "";
  }
}

/** What a message sent, in a few words, when it has no text. */
export function attachmentSummary(attachments: readonly { kind: ChatAttachmentKind }[]): string {
  const n = attachments.length;
  if (n === 0) return "";
  const first = attachments[0]?.kind ?? "file";
  const kind = attachments.every((a) => a.kind === first) ? first : "file";
  const words: Record<ChatAttachmentKind, [string, string]> = {
    image: ["Photo", "photos"],
    video: ["Video", "videos"],
    audio: ["Audio", "audio files"],
    file: ["File", "files"],
  };
  const [one, many] = words[kind];
  return n === 1 ? one : `${n} ${many}`;
}

/** The inbox line for a conversation's latest message ("You: …" for your own). */
export function messagePreview(m: ChatMessage | null | undefined, meId?: string, group = false): string {
  if (!m) return "";
  if (m.kind === "system") return systemEventText(m, meId);
  if (m.deleted) return "Message deleted";
  const text = m.body.trim() || attachmentSummary(m.attachments);
  if (m.sender.id === meId) return `You: ${text}`;
  return group ? `${m.sender.display_name}: ${text}` : text;
}

/** "1.2 MB", "340 KB". */
export function formatFileSize(bytes: number | null | undefined): string {
  if (!bytes || bytes <= 0) return "";
  if (bytes >= 1024 * 1024) return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

/** Content types chat accepts, by file extension (mirrors the backend's list). */
export const CHAT_FILE_TYPES: Record<string, string> = {
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  png: "image/png",
  heic: "image/heic",
  heif: "image/heic",
  webp: "image/webp",
  gif: "image/gif",
  mp4: "video/mp4",
  mov: "video/quicktime",
  m4a: "audio/mp4",
  mp3: "audio/mpeg",
  pdf: "application/pdf",
  txt: "text/plain",
  csv: "text/csv",
  zip: "application/zip",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};

/** `accept` for a file input. */
export const CHAT_FILE_ACCEPT = Object.keys(CHAT_FILE_TYPES)
  .map((ext) => `.${ext}`)
  .join(",");

/** Largest attachment the default backend accepts (`CHAT_ATTACHMENT_MAX_MB`). */
export const CHAT_ATTACHMENT_MAX_BYTES = 25 * 1024 * 1024;

/** Most files on one message. */
export const CHAT_MAX_ATTACHMENTS = 10;

/**
 * The content type to upload a file as, from its name (browsers report some types
 * inconsistently, e.g. CSV), or null when chat doesn't accept it.
 */
export function chatContentType(fileName: string, reported?: string | null): string | null {
  const ext = /\.([a-z0-9]+)$/i.exec(fileName)?.[1]?.toLowerCase();
  const byExt = ext ? CHAT_FILE_TYPES[ext] : undefined;
  if (byExt) return byExt;
  if (reported && Object.values(CHAT_FILE_TYPES).includes(reported)) return reported;
  return null;
}

/** The attachment kind for a content type (or a storage key's extension). */
export function attachmentKindOf(contentType: string, keyOrName = ""): ChatAttachmentKind {
  const type = contentType || chatContentType(keyOrName) || "";
  if (type.startsWith("image/")) return "image";
  if (type.startsWith("video/")) return "video";
  if (type.startsWith("audio/")) return "audio";
  return "file";
}

/** Same calendar day in the viewer's zone. */
export function sameDay(a: string, b: string): boolean {
  const da = new Date(a);
  const db = new Date(b);
  return da.getFullYear() === db.getFullYear() && da.getMonth() === db.getMonth() && da.getDate() === db.getDate();
}

/** Whether two adjacent messages group under one sender header (same sender within 5 minutes). */
export function groupsWith(prev: ChatMessage | undefined, next: ChatMessage): boolean {
  return (
    !!prev &&
    prev.kind === "text" &&
    next.kind === "text" &&
    prev.sender.id === next.sender.id &&
    sameDay(prev.created_at, next.created_at) &&
    Date.parse(next.created_at) - Date.parse(prev.created_at) < 5 * 60_000
  );
}

/** Emoji offered for quick reactions (any emoji is accepted). */
export const CHAT_QUICK_REACTIONS = ["👍", "❤️", "😂", "😮", "😢", "🙏"] as const;

/** Mute choices: label and duration in minutes (null = until unmuted). */
export const CHAT_MUTE_OPTIONS: readonly { label: string; minutes: number | null }[] = [
  { label: "For 1 hour", minutes: 60 },
  { label: "For 8 hours", minutes: 8 * 60 },
  { label: "For 1 week", minutes: 7 * 24 * 60 },
  { label: "Until I unmute", minutes: null },
];

/** The mute request for a choice from `CHAT_MUTE_OPTIONS`. */
export function muteRequest(minutes: number | null, now = Date.now()): { until: string } | { forever: true } {
  return minutes === null ? { forever: true } : { until: new Date(now + minutes * 60_000).toISOString() };
}

// ───────────────────────────────────────────────────────────── time ──

const chatTimeFmt = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const weekdayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short" });
const shortDateFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const longDateFmt = new Intl.DateTimeFormat(undefined, { weekday: "long", month: "long", day: "numeric" });
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** "4:05 PM". */
export function chatMessageTime(iso: string): string {
  return chatTimeFmt.format(new Date(iso));
}

/** "Oct 30, 2026, 4:05 PM" (tooltips, staff view). */
export function chatFullTime(iso: string): string {
  return fullFmt.format(new Date(iso));
}

/** Inbox column: today → time, this week → weekday, else "Oct 3". */
export function chatInboxTime(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  if (days <= 0) return chatTimeFmt.format(d);
  if (days < 7) return weekdayFmt.format(d);
  return shortDateFmt.format(d);
}

/** Day separator: "Today", "Yesterday", "Monday, October 27". */
export function chatDayLabel(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return longDateFmt.format(d);
}
