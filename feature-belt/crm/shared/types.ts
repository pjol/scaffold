import type { AccessLevel } from "../access/types";
import type { ConnectionOffer, FeatureSource, SourceRef } from "../sources";

/** The three record types. */
export type CrmObjectType = "company" | "contact" | "deal";

export const CRM_OBJECT_TYPES: readonly CrmObjectType[] = ["company", "contact", "deal"];

/** Access kind of each record type (for ShareDialog / ResourceMenu). */
export const CRM_KIND: Record<CrmObjectType, string> = { company: "crm.company", contact: "crm.contact", deal: "crm.deal" };

/** URL segment of each type: /crm/companies/<id>. */
export const CRM_PATH: Record<CrmObjectType, "companies" | "contacts" | "deals"> = { company: "companies", contact: "contacts", deal: "deals" };

export type CrmLevel = AccessLevel | "none";

export type CrmStageOutcome = "open" | "won" | "lost";
export type CrmTagTone = "neutral" | "primary" | "accent" | "success" | "warning" | "danger" | "info";
export type CrmLifecycleStage = "subscriber" | "lead" | "mql" | "sql" | "opportunity" | "customer" | "evangelist" | "other";
export type CrmRecordAccess = "everyone_edits" | "everyone_views" | "owners_teams";

export interface CrmUserRef {
  id: string;
  name: string;
  avatar_url: string | null;
}

export interface CrmTeamRef {
  id: string;
  name: string;
  handle: string;
}

export interface CrmTag {
  id: string;
  name: string;
  tone: CrmTagTone;
}

/** A record you can open, by name. */
export interface CrmRecordRef {
  type: CrmObjectType;
  id: string;
  name: string;
}

export interface CrmStageRef {
  id: string;
  name: string;
  probability: number;
  outcome: CrmStageOutcome;
  position: number;
}

/** A custom property value as stored (select values, ids for people, minor units for currency). */
export type CrmPropValue = string | number | boolean | string[] | null;

/** Fields every record has. */
export interface CrmRecordBase {
  id: string;
  org_id: string;
  type: CrmObjectType;
  /** Display name (contacts: full name, else email). */
  title: string;
  owner: CrmUserRef | null;
  team: CrmTeamRef | null;
  tags: CrmTag[];
  /** Custom property values by key. */
  props: Record<string, CrmPropValue>;
  /** Names for person-valued custom properties. */
  props_display: Record<string, string>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  last_activity_at: string | null;
  my_level: CrmLevel;
  /**
   * Where the record lives: absent for the app's own; a connected HubSpot or Salesforce
   * account otherwise (read live with your own connection; its id is an xref, see `crmIsConnectedId`).
   */
  source?: SourceRef | null;
}

export interface CrmCompany extends CrmRecordBase {
  type: "company";
  name: string;
  domain: string | null;
  industry: string;
  employees: number | null;
  phone: string;
  city: string;
  country: string;
  description: string;
}

export interface CrmContact extends CrmRecordBase {
  type: "contact";
  first_name: string;
  last_name: string;
  name: string;
  /** The first is primary. */
  emails: string[];
  phones: string[];
  job_title: string;
  lifecycle_stage: CrmLifecycleStage;
  city: string;
  country: string;
  /** Primary company, when you can see it. */
  company: CrmRecordRef | null;
}

export interface CrmDeal extends CrmRecordBase {
  type: "deal";
  name: string;
  /** Minor units of `currency`. */
  amount: number | null;
  currency: string;
  /** amount × stage probability. */
  weighted_amount: number | null;
  pipeline_id: string;
  pipeline_name: string;
  stage: CrmStageRef;
  /** YYYY-MM-DD */
  close_date: string | null;
  company: CrmRecordRef | null;
  stage_entered_at: string;
  closed_at: string | null;
}

export type CrmRecord = CrmCompany | CrmContact | CrmDeal;

export interface CrmRecordOf {
  company: CrmCompany;
  contact: CrmContact;
  deal: CrmDeal;
}

/** A list page; `total` is the number of matches, on the first page. */
export interface CrmRecordPage<T extends CrmRecord = CrmRecord> {
  items: T[];
  next_cursor: string | null;
  total: number | null;
  /** `source=all`: sources left out of this page (filters they can't take, a connection that failed). */
  skipped_sources?: CrmSkippedSource[];
}

export interface CrmAssocCompany {
  id: string;
  name: string;
  domain: string | null;
  is_primary: boolean;
}

export interface CrmAssocContact {
  id: string;
  name: string;
  email: string | null;
  phone: string | null;
  job_title: string;
}

export interface CrmAssocDeal {
  id: string;
  name: string;
  amount: number | null;
  currency: string;
  stage_name: string;
  stage_outcome: CrmStageOutcome;
  close_date: string | null;
}

/** Linked records you can see. */
export interface CrmAssociations {
  companies: CrmAssocCompany[];
  contacts: CrmAssocContact[];
  deals: CrmAssocDeal[];
}

export interface CrmRecordDetail<T extends CrmRecord = CrmRecord> {
  record: T;
  associations: CrmAssociations;
  can_delete: boolean;
}

// ---- properties, filters, views ------------------------------------------------

/** Property types: built-in ones add long_text, datetime, team, tags, stage, pipeline, company, domain, emails, phones, currency_code. */
export type CrmPropertyType =
  | "text"
  | "long_text"
  | "number"
  | "date"
  | "datetime"
  | "select"
  | "multi_select"
  | "user"
  | "team"
  | "url"
  | "email"
  | "phone"
  | "currency"
  | "currency_code"
  | "checkbox"
  | "tags"
  | "stage"
  | "pipeline"
  | "company"
  | "domain"
  | "emails"
  | "phones";

/** Types an org can add as custom properties. */
export type CrmCustomPropertyType = "text" | "number" | "date" | "select" | "multi_select" | "user" | "url" | "email" | "phone" | "currency" | "checkbox";

export const CRM_CUSTOM_TYPES: readonly CrmCustomPropertyType[] = ["text", "number", "date", "select", "multi_select", "user", "url", "email", "phone", "currency", "checkbox"];

export interface CrmOption {
  value: string;
  label: string;
}

export interface CrmProperty {
  /** Custom properties only. */
  id: string | null;
  key: string;
  label: string;
  type: CrmPropertyType;
  options: CrmOption[];
  builtin: boolean;
  editable: boolean;
  filterable: boolean;
  sortable: boolean;
  required: boolean;
  importable: boolean;
  position: number;
}

export type CrmFilterOp =
  | "eq"
  | "neq"
  | "contains"
  | "not_contains"
  | "empty"
  | "not_empty"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "on"
  | "before"
  | "after"
  | "last_days"
  | "next_days"
  | "is_any"
  | "is_none"
  | "has_any"
  | "has_all"
  | "has_none"
  | "is_true"
  | "is_false";

/** One condition of a list; all conditions must hold. */
export interface CrmFilter {
  key: string;
  op: CrmFilterOp;
  value?: string | number | string[];
}

export type CrmSortDir = "asc" | "desc";

/** A list request. */
export interface CrmListParams {
  filters?: CrmFilter[];
  q?: string;
  sort?: string;
  dir?: CrmSortDir;
  tz?: string;
  limit?: number;
  /** "app" (default where the workspace shows the app's records), "all", or a connection id. */
  source?: string;
}

export interface CrmView {
  id: string;
  org_id: string;
  object_type: CrmObjectType;
  name: string;
  owner: CrmUserRef | null;
  shared: boolean;
  columns: string[];
  sort_key: string;
  sort_dir: CrmSortDir;
  filters: CrmFilter[];
  q: string;
  can_edit: boolean;
  created_at: string;
  updated_at: string;
}

export interface CrmViewRequest {
  object_type?: CrmObjectType;
  name?: string;
  shared?: boolean;
  columns?: string[];
  sort_key?: string;
  sort_dir?: CrmSortDir;
  filters?: CrmFilter[];
  q?: string;
}

// ---- settings and pipelines ------------------------------------------------------

export interface CrmSettings {
  org_id: string;
  record_access: CrmRecordAccess;
  currency: string;
  /** The app's own CRM, connected CRMs, or both side by side. */
  source_mode: CrmSourceMode;
  /** Platforms people may connect to the CRM. */
  source_providers: CrmProvider[];
  updated_at: string;
  /** What you may do here. */
  can: { manage_settings: boolean; export: boolean; import: boolean; delete: boolean };
}

export interface CrmStage {
  id: string;
  name: string;
  position: number;
  probability: number;
  outcome: CrmStageOutcome;
}

export interface CrmPipeline {
  id: string;
  org_id: string;
  name: string;
  position: number;
  stages: CrmStage[];
}

export interface CrmStageInput {
  id?: string;
  name: string;
  probability: number;
  outcome: CrmStageOutcome;
}

export interface CrmPipelineRequest {
  name?: string;
  position?: number;
  stages?: CrmStageInput[];
  /** Removed stage id → stage its deals move to. */
  stage_moves?: Record<string, string>;
}

export interface CrmPropertyRequest {
  object_type?: CrmObjectType;
  key?: string;
  label?: string;
  type?: CrmCustomPropertyType;
  options?: CrmOption[];
  position?: number;
}

// ---- activities and timeline -------------------------------------------------------

export type CrmActivityType = "note" | "call" | "email" | "meeting" | "task";
export type CrmCallOutcome = "connected" | "no_answer" | "left_voicemail" | "busy" | "wrong_number";
export type CrmMeetingOutcome = "scheduled" | "completed" | "rescheduled" | "no_show" | "canceled";
export type CrmPriority = "none" | "low" | "medium" | "high";
export type CrmDirection = "inbound" | "outbound";

export interface CrmMention {
  id: string;
  type: "user" | "team";
  name: string;
}

export interface CrmActivity {
  id: string;
  org_id: string;
  type: CrmActivityType;
  /** Linked records you can see; the one it was logged on first. */
  records: CrmRecordRef[];
  subject: string;
  /** Plain text with mention tokens `<@userId>` / `<@team:teamId>`. */
  body: string;
  mentions: CrmMention[];
  occurred_at: string;
  duration_minutes: number | null;
  outcome: string | null;
  direction: CrmDirection | null;
  due_at: string | null;
  assignee: CrmUserRef | null;
  reminder_at: string | null;
  done_at: string | null;
  priority: CrmPriority;
  created_by: CrmUserRef | null;
  /** The AI agent that logged it for created_by. */
  agent: string | null;
  created_at: string;
  updated_at: string;
  can_edit: boolean;
  /** On create/edit: people mentioned who can't see the record (not notified). */
  unreachable?: string[];
  /** Connected activities: the platform they live on. */
  source?: SourceRef | null;
}

export interface CrmActivityRequest {
  type?: CrmActivityType;
  subject?: string;
  body?: string;
  occurred_at?: string;
  duration_minutes?: number;
  outcome?: string;
  direction?: CrmDirection;
  due_at?: string;
  assignee_id?: string;
  reminder_at?: string;
  priority?: CrmPriority;
  done?: boolean;
  also_on?: { type: CrmObjectType; id: string }[];
  clear_due?: boolean;
  clear_reminder?: boolean;
}

export type CrmEventKind = "created" | "updated" | "stage_changed" | "associated" | "dissociated" | "merged" | "imported";

export interface CrmChange {
  key: string;
  label: string;
  before: string;
  after: string;
}

/** System entry on a timeline; `data` depends on kind (changes, from_stage/to_stage, other_type/other_name or other_hidden, merged_name). */
export interface CrmEvent {
  kind: CrmEventKind;
  actor: CrmUserRef | null;
  agent: string | null;
  data: {
    changes?: CrmChange[];
    from_stage?: string;
    to_stage?: string;
    to_outcome?: CrmStageOutcome;
    seconds_in_stage?: number;
    from_pipeline?: string;
    to_pipeline?: string;
    other_type?: CrmObjectType;
    other_id?: string;
    other_name?: string;
    other_hidden?: boolean;
    merged_name?: string;
  };
}

export interface CrmTimelineItem {
  id: string;
  kind: "activity" | "event";
  at: string;
  activity?: CrmActivity;
  event?: CrmEvent;
}

export type CrmTimelineFilter = "all" | "activities" | "notes" | "calls" | "emails" | "meetings" | "tasks" | "changes";

export interface CrmTimelinePage {
  items: CrmTimelineItem[];
  next_cursor: string | null;
  /** First page only: open tasks on the record, soonest first. */
  upcoming: CrmActivity[] | null;
}

export type CrmTaskBucket = "open" | "overdue" | "today" | "upcoming" | "done";

export interface CrmTaskSummary {
  overdue: number;
  today: number;
  upcoming: number;
}

export interface CrmStageStay {
  stage_id: string;
  stage_name: string;
  entered_at: string;
  exited_at: string | null;
  seconds: number;
  by: CrmUserRef | null;
}

export interface CrmStageHistory {
  stays: CrmStageStay[];
  totals: { stage_id: string; stage_name: string; seconds: number }[];
}

// ---- board, dashboard, search ------------------------------------------------------

export interface CrmMoneyTotal {
  currency: string;
  amount: number;
  weighted: number;
}

export interface CrmBoardColumn {
  stage: CrmStage;
  count: number;
  totals: CrmMoneyTotal[];
  deals: CrmDeal[];
  next_cursor: string | null;
}

export interface CrmBoard {
  pipeline: CrmPipeline;
  columns: CrmBoardColumn[];
  open_totals: CrmMoneyTotal[];
  /** Connected boards: sums cover the first 1,000 deals (counts stay exact). */
  partial?: boolean;
  source?: SourceRef | null;
}

export interface CrmDashboard {
  pipeline: CrmPipeline;
  currency: string;
  open_by_stage: { stage_id: string; name: string; count: number; totals: CrmMoneyTotal[] }[];
  open_totals: CrmMoneyTotal[];
  won_this_month: { count: number; totals: CrmMoneyTotal[] };
  lost_this_month: { count: number; totals: CrmMoneyTotal[] };
  activity_this_week: { user: CrmUserRef; notes: number; calls: number; emails: number; meetings: number; tasks_done: number; total: number }[];
  my_tasks: CrmTaskSummary;
  source?: SourceRef | null;
}

export interface CrmSearchHit {
  type: CrmObjectType;
  id: string;
  org_id: string;
  title: string;
  /** Email, domain or stage. */
  detail: string;
  score: number;
  updated_at: string;
  source?: SourceRef | null;
}

export interface CrmDuplicate extends CrmRecordRef {
  /** The email or domain that matched. */
  match: string;
}

// ---- bulk, merge, import -----------------------------------------------------------

export type CrmBulkAction = "assign_owner" | "add_tag" | "remove_tag" | "move_stage" | "delete";

export interface CrmBulkRequest {
  object_type: CrmObjectType;
  ids: string[];
  action: CrmBulkAction;
  owner_id?: string | null;
  tag_id?: string;
  stage_id?: string;
}

export interface CrmBulkResult {
  updated: number;
  skipped: { id: string; reason: string }[];
}

export type CrmDedupe = "update" | "skip" | "create";

export interface CrmImportTarget {
  key: string;
  label: string;
  type: CrmPropertyType;
  required: boolean;
}

export interface CrmImportInspect {
  headers: string[];
  sample: string[][];
  row_count: number;
  targets: CrmImportTarget[];
  /** Suggested target per column (null: skip). */
  mapping: (string | null)[];
}

export interface CrmImportRequest {
  object_type: CrmObjectType;
  filename?: string;
  csv: string;
  mapping?: (string | null)[];
  dedupe?: CrmDedupe;
  pipeline_id?: string;
  create_companies?: boolean;
}

export interface CrmImportRow {
  row: number;
  action: "create" | "update" | "skip" | "error";
  message?: string;
  title?: string;
  record_id?: string;
}

export interface CrmImportPreview {
  total: number;
  create: number;
  update: number;
  skip: number;
  error: number;
  rows: CrmImportRow[];
  new_tags: string[];
  new_companies: number;
}

export interface CrmImport {
  id: string;
  object_type: CrmObjectType;
  filename: string;
  status: "running" | "done" | "failed";
  dedupe: CrmDedupe;
  total_rows: number;
  processed: number;
  created_count: number;
  updated_count: number;
  skipped_count: number;
  error_count: number;
  errors: CrmImportRow[];
  created_by: CrmUserRef | null;
  created_at: string;
  finished_at: string | null;
}

// ---- connected CRMs (HubSpot, Salesforce) ------------------------------------------

export type CrmSourceMode = "app" | "connected" | "both";
export type CrmProvider = "hubspot" | "salesforce";
export const CRM_PROVIDERS: readonly CrmProvider[] = ["hubspot", "salesforce"];

/** Capability keys (Settings → Connections). */
export const CRM_CAPABILITIES = {
  hubspot: { read: "crm.hubspot.read", write: "crm.hubspot.write" },
  salesforce: { read: "crm.salesforce.read", write: "crm.salesforce.write" },
} as const;

/** Source ids lists, boards and dashboards take besides a connection id. */
export const CRM_SOURCE_APP = "app";
export const CRM_SOURCE_ALL = "all";

/** One source with whether it can be edited from here. */
export interface CrmFeatureSource extends FeatureSource {
  writable: boolean;
  /** Opens the account on the platform. */
  url?: string;
}

/** `GET /v1/crm/sources`: the shared FeatureSources plus the workspace's mode. */
export interface CrmSources {
  sources: CrmFeatureSource[];
  offers: ConnectionOffer[];
  mode: CrmSourceMode;
  providers: CrmProvider[];
}

export interface CrmSkippedSource {
  id: string;
  label: string;
  code: string;
  message: string;
}

/** A platform owner (owner pickers and assignees on connected records). */
export interface CrmOwner extends CrmUserRef {
  email?: string;
}

/** One of your companies linked to a platform company; the platform side is read with your own connection. */
export interface CrmCompanyLink {
  id: string;
  provider: CrmProvider;
  account_id: string;
  external_id: string;
  created_at: string;
  /** For you: ok, no_connection (connect it to see), not_visible (your account can't see it), error, off. */
  status: "ok" | "no_connection" | "not_visible" | "error" | "off";
  message?: string;
  source?: SourceRef | null;
  record?: CrmCompany;
  deals: CrmAssocDeal[];
  contacts: CrmAssocContact[];
  timeline: CrmActivity[];
  can_remove: boolean;
  capability?: string;
}

export type CrmImportDedupe = "update" | "skip" | "create";

export interface CrmSourceImportResult {
  created: number;
  updated: number;
  skipped: number;
  rows: { source_id: string; action: "created" | "updated" | "skipped"; record?: CrmRecordRef }[];
  errors: { source_id: string; message: string }[];
}

/** True for a connected record's id (`<connection id>~<platform id>`); the app's own ids are UUIDs. */
export function crmIsConnectedId(id: string): boolean {
  return id.includes("~");
}

/** The connection a connected record id belongs to, else null. */
export function crmConnectionOfId(id: string): string | null {
  const at = id.indexOf("~");
  return at > 0 ? id.slice(0, at) : null;
}

/** The source id for a record: its connection, or "app". */
export function crmSourceIdOf(row: { id: string; source?: SourceRef | null }): string {
  if (row.source && row.source.type === "connected") return row.source.connection_id;
  return crmConnectionOfId(row.id) ?? CRM_SOURCE_APP;
}

/** Org permission keys. */
export const CRM_PERMISSION_ACCESS = "crm.access";
export const CRM_PERMISSION_MANAGE_SETTINGS = "crm.manage_settings";
export const CRM_PERMISSION_EXPORT = "crm.export";
export const CRM_PERMISSION_IMPORT = "crm.import";
export const CRM_PERMISSION_DELETE = "crm.delete";

declare module "../types" {
  interface RealtimeEventMap {
    "crm.record_changed": { type: CrmObjectType; id: string };
    "crm.record_deleted": { type: CrmObjectType; id: string };
    "crm.timeline_changed": { type: CrmObjectType; id: string };
    "crm.board_changed": { pipeline_id: string };
    "crm.settings_changed": { org_id: string; what: "settings" | "pipelines" | "properties" | "tags" | "views" };
  }
}
