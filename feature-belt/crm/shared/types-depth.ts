import type { AccessLevel } from "../access/types";
import type {
  CrmFilter,
  CrmRecordRef,
  CrmStage,
  CrmPipeline,
  CrmTicket,
  CrmUserRef,
  CrmTeamRef,
  CrmObjectType,
  CrmOption,
  CrmPriority,
  CrmPropertyType,
  CrmLifecycleStage,
} from "./types";

// ---- products, line items, quotes ------------------------------------------------

export type CrmBilling = "one_time" | "recurring";
export type CrmBillingPeriod = "monthly" | "quarterly" | "semiannual" | "annual";

export const CRM_BILLING_PERIODS: readonly CrmBillingPeriod[] = ["monthly", "quarterly", "semiannual", "annual"];

/** A product in the library. Money in minor units of `currency`; rates in basis points (825 = 8.25 %). */
export interface CrmProduct {
  id: string;
  org_id: string;
  name: string;
  sku: string | null;
  description: string;
  unit_price: number;
  currency: string;
  billing: CrmBilling;
  billing_period: CrmBillingPeriod | null;
  /** Default term for new line items (recurring), in billing periods. */
  term_periods: number | null;
  tax_bp: number;
  discount_bp: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CrmProductRequest {
  name?: string;
  sku?: string;
  description?: string;
  unit_price?: number;
  currency?: string;
  billing?: CrmBilling;
  billing_period?: CrmBillingPeriod | "";
  term_periods?: number;
  tax_bp?: number;
  discount_bp?: number;
  archived?: boolean;
}

/** A line's money: subtotal = unit price × quantity, then discount, tax, the total per billing period and the contract value (× term). */
export interface CrmLineTotals {
  subtotal: number;
  discount: number;
  tax: number;
  total: number;
  contract_value: number;
}

export interface CrmLineItem extends CrmLineTotals {
  id: string;
  deal_id: string;
  product_id: string | null;
  position: number;
  name: string;
  sku: string;
  description: string;
  unit_price: number;
  /** Up to 3 decimals. */
  quantity: number;
  discount_bp: number;
  /** A fixed discount on the line (or discount_bp, not both). */
  discount_amount: number;
  tax_bp: number;
  billing: CrmBilling;
  billing_period: CrmBillingPeriod | null;
  term_periods: number;
  created_at: string;
}

export interface CrmTotals {
  subtotal: number;
  discount: number;
  tax: number;
  /** One-time lines plus one billing period of each recurring line. */
  total: number;
  one_time: number;
  recurring: { billing_period: CrmBillingPeriod; total: number }[];
  /** Recurring lines for their whole term: what a deal's amount follows. */
  contract_value: number;
}

export interface CrmLineItems {
  items: CrmLineItem[];
  totals: CrmTotals;
  currency: string;
  amount_from_items: boolean;
  can_edit: boolean;
}

/** Add a line (product_id copies the product; any field overrides it) or change one. */
export interface CrmLineItemRequest {
  product_id?: string;
  name?: string;
  sku?: string;
  description?: string;
  unit_price?: number;
  quantity?: number;
  discount_bp?: number;
  discount_amount?: number;
  tax_bp?: number;
  billing?: CrmBilling;
  billing_period?: CrmBillingPeriod | "";
  term_periods?: number;
}

export type CrmQuoteStatus = "draft" | "published" | "accepted" | "declined";

export interface CrmQuoteItem extends CrmLineTotals {
  id: string;
  product_id: string | null;
  position: number;
  name: string;
  sku: string;
  description: string;
  unit_price: number;
  quantity: number;
  discount_bp: number;
  discount_amount: number;
  tax_bp: number;
  billing: CrmBilling;
  billing_period: CrmBillingPeriod | null;
  term_periods: number;
}

export interface CrmQuote {
  id: string;
  org_id: string;
  deal: CrmRecordRef;
  /** Q-0001 */
  number: string;
  title: string;
  status: CrmQuoteStatus;
  /** Published and past expires_at. */
  expired: boolean;
  currency: string;
  expires_at: string;
  terms: string;
  comments: string;
  contact: CrmRecordRef | null;
  sender: CrmUserRef | null;
  /** Frozen when published. */
  sender_name: string;
  sender_email: string;
  buyer_name: string;
  buyer_company: string;
  seller_name: string;
  items: CrmQuoteItem[];
  totals: CrmTotals;
  /** Published: the public link (people who can edit the quote). */
  url: string | null;
  published_at: string | null;
  first_viewed_at: string | null;
  last_viewed_at: string | null;
  view_count: number;
  signer_name: string | null;
  signer_email: string | null;
  /** People who can edit the quote. */
  signer_ip: string | null;
  decided_at: string | null;
  decline_reason: string | null;
  /** The AI agent that drafted it. */
  agent: string | null;
  created_by: CrmUserRef | null;
  created_at: string;
  updated_at: string;
  my_level: AccessLevel;
  /** You may publish it (edit, crm.publish_quotes, access.share_public; never AI agents). */
  can_publish: boolean;
}

export interface CrmQuoteSummary {
  id: string;
  number: string;
  title: string;
  status: CrmQuoteStatus;
  expired: boolean;
  currency: string;
  total: number;
  expires_at: string;
  published_at: string | null;
  decided_at: string | null;
  view_count: number;
  signer_name: string | null;
  created_at: string;
}

export interface CrmQuoteRequest {
  title?: string;
  expires_at?: string;
  contact_id?: string | null;
  terms?: string;
  comments?: string;
  /** Replaces every line (drafts). */
  items?: CrmLineItemRequest[];
}

/** A quote as the buyer sees it at /q/<token>. */
export interface CrmPublicQuote {
  number: string;
  title: string;
  status: Exclude<CrmQuoteStatus, "draft">;
  currency: string;
  expires_at: string;
  published_at: string;
  seller_name: string;
  sender_name: string;
  sender_email: string;
  buyer_name: string;
  buyer_company: string;
  terms: string;
  comments: string;
  items: CrmQuoteItem[];
  totals: CrmTotals;
  signer_name: string | null;
  decided_at: string | null;
  decline_reason: string | null;
}

export interface CrmQuoteDecision {
  name: string;
  email?: string;
  /** Accept: the buyer ticked "I accept". */
  accept?: boolean;
  /** Decline: optional reason. */
  reason?: string;
}

// ---- tickets board -------------------------------------------------------------

export interface CrmTicketBoardColumn {
  stage: CrmStage;
  count: number;
  deals: CrmTicket[];
  next_cursor: string | null;
}

export interface CrmTicketBoard {
  pipeline: CrmPipeline;
  columns: CrmTicketBoardColumn[];
}

// ---- email ----------------------------------------------------------------------

export interface CrmMailAccount {
  connection_id: string;
  provider: "google" | "microsoft";
  account: string;
  status: string;
  can_send: boolean;
  can_read: boolean;
}

export interface CrmMailAccounts {
  accounts: CrmMailAccount[];
  /** Mail capabilities you could still connect or grant (Settings → Connections). */
  offers: { capability: string; provider: string; label: string; connect_url: string }[];
  /** The product wired calendars for "Log meeting" / "Insert booking link". */
  meetings: boolean;
  booking_links: boolean;
}

export interface CrmSendMailRequest {
  connection_id: string;
  to: string[];
  cc?: string[];
  subject: string;
  body: string;
  deal_id?: string;
  ticket_id?: string;
  draft_id?: string;
}

/** A message in your mailbox (read live, not stored). */
export interface CrmMailSummary {
  message_id: string;
  thread_id: string;
  subject: string;
  snippet: string;
  from: string;
  to: string[];
  at: string;
  inbound: boolean;
  logged: boolean;
}

export interface CrmEmailTemplate {
  id: string;
  org_id: string;
  owner: CrmUserRef | null;
  shared: boolean;
  name: string;
  subject: string;
  body: string;
  can_edit: boolean;
  updated_at: string;
}

export interface CrmMergeField {
  key: string;
  label: string;
}

export interface CrmEmailTemplateRequest {
  name?: string;
  subject?: string;
  body?: string;
  shared?: boolean;
}

export interface CrmRendered {
  subject: string;
  body: string;
  /** Merge fields that rendered empty without a fallback. */
  missing: string[];
}

export interface CrmEmailDraft {
  id: string;
  org_id: string;
  contact: CrmRecordRef;
  deal: CrmRecordRef | null;
  ticket: CrmRecordRef | null;
  to: string[];
  cc: string[];
  subject: string;
  body: string;
  /** The AI agent that wrote it. */
  agent: string | null;
  created_at: string;
  updated_at: string;
}

export interface CrmEmailDraftRequest {
  to?: string[];
  cc?: string[];
  subject?: string;
  body?: string;
  deal_id?: string | null;
  ticket_id?: string | null;
}

export interface CrmBookingLink {
  title: string;
  url: string;
}

export interface CrmMeetingCandidate {
  id: string;
  title: string;
  start: string;
  end: string;
  attendees: string[];
  location?: string;
  source?: string;
}

// ---- reports, dashboards, forecast, goals -----------------------------------------

export type CrmReportMeasure = "count" | "sum" | "avg";
export type CrmReportChart = "bar" | "line" | "donut" | "table" | "number";
export type CrmReportBucket = "day" | "week" | "month" | "quarter" | "year";

export interface CrmReportSpec {
  object: CrmObjectType;
  measure: CrmReportMeasure;
  /** sum/avg: a number or amount property. */
  property?: string;
  /** A property key ("" = one total). */
  group_by?: string;
  /** Date group_by. */
  bucket?: CrmReportBucket;
  filters?: CrmFilter[];
  chart: CrmReportChart;
  /** Money sums: the currency (default the workspace's). */
  currency?: string;
}

export interface CrmReportRow {
  key: string;
  label: string;
  value: number;
  count: number;
}

export interface CrmReportResult {
  rows: CrmReportRow[];
  total: number;
  count: number;
  value_type: "count" | "money" | "number";
  currency?: string;
  truncated: boolean;
  spec: CrmReportSpec;
}

export interface CrmReport {
  id: string;
  name: string;
  position: number;
  spec: CrmReportSpec;
  result?: CrmReportResult;
  /** The report can't run any more (a property was deleted…). */
  error?: string;
}

export interface CrmReportDashboard {
  id: string;
  org_id: string;
  name: string;
  owner: CrmUserRef | null;
  my_level: AccessLevel;
  reports: CrmReport[] | null;
  report_count?: number;
  created_at: string;
  updated_at: string;
}

export interface CrmForecastCell {
  /** YYYY-MM */
  month: string;
  pipeline: number;
  best_case: number;
  commit: number;
  closed: number;
  /** Open deals × stage probability. */
  weighted: number;
  count: number;
}

export interface CrmForecastRow {
  /** null: deals without an owner. */
  owner: CrmUserRef | null;
  cells: CrmForecastCell[];
  total: CrmForecastCell;
}

export interface CrmForecast {
  currency: string;
  pipeline_id: string | null;
  months: string[];
  rows: CrmForecastRow[];
  totals: CrmForecastCell[];
  total: CrmForecastCell;
}

export type CrmGoalMetric = "revenue_won" | "deals_won" | "deals_created";
export type CrmGoalPeriod = "month" | "quarter" | "year";

export interface CrmGoal {
  id: string;
  metric: CrmGoalMetric;
  user: CrmUserRef | null;
  team: CrmTeamRef | null;
  period: CrmGoalPeriod;
  period_start: string;
  /** Exclusive. */
  period_end: string;
  /** Minor units for revenue, a count otherwise. */
  target: number;
  currency: string | null;
  /** Over the deals you can see. */
  progress: number;
}

export interface CrmGoalRequest {
  metric: CrmGoalMetric;
  user_id?: string;
  team_id?: string;
  period: CrmGoalPeriod;
  period_start: string;
  target: number;
  currency?: string;
}

// ---- scoring ----------------------------------------------------------------------

export interface CrmScoreRule {
  id: string;
  label: string;
  /** Contact filters; every one must hold. */
  filters: CrmFilter[];
  points: number;
  position: number;
}

export interface CrmScoreRuleRequest {
  label?: string;
  filters?: CrmFilter[];
  points?: number;
  position?: number;
}

// ---- workflows ----------------------------------------------------------------------

export type CrmWorkflowTriggerType = "created" | "property_changed" | "stage_changed" | "date_reached";

export interface CrmWorkflowTrigger {
  type: CrmWorkflowTriggerType;
  property?: string;
  stage_id?: string;
  /** date_reached: days after (negative: before) the date. */
  offset_days?: number;
}

export type CrmWorkflowActionType =
  | "delay"
  | "set_property"
  | "create_task"
  | "rotate_owner"
  | "notify"
  | "add_tag"
  | "create_deal"
  | "create_ticket"
  | "webhook";

export interface CrmWorkflowAction {
  type: CrmWorkflowActionType;
  minutes?: number;
  property?: string;
  value?: unknown;
  /** create_task title; create_deal / create_ticket name ({{record.name}}, {{record.owner}}). */
  title?: string;
  body?: string;
  due_days?: number;
  assignee_id?: string;
  priority?: CrmPriority;
  team_id?: string;
  user_ids?: string[];
  notify_owner?: boolean;
  tag_id?: string;
  pipeline_id?: string;
  stage_id?: string;
  url?: string;
}

export interface CrmWorkflow {
  id: string;
  org_id: string;
  name: string;
  object_type: CrmObjectType;
  enabled: boolean;
  trigger: CrmWorkflowTrigger;
  conditions: CrmFilter[];
  actions: CrmWorkflowAction[];
  reenroll: boolean;
  /** Who it runs as: whoever last turned it on. */
  owner: CrmUserRef | null;
  /** Signs webhook actions (people who manage workflows). */
  secret?: string;
  /** Run counts: people who manage workflows only. */
  stats: { enrolled: number; waiting: number; done: number; failed: number } | null;
  created_at: string;
  updated_at: string;
}

export interface CrmWorkflowRequest {
  name?: string;
  object_type?: CrmObjectType;
  enabled?: boolean;
  trigger?: CrmWorkflowTrigger;
  conditions?: CrmFilter[];
  actions?: CrmWorkflowAction[];
  reenroll?: boolean;
}

export interface CrmWorkflowRunStep {
  step: number;
  type: string;
  status: "done" | "failed" | "skipped" | "waiting";
  message: string;
  at: string;
}

export interface CrmWorkflowRun {
  id: string;
  /** name is empty for records you can't open. */
  record: CrmRecordRef;
  status: "waiting" | "running" | "done" | "failed" | "stopped";
  step: number;
  resume_at: string | null;
  log: CrmWorkflowRunStep[];
  created_at: string;
  finished_at: string | null;
}

// ---- forms --------------------------------------------------------------------------

export interface CrmFormField {
  /** Property key; contact "email" is the primary email. */
  key: string;
  object: "contact" | "company";
  label: string;
  required: boolean;
  /** Read only: from the property. */
  type?: CrmPropertyType;
  options?: CrmOption[];
}

export interface CrmForm {
  id: string;
  org_id: string;
  name: string;
  fields: CrmFormField[];
  submit_label: string;
  success_message: string;
  consent_text: string;
  owner: CrmUserRef | null;
  notify_ids: string[];
  /** The people notified, by name. */
  notify: CrmUserRef[];
  lifecycle_stage: CrmLifecycleStage | null;
  tag_ids: string[];
  published: boolean;
  /** People who manage forms. */
  url: string | null;
  embed_html: string | null;
  submission_count: number;
  created_at: string;
  updated_at: string;
}

export interface CrmFormRequest {
  name?: string;
  fields?: CrmFormField[];
  submit_label?: string;
  success_message?: string;
  consent_text?: string;
  owner_id?: string;
  notify_ids?: string[];
  lifecycle_stage?: CrmLifecycleStage | "";
  tag_ids?: string[];
}

export interface CrmFormSubmission {
  id: string;
  contact: CrmRecordRef | null;
  created: boolean;
  /** Empty for contacts you can't open. */
  values: Record<string, string>;
  consent: boolean;
  page_url: string;
  created_at: string;
}

/** A form as anyone with the link sees it at /forms/<token>. */
export interface CrmPublicForm {
  name: string;
  seller_name: string;
  fields: CrmFormField[];
  submit_label: string;
  success_message: string;
  consent_text: string;
}

export interface CrmFormSubmit {
  /** "contact.first_name" → value. */
  values: Record<string, string | number | boolean | string[]>;
  consent?: boolean;
  /** Honeypot: never filled by people. */
  website?: string;
  page_url?: string;
}
