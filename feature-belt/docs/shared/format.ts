/** Labels, slash-menu catalogue and Markdown shortcuts (database formatting: database.ts). Portable. */
import { docsPlain } from "./rich";
import type { DocsBlock, DocsBlockType, DocsTextBlockType } from "./types";

export const DOCS_TEXT_TYPES: readonly DocsTextBlockType[] = [
  "paragraph",
  "heading_1",
  "heading_2",
  "heading_3",
  "heading_4",
  "bulleted_list",
  "numbered_list",
  "to_do",
  "toggle",
  "quote",
  "callout",
];

export function docsIsTextBlock(type: DocsBlockType): type is DocsTextBlockType {
  return (DOCS_TEXT_TYPES as readonly string[]).includes(type);
}

/** "Untitled" for an empty title. */
export function docsTitle(title: string | null | undefined): string {
  return title && title.trim() ? title : "Untitled";
}

export const DOCS_BLOCK_LABEL: Record<DocsBlockType, string> = {
  paragraph: "Text",
  heading_1: "Heading 1",
  heading_2: "Heading 2",
  heading_3: "Heading 3",
  heading_4: "Heading 4",
  bulleted_list: "Bulleted list",
  numbered_list: "Numbered list",
  to_do: "To-do list",
  toggle: "Toggle list",
  quote: "Quote",
  callout: "Callout",
  code: "Code",
  divider: "Divider",
  image: "Image",
  file: "File",
  bookmark: "Bookmark",
  page: "Page",
  table: "Table",
  database: "Database",
  connected_page: "Connected page",
  column_list: "Columns",
  column: "Column",
  synced_block: "Synced block",
  table_of_contents: "Table of contents",
  breadcrumb: "Breadcrumb",
  equation: "Block equation",
  button: "Button",
  link_to_page: "Link to page",
  video: "Video",
  audio: "Audio",
  pdf: "PDF",
  embed: "Embed",
};

/** Numbered-list numbers per block id (runs restart after any other block). */
export function docsListNumbers(siblings: readonly DocsBlock[]): Map<string, number> {
  const out = new Map<string, number>();
  let n = 0;
  for (const b of siblings) {
    n = b.type === "numbered_list" ? n + 1 : 0;
    if (n > 0) out.set(b.id, n);
  }
  return out;
}

export const DOCS_CODE_LANGUAGES: readonly string[] = [
  "plain",
  "bash",
  "c",
  "cpp",
  "csharp",
  "css",
  "go",
  "html",
  "java",
  "javascript",
  "json",
  "kotlin",
  "markdown",
  "mermaid",
  "php",
  "python",
  "ruby",
  "rust",
  "sql",
  "swift",
  "typescript",
  "xml",
  "yaml",
  "dart",
  "dockerfile",
  "elixir",
  "graphql",
  "haskell",
  "lua",
  "makefile",
  "objectivec",
  "perl",
  "powershell",
  "r",
  "scala",
  "scss",
  "toml",
];

/** Text a block shows in lists and previews. */
export function docsBlockText(b: Pick<DocsBlock, "type" | "props">): string {
  if (b.type === "code") return b.props.code ?? "";
  if (b.type === "equation") return b.props.expression ?? "";
  if (b.type === "button") return b.props.label ?? "";
  if (b.type === "bookmark" || b.type === "embed") return b.props.title || b.props.url || "";
  if (b.type === "image" || b.type === "file" || b.type === "video" || b.type === "audio" || b.type === "pdf") return docsPlain(b.props.caption) || b.props.name || "";
  return docsPlain(b.props.text);
}

// ---- databases: labels, filters and value formatting live in database.ts.

/** A local calendar day as YYYY-MM-DD. */
export function docsToday(offsetDays = 0): string {
  const d = new Date();
  d.setDate(d.getDate() + offsetDays);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/** Short "2 min ago" style label for list rows. */
export function docsTimeAgo(iso: string, now: number = Date.now()): string {
  const t = new Date(iso).getTime();
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 60) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  if (d < 7) return `${d} d ago`;
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(new Date(t));
}
