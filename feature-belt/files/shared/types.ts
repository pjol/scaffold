import type { AccessLevel } from "../access/types";
import type { FeatureSource, FeatureSources, SourceProvider, SourceRef } from "../sources";

/** Where an item lives: a person's My drive, an org-owned shared folder tree, or a connected platform. */
export type FilesDrive = "my" | "shared" | "connected";

/** Sections of an org's drive. */
export type FilesSection = "my" | "shared" | "shared_with_me" | "recent" | "starred" | "trash";

/** How clients show an item (registered item types preview as their own type, e.g. "repo"). */
export type FilesPreview = "folder" | "image" | "pdf" | "video" | "audio" | "text" | "markdown" | "code" | "csv" | "svg" | "office" | "other" | (string & {});

export type FilesSort = "name" | "updated" | "size";

export interface FilesPerson {
  id: string;
  /** "" for a deleted account. */
  name: string;
  avatar_url: string | null;
}

/** A registered item type on items of that type. */
export interface FilesTypeInfo {
  type: string;
  title: string;
  /** lucide icon name ("git-branch"). */
  icon: string;
}

/** One drive entry as the caller sees it (Go files.Item). */
export interface FilesItem {
  id: string;
  org_id: string;
  parent_id: string | null;
  /** folder | file | a registered type ("repo"). */
  type: string;
  name: string;
  drive: FilesDrive;
  /** Whose My drive it's in; null for org-owned items. */
  owner_id: string | null;
  mime_type: string;
  size: number;
  text_like: boolean;
  version_id: string | null;
  version_count: number;
  preview: FilesPreview;
  created_by: FilesPerson | null;
  updated_by: FilesPerson | null;
  created_at: string;
  updated_at: string;
  my_level: AccessLevel | "none";
  starred: boolean;
  /** Someone besides the owner has access, or a public link opens it. */
  shared: boolean;
  public_link: boolean;
  public_disabled: boolean;
  trashed_at: string | null;
  /** Who trashed it; null in the trash for a deleted account's private item. */
  trashed_by: FilesPerson | null;
  type_info: FilesTypeInfo | null;
  /** App path that opens it (/files/<id>, or the registered type's page). */
  link: string;
  /** Images, when the list asked for thumbnails (valid 15 minutes). */
  thumb_url: string | null;
  /** Shortcuts (type "link"): what the shortcut points at on a connected platform. */
  shortcut?: FilesShortcutInfo | null;
  /** Rows from a connected platform carry their source; the app's own have none. */
  source?: SourceRef | null;
  /** "Google Docs", "Word document": the platform's kind, when it names one. */
  kind_label?: string;
}

/** What a shortcut item points at. */
export interface FilesShortcutInfo {
  provider: SourceProvider;
  /** drive | onedrive | sharepoint */
  product: string;
  mime_type: string;
  folder: boolean;
}

/** A connected source in Files: the shared FeatureSource plus whether changes are allowed. */
export interface FilesConnectedSource extends FeatureSource {
  /** Products this account opens here: drive · onedrive, sharepoint. */
  products?: string[];
  /** The connection may change files (its write capability is granted). */
  can_write: boolean;
  /** Grants changes, when can_write is false. */
  write_url?: string;
}

/** `GET /v1/files/sources`. */
export interface FilesSources extends FeatureSources {
  sources: FilesConnectedSource[];
}

/** A file or folder on a connected platform, in the FilesItem shape (rows, previews, details) plus where it lives. */
export interface FilesConnectedItem extends FilesItem {
  source: Extract<SourceRef, { type: "connected" }>;
  connection_id: string;
  /** The platform's id for it in this connection (Graph: "<drive>~<item>"). */
  ref: string;
  parent_ref: string | null;
  /** The drive it's in; moves stay inside it. */
  drive_ref: string;
  /** A platform document (Google Docs…): read-only preview here, edited on the platform. */
  native: boolean;
  /** Opens it on the platform. */
  web_url: string;
  /** The account may change it on the platform. */
  can_edit: boolean;
  /** The connection allows changes from here. */
  can_write: boolean;
}

export interface FilesConnectedCrumb {
  ref: string;
  name: string;
  link: string;
}

/** `GET /v1/files/connected/{connID}/items/{ref}`. */
export interface FilesConnectedDetail {
  item: FilesConnectedItem;
  /** Top first, the drive itself included. */
  path: FilesConnectedCrumb[];
  source: FilesConnectedSource;
}

export type FilesConnectedDriveKind = "my_drive" | "shared_with_me" | "shared_drive" | "onedrive" | "library";

/** A place to browse in one connection. */
export interface FilesConnectedDrive {
  ref: string;
  name: string;
  kind: FilesConnectedDriveKind;
  /** SharePoint site of a library. */
  site?: string;
  link: string;
}

/** The platform's sharing summary, read-only. */
export interface FilesConnectedSharing {
  /** False when the platform doesn't show this account the sharing. */
  available: boolean;
  owner?: string;
  link?: "anyone" | "organization" | "domain" | "";
  link_role?: string;
  domain?: string;
  people: { name: string; role: "owner" | "edit" | "comment" | "view"; kind: string }[];
  manage_url?: string;
}

export interface FilesConnectedText {
  text: string;
  truncated: boolean;
  size: number;
  version_id: string;
}

/** A resumable session on the platform: PUT chunks (Content-Range) to upload_url directly. */
export interface FilesConnectedUploadTicket {
  upload_url: string;
  /** Bytes per chunk (Drive: × 256 KiB, Graph: × 320 KiB). */
  chunk_size: number;
  headers: Record<string, string>;
  /** The new item's ref is ref_prefix + the id in the last answer. */
  ref_prefix: string;
  expires_at: string;
  connection_id: string;
  provider: SourceProvider;
  /** Empty files are made at once. */
  item?: FilesConnectedItem;
}

/** What a shortcut opens for you, checked live with your own connection. */
export interface FilesShortcutTarget {
  status: "ok" | "not_connected" | "no_access" | "needs_reauth" | "blocked" | "unavailable";
  provider: SourceProvider;
  product: string;
  product_label: string;
  name: string;
  mime_type: string;
  folder: boolean;
  kind_label?: string;
  connection_id?: string;
  account?: string;
  ref?: string;
  /** App path when status is ok. */
  link?: string;
  connect_url?: string;
  /** no_access: ask for access on the platform. */
  web_url?: string;
  message: string;
}

export interface FilesCrumb {
  id: string;
  name: string;
  type: string;
}

/** `GET /v1/files/items/{id}`: the item, the part of its path the caller can open (top first), and where it starts. */
export interface FilesItemDetail {
  item: FilesItem;
  path: FilesCrumb[];
  root: "my" | "shared" | "shared_with_me";
}

export interface FilesVersion {
  id: string;
  number: number;
  size: number;
  mime_type: string;
  uploaded_by: FilesPerson | null;
  /** "Restored from version 2", "Edited lines 3–5 by Claude". */
  note: string;
  current: boolean;
  created_at: string;
}

export type FilesActivityAction =
  | "created"
  | "uploaded"
  | "version"
  | "version_restored"
  | "edited"
  | "renamed"
  | "moved"
  | "copied"
  | "trashed"
  | "restored"
  | "shared"
  | "link_created";

export interface FilesActivity {
  id: string;
  item_id: string;
  item_name: string;
  action: FilesActivityAction;
  actor: FilesPerson | null;
  /** renamed {from, to} · moved {to_name} · shared {subject_name, subject_type, level 1–4} · edited {start_line, end_line} · agent: the AI agent's name. */
  data: Record<string, unknown>;
  created_at: string;
}

/** A registered item type as the New menu and folder menus offer it. */
export interface FilesTypeSummary {
  type: string;
  title: string;
  icon: string;
  /** App path with "{folder}" for the folder id ("" at the top of My drive); "" when not offered. */
  create_path: string;
  create_label: string;
  convert_path: string;
  convert_label: string;
}

/** `GET /v1/files/orgs/{orgID}/drive`. */
export interface FilesDriveInfo {
  org_id: string;
  used_bytes: number;
  /** 0: no quota. */
  quota_bytes: number;
  max_upload_bytes: number;
  trash_days: number;
  can_create_shared: boolean;
  types: FilesTypeSummary[];
}

export interface FilesListParams {
  sort?: FilesSort;
  dir?: "asc" | "desc";
  /** Include `thumb_url` for images. */
  thumbs?: boolean;
  limit?: number;
}

export interface FilesCreateFolderRequest {
  parent_id?: string | null;
  name: string;
  /** A top-level shared folder the org owns (needs `files.create_shared_folders`). */
  shared?: boolean;
  /** Shared folders: everyone in the org at this level. */
  general?: "view" | "comment" | "edit";
  on_conflict?: "rename" | "fail";
}

export interface FilesCreateFileRequest {
  parent_id?: string | null;
  name: string;
  text: string;
  content_type?: string;
  on_conflict?: "rename" | "fail";
}

export interface FilesStartUploadRequest {
  parent_id?: string | null;
  /** Upload a new version of this file instead. */
  item_id?: string;
  name: string;
  content_type: string;
  size: number;
}

export interface FilesUploadTicket {
  upload_id: string;
  upload_url: string;
  headers: Record<string, string>;
  expires_at: string;
}

/** A short-lived download or preview link. */
export interface FilesUrl {
  url: string;
  expires_at: string;
  disposition: "inline" | "attachment";
  content_type: string;
  filename: string;
}

export interface FilesTextContent {
  text: string;
  /** Only the first 1 MB came back. */
  truncated: boolean;
  size: number;
  version_id: string;
}

export interface FilesSearchHit {
  item: FilesItem;
  /** Excerpt around a match in the text ("" for name matches). */
  snippet: string;
  score: number;
}

export type FilesBatchAction = "move" | "trash" | "restore" | "delete" | "star" | "unstar";

export interface FilesBatchResult {
  id: string;
  ok: boolean;
  error?: { code: string; message: string; details?: Record<string, unknown> };
}

export interface FilesArchive {
  url: string;
  expires_at: string;
}

/** An item as a public link shows it (no people, no sharing). */
export interface FilesPublicItem {
  id: string;
  /** Null at the link's own item. */
  parent_id: string | null;
  type: string;
  name: string;
  mime_type: string;
  size: number;
  preview: FilesPreview;
  updated_at: string;
  type_info: FilesTypeInfo | null;
  thumb_url: string | null;
}

/** `GET /v1/files/public/{token}[/items/{id}]`. */
export interface FilesPublicView {
  level: AccessLevel;
  expires_at: string | null;
  root_id: string;
  item: FilesPublicItem;
  /** From the link's item down to the item's folder. */
  path: FilesCrumb[];
}

/** A public (or staff-disabled) item for staff. */
export interface FilesAdminPublicItem {
  id: string;
  name: string;
  type: string;
  mime_type: string;
  size: number;
  preview: FilesPreview;
  org_id: string;
  org_name: string;
  created_by: FilesPerson | null;
  links: number;
  link_created_at: string | null;
  views_30d: number;
  downloads_30d: number;
  disabled_at: string | null;
  disabled_reason: string;
  disabled_by: FilesPerson | null;
  trashed_at: string | null;
  /** Detail only: a preview link. */
  url?: string;
}

/** Access resource kind of every drive item. */
export const FILES_KIND = "files.item";

/** Agent-scope kind of an item on a connected platform ("<connection id>:<platform id>"). */
export const FILES_EXTERNAL_KIND = "files.external";

/** Capabilities Files declares (Settings → Connections). */
export const FILES_CAPABILITIES = {
  googleDrive: "files.google.drive",
  googleDriveWrite: "files.google.drive.write",
  oneDrive: "files.microsoft.onedrive",
  oneDriveWrite: "files.microsoft.onedrive.write",
  sharePoint: "files.microsoft.sharepoint",
  sharePointWrite: "files.microsoft.sharepoint.write",
} as const;

/** Org permission that lets members create top-level shared folders. */
export const FILES_PERMISSION_CREATE_SHARED = "files.create_shared_folders";

declare module "../types" {
  interface RealtimeEventMap {
    "files.changed": { org_id: string; item_id: string; parent_id: string | null; action: string };
  }
}
