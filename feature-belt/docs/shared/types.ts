import type { AccessLevel } from "../access/types";

/** A person, page or date shown as a chip inside text. `text` is the label when it was made; clients show live names. */
export type DocsMention = { type: "user"; id: string } | { type: "page"; id: string } | { type: "date"; date: string };

/** One run of rich text with the same formatting. Links are http(s) or mailto only (the server drops others). */
export interface DocsSpan {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
  mention?: DocsMention;
}

export type DocsMark = "bold" | "italic" | "underline" | "strike" | "code";

export type DocsTextBlockType =
  | "paragraph"
  | "heading_1"
  | "heading_2"
  | "heading_3"
  | "bulleted_list"
  | "numbered_list"
  | "to_do"
  | "toggle"
  | "quote"
  | "callout";

export type DocsBlockType = DocsTextBlockType | "code" | "divider" | "image" | "file" | "bookmark" | "page" | "table" | "database";

/**
 * Props by type: text types `{text}` (+ to_do `checked`, callout `icon`); code `{code, language}`;
 * image `{file_id, name, content_type, size, caption, width}`; file `{file_id, name, content_type, size, caption}`;
 * bookmark `{url, title, caption}`; page `{page_id}`; database `{database_id}`;
 * table `{rows: DocsSpan[][][], header_row, header_col}`; divider `{}`.
 */
export interface DocsBlockProps {
  text?: DocsSpan[];
  checked?: boolean;
  icon?: string;
  code?: string;
  language?: string;
  file_id?: string;
  name?: string;
  content_type?: string;
  size?: number;
  caption?: DocsSpan[];
  width?: number;
  url?: string;
  title?: string;
  page_id?: string;
  database_id?: string;
  rows?: DocsSpan[][][];
  header_row?: boolean;
  header_col?: boolean;
}

export interface DocsBlock {
  id: string;
  page_id: string;
  parent_id: string | null;
  type: DocsBlockType;
  props: DocsBlockProps;
  position: string;
  /** The page version that last changed it. */
  version: number;
  created_by: string | null;
  updated_by: string | null;
  created_at: string;
  updated_at: string;
}

export type DocsCoverColor = "neutral" | "primary" | "accent" | "success" | "warning" | "danger" | "info";

export interface DocsCover {
  file_id?: string;
  color?: DocsCoverColor;
  /** Vertical focus of an image cover, 0 (top) to 100. */
  position: number;
}

export type DocsMyLevel = AccessLevel | "none";

export type DocsPropertyType = "title" | "text" | "number" | "select" | "multi_select" | "status" | "date" | "person" | "checkbox" | "url" | "email";

export type DocsOptionColor = DocsCoverColor;

export interface DocsOption {
  id: string;
  name: string;
  color: DocsOptionColor;
  /** Status options only. */
  group?: "todo" | "doing" | "done";
}

export interface DocsProperty {
  id: string;
  name: string;
  type: DocsPropertyType;
  options?: DocsOption[];
  number_format?: "number" | "percent" | "dollar" | "euro" | "pound";
}

/** Row values by type: text/url/email string, number, select/status option id, multi_select option ids, date {start, end?}, person user ids, checkbox. */
export type DocsDateValue = { start: string; end?: string };
export type DocsValue = string | number | boolean | string[] | DocsDateValue | null;

export interface DocsPage {
  id: string;
  org_id: string;
  parent_id: string | null;
  kind: "page" | "database";
  is_row: boolean;
  inline: boolean;
  title: string;
  icon: string | null;
  cover: DocsCover | null;
  position: string;
  full_width: boolean;
  locked: boolean;
  /** Row values keyed by property id. */
  props: Record<string, DocsValue>;
  /** Databases only. */
  schema: DocsProperty[] | null;
  version: number;
  created_by: string | null;
  updated_by: string | null;
  created_at: string;
  updated_at: string;
  deleted_at: string | null;
  my_level: DocsMyLevel;
}

export interface DocsPageSummary {
  id: string;
  parent_id: string | null;
  kind: "page" | "database";
  is_row: boolean;
  title: string;
  icon: string | null;
  position: string;
  has_children: boolean;
  my_level: DocsMyLevel;
  updated_at: string;
  deleted_at?: string | null;
  snippet?: string;
}

export interface DocsSidebar {
  favorites: DocsPageSummary[];
  shared: DocsPageSummary[];
  private: DocsPageSummary[];
  can_create_shared: boolean;
}

export interface DocsCrumb {
  id: string;
  title: string;
  icon: string | null;
  kind: "page" | "database";
}

export interface DocsRefUser {
  id: string;
  name: string;
  avatar_url: string | null;
}

/** `title` null: the viewer can't open that page. */
export interface DocsRefPage {
  id: string;
  title: string | null;
  icon: string | null;
  kind: "page" | "database";
  deleted: boolean;
}

export interface DocsRefs {
  users: Record<string, DocsRefUser>;
  pages: Record<string, DocsRefPage>;
}

export interface DocsDatabaseInfo {
  id: string;
  title: string | null;
  icon: string | null;
  schema: DocsProperty[];
}

export interface DocsFilter {
  property: string;
  op: string;
  value?: unknown;
}

export interface DocsSort {
  /** A property id, or created_time / last_edited_time. */
  property: string;
  direction: "asc" | "desc";
}

export interface DocsColumn {
  property: string;
  width?: number;
  hidden?: boolean;
}

export interface DocsViewConfig {
  filters: DocsFilter[];
  sorts: DocsSort[];
  group_by?: string;
  columns: DocsColumn[];
  card_properties: string[];
  date_property?: string;
}

export type DocsViewType = "table" | "board" | "list" | "calendar";

export interface DocsView {
  id: string;
  database_id: string;
  name: string;
  type: DocsViewType;
  position: string;
  config: DocsViewConfig;
  created_at: string;
  updated_at: string;
}

export interface DocsPageDetail {
  page: DocsPage;
  path: DocsCrumb[];
  blocks: DocsBlock[];
  refs: DocsRefs;
  views: DocsView[];
  database: DocsDatabaseInfo | null;
  favorite: boolean;
  /** Open threads per block id; "" = the page itself. */
  comment_counts: Record<string, number>;
  published: boolean;
}

// ---- operations

export type DocsOp =
  | { op: "create"; id: string; type: DocsBlockType; parent_id: string | null; position: string; props: DocsBlockProps }
  | { op: "update"; id: string; type?: DocsBlockType; props: Partial<DocsBlockProps> }
  | { op: "move"; id: string; parent_id: string | null; position: string }
  | { op: "delete"; id: string };

export interface DocsRejected {
  id: string;
  op: string;
  code: string;
  message: string;
}

/** POST …/ops answer, and the `docs.ops` event (without `rejected`). */
export interface DocsOpsResult {
  page_id: string;
  version: number;
  client_id: string;
  actor_id: string;
  blocks: DocsBlock[];
  deleted: string[];
  rejected?: DocsRejected[];
}

export interface DocsSyncResult {
  page: DocsPage;
  version: number;
  full: boolean;
  blocks: DocsBlock[];
  deleted: string[];
}

// ---- databases

export interface DocsRow {
  id: string;
  title: string;
  icon: string | null;
  props: Record<string, DocsValue>;
  position: string;
  created_by: string | null;
  updated_by: string | null;
  created_at: string;
  updated_at: string;
  my_level: DocsMyLevel;
}

export interface DocsRowPage {
  items: DocsRow[];
  next_cursor: string | null;
  refs: DocsRefs;
}

export interface DocsRowsParams {
  view?: string;
  filters?: DocsFilter[];
  sorts?: DocsSort[];
  q?: string;
  cursor?: string;
  limit?: number;
}

// ---- comments, history, presence, files

export interface DocsComment {
  id: string;
  page_id: string;
  thread_id: string | null;
  block_id: string | null;
  quote: string | null;
  anchor_start: number | null;
  anchor_end: number | null;
  body: DocsSpan[];
  /** null once the account is deleted. */
  author: DocsRefUser | null;
  resolved_at: string | null;
  resolved_by: DocsRefUser | null;
  edited_at: string | null;
  deleted: boolean;
  created_at: string;
  replies: DocsComment[];
}

export interface DocsSnapshot {
  id: string;
  page_id: string;
  version: number;
  title: string;
  icon: string | null;
  editors: DocsRefUser[];
  reason: "auto" | "before_restore";
  created_at: string;
  blocks?: DocsBlock[];
}

export interface DocsViewer {
  session_id: string;
  user: DocsRefUser;
  block_id: string | null;
  seen_at: string;
}

export interface DocsFile {
  id: string;
  page_id: string;
  name: string;
  content_type: string;
  size: number;
  url?: string;
  expires_at?: string;
}

export interface DocsSearchHit extends DocsPageSummary {
  org_id: string;
  score: number;
}

export interface DocsPublicPage {
  page: DocsPage;
  blocks: DocsBlock[];
  refs: DocsRefs;
  views: DocsView[];
  children: DocsPageSummary[];
  path: DocsCrumb[];
  root_id: string;
  database: DocsDatabaseInfo | null;
}

// ---- requests

export interface DocsCreatePageRequest {
  parent_id?: string | null;
  kind?: "page" | "database";
  title?: string;
  icon?: string | null;
  /** Top level only: everyone in the org can edit it (needs docs.create_shared). */
  shared?: boolean;
  after_id?: string | null;
  /** A database shown as a block on its parent page. */
  inline?: boolean;
}

export interface DocsUpdatePageRequest {
  title?: string;
  icon?: string | null;
  cover?: DocsCover | null;
  full_width?: boolean;
  locked?: boolean;
}

export interface DocsMoveRequest {
  /** null: the top level (needs manage). */
  parent_id: string | null;
  after_id?: string | null;
  last?: boolean;
}

export interface DocsRowValuesRequest {
  title?: string;
  values?: Record<string, DocsValue>;
  after_id?: string | null;
  first?: boolean;
  position?: string;
}

export interface DocsCreateRowRequest {
  title?: string;
  values?: Record<string, DocsValue>;
  after_id?: string | null;
  first?: boolean;
}

export interface DocsViewRequest {
  name?: string;
  type?: DocsViewType;
  config?: DocsViewConfig;
  position?: string;
}

export interface DocsCommentRequest {
  thread_id?: string;
  block_id?: string;
  quote?: string;
  anchor_start?: number;
  anchor_end?: number;
  body: DocsSpan[];
}

export interface DocsImportRequest {
  markdown: string;
  title?: string;
  parent_id?: string | null;
  shared?: boolean;
}

// ---- realtime events

export interface DocsPresenceEvent {
  page_id: string;
  session_id: string;
  block_id?: string | null;
  user?: DocsRefUser;
  left: boolean;
}

declare module "../types" {
  interface RealtimeEventMap {
    "docs.ops": DocsOpsResult;
    "docs.page": { page: DocsPage };
    "docs.presence": DocsPresenceEvent;
    "docs.comments": { page_id: string; thread_id: string };
    "docs.reload": { page_id: string; version: number };
    "docs.rows": { database_id: string; row_id?: string };
    "docs.schema": { database_id: string };
    "docs.tree": { org_id: string };
  }
}
