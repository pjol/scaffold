"use client";

import { keepPreviousData, useMutation, useQueryClient, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import { useEffect } from "react";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { crmEndpoints } from "./api";
import type {
  CrmActivity,
  CrmActivityRequest,
  CrmAssociations,
  CrmBoard,
  CrmBulkRequest,
  CrmBulkResult,
  CrmDashboard,
  CrmDeal,
  CrmDuplicate,
  CrmImport,
  CrmImportInspect,
  CrmImportPreview,
  CrmImportRequest,
  CrmListParams,
  CrmObjectType,
  CrmPipeline,
  CrmPipelineRequest,
  CrmProperty,
  CrmPropertyRequest,
  CrmRecord,
  CrmRecordAccess,
  CrmRecordDetail,
  CrmRecordOf,
  CrmRecordPage,
  CrmSearchHit,
  CrmSettings,
  CrmStageHistory,
  CrmTag,
  CrmTagTone,
  CrmTaskBucket,
  CrmTaskSummary,
  CrmTimelineFilter,
  CrmTimelineItem,
  CrmTimelinePage,
  CrmView,
  CrmViewRequest,
  CrmCompanyLink,
  CrmImportDedupe,
  CrmOwner,
  CrmProvider,
  CrmSourceImportResult,
  CrmSourceMode,
  CrmSources,
} from "./types";
import { CRM_SOURCE_APP } from "./types";

/** Query keys. `crmKeys.all()` invalidates everything this feature caches. */
export const crmKeys = {
  all: () => ["crm"] as const,
  org: (orgId: string) => ["crm", "org", orgId] as const,
  settings: (orgId: string) => ["crm", "org", orgId, "settings"] as const,
  pipelines: (orgId: string, source = CRM_SOURCE_APP) => ["crm", "org", orgId, "pipelines", source] as const,
  properties: (orgId: string, type: CrmObjectType, source = CRM_SOURCE_APP) => ["crm", "org", orgId, "properties", type, source] as const,
  sources: (orgId: string) => ["crm", "org", orgId, "sources"] as const,
  owners: (orgId: string, source: string) => ["crm", "org", orgId, "owners", source] as const,
  links: (companyId: string) => ["crm", "links", companyId] as const,
  tags: (orgId: string) => ["crm", "org", orgId, "tags"] as const,
  views: (orgId: string, type: CrmObjectType) => ["crm", "org", orgId, "views", type] as const,
  lists: (type: CrmObjectType) => ["crm", "list", type] as const,
  list: (orgId: string, type: CrmObjectType, params: CrmListParams) => ["crm", "list", type, orgId, params] as const,
  record: (type: CrmObjectType, id: string) => ["crm", "record", type, id] as const,
  timeline: (type: CrmObjectType, id: string) => ["crm", "timeline", type, id] as const,
  stageHistory: (dealId: string) => ["crm", "stage-history", dealId] as const,
  tasks: (orgId: string) => ["crm", "tasks", orgId] as const,
  taskSummary: (orgId: string, source = CRM_SOURCE_APP) => ["crm", "tasks", orgId, "summary", source] as const,
  boards: (orgId: string) => ["crm", "board", orgId] as const,
  board: (orgId: string, pipelineId: string | undefined, params: CrmListParams) => ["crm", "board", orgId, pipelineId ?? "", params] as const,
  dashboard: (orgId: string, pipelineId: string | undefined, source = CRM_SOURCE_APP) => ["crm", "dashboard", orgId, pipelineId ?? "", source] as const,
  search: (orgId: string, q: string, types: string, source = CRM_SOURCE_APP) => ["crm", "search", orgId, q, types, source] as const,
  duplicates: (orgId: string, key: string) => ["crm", "duplicates", orgId, key] as const,
  imports: (orgId: string) => ["crm", "org", orgId, "imports"] as const,
  importRun: (orgId: string, id: string) => ["crm", "import-run", orgId, id] as const,
};

/** The device's IANA time zone (for "today" and "this week"). */
export function crmTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

function retryUnlessClientError(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

// ---- settings, pipelines, properties, tags, views --------------------------------------

/** The org's CRM settings and what you may do (`can`). 403 permission_required without crm.access. */
export function useCrmSettings(orgId: string | null | undefined, options?: QueryOverrides<CrmSettings>): UseQueryResult<CrmSettings, ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.settings(orgId ?? ""), ({ signal }) => c.settings(orgId ?? "", { signal }), { retry: retryUnlessClientError, staleTime: 60_000, ...options }, !!orgId);
}

type CrmSettingsChange = { record_access?: CrmRecordAccess; currency?: string; source_mode?: CrmSourceMode; source_providers?: CrmProvider[] };

export function useCrmUpdateSettings(orgId: string): UseMutationResult<CrmSettings, ApiError, CrmSettingsChange> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmSettings, ApiError, CrmSettingsChange>({
    mutationFn: (body) => c.updateSettings(orgId, body),
    onSuccess: (s) => {
      qc.setQueryData(crmKeys.settings(orgId), s);
      void qc.invalidateQueries({ queryKey: crmKeys.all() });
    },
  });
}

/** Pipelines of the app's CRM, or of a connected account (`source` = connection id). */
export function useCrmPipelines(orgId: string | null | undefined, source: string = CRM_SOURCE_APP): UseQueryResult<CrmPipeline[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(
    crmKeys.pipelines(orgId ?? "", source),
    ({ signal }) => c.pipelines(orgId ?? "", { signal, ...(source !== CRM_SOURCE_APP ? { source } : {}) }).then((r) => r.items),
    { staleTime: 60_000, retry: retryUnlessClientError },
    !!orgId,
  );
}

function settingsMutation<TVars, TData>(qc: QueryClient, fn: (v: TVars) => Promise<TData>, keys: readonly (readonly unknown[])[]) {
  return {
    mutationFn: fn,
    onSuccess: () => {
      for (const k of keys) void qc.invalidateQueries({ queryKey: k });
    },
  };
}

export function useCrmCreatePipeline(orgId: string): UseMutationResult<CrmPipeline, ApiError, CrmPipelineRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmPipeline, ApiError, CrmPipelineRequest>(settingsMutation(qc, (b: CrmPipelineRequest) => c.createPipeline(orgId, b), [crmKeys.pipelines(orgId)]));
}

export function useCrmUpdatePipeline(orgId: string): UseMutationResult<CrmPipeline, ApiError, { id: string } & CrmPipelineRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmPipeline, ApiError, { id: string } & CrmPipelineRequest>(
    settingsMutation(qc, ({ id, ...b }: { id: string } & CrmPipelineRequest) => c.updatePipeline(orgId, id, b), [crmKeys.pipelines(orgId), crmKeys.boards(orgId), crmKeys.lists("deal")]),
  );
}

export function useCrmDeletePipeline(orgId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>(settingsMutation(qc, (id: string) => c.deletePipeline(orgId, id), [crmKeys.pipelines(orgId)]));
}

/** Built-in then custom properties of one object type (columns, filters, forms); a connected account's come from its schema. */
export function useCrmProperties(orgId: string | null | undefined, type: CrmObjectType, source: string = CRM_SOURCE_APP): UseQueryResult<CrmProperty[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(
    crmKeys.properties(orgId ?? "", type, source),
    ({ signal }) => c.properties(orgId ?? "", type, { signal, ...(source !== CRM_SOURCE_APP ? { source } : {}) }).then((r) => r.items),
    { staleTime: 5 * 60_000, retry: retryUnlessClientError },
    !!orgId,
  );
}

export function useCrmCreateProperty(orgId: string): UseMutationResult<CrmProperty, ApiError, CrmPropertyRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmProperty, ApiError, CrmPropertyRequest>(settingsMutation(qc, (b: CrmPropertyRequest) => c.createProperty(orgId, b), [["crm", "org", orgId, "properties"]]));
}

export function useCrmUpdateProperty(orgId: string): UseMutationResult<CrmProperty, ApiError, { id: string } & CrmPropertyRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmProperty, ApiError, { id: string } & CrmPropertyRequest>(
    settingsMutation(qc, ({ id, ...b }: { id: string } & CrmPropertyRequest) => c.updateProperty(orgId, id, b), [["crm", "org", orgId, "properties"], ["crm", "record"]]),
  );
}

export function useCrmDeleteProperty(orgId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>(settingsMutation(qc, (id: string) => c.deleteProperty(orgId, id), [["crm", "org", orgId, "properties"], ["crm", "record"]]));
}

export function useCrmTags(orgId: string | null | undefined): UseQueryResult<CrmTag[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.tags(orgId ?? ""), ({ signal }) => c.tags(orgId ?? "", { signal }).then((r) => r.items), { staleTime: 60_000 }, !!orgId);
}

/** Add a tag (or get the existing one with that name). */
export function useCrmCreateTag(orgId: string): UseMutationResult<CrmTag, ApiError, { name: string; tone?: CrmTagTone }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmTag, ApiError, { name: string; tone?: CrmTagTone }>({
    mutationFn: (b) => c.createTag(orgId, b),
    onSuccess: (t) => {
      qc.setQueryData<CrmTag[]>(crmKeys.tags(orgId), (list) => (list && !list.some((x) => x.id === t.id) ? [...list, t].sort((a, b) => a.name.localeCompare(b.name)) : list));
    },
  });
}

export function useCrmUpdateTag(orgId: string): UseMutationResult<CrmTag, ApiError, { id: string; name?: string; tone?: CrmTagTone }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmTag, ApiError, { id: string; name?: string; tone?: CrmTagTone }>(
    settingsMutation(qc, ({ id, ...b }: { id: string; name?: string; tone?: CrmTagTone }) => c.updateTag(orgId, id, b), [crmKeys.tags(orgId), ["crm", "list"], ["crm", "record"]]),
  );
}

export function useCrmDeleteTag(orgId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>(settingsMutation(qc, (id: string) => c.deleteTag(orgId, id), [crmKeys.tags(orgId), ["crm", "list"], ["crm", "record"]]));
}

/** Your views and shared ones for an object type. */
export function useCrmViews(orgId: string | null | undefined, type: CrmObjectType): UseQueryResult<CrmView[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.views(orgId ?? "", type), ({ signal }) => c.views(orgId ?? "", type, { signal }).then((r) => r.items), { staleTime: 60_000 }, !!orgId);
}

export function useCrmCreateView(orgId: string, type: CrmObjectType): UseMutationResult<CrmView, ApiError, CrmViewRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmView, ApiError, CrmViewRequest>({
    mutationFn: (b) => c.createView(orgId, { ...b, object_type: type }),
    onSuccess: (v) => qc.setQueryData<CrmView[]>(crmKeys.views(orgId, type), (list) => (list ? [...list, v] : [v])),
  });
}

export function useCrmUpdateView(orgId: string, type: CrmObjectType): UseMutationResult<CrmView, ApiError, { id: string } & CrmViewRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmView, ApiError, { id: string } & CrmViewRequest>({
    mutationFn: ({ id, ...b }) => c.updateView(orgId, id, b),
    onSuccess: (v) => qc.setQueryData<CrmView[]>(crmKeys.views(orgId, type), (list) => list?.map((x) => (x.id === v.id ? v : x))),
  });
}

export function useCrmDeleteView(orgId: string, type: CrmObjectType): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => c.deleteView(orgId, id),
    onSuccess: (_d, id) => qc.setQueryData<CrmView[]>(crmKeys.views(orgId, type), (list) => list?.filter((x) => x.id !== id)),
  });
}

// ---- records ---------------------------------------------------------------------------

/** A filtered, sorted keyset list; `crmTotal(data)` is the number of matches. */
export function useCrmRecords<T extends CrmObjectType>(
  orgId: string | null | undefined,
  type: T,
  params: CrmListParams = {},
  options?: InfiniteOverrides<CrmRecordOf[T]>,
): PagedQueryResult<CrmRecordOf[T]> {
  const c = useEndpoints(crmEndpoints);
  return usePagedQuery<CrmRecordOf[T]>(
    crmKeys.list(orgId ?? "", type, params),
    (cursor, signal) => c.list(orgId ?? "", type, { ...params, cursor }, { signal }),
    { placeholderData: keepPreviousData, ...options },
    !!orgId,
  );
}

/** The number of matches of a list (from its first page). */
export function crmTotal(data: { pages: Page<unknown>[] } | undefined): number | null {
  const first = data?.pages[0] as CrmRecordPage | undefined;
  return first?.total ?? null;
}

/** A record with its visible associations (404 when you can't see it). */
export function useCrmRecord<T extends CrmObjectType>(type: T, id: string | null | undefined, options?: QueryOverrides<CrmRecordDetail<CrmRecordOf[T]>>): UseQueryResult<CrmRecordDetail<CrmRecordOf[T]>, ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.record(type, id ?? ""), ({ signal }) => c.get(type, id ?? "", { signal }), { retry: retryUnlessClientError, ...options }, !!id);
}

function afterRecordWrite(qc: QueryClient, rec: CrmRecord) {
  qc.setQueryData<CrmRecordDetail>(crmKeys.record(rec.type, rec.id), (d) => (d ? { ...d, record: rec } : d));
  void qc.invalidateQueries({ queryKey: crmKeys.lists(rec.type) });
  void qc.invalidateQueries({ queryKey: crmKeys.timeline(rec.type, rec.id) });
  if (rec.type === "deal") {
    void qc.invalidateQueries({ queryKey: crmKeys.boards(rec.org_id) });
    void qc.invalidateQueries({ queryKey: ["crm", "dashboard", rec.org_id] });
  }
}

/** Create a record. A 409 `duplicate` error carries `details.matches` (CrmDuplicate[]); resend with allow_duplicate. */
export function useCrmCreateRecord<T extends CrmObjectType>(orgId: string, type: T, source: string = CRM_SOURCE_APP): UseMutationResult<CrmRecordOf[T], ApiError, Record<string, unknown>> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmRecordOf[T], ApiError, Record<string, unknown>>({
    mutationFn: (body) => c.create(orgId, type, body, source !== CRM_SOURCE_APP ? { source } : undefined),
    onSuccess: (rec) => afterRecordWrite(qc, rec),
  });
}

/** Change fields by property key; optimistic for plain values on the open record. */
export function useCrmUpdateRecord<T extends CrmObjectType>(type: T, id: string): UseMutationResult<CrmRecordOf[T], ApiError, Record<string, unknown>, { prev: CrmRecordDetail | undefined }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  const key = crmKeys.record(type, id);
  return useMutation<CrmRecordOf[T], ApiError, Record<string, unknown>, { prev: CrmRecordDetail | undefined }>({
    mutationFn: (body) => c.update(type, id, body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<CrmRecordDetail>(key);
      if (prev) {
        const next = { ...prev.record } as unknown as Record<string, unknown>;
        const props = { ...prev.record.props };
        for (const [k, v] of Object.entries(body)) {
          if (k in next && typeof v !== "object") next[k] = v;
          else if (k in props || !(k in next)) props[k] = v as never;
        }
        next["props"] = props;
        qc.setQueryData<CrmRecordDetail>(key, { ...prev, record: next as unknown as CrmRecord });
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSuccess: (rec) => afterRecordWrite(qc, rec),
  });
}

export function useCrmDeleteRecord(type: CrmObjectType): UseMutationResult<void, ApiError, { id: string; orgId: string }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; orgId: string }>({
    mutationFn: ({ id }) => c.remove(type, id),
    onSuccess: (_d, { id, orgId }) => {
      qc.removeQueries({ queryKey: crmKeys.record(type, id) });
      void qc.invalidateQueries({ queryKey: crmKeys.lists(type) });
      void qc.invalidateQueries({ queryKey: crmKeys.boards(orgId) });
      void qc.invalidateQueries({ queryKey: ["crm", "dashboard", orgId] });
    },
  });
}

/**
 * Move a deal to a stage (the board's drag). Optimistic on every cached board of the org:
 * the card moves, counts and totals follow; rolled back on error.
 */
export function useCrmMoveStage(orgId: string): UseMutationResult<CrmDeal, ApiError, { deal: CrmDeal; stageId: string }, { prev: [readonly unknown[], CrmBoard | undefined][] }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmDeal, ApiError, { deal: CrmDeal; stageId: string }, { prev: [readonly unknown[], CrmBoard | undefined][] }>({
    mutationFn: ({ deal, stageId }) => c.moveStage(deal.id, stageId),
    onMutate: async ({ deal, stageId }) => {
      await qc.cancelQueries({ queryKey: crmKeys.boards(orgId) });
      const prev = qc.getQueriesData<CrmBoard>({ queryKey: crmKeys.boards(orgId) });
      for (const [key, board] of prev) {
        if (board) qc.setQueryData<CrmBoard>(key, moveCard(board, deal, stageId));
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, board] of ctx?.prev ?? []) qc.setQueryData(key, board);
    },
    onSuccess: (rec) => afterRecordWrite(qc, rec),
  });
}

function moveCard(board: CrmBoard, deal: CrmDeal, stageId: string): CrmBoard {
  const target = board.columns.find((col) => col.stage.id === stageId);
  if (!target) return board;
  const moved: CrmDeal = {
    ...deal,
    stage: { id: target.stage.id, name: target.stage.name, probability: target.stage.probability, outcome: target.stage.outcome, position: target.stage.position },
    weighted_amount: deal.amount === null ? null : Math.trunc((deal.amount * target.stage.probability) / 100),
  };
  const adjust = (totals: CrmBoard["columns"][number]["totals"], sign: 1 | -1, d: CrmDeal) => {
    if (d.amount === null) return totals;
    const list = totals.map((t) => ({ ...t }));
    let t = list.find((x) => x.currency === d.currency);
    if (!t) {
      t = { currency: d.currency, amount: 0, weighted: 0 };
      list.push(t);
    }
    t.amount += sign * d.amount;
    t.weighted += sign * (d.weighted_amount ?? 0);
    return list.filter((x) => x.amount !== 0 || x.weighted !== 0);
  };
  return {
    ...board,
    columns: board.columns.map((col) => {
      if (col.stage.id === deal.stage.id && col.stage.id !== stageId) {
        return { ...col, count: Math.max(0, col.count - 1), deals: col.deals.filter((d) => d.id !== deal.id), totals: adjust(col.totals, -1, deal) };
      }
      if (col.stage.id === stageId && deal.stage.id !== stageId) {
        return { ...col, count: col.count + 1, deals: [moved, ...col.deals.filter((d) => d.id !== deal.id)], totals: adjust(col.totals, 1, moved) };
      }
      return col;
    }),
  };
}

export function useCrmStageHistory(dealId: string | null | undefined): UseQueryResult<CrmStageHistory, ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.stageHistory(dealId ?? ""), ({ signal }) => c.stageHistory(dealId ?? "", { signal }), undefined, !!dealId);
}

/** Records you can see whose email or domain matches, while someone types (debounced). */
export function useCrmDuplicates(
  orgId: string | null | undefined,
  params: { object: "company" | "contact"; emails?: string[]; domain?: string; exclude?: string },
): UseQueryResult<CrmDuplicate[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  const key = JSON.stringify(params);
  const debounced = useDebouncedValue(key, 400);
  const p = JSON.parse(debounced) as typeof params;
  const enabled = !!orgId && ((p.emails?.length ?? 0) > 0 || !!p.domain);
  return useApiQuery(crmKeys.duplicates(orgId ?? "", debounced), ({ signal }) => c.duplicates(orgId ?? "", p, { signal }).then((r) => r.matches), { staleTime: 30_000 }, enabled);
}

/** Search records by name, email, phone or domain (debounced; an empty query lists recently updated). */
export function useCrmSearch(
  orgId: string | null | undefined,
  q: string,
  types?: CrmObjectType[],
  options?: { limit?: number; enabled?: boolean; source?: string },
): UseQueryResult<CrmSearchHit[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  const debounced = useDebouncedValue(q.trim(), 250);
  const t = (types ?? []).join(",");
  const source = options?.source ?? CRM_SOURCE_APP;
  return useApiQuery<CrmSearchHit[]>(
    crmKeys.search(orgId ?? "", debounced, t, source),
    ({ signal }) =>
      c
        .search(
          orgId ?? "",
          { q: debounced, ...(types ? { types } : {}), ...(options?.limit ? { limit: options.limit } : {}), ...(source !== CRM_SOURCE_APP ? { source } : {}) },
          { signal },
        )
        .then((r) => r.items),
    { placeholderData: keepPreviousData, staleTime: 15_000 },
    !!orgId && (options?.enabled ?? true),
  );
}

// ---- associations -------------------------------------------------------------------------

/** An association change between two records (edit needed on the record whose side changes). */
export type CrmLinkVars =
  | { op: "add_company"; contactId: string; companyId: string; primary?: boolean }
  | { op: "primary_company"; contactId: string; companyId: string }
  | { op: "remove_company"; contactId: string; companyId: string }
  | { op: "add_contact"; dealId: string; contactId: string }
  | { op: "remove_contact"; dealId: string; contactId: string };

/** Link or unlink a contact and a company, or a deal and a contact; both records refresh. */
export function useCrmLink(): UseMutationResult<CrmAssociations, ApiError, CrmLinkVars> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmAssociations, ApiError, CrmLinkVars>({
    mutationFn: (v) => {
      switch (v.op) {
        case "add_company":
          return c.addContactCompany(v.contactId, v.companyId, v.primary ?? false);
        case "primary_company":
          return c.setPrimaryCompany(v.contactId, v.companyId);
        case "remove_company":
          return c.removeContactCompany(v.contactId, v.companyId);
        case "add_contact":
          return c.addDealContact(v.dealId, v.contactId);
        case "remove_contact":
          return c.removeDealContact(v.dealId, v.contactId);
      }
    },
    onSuccess: (_a, v) => {
      const touched: [CrmObjectType, string][] =
        v.op === "add_contact" || v.op === "remove_contact"
          ? [
              ["deal", v.dealId],
              ["contact", v.contactId],
            ]
          : [
              ["contact", v.contactId],
              ["company", v.companyId],
            ];
      for (const [t, id] of touched) {
        void qc.invalidateQueries({ queryKey: crmKeys.record(t, id) });
        void qc.invalidateQueries({ queryKey: crmKeys.timeline(t, id) });
        void qc.invalidateQueries({ queryKey: crmKeys.lists(t) });
      }
    },
  });
}

// ---- timeline, activities, tasks -----------------------------------------------------

/** A record's timeline, newest first; the first page carries `upcoming` (open tasks). */
export function useCrmTimeline(type: CrmObjectType, id: string | null | undefined, filter: CrmTimelineFilter = "all"): PagedQueryResult<CrmTimelineItem> {
  const c = useEndpoints(crmEndpoints);
  return usePagedQuery<CrmTimelineItem>(
    [...crmKeys.timeline(type, id ?? ""), filter],
    (cursor, signal) => c.timeline(type, id ?? "", { filter, cursor, limit: 30 }, { signal }),
    { retry: retryUnlessClientError },
    !!id,
  );
}

/** Open tasks on the record from a timeline query's first page. */
export function crmUpcoming(data: { pages: Page<unknown>[] } | undefined): CrmActivity[] {
  return (data?.pages[0] as CrmTimelinePage | undefined)?.upcoming ?? [];
}

function afterActivity(qc: QueryClient, a: CrmActivity) {
  for (const r of a.records) {
    void qc.invalidateQueries({ queryKey: crmKeys.timeline(r.type, r.id) });
    void qc.invalidateQueries({ queryKey: crmKeys.record(r.type, r.id) });
  }
  void qc.invalidateQueries({ queryKey: crmKeys.tasks(a.org_id) });
  void qc.invalidateQueries({ queryKey: ["crm", "dashboard", a.org_id] });
}

/** Log a note, call, email or meeting, or create a task, on a record. */
export function useCrmLogActivity(type: CrmObjectType, id: string): UseMutationResult<CrmActivity, ApiError, CrmActivityRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmActivity, ApiError, CrmActivityRequest>({ mutationFn: (b) => c.logActivity(type, id, b), onSuccess: (a) => afterActivity(qc, a) });
}

/** Edit an activity, or mark a task done (`{ done: true }`, optimistic in task lists). */
export function useCrmUpdateActivity(orgId: string): UseMutationResult<CrmActivity, ApiError, { id: string } & CrmActivityRequest, { prev: [readonly unknown[], unknown][] }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmActivity, ApiError, { id: string } & CrmActivityRequest, { prev: [readonly unknown[], unknown][] }>({
    mutationFn: ({ id, ...b }) => c.updateActivity(id, b),
    onMutate: async ({ id, done }) => {
      if (done === undefined) return { prev: [] };
      await qc.cancelQueries({ queryKey: crmKeys.tasks(orgId) });
      const prev = qc.getQueriesData({ queryKey: crmKeys.tasks(orgId) });
      const doneAt = done ? new Date().toISOString() : null;
      qc.setQueriesData<{ pages: Page<CrmActivity>[]; pageParams: unknown[] }>({ queryKey: crmKeys.tasks(orgId) }, (data) =>
        data && Array.isArray(data.pages)
          ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.map((a) => (a.id === id ? { ...a, done_at: doneAt } : a)) })) }
          : data,
      );
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.prev ?? []) qc.setQueryData(key, data);
    },
    onSuccess: (a) => afterActivity(qc, a),
  });
}

export function useCrmDeleteActivity(): UseMutationResult<void, ApiError, CrmActivity> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, CrmActivity>({ mutationFn: (a) => c.deleteActivity(a.id), onSuccess: (_d, a) => afterActivity(qc, a) });
}

/** Tasks (yours by default) in a bucket, soonest first (done: latest first). */
export function useCrmTasks(
  orgId: string | null | undefined,
  bucket: CrmTaskBucket,
  assignee: "me" | "any" | (string & {}) = "me",
  options?: InfiniteOverrides<CrmActivity>,
  source: string = CRM_SOURCE_APP,
): PagedQueryResult<CrmActivity> {
  const c = useEndpoints(crmEndpoints);
  const tz = crmTimeZone();
  return usePagedQuery<CrmActivity>(
    [...crmKeys.tasks(orgId ?? ""), bucket, assignee, tz, source],
    (cursor, signal) => c.tasks(orgId ?? "", { bucket, assignee, tz, cursor, limit: 50, ...(source !== CRM_SOURCE_APP ? { source } : {}) }, { signal }),
    options,
    !!orgId,
  );
}

/** Your open tasks by when they're due (nav badge, dashboard). */
export function useCrmTaskSummary(orgId: string | null | undefined, options?: QueryOverrides<CrmTaskSummary>, source: string = CRM_SOURCE_APP): UseQueryResult<CrmTaskSummary, ApiError> {
  const c = useEndpoints(crmEndpoints);
  const tz = crmTimeZone();
  return useApiQuery(
    crmKeys.taskSummary(orgId ?? "", source),
    ({ signal }) => c.taskSummary(orgId ?? "", tz, { signal, ...(source !== CRM_SOURCE_APP ? { source } : {}) }),
    { staleTime: 60_000, retry: retryUnlessClientError, ...options },
    !!orgId,
  );
}

// ---- board, dashboard ---------------------------------------------------------------------

/** A pipeline's board: per stage the visible count, totals and first cards. */
/** `params.source` picks the app's board or a connected account's (a connection id). */
export function useCrmBoard(orgId: string | null | undefined, pipelineId: string | undefined, params: CrmListParams = {}): UseQueryResult<CrmBoard, ApiError> {
  const c = useEndpoints(crmEndpoints);
  const tz = crmTimeZone();
  return useApiQuery(
    crmKeys.board(orgId ?? "", pipelineId, params),
    ({ signal }) => c.board(orgId ?? "", { ...params, tz, pipeline_id: pipelineId }, { signal }),
    { placeholderData: keepPreviousData, retry: retryUnlessClientError },
    !!orgId,
  );
}

export function useCrmDashboard(orgId: string | null | undefined, pipelineId?: string, source: string = CRM_SOURCE_APP): UseQueryResult<CrmDashboard, ApiError> {
  const c = useEndpoints(crmEndpoints);
  const tz = crmTimeZone();
  return useApiQuery(
    crmKeys.dashboard(orgId ?? "", pipelineId, source),
    ({ signal }) => c.dashboard(orgId ?? "", { pipeline_id: pipelineId, tz, ...(source !== CRM_SOURCE_APP ? { source } : {}) }, { signal }),
    { retry: retryUnlessClientError },
    !!orgId,
  );
}

// ---- connected CRMs ------------------------------------------------------------------------

/** Where CRM records come from in this workspace, which accounts you connected, and what you could connect. */
export function useCrmSources(orgId: string | null | undefined, options?: QueryOverrides<CrmSources>): UseQueryResult<CrmSources, ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.sources(orgId ?? ""), ({ signal }) => c.sources(orgId ?? "", { signal }), { staleTime: 60_000, retry: retryUnlessClientError, ...options }, !!orgId);
}

/** A connected account's owners (assign owner, task assignee). */
export function useCrmOwners(orgId: string | null | undefined, source: string | null | undefined): UseQueryResult<CrmOwner[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(
    crmKeys.owners(orgId ?? "", source ?? ""),
    ({ signal }) => c.owners(orgId ?? "", source ?? "", { signal }).then((r) => r.items),
    { staleTime: 5 * 60_000, retry: retryUnlessClientError },
    !!orgId && !!source && source !== CRM_SOURCE_APP,
  );
}

/** Copy connected records into the app's CRM once (crm.import); lists refresh after. */
export function useCrmImportFromSource(orgId: string, source: string): UseMutationResult<CrmSourceImportResult, ApiError, { object_type: CrmObjectType; ids: string[]; dedupe?: CrmImportDedupe }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmSourceImportResult, ApiError, { object_type: CrmObjectType; ids: string[]; dedupe?: CrmImportDedupe }>({
    mutationFn: (b) => c.importFromSource(orgId, source, b),
    onSuccess: (_r, b) => {
      void qc.invalidateQueries({ queryKey: crmKeys.lists(b.object_type) });
      void qc.invalidateQueries({ queryKey: crmKeys.boards(orgId) });
      void qc.invalidateQueries({ queryKey: ["crm", "dashboard", orgId] });
    },
  });
}

/** A company's links to platform companies, each read with your own connection. */
export function useCrmCompanyLinks(companyId: string | null | undefined, options?: QueryOverrides<CrmCompanyLink[]>): UseQueryResult<CrmCompanyLink[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.links(companyId ?? ""), ({ signal }) => c.companyLinks(companyId ?? "", { signal }).then((r) => r.items), { retry: retryUnlessClientError, ...options }, !!companyId);
}

/** Link or unlink a company and a connected company (edit on the company). */
export function useCrmLinkCompany(companyId: string): UseMutationResult<CrmCompanyLink | void, ApiError, { op: "link"; recordId: string } | { op: "unlink"; linkId: string }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmCompanyLink | void, ApiError, { op: "link"; recordId: string } | { op: "unlink"; linkId: string }>({
    mutationFn: (v) => (v.op === "link" ? c.linkCompany(companyId, v.recordId) : c.unlinkCompany(companyId, v.linkId)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: crmKeys.links(companyId) });
      void qc.invalidateQueries({ queryKey: crmKeys.timeline("company", companyId) });
    },
  });
}

// ---- bulk, merge, import, export ---------------------------------------------------------

export function useCrmBulk(orgId: string): UseMutationResult<CrmBulkResult, ApiError, CrmBulkRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmBulkResult, ApiError, CrmBulkRequest>({
    mutationFn: (b) => c.bulk(orgId, b),
    onSuccess: (_r, b) => {
      void qc.invalidateQueries({ queryKey: crmKeys.lists(b.object_type) });
      void qc.invalidateQueries({ queryKey: ["crm", "record", b.object_type] });
      void qc.invalidateQueries({ queryKey: crmKeys.boards(orgId) });
      void qc.invalidateQueries({ queryKey: ["crm", "dashboard", orgId] });
    },
  });
}

/** Merge `secondary_id` into `primary_id` (crm.delete); answers with the kept record. */
export function useCrmMerge<T extends CrmObjectType>(orgId: string, type: T): UseMutationResult<CrmRecordDetail<CrmRecordOf[T]>, ApiError, { primaryId: string; secondaryId: string }> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmRecordDetail<CrmRecordOf[T]>, ApiError, { primaryId: string; secondaryId: string }>({
    mutationFn: ({ primaryId, secondaryId }) => c.merge(orgId, { object_type: type, primary_id: primaryId, secondary_id: secondaryId }),
    onSuccess: (d, { secondaryId }) => {
      qc.setQueryData(crmKeys.record(type, d.record.id), d);
      qc.removeQueries({ queryKey: crmKeys.record(type, secondaryId) });
      void qc.invalidateQueries({ queryKey: crmKeys.timeline(type, d.record.id) });
      void qc.invalidateQueries({ queryKey: crmKeys.lists(type) });
      void qc.invalidateQueries({ queryKey: crmKeys.boards(orgId) });
    },
  });
}

export function useCrmInspectImport(orgId: string): UseMutationResult<CrmImportInspect, ApiError, CrmImportRequest> {
  const c = useEndpoints(crmEndpoints);
  return useMutation<CrmImportInspect, ApiError, CrmImportRequest>({ mutationFn: (b) => c.inspectImport(orgId, b) });
}

export function useCrmPreviewImport(orgId: string): UseMutationResult<CrmImportPreview, ApiError, CrmImportRequest> {
  const c = useEndpoints(crmEndpoints);
  return useMutation<CrmImportPreview, ApiError, CrmImportRequest>({ mutationFn: (b) => c.previewImport(orgId, b) });
}

export function useCrmRunImport(orgId: string): UseMutationResult<CrmImport, ApiError, CrmImportRequest> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmImport, ApiError, CrmImportRequest>({
    mutationFn: (b) => c.runImport(orgId, b),
    onSuccess: (run) => {
      qc.setQueryData(crmKeys.importRun(orgId, run.id), run);
      void qc.invalidateQueries({ queryKey: crmKeys.imports(orgId) });
    },
  });
}

/** One import run, polled every 1.5 s while it runs; lists refresh once when it finishes. */
export function useCrmImportRun(orgId: string | null | undefined, id: string | null | undefined): UseQueryResult<CrmImport, ApiError> {
  const c = useEndpoints(crmEndpoints);
  const qc = useQueryClient();
  const run = useApiQuery(
    crmKeys.importRun(orgId ?? "", id ?? ""),
    ({ signal }) => c.importRun(orgId ?? "", id ?? "", { signal }),
    { refetchInterval: (q) => (q.state.data?.status === "running" ? 1500 : false) },
    !!orgId && !!id,
  );
  const status = run.data?.status;
  useEffect(() => {
    if (!status || status === "running") return;
    void qc.invalidateQueries({ queryKey: ["crm", "list"] });
    void qc.invalidateQueries({ queryKey: crmKeys.imports(orgId ?? ""), exact: true });
  }, [status, orgId, qc]);
  return run;
}

export function useCrmImports(orgId: string | null | undefined): UseQueryResult<CrmImport[], ApiError> {
  const c = useEndpoints(crmEndpoints);
  return useApiQuery(crmKeys.imports(orgId ?? ""), ({ signal }) => c.imports(orgId ?? "", { signal }).then((r) => r.items), { retry: retryUnlessClientError }, !!orgId);
}

/** Resolves to CSV text of the visible rows (crm.export). */
export function useCrmExport(orgId: string, type: CrmObjectType): UseMutationResult<string, ApiError, CrmListParams & { columns?: string[] }> {
  const c = useEndpoints(crmEndpoints);
  return useMutation<string, ApiError, CrmListParams & { columns?: string[] }>({ mutationFn: (p) => c.exportCsv(orgId, type, { ...p, tz: crmTimeZone() }) });
}

// ---- realtime ------------------------------------------------------------------------------

registerRealtimeHandler("crm.record_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: crmKeys.record(e.data.type, e.data.id) });
  void qc.invalidateQueries({ queryKey: crmKeys.lists(e.data.type) });
});
registerRealtimeHandler("crm.timeline_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: crmKeys.timeline(e.data.type, e.data.id) });
});
registerRealtimeHandler("crm.record_deleted", (qc, e) => {
  void qc.invalidateQueries({ queryKey: crmKeys.record(e.data.type, e.data.id) });
  void qc.invalidateQueries({ queryKey: crmKeys.lists(e.data.type) });
});
registerRealtimeHandler("crm.board_changed", (qc) => {
  void qc.invalidateQueries({ queryKey: ["crm", "board"] });
  void qc.invalidateQueries({ queryKey: ["crm", "dashboard"] });
});
registerRealtimeHandler("crm.settings_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: crmKeys.org(e.data.org_id) });
  if (e.data.what === "pipelines") void qc.invalidateQueries({ queryKey: crmKeys.boards(e.data.org_id) });
});
registerReconnectRefetch(crmKeys.all());
