import type { FilesConnectedItem, FilesItem, FilesPreview, FilesSection } from "./types";

/** Drive sections in display order, with their labels. */
export const FILES_SECTIONS: readonly { id: FilesSection; label: string }[] = [
  { id: "my", label: "My drive" },
  { id: "shared", label: "Shared folders" },
  { id: "shared_with_me", label: "Shared with me" },
  { id: "recent", label: "Recent" },
  { id: "starred", label: "Starred" },
  { id: "trash", label: "Trash" },
];

export function filesSectionLabel(section: FilesSection | "shared_with_me"): string {
  return FILES_SECTIONS.find((s) => s.id === section)?.label ?? "Files";
}

/** "0 bytes", "820 bytes", "14 KB", "3.2 MB", "1.5 GB". */
export function filesFormatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 bytes";
  if (bytes < 1024) return `${bytes} ${bytes === 1 ? "byte" : "bytes"}`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : Math.round(v * 10) / 10} ${units[i]}`;
}

const PREVIEW_LABEL: Record<string, string> = {
  folder: "Folder",
  image: "Image",
  pdf: "PDF",
  video: "Video",
  audio: "Audio",
  text: "Text",
  markdown: "Markdown",
  code: "Code",
  csv: "CSV",
  svg: "SVG image",
  office: "Document",
  other: "File",
};

/** The kind of an item in a word ("Folder", "PDF", "Repository"). */
export function filesKindLabel(item: Pick<FilesItem, "preview" | "type_info" | "name"> & { kind_label?: string | undefined }): string {
  if (item.kind_label) return item.kind_label;
  if (item.type_info) return item.type_info.title;
  if (item.preview === "office") {
    const ext = /\.([a-z0-9]+)$/i.exec(item.name)?.[1]?.toLowerCase();
    if (ext && ["xls", "xlsx", "ods", "numbers"].includes(ext)) return "Spreadsheet";
    if (ext && ["ppt", "pptx", "odp", "key"].includes(ext)) return "Presentation";
  }
  return PREVIEW_LABEL[item.preview] ?? "File";
}

/** Previews shown from the text endpoint. */
export function filesIsTextPreview(preview: FilesPreview): boolean {
  return preview === "text" || preview === "markdown" || preview === "code" || preview === "csv" || preview === "svg";
}

/** Largest text preview clients ask for (the server returns at most 1 MB). */
export const FILES_TEXT_PREVIEW_MAX = 1024 * 1024;

export function filesCanEdit(item: Pick<FilesItem, "my_level">): boolean {
  return item.my_level === "edit" || item.my_level === "manage";
}

export function filesCanManage(item: Pick<FilesItem, "my_level">): boolean {
  return item.my_level === "manage";
}

/** Fills a registered type's "{folder}" path ("" for the top of My drive). */
export function filesTypePath(template: string, folderId: string | null | undefined): string {
  return template.replaceAll("{folder}", encodeURIComponent(folderId ?? ""));
}

/**
 * Parses CSV (RFC 4180: quoted fields, doubled quotes, newlines inside quotes) into rows,
 * stopping after `maxRows`. `truncated` says rows were left out.
 */
export function filesParseCsv(text: string, maxRows = 500, delimiter = ","): { rows: string[][]; truncated: boolean } {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i++;
        } else quoted = false;
      } else field += c;
      continue;
    }
    if (c === '"' && field === "") quoted = true;
    else if (c === delimiter) {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && text[i + 1] === "\n") i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = "";
      if (rows.length >= maxRows) return { rows, truncated: i < text.length - 1 };
    } else field += c;
  }
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return { rows, truncated: false };
}

/** Splits a path into the folder names an upload needs ("a/b/c.txt" → ["a", "b"]) and the file name. */
export function filesSplitPath(relativePath: string): { folders: string[]; name: string } {
  const parts = relativePath.split("/").filter((p) => p !== "" && p !== "." && p !== "..");
  const name = parts.pop() ?? "";
  return { folders: parts, name };
}

/** True for an item from a connected platform (Google Drive, OneDrive, SharePoint). */
export function filesIsConnected(item: Pick<FilesItem, "source"> | null | undefined): item is FilesConnectedItem {
  return !!item && item.source?.type === "connected";
}

/** The app path of a connected item or folder: /files/c/<connection>/<ref>. */
export function filesConnectedPath(connId: string, ref?: string | null): string {
  return ref ? `/files/c/${encodeURIComponent(connId)}/${encodeURIComponent(ref)}` : `/files/c/${encodeURIComponent(connId)}`;
}
