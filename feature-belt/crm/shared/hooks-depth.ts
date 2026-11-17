"use client";

import { keepPreviousData, useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { registerRealtimeHandler } from "../hooks/realtime-handlers";
import { crmDepthEndpoints } from "./api-depth";
import { crmKeys, crmTimeZone } from "./hooks";
import type { CrmActivity, CrmListParams, CrmTicket } from "./types";
import type {
  CrmBookingLink,
  CrmEmailDraft,
  CrmEmailDraftRequest,
  CrmEmailTemplate,
  CrmEmailTemplateRequest,
  CrmForecast,
  CrmForm,
  CrmFormRequest,
  CrmFormSubmission,
  CrmFormSubmit,
  CrmGoal,
  CrmGoalRequest,
  CrmLineItem,
  CrmLineItemRequest,
  CrmLineItems,
  CrmMailAccounts,
  CrmMailSummary,
  CrmMeetingCandidate,
  CrmMergeField,
  CrmProduct,
  CrmProductRequest,
  CrmPublicForm,
  CrmPublicQuote,
  CrmQuote,
  CrmQuoteDecision,
  CrmQuoteRequest,
  CrmQuoteSummary,
  CrmRendered,
  CrmReport,
  CrmReportDashboard,
  CrmReportResult,
  CrmReportSpec,
  CrmScoreRule,
  CrmScoreRuleRequest,
  CrmSendMailRequest,
  CrmTicketBoard,
  CrmWorkflow,
  CrmWorkflowRequest,
  CrmWorkflowRun,
} from "./types-depth";

/** Query keys of the 1.3 surfaces. Per-deal keys sit under `crmKeys.record`, so record events refresh them. */
export const crmDepthKeys = {
  products: (orgId: string, q: string, archived: boolean) => ["crm", "org", orgId, "products", q, archived] as const,
  productsAll: (orgId: string) => ["crm", "org", orgId, "products"] as const,
  lineItems: (dealId: string) => [...crmKeys.record("deal", dealId), "line-items"] as const,
  quotes: (dealId: string) => [...crmKeys.record("deal", dealId), "quotes"] as const,
  quote: (id: string) => ["crm", "quote", id] as const,
  publicQuote: (token: string) => ["crm", "public-quote", token] as const,
  ticketBoard: (orgId: string, pipelineId: string | undefined, params: CrmListParams) => ["crm", "board", orgId, "ticket", pipelineId ?? "", params] as const,
  mailAccounts: (orgId: string) => ["crm", "org", orgId, "mail-accounts"] as const,
  bookingLinks: (orgId: string) => ["crm", "org", orgId, "booking-links"] as const,
  recentMail: (contactId: string, connectionId: string) => ["crm", "record", "contact", contactId, "mail", connectionId] as const,
  drafts: (orgId: string) => ["crm", "org", orgId, "drafts"] as const,
  templates: (orgId: string) => ["crm", "org", orgId, "templates"] as const,
  meetings: (contactId: string) => ["crm", "record", "contact", contactId, "meetings"] as const,
  dashboards: (orgId: string) => ["crm", "org", orgId, "dashboards"] as const,
  dashboard: (id: string) => ["crm", "dashboard-reports", id] as const,
  report: (orgId: string, spec: CrmReportSpec) => ["crm", "org", orgId, "report-preview", spec] as const,
  forecast: (orgId: string, params: object) => ["crm", "org", orgId, "forecast", params] as const,
  goals: (orgId: string, at: string) => ["crm", "org", orgId, "goals", at] as const,
  scoreRules: (orgId: string) => ["crm", "org", orgId, "score-rules"] as const,
  workflows: (orgId: string) => ["crm", "org", orgId, "workflows"] as const,
  workflow: (orgId: string, id: string) => ["crm", "org", orgId, "workflows", id] as const,
  workflowRuns: (orgId: string, id: string) => ["crm", "org", orgId, "workflows", id, "runs"] as const,
  forms: (orgId: string) => ["crm", "org", orgId, "forms"] as const,
  form: (orgId: string, id: string) => ["crm", "org", orgId, "forms", id] as const,
  formSubmissions: (orgId: string, id: string) => ["crm", "org", orgId, "forms", id, "submissions"] as const,
  publicForm: (token: string) => ["crm", "public-form", token] as const,
};

function noClientRetry(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

// ---- products ---------------------------------------------------------------------

/** The product library (by name). */
export function useCrmProducts(orgId: string | null | undefined, q = "", archived = false): PagedQueryResult<CrmProduct> {
  const c = useEndpoints(crmDepthEndpoints);
  return usePagedQuery(crmDepthKeys.products(orgId ?? "", q, archived), (cursor, signal) => c.products(orgId ?? "", { q, archived, cursor }, { signal }),
    { placeholderData: keepPreviousData, retry: noClientRetry }, !!orgId);
}

export function useCrmSaveProduct(orgId: string): UseMutationResult<CrmProduct, ApiError, { id?: string; body: CrmProductRequest }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmProduct, ApiError, { id?: string; body: CrmProductRequest }>({
    mutationFn: ({ id, body }) => (id ? c.updateProduct(orgId, id, body) : c.createProduct(orgId, body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: crmDepthKeys.productsAll(orgId) }),
  });
}

export function useCrmDeleteProduct(orgId: string): UseMutationResult<void, ApiError, string> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => c.deleteProduct(orgId, id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: crmDepthKeys.productsAll(orgId) }),
  });
}

// ---- line items ------------------------------------------------------------------

/** A deal's line items with totals (view on the deal). */
export function useCrmLineItems(dealId: string | null | undefined): UseQueryResult<CrmLineItems, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.lineItems(dealId ?? ""), ({ signal }) => c.lineItems(dealId ?? "", { signal }), { retry: noClientRetry }, !!dealId);
}

type LineItemChange =
  | { op: "add"; body: CrmLineItemRequest }
  | { op: "update"; id: string; body: CrmLineItemRequest }
  | { op: "remove"; id: string }
  | { op: "reorder"; ids: string[] }
  | { op: "amount_source"; fromItems: boolean };

/** Add, change, remove or reorder a deal's lines, or make its amount follow them. */
export function useCrmChangeLineItems(dealId: string): UseMutationResult<CrmLineItem | CrmLineItems | void, ApiError, LineItemChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmLineItem | CrmLineItems | void, ApiError, LineItemChange>({
    mutationFn: (ch) => {
      switch (ch.op) {
        case "add":
          return c.addLineItem(dealId, ch.body);
        case "update":
          return c.updateLineItem(ch.id, ch.body);
        case "remove":
          return c.removeLineItem(ch.id);
        case "reorder":
          return c.reorderLineItems(dealId, ch.ids);
        default:
          return c.setAmountSource(dealId, ch.fromItems);
      }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: crmKeys.record("deal", dealId) });
      void qc.invalidateQueries({ queryKey: crmKeys.lists("deal") });
    },
  });
}

// ---- quotes ------------------------------------------------------------------------

export function useCrmQuotes(dealId: string | null | undefined): UseQueryResult<{ items: CrmQuoteSummary[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.quotes(dealId ?? ""), ({ signal }) => c.quotes(dealId ?? "", { signal }), { retry: noClientRetry }, !!dealId);
}

export function useCrmQuote(id: string | null | undefined): UseQueryResult<CrmQuote, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.quote(id ?? ""), ({ signal }) => c.quote(id ?? "", { signal }), { retry: noClientRetry }, !!id);
}

export function useCrmCreateQuote(dealId: string): UseMutationResult<CrmQuote, ApiError, CrmQuoteRequest | void> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmQuote, ApiError, CrmQuoteRequest | void>({
    mutationFn: (body) => c.createQuote(dealId, body ?? {}),
    onSuccess: (q) => {
      qc.setQueryData(crmDepthKeys.quote(q.id), q);
      void qc.invalidateQueries({ queryKey: crmKeys.record("deal", dealId) });
    },
  });
}

type QuoteChange =
  | { op: "update"; body: CrmQuoteRequest }
  | { op: "refresh" }
  | { op: "publish" }
  | { op: "unpublish" };

/** Edit a draft, copy the deal's lines again, publish (a person's action) or take back to a draft. */
export function useCrmChangeQuote(id: string): UseMutationResult<CrmQuote, ApiError, QuoteChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmQuote, ApiError, QuoteChange>({
    mutationFn: (ch) =>
      ch.op === "update" ? c.updateQuote(id, ch.body) : ch.op === "refresh" ? c.refreshQuote(id) : ch.op === "publish" ? c.publishQuote(id) : c.unpublishQuote(id),
    onSuccess: (q) => {
      qc.setQueryData(crmDepthKeys.quote(id), q);
      void qc.invalidateQueries({ queryKey: crmDepthKeys.quotes(q.deal.id) });
    },
  });
}

export function useCrmDeleteQuote(): UseMutationResult<void, ApiError, { id: string; dealId: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { id: string; dealId: string }>({
    mutationFn: ({ id }) => c.deleteQuote(id),
    onSuccess: (_, { id, dealId }) => {
      qc.removeQueries({ queryKey: crmDepthKeys.quote(id) });
      void qc.invalidateQueries({ queryKey: crmKeys.record("deal", dealId) });
    },
  });
}

/** The buyer's page (no account). */
export function useCrmPublicQuote(token: string | null | undefined): UseQueryResult<CrmPublicQuote, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.publicQuote(token ?? ""), ({ signal }) => c.publicQuote(token ?? "", { signal }), { retry: noClientRetry, staleTime: 60_000 }, !!token);
}

export function useCrmDecideQuote(token: string): UseMutationResult<CrmPublicQuote, ApiError, { accept: boolean; body: CrmQuoteDecision }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmPublicQuote, ApiError, { accept: boolean; body: CrmQuoteDecision }>({
    mutationFn: ({ accept, body }) => (accept ? c.acceptQuote(token, { ...body, accept: true }) : c.declineQuote(token, body)),
    onSuccess: (q) => qc.setQueryData(crmDepthKeys.publicQuote(token), q),
  });
}

// ---- tickets -----------------------------------------------------------------------

/** A ticket pipeline's board: per status the visible count and first cards. */
export function useCrmTicketBoard(orgId: string | null | undefined, pipelineId: string | undefined, params: CrmListParams = {}): UseQueryResult<CrmTicketBoard, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(
    crmDepthKeys.ticketBoard(orgId ?? "", pipelineId, params),
    ({ signal }) => c.ticketBoard(orgId ?? "", { ...params, pipeline_id: pipelineId, tz: crmTimeZone() }, { signal }),
    { retry: noClientRetry, placeholderData: keepPreviousData },
    !!orgId,
  );
}

export function useCrmMoveTicket(orgId: string): UseMutationResult<CrmTicket, ApiError, { ticketId: string; stageId: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmTicket, ApiError, { ticketId: string; stageId: string }>({
    mutationFn: ({ ticketId, stageId }) => c.moveTicketStage(ticketId, stageId),
    onSuccess: (t) => {
      void qc.invalidateQueries({ queryKey: ["crm", "board", orgId, "ticket"] });
      void qc.invalidateQueries({ queryKey: crmKeys.record("ticket", t.id) });
      void qc.invalidateQueries({ queryKey: crmKeys.lists("ticket") });
    },
  });
}

/** Link or unlink a contact on a ticket (company and deal change through useCrmUpdateRecord("ticket", id)). */
export function useCrmLinkTicketContact(ticketId: string): UseMutationResult<unknown, ApiError, { op: "add" | "remove"; contactId: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<unknown, ApiError, { op: "add" | "remove"; contactId: string }>({
    mutationFn: ({ op, contactId }) => (op === "add" ? c.addTicketContact(ticketId, contactId) : c.removeTicketContact(ticketId, contactId)),
    onSuccess: (_, { contactId }) => {
      void qc.invalidateQueries({ queryKey: crmKeys.record("ticket", ticketId) });
      void qc.invalidateQueries({ queryKey: crmKeys.record("contact", contactId) });
    },
  });
}

// ---- mail ----------------------------------------------------------------------------

/** Your connected mailboxes the CRM can send from or read, and mail capabilities you could still connect. */
export function useCrmMailAccounts(orgId: string | null | undefined): UseQueryResult<CrmMailAccounts, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.mailAccounts(orgId ?? ""), ({ signal }) => c.mailAccounts(orgId ?? "", { signal }), { retry: noClientRetry, staleTime: 60_000 }, !!orgId);
}

export function useCrmBookingLinks(orgId: string | null | undefined): UseQueryResult<{ available: boolean; items: CrmBookingLink[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.bookingLinks(orgId ?? ""), ({ signal }) => c.bookingLinks(orgId ?? "", { signal }), { retry: noClientRetry, staleTime: 300_000 }, !!orgId);
}

export function useCrmRenderMail(): UseMutationResult<CrmRendered, ApiError, { contact_id: string; deal_id?: string; ticket_id?: string; subject: string; body: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  return useMutation({ mutationFn: (body) => c.render(body) });
}

/** Send from your mailbox; the email lands on the contact's timeline. */
export function useCrmSendMail(contactId: string): UseMutationResult<CrmActivity, ApiError, CrmSendMailRequest> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmActivity, ApiError, CrmSendMailRequest>({
    mutationFn: (body) => c.sendMail(contactId, body),
    onSuccess: (a) => {
      for (const r of a.records) void qc.invalidateQueries({ queryKey: crmKeys.timeline(r.type, r.id) });
      void qc.invalidateQueries({ queryKey: ["crm", "org", a.org_id, "drafts"] });
    },
  });
}

/** The latest mail with a contact, read live from one of your mailboxes (not stored). */
export function useCrmRecentMail(contactId: string | null | undefined, connectionId: string | null | undefined): UseQueryResult<{ items: CrmMailSummary[]; account?: string }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(
    crmDepthKeys.recentMail(contactId ?? "", connectionId ?? ""),
    ({ signal }) => c.recentMail(contactId ?? "", connectionId ?? "", { signal }),
    { retry: noClientRetry, staleTime: 30_000 },
    !!contactId && !!connectionId,
  );
}

/** Keep one message from your mailbox on the timeline. */
export function useCrmLogMail(contactId: string): UseMutationResult<CrmActivity, ApiError, { connection_id: string; message_id: string; deal_id?: string; ticket_id?: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => c.logMail(contactId, body),
    onSuccess: (a, v) => {
      for (const r of a.records) void qc.invalidateQueries({ queryKey: crmKeys.timeline(r.type, r.id) });
      void qc.invalidateQueries({ queryKey: crmDepthKeys.recentMail(contactId, v.connection_id) });
    },
  });
}

export function useCrmDrafts(orgId: string | null | undefined): UseQueryResult<{ items: CrmEmailDraft[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.drafts(orgId ?? ""), ({ signal }) => c.drafts(orgId ?? "", undefined, { signal }), { retry: noClientRetry }, !!orgId);
}

type DraftChange = { op: "create"; contactId: string; body: CrmEmailDraftRequest } | { op: "update"; id: string; body: CrmEmailDraftRequest } | { op: "delete"; id: string };

export function useCrmChangeDraft(orgId: string): UseMutationResult<CrmEmailDraft | void, ApiError, DraftChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmEmailDraft | void, ApiError, DraftChange>({
    mutationFn: (ch) => (ch.op === "create" ? c.createDraft(ch.contactId, ch.body) : ch.op === "update" ? c.updateDraft(ch.id, ch.body) : c.deleteDraft(ch.id)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: crmDepthKeys.drafts(orgId) }),
  });
}

export function useCrmTemplates(orgId: string | null | undefined): UseQueryResult<{ items: CrmEmailTemplate[]; merge_fields: CrmMergeField[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.templates(orgId ?? ""), ({ signal }) => c.templates(orgId ?? "", { signal }), { retry: noClientRetry, staleTime: 60_000 }, !!orgId);
}

type TemplateChange = { op: "save"; id?: string; body: CrmEmailTemplateRequest } | { op: "delete"; id: string };

export function useCrmChangeTemplate(orgId: string): UseMutationResult<CrmEmailTemplate | void, ApiError, TemplateChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmEmailTemplate | void, ApiError, TemplateChange>({
    mutationFn: (ch) => (ch.op === "delete" ? c.deleteTemplate(orgId, ch.id) : ch.id ? c.updateTemplate(orgId, ch.id, ch.body) : c.createTemplate(orgId, ch.body)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: crmDepthKeys.templates(orgId) }),
  });
}

/** Events on your calendars with the contact (available: false until the product wires a calendar). */
export function useCrmMeetings(contactId: string | null | undefined, enabled = true): UseQueryResult<{ available: boolean; items: CrmMeetingCandidate[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.meetings(contactId ?? ""), ({ signal }) => c.meetings(contactId ?? "", { signal }), { retry: noClientRetry }, !!contactId && enabled);
}

export function useCrmLogMeeting(contactId: string): UseMutationResult<CrmActivity, ApiError, { event_id: string; body?: string; outcome?: string; deal_id?: string; ticket_id?: string }> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body) => c.logMeeting(contactId, body),
    onSuccess: (a) => {
      for (const r of a.records) void qc.invalidateQueries({ queryKey: crmKeys.timeline(r.type, r.id) });
    },
  });
}

// ---- reports, dashboards, forecast, goals ----------------------------------------------------

export function useCrmDashboards(orgId: string | null | undefined): UseQueryResult<{ items: CrmReportDashboard[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.dashboards(orgId ?? ""), ({ signal }) => c.dashboards(orgId ?? "", { signal }), { retry: noClientRetry }, !!orgId);
}

/** A dashboard with every report computed over the records you can see. */
export function useCrmReportDashboard(id: string | null | undefined): UseQueryResult<CrmReportDashboard, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.dashboard(id ?? ""), ({ signal }) => c.dashboard(id ?? "", crmTimeZone(), { signal }), { retry: noClientRetry }, !!id);
}

/** The builder's live preview. */
export function useCrmRunReport(orgId: string | null | undefined, spec: CrmReportSpec | null): UseQueryResult<CrmReportResult, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(
    crmDepthKeys.report(orgId ?? "", spec ?? ({} as CrmReportSpec)),
    ({ signal }) => c.runReport(orgId ?? "", spec as CrmReportSpec, crmTimeZone(), { signal }),
    { retry: noClientRetry, placeholderData: keepPreviousData },
    !!orgId && !!spec,
  );
}

type DashboardChange =
  | { op: "create"; name: string }
  | { op: "rename"; id: string; name: string }
  | { op: "delete"; id: string }
  | { op: "add_report"; id: string; name: string; spec: CrmReportSpec }
  | { op: "update_report"; id: string; reportId: string; name?: string; position?: number; spec?: CrmReportSpec }
  | { op: "delete_report"; id: string; reportId: string };

export function useCrmChangeDashboard(orgId: string): UseMutationResult<CrmReportDashboard | CrmReport | void, ApiError, DashboardChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmReportDashboard | CrmReport | void, ApiError, DashboardChange>({
    mutationFn: (ch) => {
      switch (ch.op) {
        case "create":
          return c.createDashboard(orgId, ch.name);
        case "rename":
          return c.renameDashboard(ch.id, ch.name);
        case "delete":
          return c.deleteDashboard(ch.id);
        case "add_report":
          return c.createReport(ch.id, { name: ch.name, spec: ch.spec });
        case "update_report":
          return c.updateReport(ch.id, ch.reportId, { name: ch.name, position: ch.position, spec: ch.spec });
        default:
          return c.deleteReport(ch.id, ch.reportId);
      }
    },
    onSuccess: (_, ch) => {
      void qc.invalidateQueries({ queryKey: crmDepthKeys.dashboards(orgId) });
      if (ch.op !== "create") void qc.invalidateQueries({ queryKey: crmDepthKeys.dashboard(ch.id) });
    },
  });
}

export function useCrmForecast(orgId: string | null | undefined, params: { from?: string; months?: number; pipeline_id?: string; currency?: string; owner?: string }): UseQueryResult<CrmForecast, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.forecast(orgId ?? "", params), ({ signal }) => c.forecast(orgId ?? "", params, { signal }),
    { retry: noClientRetry, placeholderData: keepPreviousData }, !!orgId);
}

export function useCrmGoals(orgId: string | null | undefined, at = ""): UseQueryResult<{ items: CrmGoal[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.goals(orgId ?? "", at), ({ signal }) => c.goals(orgId ?? "", at || undefined, { signal }), { retry: noClientRetry }, !!orgId);
}

type GoalChange = { op: "create"; body: CrmGoalRequest } | { op: "update"; id: string; target: number } | { op: "delete"; id: string };

export function useCrmChangeGoal(orgId: string): UseMutationResult<CrmGoal | void, ApiError, GoalChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmGoal | void, ApiError, GoalChange>({
    mutationFn: (ch) => (ch.op === "create" ? c.createGoal(orgId, ch.body) : ch.op === "update" ? c.updateGoal(orgId, ch.id, ch.target) : c.deleteGoal(orgId, ch.id)),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["crm", "org", orgId, "goals"] }),
  });
}

// ---- scoring -------------------------------------------------------------------------

export function useCrmScoreRules(orgId: string | null | undefined): UseQueryResult<{ items: CrmScoreRule[] }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.scoreRules(orgId ?? ""), ({ signal }) => c.scoreRules(orgId ?? "", { signal }), { retry: noClientRetry }, !!orgId);
}

type ScoreRuleChange = { op: "save"; id?: string; body: CrmScoreRuleRequest } | { op: "delete"; id: string };

export function useCrmChangeScoreRule(orgId: string): UseMutationResult<{ items: CrmScoreRule[] } | void, ApiError, ScoreRuleChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<{ items: CrmScoreRule[] } | void, ApiError, ScoreRuleChange>({
    mutationFn: (ch) => (ch.op === "delete" ? c.deleteScoreRule(orgId, ch.id) : ch.id ? c.updateScoreRule(orgId, ch.id, ch.body) : c.createScoreRule(orgId, ch.body)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: crmDepthKeys.scoreRules(orgId) });
      void qc.invalidateQueries({ queryKey: crmKeys.lists("contact") });
    },
  });
}

// ---- workflows ------------------------------------------------------------------------

export function useCrmWorkflows(orgId: string | null | undefined): UseQueryResult<{ items: CrmWorkflow[]; can_manage: boolean }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.workflows(orgId ?? ""), ({ signal }) => c.workflows(orgId ?? "", { signal }), { retry: noClientRetry }, !!orgId);
}

export function useCrmWorkflow(orgId: string | null | undefined, id: string | null | undefined): UseQueryResult<CrmWorkflow, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.workflow(orgId ?? "", id ?? ""), ({ signal }) => c.workflow(orgId ?? "", id ?? "", { signal }), { retry: noClientRetry }, !!orgId && !!id);
}

export function useCrmWorkflowRuns(orgId: string | null | undefined, id: string | null | undefined): PagedQueryResult<CrmWorkflowRun> {
  const c = useEndpoints(crmDepthEndpoints);
  return usePagedQuery(crmDepthKeys.workflowRuns(orgId ?? "", id ?? ""), (cursor, signal) => c.workflowRuns(orgId ?? "", id ?? "", cursor, { signal }),
    { retry: noClientRetry, refetchInterval: 15_000 }, !!orgId && !!id);
}

type WorkflowChange = { op: "save"; id?: string; body: CrmWorkflowRequest } | { op: "delete"; id: string };

export function useCrmChangeWorkflow(orgId: string): UseMutationResult<CrmWorkflow | void, ApiError, WorkflowChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmWorkflow | void, ApiError, WorkflowChange>({
    mutationFn: (ch) => (ch.op === "delete" ? c.deleteWorkflow(orgId, ch.id) : ch.id ? c.updateWorkflow(orgId, ch.id, ch.body) : c.createWorkflow(orgId, ch.body)),
    onSuccess: (wf) => {
      if (wf) qc.setQueryData(crmDepthKeys.workflow(orgId, wf.id), wf);
      void qc.invalidateQueries({ queryKey: crmDepthKeys.workflows(orgId), exact: true });
    },
  });
}

// ---- forms -------------------------------------------------------------------------------

export function useCrmForms(orgId: string | null | undefined): UseQueryResult<{ items: CrmForm[]; can_manage: boolean }, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.forms(orgId ?? ""), ({ signal }) => c.forms(orgId ?? "", { signal }), { retry: noClientRetry }, !!orgId);
}

export function useCrmForm(orgId: string | null | undefined, id: string | null | undefined): UseQueryResult<CrmForm, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.form(orgId ?? "", id ?? ""), ({ signal }) => c.form(orgId ?? "", id ?? "", { signal }), { retry: noClientRetry }, !!orgId && !!id);
}

export function useCrmFormSubmissions(orgId: string | null | undefined, id: string | null | undefined): PagedQueryResult<CrmFormSubmission> {
  const c = useEndpoints(crmDepthEndpoints);
  return usePagedQuery(crmDepthKeys.formSubmissions(orgId ?? "", id ?? ""), (cursor, signal) => c.formSubmissions(orgId ?? "", id ?? "", cursor, { signal }),
    { retry: noClientRetry }, !!orgId && !!id);
}

type FormChange = { op: "save"; id?: string; body: CrmFormRequest } | { op: "publish"; id: string; publish: boolean } | { op: "delete"; id: string };

export function useCrmChangeForm(orgId: string): UseMutationResult<CrmForm | void, ApiError, FormChange> {
  const c = useEndpoints(crmDepthEndpoints);
  const qc = useQueryClient();
  return useMutation<CrmForm | void, ApiError, FormChange>({
    mutationFn: (ch) =>
      ch.op === "delete" ? c.deleteForm(orgId, ch.id) : ch.op === "publish" ? c.publishForm(orgId, ch.id, ch.publish) : ch.id ? c.updateForm(orgId, ch.id, ch.body) : c.createForm(orgId, ch.body),
    onSuccess: (f) => {
      if (f) qc.setQueryData(crmDepthKeys.form(orgId, f.id), f);
      void qc.invalidateQueries({ queryKey: crmDepthKeys.forms(orgId), exact: true });
    },
  });
}

/** A lead form as anyone with the link sees it (no account). */
export function useCrmPublicForm(token: string | null | undefined): UseQueryResult<CrmPublicForm, ApiError> {
  const c = useEndpoints(crmDepthEndpoints);
  return useApiQuery(crmDepthKeys.publicForm(token ?? ""), ({ signal }) => c.publicForm(token ?? "", { signal }), { retry: noClientRetry, staleTime: 300_000 }, !!token);
}

export function useCrmSubmitForm(token: string): UseMutationResult<{ ok: boolean; message: string }, ApiError, CrmFormSubmit> {
  const c = useEndpoints(crmDepthEndpoints);
  return useMutation({ mutationFn: (body) => c.submitForm(token, body) });
}

// ---- realtime ------------------------------------------------------------------------------

registerRealtimeHandler("crm.record_changed", (qc, e) => {
  if (e.data.type === "deal") void qc.invalidateQueries({ queryKey: ["crm", "quote"] });
});
registerRealtimeHandler("crm.board_changed", (qc) => {
  void qc.invalidateQueries({ queryKey: ["crm", "dashboard-reports"] });
});
registerRealtimeHandler("crm.settings_changed", (qc, e) => {
  if (e.data.what === "scores") void qc.invalidateQueries({ queryKey: crmKeys.lists("contact") });
});
