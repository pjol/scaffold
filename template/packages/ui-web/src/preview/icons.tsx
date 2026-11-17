import { fileOfficeKind, filePreviewKind, type FilePreviewKind } from "@scaffoldapp/shared/preview";
import {
  File,
  FileArchive,
  FileCode,
  FileHeadphone,
  FileImage,
  FileJson,
  FileSpreadsheet,
  FileText,
  FileType,
  FileVideoCamera,
  Presentation,
  type LucideIcon,
} from "lucide-react";
import type { SVGProps } from "react";
import { cn } from "../lib/cn";

/** The lucide icon for a preview kind (office files by document, spreadsheet or slides). */
export function filePreviewIcon(kind: FilePreviewKind, name = "", mime?: string | null): LucideIcon {
  switch (kind) {
    case "image":
    case "svg":
      return FileImage;
    case "video":
      return FileVideoCamera;
    case "audio":
      return FileHeadphone;
    case "code":
      return FileCode;
    case "json":
      return FileJson;
    case "csv":
      return FileSpreadsheet;
    case "pdf":
    case "text":
    case "markdown":
      return FileText;
    case "archive":
      return FileArchive;
    case "font":
      return FileType;
    case "office": {
      const k = fileOfficeKind(name, mime);
      return k === "sheet" ? FileSpreadsheet : k === "slides" ? Presentation : FileText;
    }
  }
  return File;
}

/** Tint class per kind so lists scan by colour as well as icon (never colour alone). */
export function filePreviewTone(kind: FilePreviewKind, name = "", mime?: string | null): string {
  switch (kind) {
    case "image":
    case "svg":
    case "video":
      return "text-accent-strong";
    case "pdf":
      return "text-danger-strong";
    case "csv":
      return "text-success-strong";
    case "audio":
    case "font":
      return "text-info-strong";
    case "code":
    case "json":
      return "text-primary-strong";
    case "office": {
      const k = fileOfficeKind(name, mime);
      return k === "sheet" ? "text-success-strong" : k === "slides" ? "text-warning-strong" : "text-info-strong";
    }
  }
  return "text-fg-muted";
}

export interface FileIconProps extends Omit<SVGProps<SVGSVGElement>, "ref"> {
  name: string;
  mime?: string | null;
  /** Skip detection when the kind is known. */
  kind?: FilePreviewKind;
  /** Tint by kind. @default true */
  tone?: boolean;
}

/** A file's icon by kind, tinted (decorative: pair it with the name). */
export function FileIcon({ name, mime, kind, tone = true, className, ...rest }: FileIconProps) {
  const k = kind ?? filePreviewKind(name, mime);
  const Icon = filePreviewIcon(k, name, mime);
  return <Icon aria-hidden className={cn("size-5 shrink-0", tone && filePreviewTone(k, name, mime), className)} {...rest} />;
}
