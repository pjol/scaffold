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
  Link2,
  Presentation,
  type LucideIcon,
} from "lucide-react-native";

const BY_NAME: Record<string, LucideIcon> = { "git-branch": GitBranch, folder: Folder, file: File, "file-text": FileText, box: Box, link: Link2 };

/** How a shortcut's target previews, from its type (same rules as web). */
function shortcutPreview(mime: string, folder: boolean): string {
  if (folder) return "folder";
  if (mime.startsWith("image/")) return "image";
  if (mime.startsWith("video/")) return "video";
  if (mime.startsWith("audio/")) return "audio";
  if (mime === "application/pdf" || mime.includes("google-apps.document")) return "pdf";
  if (mime.includes("spreadsheet") || mime.includes("excel")) return "csv";
  if (mime.startsWith("text/")) return "text";
  return "other";
}

/** The icon for an item: its registered type's, else by how it previews (same rules as web). */
export function filesItemIcon(item: Pick<FilesItem | FilesPublicItem, "preview" | "name" | "type_info"> & { shortcut?: FilesItem["shortcut"] }): LucideIcon {
  if (item.shortcut) return filesItemIcon({ preview: shortcutPreview(item.shortcut.mime_type, item.shortcut.folder), name: item.name, type_info: null });
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
      return /\.(xlsx?|ods|numbers)$/i.test(item.name) ? FileSpreadsheet : /\.(pptx?|odp|key)$/i.test(item.name) ? Presentation : FileText;
  }
  return /\.(zip|gz|tgz|tar|rar|7z)$/i.test(item.name) ? FileArchive : File;
}

/** Text tone for an item's icon (colour adds to the icon, never replaces it). */
export function filesIconTone(item: Pick<FilesItem, "preview" | "type_info"> & { shortcut?: FilesItem["shortcut"] }): "primary" | "accent" | "danger" | "success" | "muted" {
  if (item.shortcut) return filesIconTone({ preview: shortcutPreview(item.shortcut.mime_type, item.shortcut.folder), type_info: null });
  if (item.type_info) return "accent";
  switch (item.preview) {
    case "folder":
      return "primary";
    case "image":
    case "svg":
    case "video":
      return "accent";
    case "pdf":
      return "danger";
    case "csv":
      return "success";
  }
  return "muted";
}

export function filesTypeIcon(name: string): LucideIcon {
  return BY_NAME[name] ?? Box;
}
