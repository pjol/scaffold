import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  CrmActivity,
  CrmActivityRequest,
  CrmAssociations,
  CrmBoard,
  CrmBulkRequest,
  CrmBulkResult,
  CrmDashboard,
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
  CrmTimelinePage,
  CrmView,
  CrmViewRequest,
  CrmDeal,
  CrmRecordAccess,
  CrmCompanyLink,
  CrmImportDedupe,
  CrmOwner,
  CrmSourceImportResult,
  CrmSourceMode,
  CrmProvider,
  CrmSources,
} from "./types";
import { CRM_PATH } from "./types";

/** Query params of a list (filters travel as JSON). */
export function crmListQuery(p: CrmListParams & { cursor?: string | undefined }) {
  return {
    source: p.source,
    filters: p.filters && p.filters.length > 0 ? JSON.stringify(p.filters) : undefined,
    q: p.q || undefined,
    sort: p.sort,
    dir: p.dir,
    tz: p.tz,
    limit: p.limit,
    cursor: p.cursor,
  };
}

/** CRM endpoints: records, associations, activities, pipelines, settings, views, board, dashboard, import/export. */
export function crmEndpoints(http: HttpClient) {
  const C = "/v1/crm";
  const org = (orgId: string) => `${C}/orgs/${seg(orgId)}`;
  const rec = (type: CrmObjectType, id: string) => `${C}/${CRM_PATH[type]}/${seg(id)}`;
  return {
    // ---- records
    list: <T extends CrmObjectType>(orgId: string, type: T, params: CrmListParams & { cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<CrmRecordPage<CrmRecordOf[T]>>(`${org(orgId)}/${CRM_PATH[type]}`, { ...o, query: crmListQuery(params) }),
    /** Field values by property key, plus company_id (contacts), contact_ids / pipeline_id (deals) and allow_duplicate. 409 `duplicate` lists matches. */
    create: <T extends CrmObjectType>(orgId: string, type: T, body: Record<string, unknown>, o?: CallOptions & { source?: string | undefined }) =>
      http.post<CrmRecordOf[T]>(`${org(orgId)}/${CRM_PATH[type]}`, body, { ...o, query: { source: o?.source } }),
    get: <T extends CrmObjectType>(type: T, id: string, o?: CallOptions) => http.get<CrmRecordDetail<CrmRecordOf[T]>>(rec(type, id), o),
    /** Changed fields by property key (null clears). */
    update: <T extends CrmObjectType>(type: T, id: string, body: Record<string, unknown>, o?: CallOptions) => http.patch<CrmRecordOf[T]>(rec(type, id), body, o),
    remove: (type: CrmObjectType, id: string, o?: CallOptions) => http.delete<void>(rec(type, id), o),
    moveStage: (dealId: string, stageId: string, o?: CallOptions) => http.post<CrmDeal>(`${C}/deals/${seg(dealId)}/stage`, { stage_id: stageId }, o),
    stageHistory: (dealId: string, o?: CallOptions) => http.get<CrmStageHistory>(`${C}/deals/${seg(dealId)}/stages`, o),
    duplicates: (orgId: string, params: { object: "company" | "contact"; emails?: string[]; domain?: string; exclude?: string }, o?: CallOptions) =>
      http.get<{ matches: CrmDuplicate[] }>(`${org(orgId)}/duplicates`, {
        ...o,
        query: { object: params.object, emails: params.emails?.join(","), domain: params.domain, exclude: params.exclude },
      }),
    /** `source`: a connection id searches that account; "all" adds every connected account to the app's own hits. */
    search: (orgId: string, params: { q: string; types?: CrmObjectType[]; limit?: number; source?: string | undefined }, o?: CallOptions) =>
      http.get<{ items: CrmSearchHit[] }>(`${org(orgId)}/search`, {
        ...o,
        query: { q: params.q, types: params.types?.join(","), limit: params.limit, source: params.source },
      }),

    // ---- associations
    addContactCompany: (contactId: string, companyId: string, primary = false, o?: CallOptions) =>
      http.post<CrmAssociations>(`${C}/contacts/${seg(contactId)}/companies`, { company_id: companyId, primary }, o),
    setPrimaryCompany: (contactId: string, companyId: string, o?: CallOptions) =>
      http.put<CrmAssociations>(`${C}/contacts/${seg(contactId)}/companies/${seg(companyId)}/primary`, {}, o),
    removeContactCompany: (contactId: string, companyId: string, o?: CallOptions) =>
      http.delete<CrmAssociations>(`${C}/contacts/${seg(contactId)}/companies/${seg(companyId)}`, o),
    addDealContact: (dealId: string, contactId: string, o?: CallOptions) => http.post<CrmAssociations>(`${C}/deals/${seg(dealId)}/contacts`, { contact_id: contactId }, o),
    removeDealContact: (dealId: string, contactId: string, o?: CallOptions) => http.delete<CrmAssociations>(`${C}/deals/${seg(dealId)}/contacts/${seg(contactId)}`, o),

    // ---- timeline and activities
    timeline: (type: CrmObjectType, id: string, params: { filter?: CrmTimelineFilter; cursor?: string | undefined; limit?: number } = {}, o?: CallOptions) =>
      http.get<CrmTimelinePage>(`${rec(type, id)}/timeline`, { ...o, query: { filter: params.filter, cursor: params.cursor, limit: params.limit } }),
    logActivity: (type: CrmObjectType, id: string, body: CrmActivityRequest, o?: CallOptions) => http.post<CrmActivity>(`${rec(type, id)}/activities`, body, o),
    updateActivity: (activityId: string, body: CrmActivityRequest, o?: CallOptions) => http.patch<CrmActivity>(`${C}/activities/${seg(activityId)}`, body, o),
    deleteActivity: (activityId: string, o?: CallOptions) => http.delete<void>(`${C}/activities/${seg(activityId)}`, o),
    tasks: (
      orgId: string,
      params: { bucket?: CrmTaskBucket; assignee?: string; tz?: string; cursor?: string | undefined; limit?: number; source?: string | undefined } = {},
      o?: CallOptions,
    ) => http.get<Page<CrmActivity>>(`${org(orgId)}/tasks`, { ...o, query: params }),
    taskSummary: (orgId: string, tz?: string, o?: CallOptions & { source?: string | undefined }) =>
      http.get<CrmTaskSummary>(`${org(orgId)}/tasks/summary`, { ...o, query: { tz, source: o?.source } }),

    // ---- board and dashboard
    board: (orgId: string, params: CrmListParams & { pipeline_id?: string | undefined } = {}, o?: CallOptions) =>
      http.get<CrmBoard>(`${org(orgId)}/board`, { ...o, query: { ...crmListQuery(params), pipeline_id: params.pipeline_id } }),
    dashboard: (orgId: string, params: { pipeline_id?: string | undefined; tz?: string; source?: string | undefined } = {}, o?: CallOptions) =>
      http.get<CrmDashboard>(`${org(orgId)}/dashboard`, { ...o, query: params }),

    // ---- settings
    settings: (orgId: string, o?: CallOptions) => http.get<CrmSettings>(`${org(orgId)}/settings`, o),
    updateSettings: (
      orgId: string,
      body: { record_access?: CrmRecordAccess; currency?: string; source_mode?: CrmSourceMode; source_providers?: CrmProvider[] },
      o?: CallOptions,
    ) => http.put<CrmSettings>(`${org(orgId)}/settings`, body, o),
    pipelines: (orgId: string, o?: CallOptions & { source?: string | undefined }) =>
      http.get<{ items: CrmPipeline[] }>(`${org(orgId)}/pipelines`, { ...o, query: { source: o?.source } }),
    createPipeline: (orgId: string, body: CrmPipelineRequest, o?: CallOptions) => http.post<CrmPipeline>(`${org(orgId)}/pipelines`, body, o),
    updatePipeline: (orgId: string, pipelineId: string, body: CrmPipelineRequest, o?: CallOptions) =>
      http.patch<CrmPipeline>(`${org(orgId)}/pipelines/${seg(pipelineId)}`, body, o),
    deletePipeline: (orgId: string, pipelineId: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/pipelines/${seg(pipelineId)}`, o),
    properties: (orgId: string, type: CrmObjectType, o?: CallOptions & { source?: string | undefined }) =>
      http.get<{ items: CrmProperty[] }>(`${org(orgId)}/properties`, { ...o, query: { object: type, source: o?.source } }),
    createProperty: (orgId: string, body: CrmPropertyRequest, o?: CallOptions) => http.post<CrmProperty>(`${org(orgId)}/properties`, body, o),
    updateProperty: (orgId: string, propertyId: string, body: CrmPropertyRequest, o?: CallOptions) =>
      http.patch<CrmProperty>(`${org(orgId)}/properties/${seg(propertyId)}`, body, o),
    deleteProperty: (orgId: string, propertyId: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/properties/${seg(propertyId)}`, o),
    tags: (orgId: string, o?: CallOptions) => http.get<{ items: CrmTag[] }>(`${org(orgId)}/tags`, o),
    /** An existing name returns that tag. */
    createTag: (orgId: string, body: { name: string; tone?: CrmTagTone }, o?: CallOptions) => http.post<CrmTag>(`${org(orgId)}/tags`, body, o),
    updateTag: (orgId: string, tagId: string, body: { name?: string; tone?: CrmTagTone }, o?: CallOptions) => http.patch<CrmTag>(`${org(orgId)}/tags/${seg(tagId)}`, body, o),
    deleteTag: (orgId: string, tagId: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/tags/${seg(tagId)}`, o),

    // ---- views
    views: (orgId: string, type: CrmObjectType, o?: CallOptions) => http.get<{ items: CrmView[] }>(`${org(orgId)}/views`, { ...o, query: { object: type } }),
    createView: (orgId: string, body: CrmViewRequest, o?: CallOptions) => http.post<CrmView>(`${org(orgId)}/views`, body, o),
    updateView: (orgId: string, viewId: string, body: CrmViewRequest, o?: CallOptions) => http.patch<CrmView>(`${org(orgId)}/views/${seg(viewId)}`, body, o),
    deleteView: (orgId: string, viewId: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/views/${seg(viewId)}`, o),

    // ---- connected CRMs
    /** Where CRM records come from in this workspace (the app, connected accounts) and what could still be connected. */
    sources: (orgId: string, o?: CallOptions) => http.get<CrmSources>(`${C}/sources`, { ...o, query: { org_id: orgId } }),
    /** The platform's owners (owner pickers, task assignees on connected records). */
    owners: (orgId: string, sourceId: string, o?: CallOptions) => http.get<{ items: CrmOwner[] }>(`${org(orgId)}/sources/${seg(sourceId)}/owners`, o),
    /** One-time copy of connected records into the app's CRM (crm.import), deduped by email and domain. */
    importFromSource: (orgId: string, sourceId: string, body: { object_type: CrmObjectType; ids: string[]; dedupe?: CrmImportDedupe }, o?: CallOptions) =>
      http.post<CrmSourceImportResult>(`${org(orgId)}/sources/${seg(sourceId)}/import`, body, { timeoutMs: 120_000, ...o }),
    /** A company's links to platform companies, read with your own connection. */
    companyLinks: (companyId: string, o?: CallOptions) => http.get<{ items: CrmCompanyLink[] }>(`${C}/companies/${seg(companyId)}/links`, o),
    linkCompany: (companyId: string, recordId: string, o?: CallOptions) => http.post<CrmCompanyLink>(`${C}/companies/${seg(companyId)}/links`, { record_id: recordId }, o),
    unlinkCompany: (companyId: string, linkId: string, o?: CallOptions) => http.delete<void>(`${C}/companies/${seg(companyId)}/links/${seg(linkId)}`, o),

    // ---- bulk, merge, import, export
    bulk: (orgId: string, body: CrmBulkRequest, o?: CallOptions) => http.post<CrmBulkResult>(`${org(orgId)}/bulk`, body, o),
    merge: <T extends CrmObjectType>(orgId: string, body: { object_type: T; primary_id: string; secondary_id: string }, o?: CallOptions) =>
      http.post<CrmRecordDetail<CrmRecordOf[T]>>(`${org(orgId)}/merge`, body, o),
    inspectImport: (orgId: string, body: CrmImportRequest, o?: CallOptions) => http.post<CrmImportInspect>(`${org(orgId)}/imports/inspect`, body, { timeoutMs: 60_000, ...o }),
    previewImport: (orgId: string, body: CrmImportRequest, o?: CallOptions) => http.post<CrmImportPreview>(`${org(orgId)}/imports/preview`, body, { timeoutMs: 60_000, ...o }),
    /** Starts the import; poll `importRun` until status isn't running. */
    runImport: (orgId: string, body: CrmImportRequest, o?: CallOptions) => http.post<CrmImport>(`${org(orgId)}/imports`, body, { timeoutMs: 60_000, ...o }),
    imports: (orgId: string, o?: CallOptions) => http.get<{ items: CrmImport[] }>(`${org(orgId)}/imports`, o),
    importRun: (orgId: string, importId: string, o?: CallOptions) => http.get<CrmImport>(`${org(orgId)}/imports/${seg(importId)}`, o),
    /** CSV text of the visible rows (at most 10,000). */
    exportCsv: (orgId: string, type: CrmObjectType, params: CrmListParams & { columns?: string[] } = {}, o?: CallOptions) =>
      http.text("GET", `${org(orgId)}/export`, {
        ...o,
        timeoutMs: 60_000,
        query: { ...crmListQuery(params), object: type, columns: params.columns?.join(",") },
      }),
  };
}

export type CrmEndpoints = ReturnType<typeof crmEndpoints>;
