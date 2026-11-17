import type { AccessLevel } from "../access/types";
import type { ConnectionOffer, FeatureSource, SourceRef } from "../sources";
import type { DocsCoverGallery, DocsCustomIcon, DocsPageFont, DocsPublishSettings, DocsReaction, DocsTeamspace } from "./pages";
import type { DocsProperty, DocsValue, DocsView } from "./database";

// Database DTOs live in database.ts (docs 1.2); re-exported here for existing imports.
export type {
  DocsColumn,
  DocsCreateRowRequest,
  DocsDateValue,
  DocsFilter,
  DocsOption,
  DocsOptionColor,
  DocsProperty,
  DocsPropertyType,
  DocsRow,
  DocsRowPage,
  DocsRowsParams,
  DocsRowValuesRequest,
  DocsSort,
  DocsValue,
  DocsView,
  DocsViewConfig,
  DocsViewRequest,
  DocsViewType,
} from "./database";

/**
 * A person, page or date shown as a chip inside text. `text` is the label when it was made; clients show live names.
 * A date may carry a local time (`HH:MM`) and `remind_at` (UTC instant): whoever set it is notified then.
 */
export type DocsMention = { type: "user"; id: string } | { type: "page"; id: string } | { type: "date"; date: string; time?: string; remind_at?: string };

/** Block and text colours (Notion's set): a hue for text, `<hue>_background` for a highlight; absent = default. */
export type DocsColorHue = "gray" | "brown" | "orange" | "yellow" | "green" | "blue" | "purple" | "pink" | "red";
export type DocsColor = DocsColorHue | `${DocsColorHue}_background`;

/** One run of rich text with the same formatting. Links are http(s) or mailto only (the server drops others). */
export interface DocsSpan {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
  code?: boolean;
  link?: string;
  /** Text colour or highlight. */
  color?: DocsColor;
  /** The text is TeX shown as one inline equation (never split by edits). */
  equation?: boolean;
  mention?: DocsMention;
}

export type DocsMark = "bold" | "italic" | "underline" | "strike" | "code";

export type DocsTextBlockType =
  | "paragraph"
  | "heading_1"
  | "heading_2"
  | "heading_3"
  | "heading_4"
  | "bulleted_list"
  | "numbered_list"
  | "to_do"
  | "toggle"
  | "quote"
  | "callout";

export type DocsBlockType =
  | DocsTextBlockType
  | "code"
  | "divider"
  | "image"
  | "file"
  | "bookmark"
  | "page"
  | "table"
  | "database"
  | "connected_page"
  | "column_list"
  | "column"
  | "synced_block"
  | "table_of_contents"
  | "breadcrumb"
  | "equation"
  | "button"
  | "link_to_page"
  | "video"
  | "audio"
  | "pdf"
  | "embed";

/** A block a button inserts (a template, not on the page). */
export interface DocsBlockButtonBlock {
  type: DocsBlockType;
  props: DocsBlockProps;
  children: DocsBlockButtonBlock[];
}

/** What a button does when clicked: insert its blocks below it, open a link, or add a database row with preset values (property id → value). */
export type DocsBlockButtonAction =
  | { type: "insert"; blocks: DocsBlockButtonBlock[] }
  | { type: "link"; url: string }
  | { type: "add_row"; database_id: string; title?: string; values: Record<string, DocsValue> };

/**
 * Props by type (every block may carry `color`): text types `{text}` (+ to_do `checked`, callout `icon`, headings `toggleable`);
 * code `{code, language, wrap?, caption}`; image `{file_id, name, content_type, size, caption, width, align?, alt?}`;
 * video `{… width, align?}`; audio, file `{file_id, name, content_type, size, caption}`; pdf `{… height?}`;
 * bookmark `{url, title, description?, site_name?, preview_id?, caption}`; embed `{url, provider, src, caption, height?, width?, align?}`
 * (`src` is computed by the server from an allowlist); page and link_to_page `{page_id}`; database `{database_id}`;
 * table `{rows: DocsSpan[][][], header_row, header_col}`; equation `{expression}` (TeX); button `{label, actions}`;
 * column `{width?}` (a fraction; columns only inside a column_list); synced_block `{}` (an original holding blocks) or
 * `{source_id, source_page_id}` (a copy: the original's blocks, opened with the viewer's own access);
 * divider, table_of_contents, breadcrumb, column_list `{}`;
 * connected_page `{provider, account_id, ref, title, url}` (a Notion page or Google Doc each viewer opens with their own connection).
 * Blocks of connected pages: image, file, video, audio and pdf carry `url` (the platform's link) instead of `file_id`.
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
  provider?: string;
  account_id?: string;
  ref?: string;
  color?: DocsColor;
  toggleable?: boolean;
  wrap?: boolean;
  align?: "left" | "center" | "right";
  alt?: string;
  height?: number;
  description?: string;
  site_name?: string;
  preview_id?: string;
  src?: string;
  expression?: string;
  label?: string;
  actions?: DocsBlockButtonAction[];
  source_id?: string;
  source_page_id?: string;
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
  /** A built-in cover drawn from the palette (DOCS_COVER_GALLERY). */
  gallery?: DocsCoverGallery;
  /** Vertical focus of an image cover, 0 (top) to 100. */
  position: number;
}

export type DocsMyLevel = AccessLevel | "none";

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
  /** An icon from the app's set or an uploaded image (icon is null then). */
  custom_icon?: DocsCustomIcon | null;
  font?: DocsPageFont;
  small_text?: boolean;
  /** A workspace template (kept out of the tree and search). */
  is_template?: boolean;
  /** Verified and not expired. */
  verified?: boolean;
  verified_by?: string | null;
  verified_at?: string | null;
  verified_until?: string | null;
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
  custom_icon?: DocsCustomIcon | null;
  /** Trash lists: who trashed it and the page it was in. */
  deleted_by?: DocsRefUser | null;
  parent_title?: string | null;
}

export interface DocsSidebar {
  favorites: DocsPageSummary[];
  /** Teams the caller is on with the pages shared with them. */
  teamspaces?: DocsTeamspace[];
  hidden_teamspaces?: DocsTeamspace["team"][];
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
  /** The caller gets edits (batched) and comments. */
  following?: boolean;
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
  reactions?: DocsReaction[];
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

/** A link's preview (bookmark cards): the server fetched it once; `icon_url` and `image_url` are short-lived links to its copies. */
export interface DocsLinkPreview {
  id: string;
  url: string;
  title: string;
  description: string;
  site_name: string;
  icon_url?: string;
  image_url?: string;
  /** The link can also be shown as an embed. */
  embed?: { provider: string; name: string };
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
  /** The published page's options. */
  settings?: DocsPublishSettings;
}

// ---- connected platforms (Notion, Google Docs)

/** Where connected content comes from (always a connection). */
export type DocsConnectedSourceRef = Extract<SourceRef, { type: "connected" }>;

/** A connected account in Docs (`FeatureSource` plus whether changes are allowed). */
export interface DocsConnectedSource extends FeatureSource {
  /** The platform's id for the account (a Notion workspace), which connected-page blocks record. */
  account_id?: string;
  can_write: boolean;
  /** Grants changes when `can_write` is false. */
  write_url?: string;
}

/** `GET /v1/docs/sources` (`FeatureSources`). */
export interface DocsConnectedSources {
  sources: DocsConnectedSource[];
  offers: ConnectionOffer[];
}

/** A connected page or database in trees, lists and search. `id` is the platform's id. */
export interface DocsConnectedItem {
  id: string;
  connection_id: string;
  kind: "page" | "database";
  title: string;
  icon: string | null;
  has_children: boolean;
  parent_ref: string | null;
  updated_at: string;
  /** Google: the owner's name. */
  snippet?: string;
  /** The page on the platform. */
  url: string;
  /** The app path that opens it here. */
  link: string;
  source: DocsConnectedSourceRef;
}

export interface DocsConnectedList {
  items: DocsConnectedItem[];
  next_cursor: string | null;
}

/** `GET /v1/docs/connected/{connection}/pages/{ref}`: a page detail plus where it comes from and its editing session. */
export interface DocsConnectedPageDetail extends DocsPageDetail {
  ref: string;
  connection_id: string;
  /** Sent with every edit; a new one comes with each load. */
  session_id: string;
  source: DocsConnectedSourceRef;
  connection: DocsConnectedSource;
  url: string;
  /** The account may change it on the platform and the connection allows changes from here. */
  can_edit: boolean;
  /** Blocks shown but not changed here ("Edit in Notion"). */
  read_only_blocks: string[];
  /** More blocks than Docs shows: open it on the platform for the rest. */
  partial: boolean;
  /** A database row's database (platform id). */
  database_ref?: string;
}

export interface DocsConnectedComment {
  id: string;
  thread_id: string;
  block_id: string | null;
  quote: string | null;
  body: DocsSpan[];
  author: { id: string; name: string; avatar_url: string | null } | null;
  created_at: string;
  resolved: boolean;
  replies: DocsConnectedComment[];
}

/** What a viewer sees of a connected-page block, through their own connection. */
export interface DocsResolvedLink {
  state: "ok" | "connect" | "reconnect" | "no_access" | "blocked";
  connection_id?: string;
  item?: DocsConnectedItem;
  /** The first top-level blocks, for the live preview. */
  blocks: DocsBlock[];
  refs?: DocsRefs;
  offer?: ConnectionOffer;
  fix_url?: string;
  more: boolean;
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
  /** Top level only: a page in this teamspace (the team can edit it). */
  team_id?: string | null;
}

export interface DocsUpdatePageRequest {
  title?: string;
  icon?: string | null;
  custom_icon?: DocsCustomIcon | null;
  font?: DocsPageFont;
  small_text?: boolean;
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
    /** A page changed; or { row_id, reload } for a row whose relation changed from the other side. */
    "docs.page": { page?: DocsPage; row_id?: string; reload?: boolean };
    "docs.presence": DocsPresenceEvent;
    "docs.comments": { page_id: string; thread_id: string };
    "docs.reload": { page_id: string; version: number };
    "docs.rows": { database_id: string; row_id?: string };
    "docs.schema": { database_id: string };
    "docs.tree": { org_id: string };
  }
}
