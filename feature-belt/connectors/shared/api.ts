import type { CallOptions, HttpClient } from "../api/http";
import type { Page } from "../types";
import type {
  AdminConnector,
  AdminConnectorInput,
  Connector,
  ConnectorCheckResult,
  ConnectorConnectStart,
  ConnectorCustomRequest,
  ConnectorDisconnected,
  ConnectorList,
  ConnectorToolList,
} from "./types";

const conn = (id: string) => `/v1/connectors/connections/${encodeURIComponent(id)}`;

/** Connectors: the catalog with your connections, the connect flow, per-connection settings, and the admin catalog. */
export const connectorsEndpoints = (http: HttpClient) => ({
  list: (o?: CallOptions) => http.get<ConnectorList>("/v1/connectors", o),
  /** OAuth: `{client}` → `authorize_url`. Pasted token: `{token}` → `connector`. */
  connect: (id: string, body: { client?: "web" | "mobile"; token?: string }) =>
    http.post<ConnectorConnectStart>(`/v1/connectors/${encodeURIComponent(id)}/connect`, body),
  /** The `finish` secret from the return URL; only the account that started the flow can finish it. */
  finish: (secret: string) => http.post<Connector>("/v1/connectors/oauth/finish", { secret }),
  connection: (id: string, o?: CallOptions) => http.get<Connector>(conn(id), o),
  setWrites: (id: string, writesEnabled: boolean) => http.patch<Connector>(conn(id), { writes_enabled: writesEnabled }),
  setToolEnabled: (id: string, tool: string, enabled: boolean) =>
    http.put<Connector>(`${conn(id)}/tools/${encodeURIComponent(tool)}`, { enabled }),
  tools: (id: string, refresh = false, o?: CallOptions) =>
    http.get<ConnectorToolList>(`${conn(id)}/tools`, { ...o, query: { refresh: refresh ? "true" : undefined } }),
  disconnect: (id: string) => http.delete<ConnectorDisconnected>(conn(id)),
  createCustom: (body: ConnectorCustomRequest) => http.post<Connector>("/v1/connectors/custom", body),
  deleteCustom: (id: string) => http.delete<void>(`/v1/connectors/custom/${encodeURIComponent(id)}`),
  admin: {
    list: (params: { cursor?: string; limit?: number } = {}, o?: CallOptions) =>
      http.get<Page<AdminConnector>>("/v1/admin/connectors", { ...o, query: { cursor: params.cursor, limit: params.limit } }),
    get: (id: string, o?: CallOptions) => http.get<AdminConnector>(`/v1/admin/connectors/${encodeURIComponent(id)}`, o),
    create: (body: AdminConnectorInput) => http.post<AdminConnector>("/v1/admin/connectors", body),
    update: (id: string, body: AdminConnectorInput) => http.patch<AdminConnector>(`/v1/admin/connectors/${encodeURIComponent(id)}`, body),
    remove: (id: string) => http.delete<void>(`/v1/admin/connectors/${encodeURIComponent(id)}`),
    check: (body: { server_url: string; auth_type: string; oauth_client_id?: string }) =>
      http.post<ConnectorCheckResult>("/v1/admin/connectors/check", body),
  },
});

export type ConnectorsEndpoints = ReturnType<typeof connectorsEndpoints>;
