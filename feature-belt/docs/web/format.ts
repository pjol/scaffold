import { DOCS_OPTION_COLOR_LABEL, DOCS_OPTION_HUES, docsOptionHue, type DocsOptionColor, type DocsPropertyType, type DocsSwatchHue, type DocsViewType } from "@scaffoldapp/shared";
import type { BadgeTone } from "@scaffoldapp/ui-web";
import {
  ArrowUpRight,
  AtSign,
  Calendar,
  CalendarDays,
  ChartColumn,
  ChartGantt,
  CircleDot,
  Clock,
  Columns3,
  Fingerprint,
  GalleryVerticalEnd,
  Hash,
  History,
  LayoutList,
  Link2,
  List,
  MousePointerClick,
  Newspaper,
  Paperclip,
  Phone,
  Search,
  Sigma,
  SquareCheckBig,
  Table2,
  Tags,
  Text,
  TextCursorInput,
  Type,
  UserPen,
  UserPlus,
  UserRound,
  type LucideIcon,
} from "lucide-react";

/** "1.2 MB". */
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** docs 1.1: option colours as Badge tones (kept for callers outside databases). */
export function optionTone(c: DocsOptionColor | undefined): BadgeTone {
  switch (c) {
    case "primary":
    case "accent":
    case "info":
    case "success":
    case "warning":
    case "danger":
      return c;
  }
  return "neutral";
}

/** Colours a person picks for options: Notion's hues (`default` is the grey chip). */
export const OPTION_COLORS: readonly DocsOptionColor[] = DOCS_OPTION_HUES;

export const OPTION_COLOR_LABEL: Record<string, string> = DOCS_OPTION_COLOR_LABEL;

/**
 * Swatch classes per hue, written out so Tailwind sees every class. `fg` text, `bg` chip
 * background, `dot` a solid mark, `fill`/`stroke` for SVG marks.
 */
export const SWATCH_CLASS: Record<DocsSwatchHue, { fg: string; bg: string; dot: string; fill: string; stroke: string; border: string }> = {
  gray: { fg: "text-swatch-gray", bg: "bg-swatch-gray-bg", dot: "bg-swatch-gray", fill: "fill-swatch-gray", stroke: "stroke-swatch-gray", border: "border-swatch-gray" },
  brown: { fg: "text-swatch-brown", bg: "bg-swatch-brown-bg", dot: "bg-swatch-brown", fill: "fill-swatch-brown", stroke: "stroke-swatch-brown", border: "border-swatch-brown" },
  orange: { fg: "text-swatch-orange", bg: "bg-swatch-orange-bg", dot: "bg-swatch-orange", fill: "fill-swatch-orange", stroke: "stroke-swatch-orange", border: "border-swatch-orange" },
  yellow: { fg: "text-swatch-yellow", bg: "bg-swatch-yellow-bg", dot: "bg-swatch-yellow", fill: "fill-swatch-yellow", stroke: "stroke-swatch-yellow", border: "border-swatch-yellow" },
  green: { fg: "text-swatch-green", bg: "bg-swatch-green-bg", dot: "bg-swatch-green", fill: "fill-swatch-green", stroke: "stroke-swatch-green", border: "border-swatch-green" },
  blue: { fg: "text-swatch-blue", bg: "bg-swatch-blue-bg", dot: "bg-swatch-blue", fill: "fill-swatch-blue", stroke: "stroke-swatch-blue", border: "border-swatch-blue" },
  purple: { fg: "text-swatch-purple", bg: "bg-swatch-purple-bg", dot: "bg-swatch-purple", fill: "fill-swatch-purple", stroke: "stroke-swatch-purple", border: "border-swatch-purple" },
  pink: { fg: "text-swatch-pink", bg: "bg-swatch-pink-bg", dot: "bg-swatch-pink", fill: "fill-swatch-pink", stroke: "stroke-swatch-pink", border: "border-swatch-pink" },
  red: { fg: "text-swatch-red", bg: "bg-swatch-red-bg", dot: "bg-swatch-red", fill: "fill-swatch-red", stroke: "stroke-swatch-red", border: "border-swatch-red" },
};

/** Neutral (default / grey chip) classes. */
export const NEUTRAL_SWATCH = { fg: "text-fg-muted", bg: "bg-sunken", dot: "bg-border-strong", fill: "fill-ink-400", stroke: "stroke-ink-400", border: "border-border-strong" };

/** The classes an option colour draws with. */
export function swatchFor(c: DocsOptionColor | undefined | null) {
  const h = docsOptionHue(c ?? undefined);
  return h ? SWATCH_CLASS[h] : NEUTRAL_SWATCH;
}

/**
 * Chart series colours when values have no option colour: a fixed order, never cycled (checked with the
 * dataviz validator in light mode: adjacent pairs clear CVD and normal-vision floors). More series fold into
 * "Other" (grey).
 */
export const SERIES_HUES: readonly DocsSwatchHue[] = ["blue", "orange", "purple", "yellow", "pink"];

export const PROPERTY_ICON: Record<DocsPropertyType, LucideIcon> = {
  title: Type,
  text: Text,
  number: Hash,
  select: CircleDot,
  multi_select: Tags,
  status: List,
  date: Calendar,
  person: UserRound,
  checkbox: SquareCheckBig,
  url: Link2,
  email: AtSign,
  phone: Phone,
  files: Paperclip,
  formula: Sigma,
  relation: ArrowUpRight,
  rollup: Search,
  created_time: Clock,
  created_by: UserPlus,
  last_edited_time: History,
  last_edited_by: UserPen,
  unique_id: Fingerprint,
  button: MousePointerClick,
};

export const VIEW_ICON: Record<DocsViewType, LucideIcon> = {
  table: Table2,
  board: Columns3,
  list: LayoutList,
  calendar: CalendarDays,
  gallery: GalleryVerticalEnd,
  timeline: ChartGantt,
  chart: ChartColumn,
  form: TextCursorInput,
  feed: Newspaper,
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
