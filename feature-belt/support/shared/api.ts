import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminReplyRequest,
  AdminSupportTicket,
  AdminSupportTicketDetail,
  AdminTicketListParams,
  AdminUpdateTicketRequest,
  CreateTicketRequest,
  MyTicketListParams,
  ReplyTicketRequest,
  SupportStaffRef,
  SupportStats,
  SupportTicket,
  SupportTicketDetail,
} from "./types";

/** Support endpoints: the caller's tickets, and the staff queue. */
export function supportEndpoints(http: HttpClient) {
  const T = "/v1/support/tickets";
  const A = "/v1/admin/support";
  return {
    /** `GET /v1/support/tickets`, newest first. */
    tickets: (params: MyTicketListParams = {}, o?: CallOptions) =>
      http.get<Page<SupportTicket>>(T, { ...o, query: { status: params.status, limit: params.limit, cursor: params.cursor } }),
    /** `POST /v1/support/tickets` (consent-exempt, 5 an hour) → 201 with the thread. */
    create: (body: CreateTicketRequest, o?: CallOptions) => http.post<SupportTicketDetail>(T, body, o),
    /** `GET /v1/support/tickets/{id}` with the thread (no staff notes). */
    ticket: (id: string, o?: CallOptions) => http.get<SupportTicketDetail>(`${T}/${seg(id)}`, o),
    /** `POST /v1/support/tickets/{id}/messages`: reply; an answered or closed ticket reopens. */
    reply: (id: string, body: ReplyTicketRequest, o?: CallOptions) => http.post<SupportTicketDetail>(`${T}/${seg(id)}/messages`, body, o),
    /** `POST /v1/support/tickets/{id}/close` (idempotent). */
    close: (id: string, o?: CallOptions) => http.post<SupportTicketDetail>(`${T}/${seg(id)}/close`, undefined, o),
    /** `POST /v1/support/tickets/{id}/reopen` (idempotent). */
    reopen: (id: string, o?: CallOptions) => http.post<SupportTicketDetail>(`${T}/${seg(id)}/reopen`, undefined, o),
    admin: {
      /** `GET /v1/admin/support/tickets`: the queue with filters. */
      tickets: (params: AdminTicketListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminSupportTicket>>(`${A}/tickets`, {
          ...o,
          query: {
            status: params.status,
            category: params.category,
            assignee: params.assignee,
            user_id: params.user_id,
            q: params.q,
            sort: params.sort === "oldest" ? "oldest" : undefined,
            limit: params.limit,
            cursor: params.cursor,
          },
        }),
      /** `GET /v1/admin/support/tickets/{id}`: owner, assignee, full thread with notes. */
      ticket: (id: string, o?: CallOptions) => http.get<AdminSupportTicketDetail>(`${A}/tickets/${seg(id)}`, o),
      /** `POST /v1/admin/support/tickets/{id}/messages`: reply (emails and notifies the user) or note. Audited. */
      reply: (id: string, body: AdminReplyRequest, o?: CallOptions) => http.post<AdminSupportTicketDetail>(`${A}/tickets/${seg(id)}/messages`, body, o),
      /** `PATCH /v1/admin/support/tickets/{id}`: status and/or assignee. Audited. */
      update: (id: string, body: AdminUpdateTicketRequest, o?: CallOptions) => http.patch<AdminSupportTicketDetail>(`${A}/tickets/${seg(id)}`, body, o),
      /** `GET /v1/admin/support/stats`. */
      stats: (o?: CallOptions) => http.get<SupportStats>(`${A}/stats`, o),
      /** `GET /v1/admin/support/assignees`: active staff. */
      assignees: (o?: CallOptions) => http.get<{ items: SupportStaffRef[] }>(`${A}/assignees`, o),
    },
  };
}

export type SupportEndpoints = ReturnType<typeof supportEndpoints>;
