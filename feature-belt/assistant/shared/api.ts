import type { CallOptions, HttpClient } from "../api/http";
import type { Page } from "../types";
import type {
  AssistantConsent,
  AssistantConversation,
  AssistantConversationListParams,
  AssistantDailyTotals,
  AssistantMessage,
  AssistantStatus,
} from "./types";

const conv = (id: string) => `/v1/assistant/conversations/${encodeURIComponent(id)}`;

/**
 * Assistant endpoints. Sending a message streams (server-sent events), so it lives in
 * `streamAssistant` (stream.ts), not here.
 */
export const assistantEndpoints = (http: HttpClient) => ({
  status: (o?: CallOptions) => http.get<AssistantStatus>("/v1/assistant/status", o),
  /** `provider` must be the name the consent dialog showed (`status.provider.name`). */
  acceptConsent: (provider: string) => http.post<AssistantConsent>("/v1/assistant/consent", { provider }),
  withdrawConsent: () => http.delete<void>("/v1/assistant/consent"),

  conversations: (params: AssistantConversationListParams = {}, o?: CallOptions) =>
    http.get<Page<AssistantConversation>>("/v1/assistant/conversations", { ...o, query: { q: params.q, cursor: params.cursor, limit: params.limit } }),
  createConversation: (body: { title?: string } = {}) => http.post<AssistantConversation>("/v1/assistant/conversations", body),
  conversation: (id: string, o?: CallOptions) => http.get<AssistantConversation>(conv(id), o),
  renameConversation: (id: string, title: string) => http.patch<AssistantConversation>(conv(id), { title }),
  deleteConversation: (id: string) => http.delete<void>(conv(id)),
  /** Newest first. */
  messages: (id: string, params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
    http.get<Page<AssistantMessage>>(`${conv(id)}/messages`, { ...o, query: { cursor: params.cursor, limit: params.limit } }),
  stop: (id: string) => http.post<void>(`${conv(id)}/stop`),

  admin: {
    usage: (days?: number, o?: CallOptions) => http.get<Page<AssistantDailyTotals>>("/v1/admin/assistant/usage", { ...o, query: { days } }),
  },
});

export type AssistantEndpoints = ReturnType<typeof assistantEndpoints>;
