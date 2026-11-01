import type { ChannelsAttachmentKind, ChannelsMessage, ChannelsNotify, ChannelsRef, ChannelsSidebarChannel } from "./types";

/** Order of messages: `(created_at, id)`, like the server. */
export function compareChannelsMessages(a: Pick<ChannelsMessage, "created_at" | "id">, b: Pick<ChannelsMessage, "created_at" | "id">): number {
  const ta = Date.parse(a.created_at);
  const tb = Date.parse(b.created_at);
  if (ta !== tb) return ta < tb ? -1 : 1;
  if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}

/** How a channel is named in the UI: "#general", "Jordan Lee", "You". */
export function channelsDisplayName(c: Pick<ChannelsRef, "kind" | "name">): string {
  return c.kind === "public" || c.kind === "private" ? `#${c.name}` : c.name;
}

export const isChannelsDirect = (c: Pick<ChannelsRef, "kind">) => c.kind === "dm" || c.kind === "group_dm";

/** Who wrote a message: the sender, the bot's name, or "Someone". */
export function channelsAuthorName(m: Pick<ChannelsMessage, "sender" | "bot_name">, meId?: string): string {
  if (m.sender) return m.sender.id === meId ? "You" : m.sender.display_name;
  return m.bot_name ?? "Someone";
}

function who(user: { id: string; display_name: string } | null, meId: string | undefined, capital = true): string {
  if (!user) return capital ? "Someone" : "someone";
  if (user.id === meId) return capital ? "You" : "you";
  return user.display_name;
}

/** A system message in words, with current names: "Sam added Jordan". */
export function channelsSystemText(m: Pick<ChannelsMessage, "event" | "sender" | "subject">, meId?: string): string {
  const actor = who(m.sender, meId);
  const subject = who(m.subject, meId, false);
  switch (m.event) {
    case "created":
      return `${actor} created this channel`;
    case "joined":
      return `${actor} joined`;
    case "left":
      return `${actor} left`;
    case "added":
      return `${actor} added ${subject}`;
    case "removed":
      return `${actor} removed ${subject}`;
    case "renamed":
      return `${actor} renamed the channel`;
    case "archived":
      return `${actor} archived the channel`;
    case "unarchived":
      return `${actor} unarchived the channel`;
    case "topic":
      return `${actor} changed the topic`;
    case "converted":
      return `${actor} made the channel private`;
    default:
      return "";
  }
}

/** Notification choices for a channel, in menu order. */
export const CHANNELS_NOTIFY_OPTIONS: readonly { value: ChannelsNotify; label: string }[] = [
  { value: "default", label: "Default" },
  { value: "all", label: "All messages" },
  { value: "mentions", label: "Mentions" },
  { value: "nothing", label: "Nothing" },
];

/** What "Default" means for this channel: all messages in DMs, mentions in channels. */
export function channelsEffectiveNotify(c: Pick<ChannelsSidebarChannel, "kind" | "notify">): Exclude<ChannelsNotify, "default"> {
  if (c.notify !== "default") return c.notify;
  return isChannelsDirect(c) ? "all" : "mentions";
}

/** Sidebar sections, like Slack: starred, channels, direct messages. */
export function channelsSections(list: readonly ChannelsSidebarChannel[]): { starred: ChannelsSidebarChannel[]; channels: ChannelsSidebarChannel[]; direct: ChannelsSidebarChannel[] } {
  const starred = list.filter((c) => c.starred);
  const channels = list.filter((c) => !c.starred && !isChannelsDirect(c)).sort((a, b) => a.name.localeCompare(b.name));
  const direct = list.filter((c) => !c.starred && isChannelsDirect(c)).sort((a, b) => (a.last_message_at < b.last_message_at ? 1 : -1));
  return { starred, channels, direct };
}

/** Clean a channel name as you type: "Gear Swap" → "gear-swap". */
export function channelsNameFrom(input: string): string {
  return input
    .toLowerCase()
    .replace(/^#/, "")
    .replace(/\s+/g, "-")
    .replace(/[^a-z0-9_-]/g, "")
    .slice(0, 80);
}

/** "1.2 MB", "340 KB". */
export function channelsFileSize(bytes: number | null | undefined): string {
  if (!bytes || bytes <= 0) return "";
  if (bytes >= 1024 * 1024) return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`;
  return `${Math.max(1, Math.round(bytes / 1024))} KB`;
}

/** Content types channels accept, by extension (mirrors the backend). */
export const CHANNELS_FILE_TYPES: Record<string, string> = {
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
export const CHANNELS_FILE_ACCEPT = Object.keys(CHANNELS_FILE_TYPES)
  .map((ext) => `.${ext}`)
  .join(",");

/** The content type to upload a file as, or null when channels don't accept it. */
export function channelsContentType(fileName: string, reported?: string | null): string | null {
  const ext = /\.([a-z0-9]+)$/i.exec(fileName)?.[1]?.toLowerCase();
  const byExt = ext ? CHANNELS_FILE_TYPES[ext] : undefined;
  if (byExt) return byExt;
  if (reported && Object.values(CHANNELS_FILE_TYPES).includes(reported)) return reported;
  return null;
}

export function channelsAttachmentKind(contentType: string): ChannelsAttachmentKind {
  if (contentType.startsWith("image/")) return "image";
  if (contentType.startsWith("video/")) return "video";
  if (contentType.startsWith("audio/")) return "audio";
  return "file";
}

/** Same calendar day in the viewer's zone. */
export function channelsSameDay(a: string, b: string): boolean {
  const da = new Date(a);
  const db = new Date(b);
  return da.getFullYear() === db.getFullYear() && da.getMonth() === db.getMonth() && da.getDate() === db.getDate();
}

/** Two adjacent messages share one header (same author within 5 minutes, both plain timeline messages). */
export function channelsGroupsWith(prev: ChannelsMessage | undefined, next: ChannelsMessage): boolean {
  return (
    !!prev &&
    prev.kind !== "system" &&
    next.kind === prev.kind &&
    !next.title &&
    !prev.deleted &&
    (prev.sender?.id ?? prev.bot_name) === (next.sender?.id ?? next.bot_name) &&
    channelsSameDay(prev.created_at, next.created_at) &&
    Date.parse(next.created_at) - Date.parse(prev.created_at) < 5 * 60_000
  );
}

// ───────────────────────────────────────────────────────────── time ──

const timeFmt = new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" });
const weekdayFmt = new Intl.DateTimeFormat(undefined, { weekday: "short" });
const shortDateFmt = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const longDateFmt = new Intl.DateTimeFormat(undefined, { weekday: "long", month: "long", day: "numeric" });
const fullFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** "4:05 PM". */
export function channelsTime(iso: string): string {
  return timeFmt.format(new Date(iso));
}

/** "Oct 30, 2026, 4:05 PM" (tooltips). */
export function channelsFullTime(iso: string): string {
  return fullFmt.format(new Date(iso));
}

/** Lists: today → time, this week → weekday, else "Oct 3". */
export function channelsShortTime(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  if (days <= 0) return timeFmt.format(d);
  if (days < 7) return weekdayFmt.format(d);
  return shortDateFmt.format(d);
}

/** Day separator: "Today", "Yesterday", "Monday, October 27". */
export function channelsDayLabel(iso: string, now = new Date()): string {
  const d = new Date(iso);
  const days = Math.round((startOfDay(now) - startOfDay(d)) / 86_400_000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return longDateFmt.format(d);
}

/** "3 replies", "1 reply". */
export function channelsReplyCount(n: number): string {
  return n === 1 ? "1 reply" : `${n} replies`;
}

/** "Jordan is typing…", "Jordan and Sam are typing…", "3 people are typing…". */
export function channelsTypingText(users: readonly { display_name: string }[]): string {
  const [a, b] = users;
  if (!a) return "";
  if (!b) return `${a.display_name} is typing…`;
  if (users.length === 2) return `${a.display_name} and ${b.display_name} are typing…`;
  return `${users.length} people are typing…`;
}

/** The app path of a message: its channel, or its thread for a reply. */
export function channelsMessagePath(m: Pick<ChannelsMessage, "id" | "channel_id" | "thread_root_id">): string {
  return m.thread_root_id ? `/channels/${m.channel_id}?thread=${m.thread_root_id}&message=${m.id}` : `/channels/${m.channel_id}?message=${m.id}`;
}
