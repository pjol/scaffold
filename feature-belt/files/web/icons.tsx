import type { FilesItem, FilesPublicItem } from "@scaffoldapp/shared";
import {
  Box,
  File,
  FileArchive,
  FileCode,
  FileHeadphone,
  FileImage,
  FileSpreadsheet,
  FileText,
  FileVideoCamera,
  Folder,
  GitBranch,
  Presentation,
  type LucideIcon,
} from "lucide-react";

/** Icons other features name for their item types (`ItemType.Icon`); unknown names fall back to a box. */
const BY_NAME: Record<string, LucideIcon> = { "git-branch": GitBranch, folder: Folder, file: File, "file-text": FileText, box: Box };

const SPREADSHEET = /\.(xlsx?|ods|numbers|csv|tsv)$/i;
const SLIDES = /\.(pptx?|odp|key)$/i;
const ARCHIVE = /\.(zip|gz|tgz|tar|rar|7z)$/i;

/** The icon for an item: its registered type's, else by how it previews. */
export function filesItemIcon(item: Pick<FilesItem | FilesPublicItem, "type" | "preview" | "name" | "type_info">): LucideIcon {
  if (item.type_info) return BY_NAME[item.type_info.icon] ?? Box;
  switch (item.preview) {
    case "folder":
      return Folder;
    case "image":
    case "svg":
      return FileImage;
    case "video":
      return FileVideoCamera;
    case "audio":
      return FileHeadphone;
    case "code":
      return FileCode;
    case "csv":
      return FileSpreadsheet;
    case "pdf":
    case "text":
    case "markdown":
      return FileText;
    case "office":
      return SPREADSHEET.test(item.name) ? FileSpreadsheet : SLIDES.test(item.name) ? Presentation : FileText;
  }
  return ARCHIVE.test(item.name) ? FileArchive : File;
}

/** Tint class per kind, so lists scan by colour as well as icon (never colour alone). */
export function filesIconTone(item: Pick<FilesItem | FilesPublicItem, "preview" | "name" | "type_info">): string {
  if (item.type_info) return "text-accent-strong";
  switch (item.preview) {
    case "folder":
      return "text-primary-strong";
    case "image":
    case "svg":
    case "video":
      return "text-accent-strong";
    case "pdf":
      return "text-danger-strong";
    case "csv":
      return "text-success-strong";
    case "office":
      return SPREADSHEET.test(item.name) ? "text-success-strong" : SLIDES.test(item.name) ? "text-warning-strong" : "text-info-strong";
  }
  return "text-fg-muted";
}

/** The lucide icon a registered type names (for New menus). */
export function filesTypeIcon(name: string): LucideIcon {
  return BY_NAME[name] ?? Box;
}
