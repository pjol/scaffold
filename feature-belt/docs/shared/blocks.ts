/**
 * The editor's block catalogue, shared by web and mobile: colours, the slash menu (groups, aliases, fuzzy search),
 * Markdown shortcuts, "turn into" targets, keyboard turn-into keys, which blocks hold children, file → block kinds and
 * embed detection. Portable: no DOM.
 */
import type { DocsBlockProps, DocsBlockType, DocsColor, DocsColorHue, DocsTextBlockType } from "./types";

// ---- colours ---------------------------------------------------------------------

export const DOCS_COLOR_HUES: readonly DocsColorHue[] = ["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"];

export const DOCS_COLOR_LABEL: Record<DocsColorHue, string> = {
  gray: "Gray",
  brown: "Brown",
  orange: "Orange",
  yellow: "Yellow",
  green: "Green",
  blue: "Blue",
  purple: "Purple",
  pink: "Pink",
  red: "Red",
};

export function docsIsColor(v: unknown): v is DocsColor {
  return typeof v === "string" && (DOCS_COLOR_HUES as readonly string[]).includes(v.replace(/_background$/, ""));
}

/** The hue of a colour and whether it is a background ("red_background" → ["red", true]). */
export function docsColorParts(c: DocsColor): [DocsColorHue, boolean] {
  const bg = c.endsWith("_background");
  return [(bg ? c.slice(0, -"_background".length) : c) as DocsColorHue, bg];
}

/** "Red" / "Red background". */
export function docsColorLabel(c: DocsColor | null | undefined): string {
  if (!c) return "Default";
  const [hue, bg] = docsColorParts(c);
  return bg ? `${DOCS_COLOR_LABEL[hue]} background` : DOCS_COLOR_LABEL[hue];
}

// ---- structure ---------------------------------------------------------------------

const HEADINGS = new Set<DocsBlockType>(["heading_1", "heading_2", "heading_3", "heading_4"]);
const NESTABLE = new Set<DocsBlockType>(["paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout", "column_list", "column", "synced_block"]);

export function docsIsHeading(type: DocsBlockType): boolean {
  return HEADINGS.has(type);
}

/** Whether a block may hold child blocks (headings only when toggleable; synced copies never). */
export function docsHoldsChildren(type: DocsBlockType, props: DocsBlockProps = {}): boolean {
  if (HEADINGS.has(type)) return !!props.toggleable;
  if (type === "synced_block") return !props.source_id;
  return NESTABLE.has(type);
}

/** Blocks that fold their children away (toggles and toggle headings). */
export function docsFolds(type: DocsBlockType, props: DocsBlockProps = {}): boolean {
  return type === "toggle" || (HEADINGS.has(type) && !!props.toggleable);
}

/** Columns and synced blocks keep their shape: they never turn into other blocks. */
export function docsIsStructural(type: DocsBlockType): boolean {
  return type === "column_list" || type === "column" || type === "synced_block";
}

export const DOCS_MAX_COLUMNS = 5;

// ---- files ---------------------------------------------------------------------------

/** The block an uploaded file becomes: images (SVG drawn as an image; HEIC isn't shown by most browsers), video, audio, PDF, anything else a file. */
export function docsFileBlockType(mime: string, name = ""): "image" | "video" | "audio" | "pdf" | "file" {
  const t = mime.toLowerCase();
  const ext = /\.([a-z0-9]+)$/i.exec(name)?.[1]?.toLowerCase() ?? "";
  if (t === "application/pdf" || ext === "pdf") return "pdf";
  if (/^image\/(png|jpeg|gif|webp|avif|svg\+xml|bmp)$/.test(t) || ["png", "jpg", "jpeg", "gif", "webp", "avif", "svg", "bmp"].includes(ext)) return "image";
  if (/^video\/(mp4|webm|quicktime|ogg)$/.test(t) || ["mp4", "m4v", "webm", "mov", "ogv"].includes(ext)) return "video";
  if (/^audio\//.test(t) || ["mp3", "wav", "m4a", "aac", "ogg", "oga", "opus", "flac"].includes(ext)) return "audio";
  return "file";
}

/** What the file picker offers for each kind. */
export const DOCS_FILE_ACCEPT: Record<"image" | "video" | "audio" | "pdf" | "file", string> = {
  image: "image/png,image/jpeg,image/gif,image/webp,image/avif,image/svg+xml",
  video: "video/mp4,video/webm,video/quicktime,.mov",
  audio: "audio/mpeg,audio/wav,audio/x-wav,audio/mp4,audio/aac,audio/ogg,audio/flac,.m4a,.flac",
  pdf: "application/pdf",
  file: "",
};

// ---- embeds --------------------------------------------------------------------------

/**
 * Providers a link can be embedded from. The server (embeds.go) is the authority and computes what the frame loads;
 * this mirror only decides whether to offer "Embed" for a pasted link. Keep the two lists in step.
 */
const EMBEDS: { provider: string; name: string; re: RegExp }[] = [
  { provider: "youtube", name: "YouTube", re: /^https?:\/\/(?:www\.|m\.)?(?:youtube\.com\/(?:watch\?(?:.*&)?v=|shorts\/|embed\/|live\/)|youtu\.be\/|youtube-nocookie\.com\/embed\/)[A-Za-z0-9_-]{11}/ },
  { provider: "vimeo", name: "Vimeo", re: /^https?:\/\/(?:www\.)?(?:vimeo\.com\/(?:video\/)?|player\.vimeo\.com\/video\/)\d{4,12}/ },
  { provider: "loom", name: "Loom", re: /^https?:\/\/(?:www\.)?loom\.com\/(?:share|embed)\/[0-9a-f]{32}/ },
  { provider: "figma", name: "Figma", re: /^https?:\/\/(?:www\.)?figma\.com\/(?:file|design|proto|board|slides|deck)\/[A-Za-z0-9]{10,64}/ },
  { provider: "miro", name: "Miro", re: /^https?:\/\/(?:www\.)?miro\.com\/app\/(?:board|live-embed)\/[A-Za-z0-9_=-]{8,64}/ },
  { provider: "google_maps", name: "Google Maps", re: /^https?:\/\/(?:www\.|maps\.)?google\.com\/maps(?:\/embed\?pb=|\/place\/|\/@|\?q=|\/search\/)/ },
  { provider: "google_docs", name: "Google Docs", re: /^https?:\/\/docs\.google\.com\/document\/d\/[A-Za-z0-9_-]{20,100}/ },
  { provider: "google_sheets", name: "Google Sheets", re: /^https?:\/\/docs\.google\.com\/spreadsheets\/d\/[A-Za-z0-9_-]{20,100}/ },
  { provider: "google_slides", name: "Google Slides", re: /^https?:\/\/docs\.google\.com\/presentation\/d\/[A-Za-z0-9_-]{20,100}/ },
  { provider: "google_forms", name: "Google Forms", re: /^https?:\/\/docs\.google\.com\/forms\/d\/e\/[A-Za-z0-9_-]{20,120}/ },
  { provider: "google_drive", name: "Google Drive", re: /^https?:\/\/drive\.google\.com\/file\/d\/[A-Za-z0-9_-]{20,100}/ },
  { provider: "google_calendar", name: "Google Calendar", re: /^https?:\/\/calendar\.google\.com\/calendar\/(?:u\/\d\/)?embed\?/ },
  { provider: "codepen", name: "CodePen", re: /^https?:\/\/codepen\.io\/[A-Za-z0-9_-]{1,64}\/(?:pen|embed|full|details)\/[A-Za-z0-9]{3,16}/ },
  { provider: "codesandbox", name: "CodeSandbox", re: /^https?:\/\/codesandbox\.io\/(?:s|embed|p\/sandbox|p\/devbox)\/[A-Za-z0-9_-]{3,80}/ },
  { provider: "gist", name: "GitHub Gist", re: /^https?:\/\/gist\.github\.com\/[A-Za-z0-9-]{1,39}\/[0-9a-f]{8,40}/ },
  { provider: "twitter", name: "X", re: /^https?:\/\/(?:www\.|mobile\.)?(?:twitter|x)\.com\/[A-Za-z0-9_]{1,15}\/status\/\d{5,25}/ },
  { provider: "spotify", name: "Spotify", re: /^https?:\/\/open\.spotify\.com\/(?:intl-[a-z]{2}\/)?(?:track|album|playlist|episode|show|artist)\/[A-Za-z0-9]{10,40}/ },
  { provider: "soundcloud", name: "SoundCloud", re: /^https?:\/\/(?:www\.|m\.)?soundcloud\.com\/[A-Za-z0-9_-]{2,64}\/[A-Za-z0-9_-]{1,128}/ },
  { provider: "typeform", name: "Typeform", re: /^https?:\/\/(?:[a-z0-9-]{1,63}\.)?typeform\.com\/to\/[A-Za-z0-9]{4,20}/ },
  { provider: "tally", name: "Tally", re: /^https?:\/\/tally\.so\/(?:r|embed)\/[A-Za-z0-9]{4,20}/ },
  { provider: "airtable", name: "Airtable", re: /^https?:\/\/airtable\.com\/(?:embed\/)?(?:app[A-Za-z0-9]{14}\/)?shr[A-Za-z0-9]{14}/ },
  { provider: "whimsical", name: "Whimsical", re: /^https?:\/\/whimsical\.com\/(?:embed\/)?[A-Za-z0-9-]{4,120}/ },
  { provider: "excalidraw", name: "Excalidraw", re: /^https?:\/\/excalidraw\.com\/#(?:json|room)=[A-Za-z0-9_,-]{10,200}$/ },
  { provider: "framer", name: "Framer", re: /^https?:\/\/(?:[a-z0-9-]{1,63}\.framer\.website|framer\.com\/embed)\// },
  { provider: "replit", name: "Replit", re: /^https?:\/\/replit\.com\/@[A-Za-z0-9_-]{1,64}\/[A-Za-z0-9_.-]{1,100}/ },
  { provider: "lucidchart", name: "Lucidchart", re: /^https?:\/\/lucid\.app\/(?:lucidchart|lucidspark|documents\/embedded)\/[0-9a-f-]{36}/ },
  { provider: "calendly", name: "Calendly", re: /^https?:\/\/calendly\.com\/[A-Za-z0-9_-]{2,64}(?:\/[A-Za-z0-9_-]{1,80})?\/?(?:$|\?)/ },
  { provider: "canva", name: "Canva", re: /^https?:\/\/(?:www\.)?canva\.com\/design\/[A-Za-z0-9_-]{8,20}\/[A-Za-z0-9_-]{8,40}\/(?:view|watch)/ },
];

/** The provider a link can be embedded from, or null (it becomes a bookmark). */
export function docsEmbedFor(url: string): { provider: string; name: string } | null {
  const u = url.trim();
  if (!u || /[\s<>"']/.test(u)) return null;
  for (const e of EMBEDS) if (e.re.test(u)) return { provider: e.provider, name: e.name };
  return null;
}

export function docsEmbedName(provider: string | undefined): string {
  return EMBEDS.find((e) => e.provider === provider)?.name ?? "Embed";
}

/** Hosts an embed frame may load (the server's `src`); a client frames nothing else. */
export const DOCS_EMBED_HOSTS: readonly string[] = [
  "www.youtube-nocookie.com", "player.vimeo.com", "www.loom.com", "www.figma.com", "miro.com", "www.google.com", "maps.google.com",
  "docs.google.com", "drive.google.com", "calendar.google.com", "codepen.io", "codesandbox.io", "gist.github.com",
  "platform.twitter.com", "open.spotify.com", "w.soundcloud.com", "form.typeform.com", "tally.so", "airtable.com",
  "whimsical.com", "excalidraw.com", "framer.com", "framer.website", "replit.com", "lucid.app", "calendly.com", "www.canva.com",
];

export function docsEmbedSrcAllowed(src: string | undefined): boolean {
  const host = /^https:\/\/([a-z0-9.-]+)(?:[/?#]|$)/i.exec(src ?? "")?.[1]?.toLowerCase();
  return !!host && DOCS_EMBED_HOSTS.some((h) => host === h || host.endsWith(`.${h}`));
}

/** A sensible frame height per provider (px). */
export function docsEmbedHeight(provider: string | undefined): number {
  switch (provider) {
    case "spotify":
      return 152;
    case "soundcloud":
      return 166;
    case "twitter":
      return 560;
    case "typeform":
    case "tally":
    case "google_forms":
    case "calendly":
      return 640;
    case "google_docs":
    case "google_drive":
    case "gist":
      return 560;
    default:
      return 400;
  }
}

// ---- slash menu ----------------------------------------------------------------------

export type DocsSlashGroup = "Recent" | "Basic blocks" | "Media" | "Database" | "Advanced" | "Inline" | "Embeds" | "Actions" | "Turn into" | "Colours";

export type DocsSlashAction =
  | "image"
  | "video"
  | "audio"
  | "pdf"
  | "file"
  | "bookmark"
  | "embed"
  | "page"
  | "link_to_page"
  | "database"
  | "mention-page"
  | "mention-person"
  | "date"
  | "reminder"
  | "emoji"
  | "inline-equation"
  | "columns"
  | "synced"
  | "button"
  | "turn"
  | "color"
  | "duplicate"
  | "delete"
  | "move"
  | "comment";

/** What the slash menu offers. `action` items need more than a block (an upload, a new page, a block action). */
export interface DocsSlashItem {
  id: string;
  label: string;
  group: DocsSlashGroup;
  /** Aliases and search words ("h1", "todo", "db"). */
  keywords: string[];
  type?: DocsBlockType;
  props?: DocsBlockProps;
  action?: DocsSlashAction;
  /** Columns: how many. Database: the view type. Turn into: the target. Colour: the colour (null = default). */
  arg?: string | number | null;
  /** Shortcut hint shown on the right ("#", "⌘⌥1"). */
  hint?: string;
  /** An embed provider's name, for the Embeds group. */
  provider?: string;
}

const TABLE_PROPS: DocsBlockProps = {
  rows: [
    [[], [], []],
    [[], [], []],
  ],
  header_row: true,
};

const BASE_ITEMS: DocsSlashItem[] = [
  { id: "paragraph", label: "Text", group: "Basic blocks", keywords: ["text", "plain", "paragraph", "p"], type: "paragraph", hint: "⌘⌥0" },
  { id: "page", label: "Page", group: "Basic blocks", keywords: ["page", "subpage", "new", "child"], action: "page" },
  { id: "to_do", label: "To-do list", group: "Basic blocks", keywords: ["todo", "task", "checkbox", "check", "[]"], type: "to_do", props: { checked: false }, hint: "[]" },
  { id: "heading_1", label: "Heading 1", group: "Basic blocks", keywords: ["h1", "title", "heading", "#"], type: "heading_1", hint: "#" },
  { id: "heading_2", label: "Heading 2", group: "Basic blocks", keywords: ["h2", "subtitle", "heading", "##"], type: "heading_2", hint: "##" },
  { id: "heading_3", label: "Heading 3", group: "Basic blocks", keywords: ["h3", "heading", "###"], type: "heading_3", hint: "###" },
  { id: "heading_4", label: "Heading 4", group: "Basic blocks", keywords: ["h4", "heading", "####"], type: "heading_4", hint: "####" },
  { id: "table", label: "Table", group: "Basic blocks", keywords: ["table", "grid", "cells", "simple"], type: "table", props: TABLE_PROPS },
  { id: "bulleted_list", label: "Bulleted list", group: "Basic blocks", keywords: ["bullet", "ul", "list", "unordered", "-"], type: "bulleted_list", hint: "-" },
  { id: "numbered_list", label: "Numbered list", group: "Basic blocks", keywords: ["number", "ol", "ordered", "list", "1."], type: "numbered_list", hint: "1." },
  { id: "toggle", label: "Toggle list", group: "Basic blocks", keywords: ["toggle", "collapse", "details", "expand", "fold", ">"], type: "toggle", hint: ">" },
  { id: "quote", label: "Quote", group: "Basic blocks", keywords: ["quote", "blockquote", "citation", '"'], type: "quote", hint: '"' },
  { id: "divider", label: "Divider", group: "Basic blocks", keywords: ["divider", "line", "separator", "hr", "rule", "---"], type: "divider", hint: "---" },
  { id: "link_to_page", label: "Link to page", group: "Basic blocks", keywords: ["link", "page", "reference", "goto"], action: "link_to_page" },
  { id: "callout", label: "Callout", group: "Basic blocks", keywords: ["callout", "note", "tip", "warning", "info", "box"], type: "callout", props: { icon: "💡", color: "gray_background" } },

  { id: "image", label: "Image", group: "Media", keywords: ["image", "photo", "picture", "upload", "img", "png", "jpg", "gif", "svg"], action: "image" },
  { id: "video", label: "Video", group: "Media", keywords: ["video", "movie", "mp4", "mov", "webm", "clip"], action: "video" },
  { id: "audio", label: "Audio", group: "Media", keywords: ["audio", "sound", "music", "mp3", "wav", "podcast", "voice"], action: "audio" },
  { id: "pdf", label: "PDF", group: "Media", keywords: ["pdf", "document", "viewer"], action: "pdf" },
  { id: "file", label: "File", group: "Media", keywords: ["file", "attachment", "upload", "download", "doc", "zip"], action: "file" },
  { id: "bookmark", label: "Web bookmark", group: "Media", keywords: ["bookmark", "link", "url", "web", "preview"], action: "bookmark" },
  { id: "code", label: "Code", group: "Media", keywords: ["code", "snippet", "pre", "```", "mermaid", "diagram"], type: "code", props: { code: "", language: "plain" }, hint: "```" },
  { id: "embed", label: "Embed", group: "Media", keywords: ["embed", "iframe", "frame", "youtube", "figma", "loom"], action: "embed" },

  { id: "database-table", label: "Table view", group: "Database", keywords: ["database", "db", "table", "inline", "tracker"], action: "database", arg: "table" },
  { id: "database-board", label: "Board view", group: "Database", keywords: ["database", "db", "board", "kanban"], action: "database", arg: "board" },
  { id: "database-gallery", label: "Gallery view", group: "Database", keywords: ["database", "db", "gallery", "cards"], action: "database", arg: "gallery" },
  { id: "database-list", label: "List view", group: "Database", keywords: ["database", "db", "list"], action: "database", arg: "list" },
  { id: "database-calendar", label: "Calendar view", group: "Database", keywords: ["database", "db", "calendar", "dates"], action: "database", arg: "calendar" },
  { id: "database-timeline", label: "Timeline view", group: "Database", keywords: ["database", "db", "timeline", "gantt", "roadmap"], action: "database", arg: "timeline" },
  { id: "database-chart", label: "Chart view", group: "Database", keywords: ["database", "db", "chart", "graph"], action: "database", arg: "chart" },
  { id: "database-form", label: "Form", group: "Database", keywords: ["database", "db", "form", "survey", "intake"], action: "database", arg: "form" },
  { id: "database-linked", label: "Linked view of database", group: "Database", keywords: ["database", "db", "linked", "view", "existing"], action: "database", arg: "linked" },

  { id: "table_of_contents", label: "Table of contents", group: "Advanced", keywords: ["toc", "contents", "outline", "index"], type: "table_of_contents" },
  { id: "equation", label: "Block equation", group: "Advanced", keywords: ["equation", "math", "tex", "latex", "katex", "formula", "$$"], type: "equation", props: { expression: "" }, hint: "$$" },
  { id: "button", label: "Button", group: "Advanced", keywords: ["button", "template", "action", "automation"], action: "button" },
  { id: "breadcrumb", label: "Breadcrumb", group: "Advanced", keywords: ["breadcrumb", "path", "navigation"], type: "breadcrumb" },
  { id: "columns-2", label: "2 columns", group: "Advanced", keywords: ["columns", "column", "layout", "side", "2"], action: "columns", arg: 2 },
  { id: "columns-3", label: "3 columns", group: "Advanced", keywords: ["columns", "column", "layout", "3"], action: "columns", arg: 3 },
  { id: "columns-4", label: "4 columns", group: "Advanced", keywords: ["columns", "column", "layout", "4"], action: "columns", arg: 4 },
  { id: "columns-5", label: "5 columns", group: "Advanced", keywords: ["columns", "column", "layout", "5"], action: "columns", arg: 5 },
  { id: "synced", label: "Synced block", group: "Advanced", keywords: ["synced", "sync", "reuse", "mirror", "transclusion"], action: "synced" },
  { id: "toggle_heading_1", label: "Toggle heading 1", group: "Advanced", keywords: ["toggle", "heading", "h1", "collapse"], type: "heading_1", props: { toggleable: true } },
  { id: "toggle_heading_2", label: "Toggle heading 2", group: "Advanced", keywords: ["toggle", "heading", "h2", "collapse"], type: "heading_2", props: { toggleable: true } },
  { id: "toggle_heading_3", label: "Toggle heading 3", group: "Advanced", keywords: ["toggle", "heading", "h3", "collapse"], type: "heading_3", props: { toggleable: true } },
  { id: "mermaid", label: "Mermaid diagram", group: "Advanced", keywords: ["mermaid", "diagram", "flowchart", "chart", "sequence"], type: "code", props: { code: "graph TD\n  A[Start] --> B[Finish]", language: "mermaid" } },

  { id: "mention-person", label: "Mention a person", group: "Inline", keywords: ["mention", "person", "people", "user", "@"], action: "mention-person" },
  { id: "mention-page", label: "Mention a page", group: "Inline", keywords: ["mention", "page", "link", "[["], action: "mention-page" },
  { id: "date", label: "Date or reminder", group: "Inline", keywords: ["date", "today", "tomorrow", "day", "remind", "reminder"], action: "date" },
  { id: "reminder", label: "Reminder", group: "Inline", keywords: ["remind", "reminder", "alarm", "due", "notify"], action: "reminder" },
  { id: "emoji", label: "Emoji", group: "Inline", keywords: ["emoji", "icon", "smiley", ":"], action: "emoji" },
  { id: "inline-equation", label: "Inline equation", group: "Inline", keywords: ["equation", "math", "inline", "tex", "latex", "$"], action: "inline-equation" },
];

const EMBED_ITEMS: DocsSlashItem[] = [
  "YouTube", "Vimeo", "Loom", "Figma", "Miro", "Google Maps", "Google Docs", "Google Sheets", "Google Slides", "Google Drive", "CodePen",
  "GitHub Gist", "X", "Spotify", "SoundCloud", "Typeform", "Airtable", "Whimsical", "Excalidraw", "Framer", "Replit", "Lucidchart", "Calendly", "Canva",
].map((name) => ({ id: `embed-${name.toLowerCase().replace(/\s+/g, "-")}`, label: name, group: "Embeds" as const, keywords: ["embed", ...name.toLowerCase().split(/\s+/), ...(name === "X" ? ["twitter", "tweet"] : [])], action: "embed" as const, provider: name }));

/** Block types a block can turn into (with the props that make it so). */
export const DOCS_TURN_INTO: readonly { id: string; label: string; type: DocsBlockType; props?: DocsBlockProps; keywords: string[] }[] = [
  { id: "paragraph", label: "Text", type: "paragraph", keywords: ["text", "p"] },
  { id: "heading_1", label: "Heading 1", type: "heading_1", keywords: ["h1"] },
  { id: "heading_2", label: "Heading 2", type: "heading_2", keywords: ["h2"] },
  { id: "heading_3", label: "Heading 3", type: "heading_3", keywords: ["h3"] },
  { id: "heading_4", label: "Heading 4", type: "heading_4", keywords: ["h4"] },
  { id: "bulleted_list", label: "Bulleted list", type: "bulleted_list", keywords: ["bullet", "ul"] },
  { id: "numbered_list", label: "Numbered list", type: "numbered_list", keywords: ["number", "ol"] },
  { id: "to_do", label: "To-do list", type: "to_do", props: { checked: false }, keywords: ["todo", "task"] },
  { id: "toggle", label: "Toggle list", type: "toggle", keywords: ["toggle"] },
  { id: "toggle_heading_1", label: "Toggle heading 1", type: "heading_1", props: { toggleable: true }, keywords: ["toggle", "h1"] },
  { id: "toggle_heading_2", label: "Toggle heading 2", type: "heading_2", props: { toggleable: true }, keywords: ["toggle", "h2"] },
  { id: "toggle_heading_3", label: "Toggle heading 3", type: "heading_3", props: { toggleable: true }, keywords: ["toggle", "h3"] },
  { id: "quote", label: "Quote", type: "quote", keywords: ["quote"] },
  { id: "callout", label: "Callout", type: "callout", props: { icon: "💡" }, keywords: ["callout"] },
  { id: "code", label: "Code", type: "code", keywords: ["code"] },
  { id: "equation", label: "Block equation", type: "equation", keywords: ["equation", "math"] },
];

/** The turn-into target a block currently is (toggle headings count as their own). */
export function docsTurnIntoId(type: DocsBlockType, props: DocsBlockProps = {}): string {
  return HEADINGS.has(type) && props.toggleable && type !== "heading_4" ? `toggle_${type}` : type;
}

const ACTION_ITEMS: DocsSlashItem[] = [
  { id: "act-duplicate", label: "Duplicate", group: "Actions", keywords: ["duplicate", "copy", "clone"], action: "duplicate", hint: "⌘D" },
  { id: "act-delete", label: "Delete", group: "Actions", keywords: ["delete", "remove", "trash"], action: "delete", hint: "Del" },
  { id: "act-move", label: "Move to", group: "Actions", keywords: ["move", "to", "page"], action: "move" },
  { id: "act-comment", label: "Comment", group: "Actions", keywords: ["comment", "discuss", "note"], action: "comment", hint: "⌘⇧M" },
  ...DOCS_TURN_INTO.map((t) => ({ id: `turn-${t.id}`, label: `Turn into ${t.label.toLowerCase()}`, group: "Turn into" as const, keywords: ["turn", "into", "convert", ...t.keywords, ...t.label.toLowerCase().split(/\s+/)], action: "turn" as const, arg: t.id })),
  { id: "color-default", label: "Default colour", group: "Colours", keywords: ["color", "colour", "default", "reset", "clear"], action: "color", arg: null },
  ...DOCS_COLOR_HUES.flatMap((h) => [
    { id: `color-${h}`, label: DOCS_COLOR_LABEL[h], group: "Colours" as const, keywords: ["color", "colour", "text", h], action: "color" as const, arg: h },
    { id: `color-${h}_background`, label: `${DOCS_COLOR_LABEL[h]} background`, group: "Colours" as const, keywords: ["color", "colour", "background", "highlight", h], action: "color" as const, arg: `${h}_background` },
  ]),
];

/** Every slash command: blocks, media, databases, embeds, inline items, then block actions (turn into, colours, duplicate…). */
export const DOCS_SLASH_ITEMS: readonly DocsSlashItem[] = [...BASE_ITEMS, ...EMBED_ITEMS, ...ACTION_ITEMS];

/** How well `q` matches `s`: 4 exact, 3 prefix, 2 word prefix, 1 letters in order, 0 none. */
function score(s: string, q: string): number {
  if (s === q) return 4;
  if (s.startsWith(q)) return 3;
  if (s.split(/[\s-]+/).some((w) => w.startsWith(q))) return 2;
  let i = 0;
  for (const c of s) if (c === q[i]) i++;
  return i === q.length && q.length >= 2 ? 1 : 0;
}

/**
 * Slash items for a query: aliases and labels, exact before prefix before fuzzy (letters in order), block items before
 * actions. With no query: recently used first (ids, newest first), then blocks and media (actions only when searched).
 */
export function docsFilterSlash(query: string, items: readonly DocsSlashItem[] = DOCS_SLASH_ITEMS, recent: readonly string[] = []): DocsSlashItem[] {
  const q = query.trim().toLowerCase();
  const isAction = (i: DocsSlashItem) => i.group === "Actions" || i.group === "Turn into" || i.group === "Colours";
  if (!q) {
    const rec = recent
      .map((id) => items.find((i) => i.id === id))
      .filter((i): i is DocsSlashItem => !!i && !isAction(i))
      .slice(0, 4)
      .map((i) => ({ ...i, group: "Recent" as const }));
    return [...rec, ...items.filter((i) => !isAction(i) && i.group !== "Embeds")];
  }
  const ranked: { item: DocsSlashItem; s: number; i: number }[] = [];
  items.forEach((item, i) => {
    const label = item.label.toLowerCase();
    let s = Math.max(score(label, q), ...item.keywords.map((k) => (k === q ? 4 : k.startsWith(q) ? 2.5 : 0)));
    if (s === 0 && q.includes(" ")) s = q.split(/\s+/).every((w) => label.includes(w) || item.keywords.some((k) => k.startsWith(w))) ? 1.5 : 0;
    if (s > 0) ranked.push({ item, s: s - (isAction(item) ? 0.5 : 0), i });
  });
  ranked.sort((a, b) => b.s - a.s || a.i - b.i);
  return ranked.map((r) => r.item).slice(0, 60);
}

// ---- Markdown shortcuts and keys ---------------------------------------------------------

/**
 * The block a Markdown shortcut typed at the start of a block (then a space) turns it into: "#"–"####", "-", "*", "+",
 * "1." "a." "i.", "[]", "[x]", '"' (quote), ">" (toggle, as in Notion), "```", "---", "$$". null when it isn't one.
 */
export function docsShortcut(prefix: string): { type: DocsBlockType; props?: DocsBlockProps } | null {
  switch (prefix) {
    case "#":
      return { type: "heading_1" };
    case "##":
      return { type: "heading_2" };
    case "###":
      return { type: "heading_3" };
    case "####":
      return { type: "heading_4" };
    case "-":
    case "*":
    case "+":
      return { type: "bulleted_list" };
    case "[]":
    case "[ ]":
      return { type: "to_do", props: { checked: false } };
    case "[x]":
      return { type: "to_do", props: { checked: true } };
    case ">":
      return { type: "toggle" };
    case '"':
      return { type: "quote" };
    case "```":
      return { type: "code", props: { code: "", language: "plain" } };
    case "---":
      return { type: "divider" };
    case "$$":
      return { type: "equation", props: { expression: "" } };
  }
  if (/^(\d{1,3}|[a-z]|[ivx]{1,4})[.)]$/.test(prefix)) return { type: "numbered_list" };
  return null;
}

/** ⌘⌥0–9 (Notion's turn-into keys): 0 text, 1–3 headings, 4 to-do, 5 bulleted, 6 numbered, 7 toggle, 8 code, 9 page. */
export const DOCS_TURN_INTO_KEYS: Record<string, string> = {
  "0": "paragraph",
  "1": "heading_1",
  "2": "heading_2",
  "3": "heading_3",
  "4": "to_do",
  "5": "bulleted_list",
  "6": "numbered_list",
  "7": "toggle",
  "8": "code",
  "9": "page",
};

/** The keyboard shortcuts sheet (`?` / ⌘/ help): [keys, what they do], grouped. Keys use ⌘ (shown as Ctrl off Apple). */
export const DOCS_KEYBOARD_SHORTCUTS: readonly { group: string; items: readonly [string, string][] }[] = [
  {
    group: "Text",
    items: [
      ["⌘B", "Bold"],
      ["⌘I", "Italic"],
      ["⌘U", "Underline"],
      ["⌘⇧S", "Strikethrough"],
      ["⌘E", "Inline code"],
      ["⌘⇧E", "Inline equation"],
      ["⌘K", "Add a link"],
      ["⌘⇧H", "Last colour or highlight"],
      ["⌘⇧M", "Comment"],
    ],
  },
  {
    group: "Blocks",
    items: [
      ["⌘⌥0", "Text"],
      ["⌘⌥1 – 3", "Heading 1 – 3"],
      ["⌘⌥4", "To-do list"],
      ["⌘⌥5", "Bulleted list"],
      ["⌘⌥6", "Numbered list"],
      ["⌘⌥7", "Toggle list"],
      ["⌘⌥8", "Code"],
      ["⌘⌥9", "Page"],
      ["⌘D", "Duplicate"],
      ["⌘⇧↑ / ↓", "Move up or down"],
      ["⌘↵", "Open a toggle or check a to-do"],
      ["Tab / ⇧Tab", "Indent or outdent"],
      ["⌘/", "Block actions"],
      ["Esc", "Select the block"],
    ],
  },
  {
    group: "Markdown",
    items: [
      ["# ## ### ####", "Headings"],
      ["- * +", "Bulleted list"],
      ["1. a. i.", "Numbered list"],
      ["[]", "To-do list"],
      ["> ", "Toggle list"],
      ['" ', "Quote"],
      ["```", "Code"],
      ["---", "Divider"],
      ["$$", "Block equation"],
      ["**x** *x* ~x~ `x` $x$", "Bold, italic, strike, code, equation"],
      ["[[ or +", "Link or create a page"],
      ["@", "Mention a person, page or date"],
      [":", "Emoji"],
      ["/", "Every block and action"],
    ],
  },
];

/** Text blocks the editor shows (heading 4 included). */
export const DOCS_HEADING_LEVEL: Partial<Record<DocsTextBlockType, number>> = { heading_1: 1, heading_2: 2, heading_3: 3, heading_4: 4 };
