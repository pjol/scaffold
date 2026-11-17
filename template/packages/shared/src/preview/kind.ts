/**
 * What kind of preview a file gets, from its name and type. Shared by the web and native
 * `FilePreview` (ui-web / ui-native) and by any feature that lists files, so every surface
 * agrees on how a file shows. Pure and portable.
 */

/** How a file previews. */
export type FilePreviewKind =
  | "image"
  | "video"
  | "audio"
  | "pdf"
  | "text"
  | "code"
  | "markdown"
  | "csv"
  | "json"
  | "svg"
  | "office"
  | "archive"
  | "font"
  | "other";

/** Office formats a preview can open (`null`: an office file nothing here can read, such as .doc or .key). */
export type FilePreviewOfficeFormat = "docx" | "xlsx" | "pptx" | "odt" | "ods" | "odp";

/** The lower-case extension without the dot ("" for none). "Makefile" and ".gitignore" count as their own extension. */
export function fileExtension(name: string): string {
  const base = (name.split(/[\\/]/).pop() ?? "").toLowerCase();
  const i = base.lastIndexOf(".");
  if (i >= 0 && i < base.length - 1) return base.slice(i + 1);
  return base;
}

function baseMime(mime: string | null | undefined): string {
  return (mime ?? "").split(";")[0]!.trim().toLowerCase();
}

const IMAGE_EXT = new Set(["png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "apng"]);
const VIDEO_EXT = new Set(["mp4", "m4v", "mov", "webm", "ogv"]);
const AUDIO_EXT = new Set(["mp3", "m4a", "aac", "wav", "ogg", "oga", "opus", "flac", "weba"]);
const MARKDOWN_EXT = new Set(["md", "markdown", "mdown", "mkd"]);
const CSV_EXT = new Set(["csv", "tsv", "tab"]);
const JSON_EXT = new Set(["json", "geojson", "jsonc", "json5", "webmanifest", "ipynb"]);
const FONT_EXT = new Set(["woff", "woff2", "ttf", "otf"]);
const ARCHIVE_EXT = new Set(["zip", "jar", "gz", "tgz", "tar", "rar", "7z", "bz2", "xz"]);
const OFFICE_EXT = new Set(["doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "rtf", "pages", "numbers", "key"]);
const TEXT_EXT = new Set(["txt", "text", "log", "env", "ini", "cfg", "conf", "properties", "lock", "gitignore", "gitattributes", "editorconfig", "license", "readme"]);

/** Source extensions → highlight.js language names (also what makes a file "code"). */
const LANGUAGE_BY_EXT: Record<string, string> = {
  js: "javascript", mjs: "javascript", cjs: "javascript", jsx: "javascript", ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  go: "go", py: "python", rb: "ruby", rs: "rust", java: "java", kt: "kotlin", kts: "kotlin", swift: "swift", c: "c", h: "c",
  cpp: "cpp", cc: "cpp", cxx: "cpp", hpp: "cpp", hh: "cpp", cs: "csharp", php: "php", sh: "bash", bash: "bash", zsh: "bash",
  sql: "sql", yaml: "yaml", yml: "yaml", toml: "ini", xml: "xml", html: "xml", htm: "xml", vue: "xml", svelte: "xml",
  css: "css", scss: "scss", less: "css", dockerfile: "dockerfile", makefile: "makefile", mk: "makefile", diff: "diff", patch: "diff",
  lua: "plaintext", r: "plaintext", dart: "plaintext", scala: "plaintext", ex: "plaintext", exs: "plaintext", erl: "plaintext",
  hs: "plaintext", clj: "plaintext", pl: "plaintext", proto: "plaintext", graphql: "plaintext", gql: "plaintext", tex: "plaintext",
  tf: "plaintext", gradle: "plaintext", cmake: "plaintext", bat: "plaintext", ps1: "plaintext",
};

const TEXT_MIMES = new Set(["application/xml", "application/javascript", "application/x-yaml", "application/yaml", "application/toml", "application/x-sh", "application/sql", "application/xhtml+xml", "application/x-httpd-php"]);

/**
 * How a file previews, from its name and (possibly empty or generic) content type. The type
 * wins when it is specific; the extension fills in when it is missing, generic or wrong
 * (browsers report `.ts` as video/mp2t).
 */
export function filePreviewKind(name: string, mime?: string | null): FilePreviewKind {
  const mt = baseMime(mime);
  const ext = fileExtension(name);
  if (mt === "image/svg+xml" || ext === "svg") return "svg";
  if (mt === "application/pdf" || ext === "pdf") return "pdf";
  if (mt === "application/json" || mt.endsWith("+json") || JSON_EXT.has(ext)) return "json";
  if (mt === "text/markdown" || mt === "text/x-markdown" || MARKDOWN_EXT.has(ext)) return "markdown";
  if (mt === "text/csv" || mt === "text/tab-separated-values" || CSV_EXT.has(ext)) return "csv";
  if (LANGUAGE_BY_EXT[ext] && !mt.startsWith("image/") && !mt.startsWith("audio/")) return "code";
  if (mt.startsWith("image/") && mt !== "image/heic" && mt !== "image/heif" && mt !== "image/tiff") return "image";
  if (mt.startsWith("video/")) return "video";
  if (mt.startsWith("audio/")) return "audio";
  if (mt.startsWith("font/") || mt === "application/font-woff" || mt === "application/x-font-ttf" || FONT_EXT.has(ext)) return "font";
  if (fileOfficeKind(name, mt) !== null || OFFICE_EXT.has(ext)) return "office";
  if (mt === "application/zip" || mt === "application/x-zip-compressed" || mt === "application/gzip" || mt === "application/x-tar" || mt === "application/x-7z-compressed" || mt === "application/vnd.rar" || ARCHIVE_EXT.has(ext)) return "archive";
  if (!mt || mt === "application/octet-stream" || mt === "binary/octet-stream") {
    if (IMAGE_EXT.has(ext)) return "image";
    if (VIDEO_EXT.has(ext)) return "video";
    if (AUDIO_EXT.has(ext)) return "audio";
    if (TEXT_EXT.has(ext)) return "text";
    return "other";
  }
  if (mt === "text/plain") return "text";
  if (mt.startsWith("text/") || TEXT_MIMES.has(mt) || mt.endsWith("+xml")) return "code";
  return "other";
}

const OFFICE_MIME: Record<string, "doc" | "sheet" | "slides"> = {
  "application/msword": "doc",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "doc",
  "application/vnd.oasis.opendocument.text": "doc",
  "application/rtf": "doc",
  "application/x-iwork-pages-sffpages": "doc",
  "application/vnd.ms-excel": "sheet",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "sheet",
  "application/vnd.oasis.opendocument.spreadsheet": "sheet",
  "application/x-iwork-numbers-sffnumbers": "sheet",
  "application/vnd.ms-powerpoint": "slides",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation": "slides",
  "application/vnd.oasis.opendocument.presentation": "slides",
  "application/x-iwork-keynote-sffkey": "slides",
};

/** Whether an office file is a document, a spreadsheet or slides (null if it isn't an office file). */
export function fileOfficeKind(name: string, mime?: string | null): "doc" | "sheet" | "slides" | null {
  const byMime = OFFICE_MIME[baseMime(mime)];
  if (byMime) return byMime;
  const ext = fileExtension(name);
  if (["doc", "docx", "odt", "rtf", "pages"].includes(ext)) return "doc";
  if (["xls", "xlsx", "ods", "numbers"].includes(ext)) return "sheet";
  if (["ppt", "pptx", "odp", "key"].includes(ext)) return "slides";
  return null;
}

/** The office format a preview can open, or null (legacy .doc/.xls/.ppt, RTF and iWork files download instead). */
export function fileOfficeFormat(name: string, mime?: string | null): FilePreviewOfficeFormat | null {
  const ext = fileExtension(name);
  if (ext === "docx" || ext === "xlsx" || ext === "pptx" || ext === "odt" || ext === "ods" || ext === "odp") return ext;
  switch (baseMime(mime)) {
    case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
      return "docx";
    case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
      return "xlsx";
    case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
      return "pptx";
    case "application/vnd.oasis.opendocument.text":
      return "odt";
    case "application/vnd.oasis.opendocument.spreadsheet":
      return "ods";
    case "application/vnd.oasis.opendocument.presentation":
      return "odp";
  }
  return null;
}

/** True when a zip listing can be shown (other archive formats download). */
export function fileIsZip(name: string, mime?: string | null): boolean {
  const mt = baseMime(mime);
  const ext = fileExtension(name);
  return mt === "application/zip" || mt === "application/x-zip-compressed" || ext === "zip" || ext === "jar";
}

/** The highlight.js language for a code file ("plaintext" when there's none). */
export function filePreviewLanguage(name: string, mime?: string | null): string {
  const ext = fileExtension(name);
  const byExt = LANGUAGE_BY_EXT[ext];
  if (byExt) return byExt;
  const mt = baseMime(mime);
  if (mt === "application/json" || mt.endsWith("+json") || JSON_EXT.has(ext)) return "json";
  if (mt.includes("xml") || mt === "text/html") return "xml";
  if (mt === "text/css") return "css";
  if (mt.includes("javascript")) return "javascript";
  if (mt.includes("typescript")) return "typescript";
  if (mt.includes("yaml")) return "yaml";
  if (mt.includes("markdown") || MARKDOWN_EXT.has(ext)) return "markdown";
  return "plaintext";
}

/** Kinds read as text (fetched and drawn by the preview rather than handed to the browser). */
export function filePreviewIsText(kind: FilePreviewKind): boolean {
  return kind === "text" || kind === "code" || kind === "markdown" || kind === "csv" || kind === "json" || kind === "svg";
}

/** Kinds the preview can show in full (everything else is an icon, size and download). */
export function filePreviewHasView(kind: FilePreviewKind, name: string, mime?: string | null): boolean {
  if (kind === "office") return fileOfficeFormat(name, mime) !== null;
  if (kind === "archive") return fileIsZip(name, mime);
  return kind !== "other";
}

const KIND_LABEL: Record<FilePreviewKind, string> = {
  image: "Image",
  video: "Video",
  audio: "Audio",
  pdf: "PDF",
  text: "Text",
  code: "Code",
  markdown: "Markdown",
  csv: "Spreadsheet",
  json: "JSON",
  svg: "SVG image",
  office: "Document",
  archive: "Archive",
  font: "Font",
  other: "File",
};

/** The kind in a word ("PDF", "Spreadsheet", "Presentation"). */
export function filePreviewKindLabel(kind: FilePreviewKind, name = "", mime?: string | null): string {
  if (kind === "csv") return fileExtension(name) === "tsv" ? "TSV" : "CSV";
  if (kind === "office") {
    const k = fileOfficeKind(name, mime);
    return k === "sheet" ? "Spreadsheet" : k === "slides" ? "Presentation" : "Document";
  }
  return KIND_LABEL[kind];
}

/** "0 bytes", "820 bytes", "14 KB", "3.2 MB", "1.5 GB". */
export function fileFormatSize(bytes: number | null | undefined): string {
  const b = bytes ?? 0;
  if (!Number.isFinite(b) || b <= 0) return "0 bytes";
  if (b < 1024) return `${b} ${b === 1 ? "byte" : "bytes"}`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = b / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : Math.round(v * 10) / 10} ${units[i]}`;
}

/** Largest text a preview reads (bytes); longer files show their start with a note. */
export const FILE_PREVIEW_TEXT_MAX = 2 * 1024 * 1024;
