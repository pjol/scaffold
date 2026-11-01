import type { DocsOptionColor, DocsPropertyType } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";
import { AtSign, Calendar, CheckSquare, CircleDot, Hash, Link2, List, Tags, Text, Type, UserRound, type LucideIcon } from "lucide-react";

/** "1.2 MB". */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** Option colours are the palette's tones. */
export function optionTone(c: DocsOptionColor | undefined): BadgeTone {
  return c ?? "neutral";
}

export const OPTION_COLORS: readonly DocsOptionColor[] = ["neutral", "primary", "accent", "info", "success", "warning", "danger"];

export const OPTION_COLOR_LABEL: Record<DocsOptionColor, string> = {
  neutral: "Grey",
  primary: "Primary",
  accent: "Accent",
  info: "Blue",
  success: "Green",
  warning: "Yellow",
  danger: "Red",
};

export const PROPERTY_ICON: Record<DocsPropertyType, LucideIcon> = {
  title: Type,
  text: Text,
  number: Hash,
  select: CircleDot,
  multi_select: Tags,
  status: List,
  date: Calendar,
  person: UserRound,
  checkbox: CheckSquare,
  url: Link2,
  email: AtSign,
};

/** A short random id for new properties and options (the server checks the shape). */
export function newShortId(): string {
  const a = "abcdefghijklmnopqrstuvwxyz0123456789";
  let s = "";
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  for (const b of bytes) s += a[b % a.length];
  return s;
}
