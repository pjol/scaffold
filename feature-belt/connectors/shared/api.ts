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
  ConnectorPlatformChange,
  ConnectorPlatformConnectInput,
  ConnectorPlatformDisconnected,
  ConnectorPlatformFinished,
  ConnectorPlatformList,
  ConnectorPlatformSetup,
  ConnectorPlatformTokenInput,
  ConnectorPolicyInput,
  ConnectorPolicyView,
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
  // connected platforms
  platforms: (orgId?: string | null, o?: CallOptions) =>
    http.get<ConnectorPlatformList>("/v1/connectors/platforms", { ...o, query: { org_id: orgId ?? undefined } }),
  /** Sign in (`authorize_url`), or turn on a capability already granted (`connection`). */
  connectPlatform: (provider: string, body: ConnectorPlatformConnectInput) =>
    http.post<ConnectorPlatformChange>(`/v1/connectors/platforms/${encodeURIComponent(provider)}/connect`, body),
  platformToken: (provider: string, body: ConnectorPlatformTokenInput) =>
    http.post<ConnectorPlatformChange>(`/v1/connectors/platforms/${encodeURIComponent(provider)}/token`, body),
  /** The `platform_finish` secret from the return URL. */
  finishPlatform: (secret: string, orgId?: string | null) =>
    http.post<ConnectorPlatformFinished>("/v1/connectors/platforms/finish", { secret, ...(orgId ? { org_id: orgId } : {}) }),
  setPlatformCapability: (connectionId: string, key: string, body: { enabled: boolean; client?: "web" | "mobile"; return_to?: string; org_id?: string }) =>
    http.put<ConnectorPlatformChange>(`/v1/connectors/platform-connections/${encodeURIComponent(connectionId)}/capabilities/${encodeURIComponent(key)}`, body),
  disconnectPlatform: (connectionId: string) =>
    http.delete<ConnectorPlatformDisconnected>(`/v1/connectors/platform-connections/${encodeURIComponent(connectionId)}`),
  policy: (orgId: string, o?: CallOptions) => http.get<ConnectorPolicyView>(`/v1/connectors/orgs/${encodeURIComponent(orgId)}/policy`, o),
  /** provider `*` is the default for platforms without their own decision. */
  setPolicy: (orgId: string, provider: string, body: ConnectorPolicyInput) =>
    http.put<ConnectorPolicyView>(`/v1/connectors/orgs/${encodeURIComponent(orgId)}/policy/${encodeURIComponent(provider)}`, body),
  resetPolicy: (orgId: string, provider: string) =>
    http.delete<ConnectorPolicyView>(`/v1/connectors/orgs/${encodeURIComponent(orgId)}/policy/${encodeURIComponent(provider)}`),
  admin: {
    platforms: (o?: CallOptions) => http.get<{ items: ConnectorPlatformSetup[]; next_cursor: string | null }>("/v1/admin/connectors/platforms", o),
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
