import { filePreviewKind, type FilePreviewKind, type FilesItem, type FilesPublicItem } from "@scaffoldapp/shared";
import { filePreviewIcon, filePreviewTone } from "@scaffoldapp/ui-web";
import { Box, File, FileText, Folder, GitBranch, Link2, type LucideIcon } from "lucide-react";

/** Icons other features name for their item types (`ItemType.Icon`); unknown names fall back to a box. */
const BY_NAME: Record<string, LucideIcon> = { "git-branch": GitBranch, folder: Folder, file: File, "file-text": FileText, box: Box, link: Link2 };

const KINDS = new Set<string>(["image", "video", "audio", "pdf", "text", "code", "markdown", "csv", "json", "svg", "office", "archive", "font", "other"]);

/** The kind of a shortcut's target, from its platform type (Google's native types by what they are). */
function shortcutKind(name: string, mime: string): { kind: FilePreviewKind; mime: string } {
  if (mime.includes("google-apps.document")) return { kind: "office", mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document" };
  if (mime.includes("google-apps.spreadsheet")) return { kind: "office", mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" };
  if (mime.includes("google-apps.presentation")) return { kind: "office", mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation" };
  return { kind: filePreviewKind(name, mime), mime };
}

/** The core kit's kind for a file item (the server's `preview`, else from its name). */
function kindOf(item: Pick<FilesItem | FilesPublicItem, "preview" | "name">): FilePreviewKind {
  return KINDS.has(item.preview) ? (item.preview as FilePreviewKind) : filePreviewKind(item.name, null);
}

/** The icon for an item: its registered type's, a folder, else the core kit's icon for its kind. */
export function filesItemIcon(item: Pick<FilesItem | FilesPublicItem, "type" | "preview" | "name" | "type_info"> & { shortcut?: FilesItem["shortcut"] }): LucideIcon {
  if (item.shortcut) {
    if (item.shortcut.folder) return Folder;
    const s = shortcutKind(item.name, item.shortcut.mime_type);
    return filePreviewIcon(s.kind, item.name, s.mime);
  }
  if (item.type_info) return BY_NAME[item.type_info.icon] ?? Box;
  if (item.preview === "folder") return Folder;
  return filePreviewIcon(kindOf(item), item.name);
}

const FOLDER_TONE: Record<string, string> = {
  primary: "text-primary-strong",
  accent: "text-accent-strong",
  success: "text-success-strong",
  warning: "text-warning-strong",
  danger: "text-danger-strong",
  info: "text-info-strong",
};

/** Tint class per kind (folders: their colour), so lists scan by colour as well as icon (never colour alone). */
export function filesIconTone(item: Pick<FilesItem | FilesPublicItem, "preview" | "name" | "type_info"> & { shortcut?: FilesItem["shortcut"]; color?: FilesItem["color"] }): string {
  if (item.shortcut) {
    if (item.shortcut.folder) return "text-primary-strong";
    const s = shortcutKind(item.name, item.shortcut.mime_type);
    return filePreviewTone(s.kind, item.name, s.mime);
  }
  if (item.type_info) return "text-accent-strong";
  if (item.preview === "folder") return FOLDER_TONE[item.color ?? ""] ?? "text-primary-strong";
  return filePreviewTone(kindOf(item), item.name);
}

/** The lucide icon a registered type names (for New menus). */
export function filesTypeIcon(name: string): LucideIcon {
  return BY_NAME[name] ?? Box;
}
