import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import { crmListQuery } from "./api";
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

/** CRM 1.3 endpoints: products, line items, quotes (+ public page), tickets board, mail, meetings, reports, forecast, goals, scoring, workflows, forms (+ public form). */
export function crmDepthEndpoints(http: HttpClient) {
  const C = "/v1/crm";
  const org = (orgId: string) => `${C}/orgs/${seg(orgId)}`;
  return {
    // ---- products
    products: (orgId: string, params: { q?: string; archived?: boolean; cursor?: string | undefined } = {}, o?: CallOptions) =>
      http.get<Page<CrmProduct>>(`${org(orgId)}/products`, { ...o, query: { q: params.q || undefined, archived: params.archived ? "true" : undefined, cursor: params.cursor, limit: 100 } }),
    createProduct: (orgId: string, body: CrmProductRequest, o?: CallOptions) => http.post<CrmProduct>(`${org(orgId)}/products`, body, o),
    updateProduct: (orgId: string, id: string, body: CrmProductRequest, o?: CallOptions) => http.patch<CrmProduct>(`${org(orgId)}/products/${seg(id)}`, body, o),
    deleteProduct: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/products/${seg(id)}`, o),

    // ---- line items
    lineItems: (dealId: string, o?: CallOptions) => http.get<CrmLineItems>(`${C}/deals/${seg(dealId)}/line-items`, o),
    addLineItem: (dealId: string, body: CrmLineItemRequest, o?: CallOptions) => http.post<CrmLineItem>(`${C}/deals/${seg(dealId)}/line-items`, body, o),
    updateLineItem: (itemId: string, body: CrmLineItemRequest, o?: CallOptions) => http.patch<CrmLineItem>(`${C}/line-items/${seg(itemId)}`, body, o),
    removeLineItem: (itemId: string, o?: CallOptions) => http.delete<void>(`${C}/line-items/${seg(itemId)}`, o),
    reorderLineItems: (dealId: string, ids: string[], o?: CallOptions) => http.put<CrmLineItems>(`${C}/deals/${seg(dealId)}/line-items/order`, { ids }, o),
    setAmountSource: (dealId: string, fromItems: boolean, o?: CallOptions) =>
      http.put<CrmLineItems>(`${C}/deals/${seg(dealId)}/amount-source`, { from_items: fromItems }, o),

    // ---- quotes
    quotes: (dealId: string, o?: CallOptions) => http.get<{ items: CrmQuoteSummary[] }>(`${C}/deals/${seg(dealId)}/quotes`, o),
    createQuote: (dealId: string, body: CrmQuoteRequest = {}, o?: CallOptions) => http.post<CrmQuote>(`${C}/deals/${seg(dealId)}/quotes`, body, o),
    quote: (id: string, o?: CallOptions) => http.get<CrmQuote>(`${C}/quotes/${seg(id)}`, o),
    updateQuote: (id: string, body: CrmQuoteRequest, o?: CallOptions) => http.patch<CrmQuote>(`${C}/quotes/${seg(id)}`, body, o),
    refreshQuote: (id: string, o?: CallOptions) => http.post<CrmQuote>(`${C}/quotes/${seg(id)}/refresh`, {}, o),
    publishQuote: (id: string, o?: CallOptions) => http.post<CrmQuote>(`${C}/quotes/${seg(id)}/publish`, {}, o),
    unpublishQuote: (id: string, o?: CallOptions) => http.post<CrmQuote>(`${C}/quotes/${seg(id)}/unpublish`, {}, o),
    deleteQuote: (id: string, o?: CallOptions) => http.delete<void>(`${C}/quotes/${seg(id)}`, o),
    /** No account: the buyer's page. 410 quote_expired past its expiry; 404 link_not_found otherwise. */
    publicQuote: (token: string, o?: CallOptions) => http.get<CrmPublicQuote>(`${C}/public/quotes/${seg(token)}`, { ...o, auth: false }),
    acceptQuote: (token: string, body: CrmQuoteDecision, o?: CallOptions) =>
      http.post<CrmPublicQuote>(`${C}/public/quotes/${seg(token)}/accept`, body, { ...o, auth: false }),
    declineQuote: (token: string, body: CrmQuoteDecision, o?: CallOptions) =>
      http.post<CrmPublicQuote>(`${C}/public/quotes/${seg(token)}/decline`, body, { ...o, auth: false }),

    // ---- tickets
    ticketBoard: (orgId: string, params: CrmListParams & { pipeline_id?: string | undefined } = {}, o?: CallOptions) =>
      http.get<CrmTicketBoard>(`${org(orgId)}/board`, { ...o, query: { ...crmListQuery(params), object: "ticket", pipeline_id: params.pipeline_id } }),
    moveTicketStage: (ticketId: string, stageId: string, o?: CallOptions) => http.post<CrmTicket>(`${C}/tickets/${seg(ticketId)}/stage`, { stage_id: stageId }, o),
    addTicketContact: (ticketId: string, contactId: string, o?: CallOptions) => http.post<unknown>(`${C}/tickets/${seg(ticketId)}/contacts`, { contact_id: contactId }, o),
    removeTicketContact: (ticketId: string, contactId: string, o?: CallOptions) => http.delete<unknown>(`${C}/tickets/${seg(ticketId)}/contacts/${seg(contactId)}`, o),

    // ---- mail
    mailAccounts: (orgId: string, o?: CallOptions) => http.get<CrmMailAccounts>(`${C}/mail/accounts`, { ...o, query: { org_id: orgId } }),
    bookingLinks: (orgId: string, o?: CallOptions) =>
      http.get<{ available: boolean; items: CrmBookingLink[] }>(`${C}/mail/booking-links`, { ...o, query: { org_id: orgId } }),
    render: (body: { contact_id: string; deal_id?: string; ticket_id?: string; subject: string; body: string }, o?: CallOptions) =>
      http.post<CrmRendered>(`${C}/mail/render`, body, o),
    sendMail: (contactId: string, body: CrmSendMailRequest, o?: CallOptions) => http.post<CrmActivity>(`${C}/contacts/${seg(contactId)}/mail`, body, o),
    recentMail: (contactId: string, connectionId: string, o?: CallOptions) =>
      http.get<{ items: CrmMailSummary[]; account?: string }>(`${C}/contacts/${seg(contactId)}/mail/recent`, { ...o, query: { connection_id: connectionId } }),
    logMail: (contactId: string, body: { connection_id: string; message_id: string; deal_id?: string; ticket_id?: string }, o?: CallOptions) =>
      http.post<CrmActivity>(`${C}/contacts/${seg(contactId)}/mail/log`, body, o),
    drafts: (orgId: string, contactId?: string, o?: CallOptions) =>
      http.get<{ items: CrmEmailDraft[] }>(`${org(orgId)}/mail/drafts`, { ...o, query: { contact_id: contactId } }),
    createDraft: (contactId: string, body: CrmEmailDraftRequest, o?: CallOptions) => http.post<CrmEmailDraft>(`${C}/contacts/${seg(contactId)}/mail/drafts`, body, o),
    updateDraft: (id: string, body: CrmEmailDraftRequest, o?: CallOptions) => http.patch<CrmEmailDraft>(`${C}/mail/drafts/${seg(id)}`, body, o),
    deleteDraft: (id: string, o?: CallOptions) => http.delete<void>(`${C}/mail/drafts/${seg(id)}`, o),
    templates: (orgId: string, o?: CallOptions) => http.get<{ items: CrmEmailTemplate[]; merge_fields: CrmMergeField[] }>(`${org(orgId)}/mail/templates`, o),
    createTemplate: (orgId: string, body: CrmEmailTemplateRequest, o?: CallOptions) => http.post<CrmEmailTemplate>(`${org(orgId)}/mail/templates`, body, o),
    updateTemplate: (orgId: string, id: string, body: CrmEmailTemplateRequest, o?: CallOptions) =>
      http.patch<CrmEmailTemplate>(`${org(orgId)}/mail/templates/${seg(id)}`, body, o),
    deleteTemplate: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/mail/templates/${seg(id)}`, o),
    meetings: (contactId: string, o?: CallOptions) =>
      http.get<{ available: boolean; items: CrmMeetingCandidate[] }>(`${C}/contacts/${seg(contactId)}/meetings`, o),
    logMeeting: (contactId: string, body: { event_id: string; body?: string; outcome?: string; deal_id?: string; ticket_id?: string }, o?: CallOptions) =>
      http.post<CrmActivity>(`${C}/contacts/${seg(contactId)}/meetings/log`, body, o),

    // ---- reports, dashboards, forecast, goals
    runReport: (orgId: string, spec: CrmReportSpec, tz?: string, o?: CallOptions) => http.post<CrmReportResult>(`${org(orgId)}/reports/run`, spec, { ...o, query: { tz } }),
    dashboards: (orgId: string, o?: CallOptions) => http.get<{ items: CrmReportDashboard[] }>(`${org(orgId)}/dashboards`, o),
    createDashboard: (orgId: string, name: string, o?: CallOptions) => http.post<CrmReportDashboard>(`${org(orgId)}/dashboards`, { name }, o),
    dashboard: (id: string, tz?: string, o?: CallOptions) => http.get<CrmReportDashboard>(`${C}/dashboards/${seg(id)}`, { ...o, query: { tz } }),
    renameDashboard: (id: string, name: string, o?: CallOptions) => http.patch<CrmReportDashboard>(`${C}/dashboards/${seg(id)}`, { name }, o),
    deleteDashboard: (id: string, o?: CallOptions) => http.delete<void>(`${C}/dashboards/${seg(id)}`, o),
    createReport: (dashboardId: string, body: { name: string; spec: CrmReportSpec }, o?: CallOptions) =>
      http.post<CrmReport>(`${C}/dashboards/${seg(dashboardId)}/reports`, body, o),
    updateReport: (dashboardId: string, id: string, body: { name?: string; position?: number; spec?: CrmReportSpec }, o?: CallOptions) =>
      http.patch<CrmReport>(`${C}/dashboards/${seg(dashboardId)}/reports/${seg(id)}`, body, o),
    deleteReport: (dashboardId: string, id: string, o?: CallOptions) => http.delete<void>(`${C}/dashboards/${seg(dashboardId)}/reports/${seg(id)}`, o),
    forecast: (orgId: string, params: { from?: string; months?: number; pipeline_id?: string; currency?: string; owner?: string } = {}, o?: CallOptions) =>
      http.get<CrmForecast>(`${org(orgId)}/forecast`, { ...o, query: params }),
    goals: (orgId: string, at?: string, o?: CallOptions) => http.get<{ items: CrmGoal[] }>(`${org(orgId)}/goals`, { ...o, query: { at } }),
    createGoal: (orgId: string, body: CrmGoalRequest, o?: CallOptions) => http.post<CrmGoal>(`${org(orgId)}/goals`, body, o),
    updateGoal: (orgId: string, id: string, target: number, o?: CallOptions) => http.patch<CrmGoal>(`${org(orgId)}/goals/${seg(id)}`, { target }, o),
    deleteGoal: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/goals/${seg(id)}`, o),

    // ---- scoring
    scoreRules: (orgId: string, o?: CallOptions) => http.get<{ items: CrmScoreRule[] }>(`${org(orgId)}/score-rules`, o),
    createScoreRule: (orgId: string, body: CrmScoreRuleRequest, o?: CallOptions) => http.post<{ items: CrmScoreRule[] }>(`${org(orgId)}/score-rules`, body, o),
    updateScoreRule: (orgId: string, id: string, body: CrmScoreRuleRequest, o?: CallOptions) =>
      http.patch<{ items: CrmScoreRule[] }>(`${org(orgId)}/score-rules/${seg(id)}`, body, o),
    deleteScoreRule: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/score-rules/${seg(id)}`, o),

    // ---- workflows
    workflows: (orgId: string, o?: CallOptions) => http.get<{ items: CrmWorkflow[]; can_manage: boolean }>(`${org(orgId)}/workflows`, o),
    workflow: (orgId: string, id: string, o?: CallOptions) => http.get<CrmWorkflow>(`${org(orgId)}/workflows/${seg(id)}`, o),
    createWorkflow: (orgId: string, body: CrmWorkflowRequest, o?: CallOptions) => http.post<CrmWorkflow>(`${org(orgId)}/workflows`, body, o),
    updateWorkflow: (orgId: string, id: string, body: CrmWorkflowRequest, o?: CallOptions) => http.patch<CrmWorkflow>(`${org(orgId)}/workflows/${seg(id)}`, body, o),
    deleteWorkflow: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/workflows/${seg(id)}`, o),
    workflowRuns: (orgId: string, id: string, cursor?: string, o?: CallOptions) =>
      http.get<Page<CrmWorkflowRun>>(`${org(orgId)}/workflows/${seg(id)}/runs`, { ...o, query: { cursor } }),

    // ---- forms
    forms: (orgId: string, o?: CallOptions) => http.get<{ items: CrmForm[]; can_manage: boolean }>(`${org(orgId)}/forms`, o),
    form: (orgId: string, id: string, o?: CallOptions) => http.get<CrmForm>(`${org(orgId)}/forms/${seg(id)}`, o),
    createForm: (orgId: string, body: CrmFormRequest, o?: CallOptions) => http.post<CrmForm>(`${org(orgId)}/forms`, body, o),
    updateForm: (orgId: string, id: string, body: CrmFormRequest, o?: CallOptions) => http.patch<CrmForm>(`${org(orgId)}/forms/${seg(id)}`, body, o),
    deleteForm: (orgId: string, id: string, o?: CallOptions) => http.delete<void>(`${org(orgId)}/forms/${seg(id)}`, o),
    publishForm: (orgId: string, id: string, publish: boolean, o?: CallOptions) =>
      http.post<CrmForm>(`${org(orgId)}/forms/${seg(id)}/${publish ? "publish" : "unpublish"}`, {}, o),
    formSubmissions: (orgId: string, id: string, cursor?: string, o?: CallOptions) =>
      http.get<Page<CrmFormSubmission>>(`${org(orgId)}/forms/${seg(id)}/submissions`, { ...o, query: { cursor } }),
    /** No account. */
    publicForm: (token: string, o?: CallOptions) => http.get<CrmPublicForm>(`${C}/public/forms/${seg(token)}`, { ...o, auth: false }),
    submitForm: (token: string, body: CrmFormSubmit, o?: CallOptions) =>
      http.post<{ ok: boolean; message: string }>(`${C}/public/forms/${seg(token)}`, body, { ...o, auth: false }),
  };
}
