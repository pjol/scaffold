/**
 * Docs databases: property, view, filter and row DTOs (mirroring the Go
 * `properties.go`, `views.go`, `rows.go`), endpoints and hooks for what 1.2 adds
 * (templates, forms, automations, charts, CSV, buttons, linked views), and value
 * formatting. Portable.
 */
"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import type { CallOptions, HttpClient } from "../api/http";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useTopic, type QueryOverrides } from "../hooks/internal";
import { topic } from "../realtime/topics";
import type { SourceRef } from "../sources";
import type { DocsCover, DocsCoverColor, DocsMyLevel, DocsPage, DocsRefs } from "./types";

// ---- properties ----------------------------------------------------------------------

export type DocsPropertyType =
  | "title"
  | "text"
  | "number"
  | "select"
  | "multi_select"
  | "status"
  | "date"
  | "person"
  | "checkbox"
  | "url"
  | "email"
  | "phone"
  | "files"
  | "formula"
  | "relation"
  | "rollup"
  | "created_time"
  | "created_by"
  | "last_edited_time"
  | "last_edited_by"
  | "unique_id"
  | "button";

/** Notion's hues (the tokens' swatch set, `default` = grey chip) and docs 1.1's tones. */
export type DocsSwatchHue = "gray" | "brown" | "orange" | "yellow" | "green" | "blue" | "purple" | "pink" | "red";
export type DocsOptionColor = "default" | DocsSwatchHue | DocsCoverColor;

export interface DocsOption {
  id: string;
  name: string;
  color: DocsOptionColor;
  /** Status options only. */
  group?: "todo" | "doing" | "done";
}

/** number, number_commas, percent, or a currency code (usd, eur, gbp, jpy, …) stored in minor units. */
export type DocsNumberFormat = string;

export interface DocsFormulaConfig {
  expression: string;
  /** Result type (server-set): number, text, boolean, date, person, page, empty or list:<type>. */
  type?: string;
  /** Set when a property it uses changed under it. */
  error?: string;
}

export interface DocsRelationConfig {
  database_id: string;
  two_way?: boolean;
  /** The other side's property (server-set). */
  mirror_property?: string;
  /** Name for the other side when it's created. */
  mirror_name?: string;
  /** 1: one row; 0: no limit. */
  limit?: number;
}

export type DocsRollupFunction =
  | "show_original"
  | "show_unique"
  | "count_all"
  | "count_values"
  | "count_unique_values"
  | "count_empty"
  | "count_not_empty"
  | "percent_empty"
  | "percent_not_empty"
  | "sum"
  | "average"
  | "median"
  | "min"
  | "max"
  | "range"
  | "earliest_date"
  | "latest_date"
  | "date_range"
  | "checked"
  | "unchecked"
  | "percent_checked"
  | "percent_unchecked";

export interface DocsRollupConfig {
  relation: string;
  property: string;
  function: DocsRollupFunction;
  /** Result type (server-set). */
  type?: string;
  /** The related property, for formatting (server-filled when read). */
  target?: DocsProperty;
}

export interface DocsButtonAction {
  type: "set_property" | "add_row" | "open_url";
  property?: string;
  /** A value, or "@me" / "@today" / "@now". */
  value?: unknown;
  database_id?: string;
  values?: Record<string, unknown>;
  template_id?: string;
  url?: string;
}

export interface DocsProperty {
  id: string;
  name: string;
  type: DocsPropertyType;
  description?: string;
  options?: DocsOption[];
  number_format?: DocsNumberFormat;
  number_display?: "" | "bar" | "ring";
  number_max?: number;
  decimals?: number;
  date_format?: "" | "full" | "short" | "mdy" | "dmy" | "ymd" | "relative";
  time_format?: "" | "12h" | "24h";
  formula?: DocsFormulaConfig;
  relation?: DocsRelationConfig;
  rollup?: DocsRollupConfig;
  unique_id?: { prefix?: string };
  button?: { label: string; actions: DocsButtonAction[] };
  /** Shown but not changed here (a connected database's formulas, rollups, people…). */
  read_only?: boolean;
}

export type DocsReminder = "at_time" | "5m" | "10m" | "15m" | "30m" | "1h" | "2h" | "1d" | "2d" | "1w";

/** A date value: a day or local date-time (YYYY-MM-DD[THH:MM]), an end, the time zone times are in, a reminder. */
export interface DocsDateValue {
  start: string;
  end?: string;
  tz?: string;
  remind?: DocsReminder;
}

/** One entry of a files property: an upload on the row's page or a link. */
export interface DocsFileValue {
  file_id?: string;
  url?: string;
  name: string;
  mime?: string;
}

/** A formula or rollup result as JSON. */
export type DocsComputedValue = number | string | boolean | DocsDateValue | { person: string } | { page: string } | DocsComputedValue[];

/**
 * Row values by type: text/url/email/phone string; number (currencies in minor units); select/status option id;
 * multi_select option ids; date DocsDateValue; person user ids; checkbox; files DocsFileValue[]; relation row ids;
 * unique_id number; formula and rollup DocsComputedValue (read-only).
 */
export type DocsValue = string | number | boolean | string[] | DocsDateValue | DocsFileValue[] | DocsComputedValue | null;

// ---- views ---------------------------------------------------------------------------

export type DocsViewType = "table" | "board" | "list" | "calendar" | "gallery" | "timeline" | "chart" | "form" | "feed";

export interface DocsFilterCondition {
  property: string;
  op: string;
  value?: unknown;
}

export interface DocsFilterGroup {
  conj: "and" | "or";
  filters: DocsFilter[];
}

/** A condition or a group (nested up to three deep). */
export type DocsFilter = DocsFilterCondition | DocsFilterGroup;

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

export interface DocsGroupConfig {
  /** Dates: relative | day | week | month | year. */
  by?: string;
  /** Numbers: bucket size. */
  step?: number;
  sort?: "manual" | "asc" | "desc";
  order?: string[];
  hide_empty?: boolean;
  hidden?: string[];
  collapsed?: string[];
}

export interface DocsChartConfig {
  type: "bar" | "horizontal_bar" | "line" | "donut";
  x?: string;
  x_by?: "day" | "week" | "month" | "year";
  y?: "count" | "sum" | "average" | "median" | "min" | "max";
  y_property?: string;
  series?: string;
  sort?: "manual" | "x_asc" | "x_desc" | "y_asc" | "y_desc";
  hide_empty?: boolean;
  cumulative?: boolean;
  show_values?: boolean;
}

export interface DocsFormQuestion {
  property: string;
  label?: string;
  description?: string;
  required?: boolean;
}

export interface DocsFormConfig {
  title?: string;
  description?: string;
  submit_label?: string;
  success_message?: string;
  questions: DocsFormQuestion[];
  anonymous?: boolean;
  closed?: boolean;
}

export interface DocsViewConfig {
  filters: DocsFilter[];
  filter_conj?: "and" | "or";
  sorts: DocsSort[];
  group_by?: string;
  group?: DocsGroupConfig;
  sub_group_by?: string;
  sub_group?: DocsGroupConfig;
  /** Properties shown: order, width, hidden (every view type). */
  columns: DocsColumn[];
  card_properties: string[];
  /** Calendar; timeline start. */
  date_property?: string;
  /** Timeline end (else the start date's end). */
  end_property?: string;
  calculations?: Record<string, DocsRollupFunction>;
  wrap?: boolean;
  freeze_columns?: number;
  density?: "compact" | "comfortable";
  load_limit?: 10 | 25 | 50 | 100 | 200;
  sub_items?: { property: string; display: "nested" | "flat" };
  card?: { preview?: string; size?: "small" | "medium" | "large"; fit?: boolean };
  timeline?: { zoom?: "hours" | "day" | "week" | "month" | "quarter" | "year"; show_table?: boolean; table_properties?: string[]; dependency?: string };
  chart?: DocsChartConfig;
  form?: DocsFormConfig;
  template_id?: string;
}

export interface DocsView {
  id: string;
  database_id: string;
  name: string;
  type: DocsViewType;
  position: string;
  config: DocsViewConfig;
  /** Linked views: the database block showing it and that block's page. */
  block_id?: string;
  host_page_id?: string;
  created_at: string;
  updated_at: string;
}

// ---- rows ----------------------------------------------------------------------------

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
  /** The row page's cover; gallery and feed views also get the start of its content. */
  cover?: DocsCover | null;
  snippet?: string;
  /** Grouped views: the group (and sub-group) keys the row falls in. */
  groups?: string[];
  sub_groups?: string[];
  /** Nested sub-items: how many the viewer can open. */
  sub_count?: number;
  /** Connected rows: where the row comes from, its platform id and its page there. */
  source?: SourceRef;
  ref?: string;
  url?: string;
}

export interface DocsGroupInfo {
  key: string;
  label: string;
  color?: DocsOptionColor;
  count: number;
  hidden?: boolean;
}

export interface DocsRowPage {
  items: DocsRow[];
  next_cursor: string | null;
  refs: DocsRefs;
  /** First page: rows matching, groups with counts, footer calculations. */
  total?: number;
  groups?: DocsGroupInfo[];
  sub_groups?: DocsGroupInfo[];
  calculations?: Record<string, DocsComputedValue>;
}

export interface DocsRowsParams {
  view?: string;
  filters?: DocsFilter[];
  filter_conj?: "and" | "or";
  sorts?: DocsSort[];
  q?: string;
  cursor?: string;
  limit?: number;
  /** The viewer's time zone (relative dates, formulas' now()). */
  tz?: string;
  /** Nested sub-items of this row. */
  parent?: string;
}

export interface DocsRowValuesRequest {
  title?: string;
  values?: Record<string, DocsValue>;
  after_id?: string | null;
  first?: boolean;
  position?: string;
  tz?: string;
}

export interface DocsCreateRowRequest {
  title?: string;
  values?: Record<string, DocsValue>;
  after_id?: string | null;
  first?: boolean;
  template_id?: string;
  /** A sub-item of this row (the view's sub-items relation). */
  parent_row?: string;
  /** The view adding it: its default template and sub-items. */
  view_id?: string;
  tz?: string;
}

export interface DocsViewRequest {
  name?: string;
  type?: DocsViewType;
  config?: DocsViewConfig;
  position?: string;
}

// ---- what 1.2 adds -------------------------------------------------------------------

export interface DocsDatabaseCreated {
  page: DocsPage;
  views: DocsView[];
}

export interface DocsCreateDatabaseRequest {
  parent_id?: string | null;
  title?: string;
  icon?: string | null;
  inline?: boolean;
  shared?: boolean;
  after_id?: string | null;
  view_type?: DocsViewType;
}

export interface DocsLinkedDatabase {
  /** null: the viewer can't open the source. */
  database: DocsPage | null;
  views: DocsView[];
  /** The block shows a database that lives elsewhere (its views are the block's own). */
  linked: boolean;
  can_edit: boolean;
}

export interface DocsChartPoint {
  key: string;
  label: string;
  color?: DocsOptionColor;
  value: number;
  series?: Record<string, number>;
}

export interface DocsChartData {
  points: DocsChartPoint[];
  series: DocsGroupInfo[];
  total: number;
}

export interface DocsFormulaError {
  pos: number;
  message: string;
}

export interface DocsFormulaCheck {
  type?: string;
  error?: DocsFormulaError;
  preview: DocsComputedValue | null;
  text: string;
}

export interface DocsButtonResult {
  row: DocsPage;
  created: string[];
  open: string[];
}

export interface DocsRepeat {
  every: "day" | "week" | "month";
  interval: number;
  /** Week: 0 Monday … 6 Sunday. */
  weekdays?: number[];
  day?: number;
  time: string;
  tz: string;
  by?: string;
}

export interface DocsRowTemplate {
  id: string;
  title: string;
  icon: string | null;
  repeat: DocsRepeat | null;
  repeat_next_at: string | null;
  created_by: string | null;
  updated_at: string;
}

export interface DocsFormQuestionDetail {
  property: DocsProperty;
  label: string;
  description?: string;
  required: boolean;
}

export interface DocsFormDetail {
  view_id: string;
  database_id: string;
  title: string;
  description: string;
  submit_label: string;
  success_message: string;
  questions: DocsFormQuestionDetail[];
  anonymous: boolean;
  closed: boolean;
  /** Editors of the database see the response count. */
  responses?: number;
  my_level: DocsMyLevel;
}

export interface DocsAutomationTrigger {
  type: "row_added" | "property_edited" | "schedule";
  property?: string;
  values?: unknown[];
  every?: "day" | "week";
  weekdays?: number[];
  time?: string;
  tz?: string;
}

export interface DocsAutomationAction {
  type: "set_property" | "add_row" | "notify" | "webhook";
  property?: string;
  value?: unknown;
  database_id?: string;
  values?: Record<string, unknown>;
  template_id?: string;
  people?: string[];
  person_property?: string;
  message?: string;
  url?: string;
}

export interface DocsAutomation {
  id: string;
  database_id: string;
  name: string;
  enabled: boolean;
  trigger: DocsAutomationTrigger;
  conditions: DocsFilter[];
  actions: DocsAutomationAction[];
  /** Signs webhooks: X-Signature = sha256=HMAC(secret, "<X-Timestamp>.<body>"). */
  secret?: string;
  next_run_at: string | null;
  paused_reason: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  last_run_at: string | null;
}

export interface DocsAutomationRequest {
  name?: string;
  enabled?: boolean;
  trigger?: DocsAutomationTrigger;
  conditions?: DocsFilter[];
  actions?: DocsAutomationAction[];
}

export interface DocsAutomationRun {
  id: string;
  row_id: string | null;
  status: "ok" | "failed" | "skipped";
  detail: string;
  created_at: string;
}

export interface DocsCSVColumn {
  index: number;
  name: string;
  type: DocsPropertyType;
  format: string;
  sample: string[];
  /** The property with the same name, if any. */
  match: string;
}

export interface DocsCSVPreview {
  columns: DocsCSVColumn[];
  rows: number;
  title_column: number;
}

export interface DocsCSVMapping {
  column: number;
  property?: string;
  new?: { name: string; type: DocsPropertyType };
}

export interface DocsCSVSource {
  text?: string;
  /** An upload with purpose docs_import (files over about 1 MB). */
  key?: string;
}

// ---- endpoints -----------------------------------------------------------------------

const seg = encodeURIComponent;

export function docsRowsQuery(p: DocsRowsParams) {
  return {
    view: p.view,
    filters: p.filters ? JSON.stringify(p.filters) : undefined,
    filter_conj: p.filter_conj,
    sorts: p.sorts ? JSON.stringify(p.sorts) : undefined,
    q: p.q || undefined,
    cursor: p.cursor,
    limit: p.limit,
    tz: p.tz,
    parent: p.parent,
  };
}

/** The viewer's IANA time zone (UTC when unknown). */
export function docsTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

/** `/v1/docs` database endpoints that 1.2 adds. */
export function docsDatabaseEndpoints(http: HttpClient) {
  const D = "/v1/docs";
  return {
    createDatabase: (orgId: string, body: DocsCreateDatabaseRequest, o?: CallOptions) => http.post<DocsDatabaseCreated>(`${D}/orgs/${seg(orgId)}/databases`, body, o),
    chart: (dbId: string, params: DocsRowsParams, o?: CallOptions) => http.get<DocsChartData>(`${D}/databases/${seg(dbId)}/chart`, { ...o, query: docsRowsQuery(params) }),
    pressButton: (rowId: string, propId: string, o?: CallOptions) => http.post<DocsButtonResult>(`${D}/pages/${seg(rowId)}/buttons/${seg(propId)}`, { tz: docsTimeZone() }, o),
    checkFormula: (dbId: string, body: { expression: string; property_id?: string; row_id?: string; tz?: string }, o?: CallOptions) =>
      http.post<DocsFormulaCheck>(`${D}/databases/${seg(dbId)}/formula`, body, o),
    createLinkedView: (hostPageId: string, body: { block_id: string; database_id: string; type?: DocsViewType; name?: string; config?: DocsViewConfig }, o?: CallOptions) =>
      http.post<DocsView>(`${D}/pages/${seg(hostPageId)}/linked-views`, body, o),
    blockDatabase: (hostPageId: string, blockId: string, o?: CallOptions) => http.get<DocsLinkedDatabase>(`${D}/pages/${seg(hostPageId)}/blocks/${seg(blockId)}/database`, o),
    templates: (dbId: string, o?: CallOptions) => http.get<{ items: DocsRowTemplate[] }>(`${D}/databases/${seg(dbId)}/templates`, o),
    createTemplate: (dbId: string, body: { title?: string; values?: Record<string, DocsValue> }, o?: CallOptions) => http.post<DocsPage>(`${D}/databases/${seg(dbId)}/templates`, body, o),
    setTemplateRepeat: (templateId: string, repeat: DocsRepeat | null, o?: CallOptions) => http.patch<DocsRowTemplate>(`${D}/templates/${seg(templateId)}`, { repeat }, o),
    form: (viewId: string, o?: CallOptions) => http.get<DocsFormDetail>(`${D}/forms/${seg(viewId)}`, o),
    submitForm: (viewId: string, values: Record<string, DocsValue>, o?: CallOptions) =>
      http.post<{ row_id: string; message: string }>(`${D}/forms/${seg(viewId)}/submit`, { values }, o),
    publicForm: (token: string, o?: CallOptions) => http.get<DocsFormDetail>(`${D}/public/forms/${seg(token)}`, { ...o, auth: false }),
    publicSubmitForm: (token: string, values: Record<string, DocsValue>, o?: CallOptions) =>
      http.post<{ message: string }>(`${D}/public/forms/${seg(token)}`, { values }, { ...o, auth: false }),
    automations: (dbId: string, o?: CallOptions) => http.get<{ items: DocsAutomation[] }>(`${D}/databases/${seg(dbId)}/automations`, o),
    createAutomation: (dbId: string, body: DocsAutomationRequest, o?: CallOptions) => http.post<DocsAutomation>(`${D}/databases/${seg(dbId)}/automations`, body, o),
    updateAutomation: (id: string, body: DocsAutomationRequest, o?: CallOptions) => http.patch<DocsAutomation>(`${D}/automations/${seg(id)}`, body, o),
    deleteAutomation: (id: string, o?: CallOptions) => http.delete<void>(`${D}/automations/${seg(id)}`, o),
    automationRuns: (id: string, o?: CallOptions) => http.get<{ items: DocsAutomationRun[] }>(`${D}/automations/${seg(id)}/runs`, o),
    /** A view's rows (or every row and property) as CSV text. */
    exportCSV: (dbId: string, params: { view?: string; all?: boolean }, o?: CallOptions) =>
      http.text("GET", `${D}/databases/${seg(dbId)}/csv`, { timeoutMs: 60_000, ...o, query: { view: params.view, all: params.all ? "1" : undefined, tz: docsTimeZone() } }),
    previewCSV: (dbId: string, src: DocsCSVSource, o?: CallOptions) => http.post<DocsCSVPreview>(`${D}/databases/${seg(dbId)}/csv/preview`, src, o),
    importCSV: (dbId: string, body: DocsCSVSource & { title_column: number; mapping: DocsCSVMapping[] }, o?: CallOptions) =>
      http.post<{ id: string; status: string }>(`${D}/databases/${seg(dbId)}/csv/import`, body, o),
  };
}

// ---- hooks ---------------------------------------------------------------------------

export const docsDatabaseKeys = {
  chart: (dbId: string, params: DocsRowsParams) => ["docs", "rows", dbId, "chart", params] as const,
  blockDatabase: (pageId: string, blockId: string) => ["docs", "page", pageId, "block-db", blockId] as const,
  templates: (dbId: string) => ["docs", "page", dbId, "templates"] as const,
  form: (viewId: string) => ["docs", "form", viewId] as const,
  publicForm: (token: string) => ["docs", "public-form", token] as const,
  automations: (dbId: string) => ["docs", "automations", dbId] as const,
  automationRuns: (id: string) => ["docs", "automation-runs", id] as const,
};

/** A new database with its first view (the editor's /board view, /form… commands). */
export function useDocsCreateDatabase(orgId: string): UseMutationResult<DocsDatabaseCreated, ApiError, DocsCreateDatabaseRequest> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsDatabaseCreated, ApiError, DocsCreateDatabaseRequest>({
    mutationFn: (body) => d.createDatabase(orgId, body),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["docs", "sidebar"] }),
  });
}

/** A chart view's data (live with the database's rows). */
export function useDocsChart(dbId: string | null | undefined, params: DocsRowsParams, options?: QueryOverrides<DocsChartData>): UseQueryResult<DocsChartData, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  useTopic(dbId ? topic("docs", dbId) : null);
  return useApiQuery(docsDatabaseKeys.chart(dbId ?? "", params), ({ signal }) => d.chart(dbId!, params, { signal }), { staleTime: 15_000, ...options }, !!dbId && !!params.view);
}

/** The database a database block shows, with the block's own views when linked. */
export function useDocsBlockDatabase(pageId: string | null | undefined, blockId: string | null | undefined): UseQueryResult<DocsLinkedDatabase, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.blockDatabase(pageId ?? "", blockId ?? ""), ({ signal }) => d.blockDatabase(pageId!, blockId!, { signal }), { staleTime: 30_000 }, !!pageId && !!blockId);
}

export function useDocsCreateLinkedView(pageId: string, blockId: string): UseMutationResult<DocsView, ApiError, { database_id: string; type?: DocsViewType; name?: string; config?: DocsViewConfig }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => d.createLinkedView(pageId, { ...body, block_id: blockId }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsDatabaseKeys.blockDatabase(pageId, blockId) }),
  });
}

export function useDocsPressButton(dbId: string): UseMutationResult<DocsButtonResult, ApiError, { rowId: string; propId: string }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ rowId, propId }) => d.pressButton(rowId, propId),
    onSuccess: (res) => {
      void qc.invalidateQueries({ queryKey: ["docs", "rows", dbId] });
      void qc.invalidateQueries({ queryKey: ["docs", "page", res.row.id] });
    },
  });
}

export function useDocsCheckFormula(dbId: string): UseMutationResult<DocsFormulaCheck, ApiError, { expression: string; property_id?: string; row_id?: string }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useMutation({ mutationFn: (body) => d.checkFormula(dbId, { ...body, tz: docsTimeZone() }) });
}

export function useDocsRowTemplates(dbId: string | null | undefined): UseQueryResult<{ items: DocsRowTemplate[] }, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.templates(dbId ?? ""), ({ signal }) => d.templates(dbId!, { signal }), { staleTime: 30_000 }, !!dbId);
}

export function useDocsCreateRowTemplate(dbId: string): UseMutationResult<DocsPage, ApiError, { title?: string; values?: Record<string, DocsValue> }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (body) => d.createTemplate(dbId, body), onSuccess: () => void qc.invalidateQueries({ queryKey: docsDatabaseKeys.templates(dbId) }) });
}

export function useDocsSetTemplateRepeat(dbId: string): UseMutationResult<DocsRowTemplate, ApiError, { templateId: string; repeat: DocsRepeat | null }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: ({ templateId, repeat }) => d.setTemplateRepeat(templateId, repeat), onSuccess: () => void qc.invalidateQueries({ queryKey: docsDatabaseKeys.templates(dbId) }) });
}

/** A form to fill (signed in: the form must be shared with you). */
export function useDocsForm(viewId: string | null | undefined): UseQueryResult<DocsFormDetail, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.form(viewId ?? ""), ({ signal }) => d.form(viewId!, { signal }), { staleTime: 30_000 }, !!viewId);
}

export function useDocsSubmitForm(viewId: string): UseMutationResult<{ row_id: string; message: string }, ApiError, Record<string, DocsValue>> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useMutation({ mutationFn: (values) => d.submitForm(viewId, values) });
}

/** A public form (no account). 404 `link_not_found` once the link stops working. */
export function useDocsPublicForm(token: string | null | undefined): UseQueryResult<DocsFormDetail, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.publicForm(token ?? ""), ({ signal }) => d.publicForm(token!, { signal }), { staleTime: 60_000, retry: false }, !!token);
}

export function useDocsPublicSubmitForm(token: string): UseMutationResult<{ message: string }, ApiError, Record<string, DocsValue>> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useMutation({ mutationFn: (values) => d.publicSubmitForm(token, values) });
}

export function useDocsAutomations(dbId: string | null | undefined, enabled = true): UseQueryResult<{ items: DocsAutomation[] }, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.automations(dbId ?? ""), ({ signal }) => d.automations(dbId!, { signal }), { staleTime: 15_000 }, !!dbId && enabled);
}

export function useDocsSaveAutomation(dbId: string): UseMutationResult<DocsAutomation, ApiError, { id?: string } & DocsAutomationRequest> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }) => (id ? d.updateAutomation(id, body) : d.createAutomation(dbId, body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsDatabaseKeys.automations(dbId) }),
  });
}

export function useDocsDeleteAutomation(dbId: string): UseMutationResult<void, ApiError, string> {
  const d = useEndpoints(docsDatabaseEndpoints);
  const qc = useQueryClient();
  return useMutation({ mutationFn: (id) => d.deleteAutomation(id), onSuccess: () => void qc.invalidateQueries({ queryKey: docsDatabaseKeys.automations(dbId) }) });
}

export function useDocsAutomationRuns(id: string | null | undefined): UseQueryResult<{ items: DocsAutomationRun[] }, ApiError> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useApiQuery(docsDatabaseKeys.automationRuns(id ?? ""), ({ signal }) => d.automationRuns(id!, { signal }), { staleTime: 10_000 }, !!id);
}

export function useDocsPreviewCSV(dbId: string): UseMutationResult<DocsCSVPreview, ApiError, DocsCSVSource> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useMutation({ mutationFn: (src) => d.previewCSV(dbId, src) });
}

export function useDocsImportCSV(dbId: string): UseMutationResult<{ id: string; status: string }, ApiError, DocsCSVSource & { title_column: number; mapping: DocsCSVMapping[] }> {
  const d = useEndpoints(docsDatabaseEndpoints);
  return useMutation({ mutationFn: (body) => d.importCSV(dbId, body) });
}

// ---- labels and catalogues -------------------------------------------------------------

export const DOCS_PROPERTY_LABEL: Record<DocsPropertyType, string> = {
  title: "Title",
  text: "Text",
  number: "Number",
  select: "Select",
  multi_select: "Multi-select",
  status: "Status",
  date: "Date",
  person: "Person",
  checkbox: "Checkbox",
  url: "URL",
  email: "Email",
  phone: "Phone",
  files: "Files & media",
  formula: "Formula",
  relation: "Relation",
  rollup: "Rollup",
  created_time: "Created time",
  created_by: "Created by",
  last_edited_time: "Last edited time",
  last_edited_by: "Last edited by",
  unique_id: "ID",
  button: "Button",
};

/** Types a person can add (title comes with every database). */
export const DOCS_ADDABLE_TYPES: readonly DocsPropertyType[] = [
  "text",
  "number",
  "select",
  "multi_select",
  "status",
  "date",
  "person",
  "files",
  "checkbox",
  "url",
  "email",
  "phone",
  "formula",
  "relation",
  "rollup",
  "created_time",
  "created_by",
  "last_edited_time",
  "last_edited_by",
  "button",
  "unique_id",
];

/** Types whose value is worked out (never set by hand). */
export const DOCS_COMPUTED_TYPES: readonly DocsPropertyType[] = ["formula", "rollup", "created_time", "created_by", "last_edited_time", "last_edited_by", "button", "unique_id"];

export function docsIsComputed(p: DocsProperty): boolean {
  return (DOCS_COMPUTED_TYPES as readonly string[]).includes(p.type);
}

export const DOCS_VIEW_TYPES: readonly DocsViewType[] = ["table", "board", "list", "calendar", "gallery", "timeline", "chart", "form", "feed"];

export const DOCS_VIEW_LABEL: Record<DocsViewType, string> = {
  table: "Table",
  board: "Board",
  list: "List",
  calendar: "Calendar",
  gallery: "Gallery",
  timeline: "Timeline",
  chart: "Chart",
  form: "Form",
  feed: "Feed",
};

export const DOCS_STATUS_GROUP_LABEL: Record<"todo" | "doing" | "done", string> = { todo: "To-do", doing: "In progress", done: "Complete" };

export const DOCS_OPTION_HUES: readonly DocsOptionColor[] = ["default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"];

export const DOCS_OPTION_COLOR_LABEL: Record<string, string> = {
  default: "Default",
  gray: "Grey",
  brown: "Brown",
  orange: "Orange",
  yellow: "Yellow",
  green: "Green",
  blue: "Blue",
  purple: "Purple",
  pink: "Pink",
  red: "Red",
  neutral: "Grey",
  primary: "Primary",
  accent: "Accent",
  info: "Blue",
  success: "Green",
  warning: "Yellow",
  danger: "Red",
};

/** The swatch hue an option colour draws with (docs 1.1's tones map to the nearest hue). */
export function docsOptionHue(c: DocsOptionColor | undefined): DocsSwatchHue | null {
  switch (c) {
    case undefined:
    case "default":
    case "neutral":
      return null;
    case "primary":
    case "info":
      return "blue";
    case "accent":
      return "purple";
    case "success":
      return "green";
    case "warning":
      return "yellow";
    case "danger":
      return "red";
  }
  return c as DocsSwatchHue;
}

/** Currencies: ISO code → minor-unit exponent (values are stored in minor units). */
export const DOCS_CURRENCIES: Record<string, number> = {
  usd: 2, eur: 2, gbp: 2, jpy: 0, cny: 2, inr: 2, krw: 0, brl: 2, cad: 2, aud: 2, nzd: 2, chf: 2, sek: 2, nok: 2, dkk: 2,
  pln: 2, czk: 2, huf: 2, ron: 2, try: 2, ils: 2, aed: 2, sar: 2, zar: 2, mxn: 2, ars: 2, clp: 0, cop: 2, pen: 2, sgd: 2,
  hkd: 2, twd: 2, thb: 2, idr: 2, php: 2, myr: 2, vnd: 0, ngn: 2, egp: 2, kes: 2, uah: 2, rub: 2,
};

const LEGACY_CURRENCY: Record<string, string> = { dollar: "usd", euro: "eur", pound: "gbp" };

export function docsCurrencyCode(format: string | undefined): string | null {
  const f = format ? (LEGACY_CURRENCY[format] ?? format) : "";
  return f in DOCS_CURRENCIES ? f : null;
}

/** What a stored number is divided by to read it (100 for cents). */
export function docsNumberScale(p: Pick<DocsProperty, "number_format"> | undefined): number {
  const c = docsCurrencyCode(p?.number_format);
  return c ? 10 ** (DOCS_CURRENCIES[c] ?? 2) : 1;
}

export const DOCS_ROLLUP_LABEL: Record<DocsRollupFunction, string> = {
  show_original: "Show original",
  show_unique: "Show unique values",
  count_all: "Count all",
  count_values: "Count values",
  count_unique_values: "Count unique values",
  count_empty: "Count empty",
  count_not_empty: "Count not empty",
  percent_empty: "Percent empty",
  percent_not_empty: "Percent not empty",
  sum: "Sum",
  average: "Average",
  median: "Median",
  min: "Min",
  max: "Max",
  range: "Range",
  earliest_date: "Earliest date",
  latest_date: "Latest date",
  date_range: "Date range",
  checked: "Checked",
  unchecked: "Unchecked",
  percent_checked: "Percent checked",
  percent_unchecked: "Percent unchecked",
};

/** The value type (formula type names) a property's values have. */
export function docsValueType(p: DocsProperty): string {
  switch (p.type) {
    case "title":
    case "text":
    case "url":
    case "email":
    case "phone":
    case "select":
    case "status":
      return "text";
    case "multi_select":
    case "files":
      return "list:text";
    case "number":
    case "unique_id":
      return "number";
    case "checkbox":
      return "boolean";
    case "date":
    case "created_time":
    case "last_edited_time":
      return "date";
    case "person":
      return "list:person";
    case "created_by":
    case "last_edited_by":
      return "person";
    case "relation":
      return "list:page";
    case "formula":
      return p.formula?.type ?? "empty";
    case "rollup":
      return p.rollup?.type ?? "empty";
  }
  return "empty";
}

function baseType(t: string): string {
  return t.startsWith("list:") ? t.slice(5) : t;
}

/** Aggregations that fit a type (rollups and footer calculations). */
export function docsAggregations(valueType: string, forCalc = false): DocsRollupFunction[] {
  const counts: DocsRollupFunction[] = ["count_all", "count_values", "count_unique_values", "count_empty", "count_not_empty", "percent_empty", "percent_not_empty"];
  const show: DocsRollupFunction[] = forCalc ? [] : ["show_original", "show_unique"];
  const b = baseType(valueType);
  if (b === "number") return [...show, ...counts, "sum", "average", "median", "min", "max", "range"];
  if (b === "date") return [...show, ...counts, "earliest_date", "latest_date", "date_range"];
  if (b === "boolean") return [...show, ...counts, "checked", "unchecked", "percent_checked", "percent_unchecked"];
  return [...show, ...counts];
}

// ---- filters ---------------------------------------------------------------------------

const TEXT_OPS = ["is", "is_not", "contains", "not_contains", "starts_with", "ends_with", "is_empty", "is_not_empty"];
const NUMBER_OPS = ["eq", "neq", "gt", "gte", "lt", "lte", "is_empty", "is_not_empty"];
const DATE_OPS = ["is", "before", "after", "on_or_before", "on_or_after", "is_within", "is_empty", "is_not_empty"];
const LIST_OPS = ["contains", "not_contains", "is_empty", "is_not_empty"];

/** Operators a property accepts (formulas and rollups by result type). Mirrors Go filterOpsFor. */
export function docsFilterOpsFor(p: DocsProperty): string[] {
  switch (p.type) {
    case "title":
    case "text":
    case "url":
    case "email":
    case "phone":
      return TEXT_OPS;
    case "number":
    case "unique_id":
      return NUMBER_OPS;
    case "select":
    case "status":
      return ["is", "is_not", "is_empty", "is_not_empty"];
    case "multi_select":
    case "person":
    case "created_by":
    case "last_edited_by":
    case "relation":
      return LIST_OPS;
    case "files":
      return ["is_empty", "is_not_empty"];
    case "checkbox":
      return ["is"];
    case "date":
    case "created_time":
    case "last_edited_time":
      return DATE_OPS;
    case "formula":
    case "rollup": {
      const t = docsValueType(p);
      if (t === "text") return TEXT_OPS;
      if (t === "number") return NUMBER_OPS;
      if (t === "boolean") return ["is"];
      if (t === "date") return DATE_OPS;
      if (t.startsWith("list:") || t === "person" || t === "page") return LIST_OPS;
      return [];
    }
  }
  return [];
}

/** docs 1.1 name, by type only. */
export function docsFilterOps(type: DocsPropertyType): string[] {
  return docsFilterOpsFor({ id: "", name: "", type });
}

export const DOCS_FILTER_OP_LABEL: Record<string, string> = {
  contains: "contains",
  not_contains: "doesn't contain",
  is: "is",
  is_not: "is not",
  starts_with: "starts with",
  ends_with: "ends with",
  is_empty: "is empty",
  is_not_empty: "is not empty",
  eq: "=",
  neq: "≠",
  gt: ">",
  gte: "≥",
  lt: "<",
  lte: "≤",
  before: "is before",
  after: "is after",
  on_or_before: "is on or before",
  on_or_after: "is on or after",
  is_within: "is within",
};

export const DOCS_RELATIVE_DATES: readonly { value: string; label: string }[] = [
  { value: "today", label: "Today" },
  { value: "tomorrow", label: "Tomorrow" },
  { value: "yesterday", label: "Yesterday" },
  { value: "one_week_ago", label: "One week ago" },
  { value: "one_week_from_now", label: "One week from now" },
  { value: "one_month_ago", label: "One month ago" },
  { value: "one_month_from_now", label: "One month from now" },
];

export const DOCS_WITHIN_RANGES: readonly { value: string; label: string }[] = [
  { value: "past_week", label: "The past week" },
  { value: "past_month", label: "The past month" },
  { value: "past_year", label: "The past year" },
  { value: "next_week", label: "The next week" },
  { value: "next_month", label: "The next month" },
  { value: "next_year", label: "The next year" },
  { value: "this_week", label: "This week" },
  { value: "this_month", label: "This month" },
  { value: "this_year", label: "This year" },
];

/** Whether a filter op takes no value. */
export function docsOpIsUnary(op: string): boolean {
  return op === "is_empty" || op === "is_not_empty";
}

export function docsIsFilterGroup(f: DocsFilter): f is DocsFilterGroup {
  return Array.isArray((f as DocsFilterGroup).filters);
}

export function docsDefaultFilter(p: DocsProperty): DocsFilterCondition {
  const ops = docsFilterOpsFor(p);
  if (p.type === "checkbox" || docsValueType(p) === "boolean") return { property: p.id, op: "is", value: true };
  if (ops[0] === "is_within") return { property: p.id, op: "is_within", value: "this_week" };
  return { property: p.id, op: ops[0] ?? "is_not_empty" };
}

/** How many conditions a filter list holds (groups counted through). */
export function docsFilterCount(filters: readonly DocsFilter[]): number {
  let n = 0;
  for (const f of filters) n += docsIsFilterGroup(f) ? docsFilterCount(f.filters) : 1;
  return n;
}

// ---- formatting ------------------------------------------------------------------------

export function docsOption(p: DocsProperty | undefined, id: unknown): DocsOption | undefined {
  return typeof id === "string" ? p?.options?.find((o) => o.id === id) : undefined;
}

/**
 * A number as its property shows it. `stored` numbers come from props (currencies in minor units); computed values
 * (formulas, rollups, calculations) are already in major units.
 */
export function docsFormatNumber(v: number, format: DocsNumberFormat | undefined = "number", opts: { stored?: boolean; decimals?: number } = {}): string {
  const currency = docsCurrencyCode(format);
  const digits = opts.decimals;
  if (currency) {
    const exp = DOCS_CURRENCIES[currency] ?? 2;
    const major = opts.stored ? v / 10 ** exp : v;
    return new Intl.NumberFormat(undefined, { style: "currency", currency: currency.toUpperCase(), minimumFractionDigits: digits ?? exp, maximumFractionDigits: digits ?? exp }).format(major);
  }
  if (format === "percent") return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: digits ?? 2, minimumFractionDigits: digits ?? 0 }).format(v)}%`;
  if (format === "number_commas") return new Intl.NumberFormat(undefined, { useGrouping: true, maximumFractionDigits: digits ?? 6, minimumFractionDigits: digits ?? 0 }).format(v);
  return new Intl.NumberFormat(undefined, { useGrouping: false, maximumFractionDigits: digits ?? 6, minimumFractionDigits: digits ?? 0 }).format(v);
}

/** A stored number of p as text (bar/ring share the label). */
export function docsFormatPropertyNumber(p: DocsProperty, v: number, stored = p.type === "number"): string {
  return docsFormatNumber(v, p.number_format, { stored, ...(p.decimals !== undefined ? { decimals: p.decimals } : {}) });
}

/** 0–1 fill for bar/ring display. */
export function docsNumberFill(p: DocsProperty, v: number, stored = p.type === "number"): number {
  const major = stored ? v / docsNumberScale(p) : v;
  const max = p.number_max && p.number_max > 0 ? p.number_max : 100;
  return Math.max(0, Math.min(1, major / max));
}

const MONTHS_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** "Nov 20, 2026" (+ time). Dates are calendar days, never shifted by time zone; `format` and `time` follow the property. */
export function docsFormatDate(value: string | undefined | null, format: DocsProperty["date_format"] = "", time: DocsProperty["time_format"] = ""): string {
  if (!value) return "";
  const [d, t] = value.split("T");
  const [y, m, day] = (d ?? "").split("-").map(Number);
  if (!y || !m || !day) return value;
  let s: string;
  switch (format) {
    case "full":
      s = new Intl.DateTimeFormat(undefined, { month: "long", day: "numeric", year: "numeric", timeZone: "UTC" }).format(new Date(Date.UTC(y, m - 1, day)));
      break;
    case "mdy":
      s = `${pad(m)}/${pad(day)}/${y}`;
      break;
    case "dmy":
      s = `${pad(day)}/${pad(m)}/${y}`;
      break;
    case "ymd":
      s = `${y}/${pad(m)}/${pad(day)}`;
      break;
    case "relative": {
      const now = new Date();
      const today = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate());
      const diff = Math.round((Date.UTC(y, m - 1, day) - today) / 86_400_000);
      s = diff === 0 ? "Today" : diff === -1 ? "Yesterday" : diff === 1 ? "Tomorrow" : `${MONTHS_SHORT[m - 1]} ${day}, ${y}`;
      break;
    }
    default:
      s = `${MONTHS_SHORT[m - 1]} ${day}, ${y}`;
  }
  if (!t) return s;
  const [hh = 0, mm = 0] = t.split(":").map(Number);
  if (time === "24h") return `${s} ${pad(hh)}:${pad(mm)}`;
  const h12 = hh % 12 === 0 ? 12 : hh % 12;
  return `${s} ${h12}:${pad(mm)} ${hh < 12 ? "AM" : "PM"}`;
}

/** A date value as its property shows it ("start → end"). */
export function docsFormatDateValue(v: DocsDateValue | null | undefined, p?: DocsProperty): string {
  if (!v?.start) return "";
  const f = (s: string) => docsFormatDate(s, p?.date_format ?? "", p?.time_format ?? "");
  return v.end ? `${f(v.start)} → ${f(v.end)}` : f(v.start);
}

/** A timestamp (created / edited time) in the viewer's zone, as YYYY-MM-DDTHH:MM for docsFormatDate. */
export function docsLocalDateTime(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** "TASK-12". */
export function docsFormatUniqueID(p: DocsProperty, v: unknown): string {
  if (typeof v !== "number") return "";
  const prefix = p.unique_id?.prefix;
  return prefix ? `${prefix}-${v}` : String(v);
}

function isDateValue(v: unknown): v is DocsDateValue {
  return !!v && typeof v === "object" && !Array.isArray(v) && typeof (v as DocsDateValue).start === "string";
}

/** A computed value (formula, rollup, calculation) as text. */
export function docsFormatComputed(v: DocsComputedValue | DocsValue | undefined | null, p: DocsProperty | undefined, refs?: DocsRefs): string {
  if (v === null || v === undefined) return "";
  const fmt = p?.type === "rollup" && p.rollup?.target && (!p.number_format || p.number_format === "number") ? p.rollup.target : p;
  if (typeof v === "number") {
    if (p?.type === "rollup" && p.rollup && /^percent_/.test(p.rollup.function)) return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(v)}%`;
    if (p?.type === "rollup" && p.rollup?.function === "date_range") return `${v} ${v === 1 ? "day" : "days"}`;
    if (p?.type === "rollup" && p.rollup && /^count|^checked|^unchecked/.test(p.rollup.function)) return String(v);
    return docsFormatNumber(v, fmt?.number_format, { stored: false, ...(fmt?.decimals !== undefined ? { decimals: fmt.decimals } : {}) });
  }
  if (typeof v === "boolean") return v ? "Yes" : "No";
  if (typeof v === "string") return v;
  if (Array.isArray(v)) return v.map((x) => docsFormatComputed(x as DocsComputedValue, p, refs)).filter(Boolean).join(", ");
  if (isDateValue(v)) return docsFormatDateValue(v, fmt);
  const o = v as { person?: string; page?: string };
  if (o.person) return refs?.users[o.person]?.name ?? "";
  if (o.page) return refs?.pages[o.page]?.title ?? "No access";
  return "";
}

/** A row value as text (option names, people's names, formatted numbers and dates, related rows' titles). */
export function docsFormatValue(p: DocsProperty, value: DocsValue | undefined, refs?: DocsRefs, title?: string, row?: Pick<DocsRow, "created_at" | "updated_at" | "created_by" | "updated_by">): string {
  switch (p.type) {
    case "title":
      return title && title.trim() ? title : "Untitled";
    case "created_time":
      return row ? docsFormatDate(docsLocalDateTime(row.created_at), p.date_format, p.time_format) : "";
    case "last_edited_time":
      return row ? docsFormatDate(docsLocalDateTime(row.updated_at), p.date_format, p.time_format) : "";
    case "created_by":
      return row?.created_by ? (refs?.users[row.created_by]?.name ?? "") : "";
    case "last_edited_by":
      return row?.updated_by ? (refs?.users[row.updated_by]?.name ?? "") : "";
    case "button":
      return p.button?.label ?? p.name;
  }
  if (value === null || value === undefined) return "";
  switch (p.type) {
    case "select":
    case "status":
      return docsOption(p, value)?.name ?? "";
    case "multi_select":
      return (Array.isArray(value) ? value : [])
        .map((id) => docsOption(p, id)?.name)
        .filter(Boolean)
        .join(", ");
    case "person":
      return (Array.isArray(value) ? value : [])
        .map((id) => refs?.users[id as string]?.name)
        .filter(Boolean)
        .join(", ");
    case "relation":
      return (Array.isArray(value) ? value : [])
        .map((id) => {
          const ref = refs?.pages[id as string];
          return ref ? (ref.title ?? "No access") : null;
        })
        .filter(Boolean)
        .join(", ");
    case "files":
      return (Array.isArray(value) ? (value as DocsFileValue[]) : []).map((f) => f.name).join(", ");
    case "number":
      return typeof value === "number" ? docsFormatPropertyNumber(p, value, true) : "";
    case "unique_id":
      return docsFormatUniqueID(p, value);
    case "checkbox":
      return value === true ? "Yes" : "No";
    case "date":
      return isDateValue(value) ? docsFormatDateValue(value, p) : "";
    case "formula":
    case "rollup":
      return docsFormatComputed(value as DocsComputedValue, p, refs);
    default:
      return typeof value === "string" ? value : "";
  }
}

/** Rows grouped by the server's group keys (in the server's order), for grouped views. */
export function docsGroupRowsBy(rows: readonly DocsRow[], groups: readonly DocsGroupInfo[], sub = false): { group: DocsGroupInfo; rows: DocsRow[] }[] {
  const out = groups.map((g) => ({ group: g, rows: [] as DocsRow[] }));
  const byKey = new Map(out.map((x) => [x.group.key, x]));
  for (const r of rows) {
    const keys = (sub ? r.sub_groups : r.groups) ?? [""];
    for (const k of keys.length ? keys : [""]) byKey.get(k)?.rows.push(r);
  }
  return out;
}

/** Rows grouped for a board, in option order, then "No value" (docs 1.1; boards now use docsGroupRowsBy). */
export function docsGroupRows(rows: readonly DocsRow[], p: DocsProperty | undefined): { option: DocsOption | null; rows: DocsRow[] }[] {
  if (!p) return [{ option: null, rows: [...rows] }];
  const groups = (p.options ?? []).map((o) => ({ option: o as DocsOption | null, rows: [] as DocsRow[] }));
  const none = { option: null as DocsOption | null, rows: [] as DocsRow[] };
  for (const r of rows) {
    const g = groups.find((x) => x.option && x.option.id === r.props[p.id]);
    (g ?? none).rows.push(r);
  }
  return [...groups, none];
}
