"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { connectorsEndpoints } from "./api";
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

/** Query keys; `all()` invalidates everything connectors cache. */
export const connectorsKeys = {
  all: () => ["connectors"] as const,
  list: () => ["connectors", "list"] as const,
  connection: (id: string) => ["connectors", "connection", id] as const,
  tools: (id: string) => ["connectors", "tools", id] as const,
  admin: () => ["connectors", "admin"] as const,
  adminList: () => ["connectors", "admin", "list"] as const,
  adminPlatforms: () => ["connectors", "admin", "platforms"] as const,
  platforms: (orgId?: string | null) => ["connectors", "platforms", orgId ?? ""] as const,
  platformsAll: () => ["connectors", "platforms"] as const,
  policy: (orgId: string) => ["connectors", "policy", orgId] as const,
};

/** Features that show connected sources cache them under their own keys; a change here refreshes them too. */
function invalidateSources(qc: ReturnType<typeof useQueryClient>) {
  void qc.invalidateQueries({ queryKey: connectorsKeys.platformsAll() });
  void qc.invalidateQueries({ predicate: (q) => q.queryKey.some((k) => k === "sources") });
}

/** The catalog with your connection to each, and whether you may add your own servers. */
export function useConnectors(options?: QueryOverrides<ConnectorList>): UseQueryResult<ConnectorList, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.list(), ({ signal }) => ep.list({ signal }), options);
}

/** One of your connections with its connector. Pass null to disable. */
export function useConnectorConnection(id: string | null | undefined, options?: QueryOverrides<Connector>): UseQueryResult<Connector, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.connection(id ?? ""), ({ signal }) => ep.connection(id ?? "", { signal }), options, !!id);
}

/** The server's tools for one of your connections (cached on the server for a few minutes). */
export function useConnectorTools(id: string | null | undefined, options?: QueryOverrides<ConnectorToolList>): UseQueryResult<ConnectorToolList, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.tools(id ?? ""), ({ signal }) => ep.tools(id ?? "", false, { signal }), { staleTime: 60_000, ...options }, !!id);
}

/** Ask the server for a fresh tool list. */
export function useRefreshConnectorTools(): UseMutationResult<ConnectorToolList, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorToolList, ApiError, string>({
    mutationFn: (id) => ep.tools(id, true),
    onSuccess: (list, id) => qc.setQueryData(connectorsKeys.tools(id), list),
  });
}

/** Start connecting: OAuth answers with `authorize_url` to open; a pasted token answers with the connection. */
export function useStartConnectorConnect(): UseMutationResult<ConnectorConnectStart, ApiError, { id: string; client?: "web" | "mobile"; token?: string }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorConnectStart, ApiError, { id: string; client?: "web" | "mobile"; token?: string }>({
    mutationFn: ({ id, client, token }) => ep.connect(id, { ...(client ? { client } : {}), ...(token !== undefined ? { token } : {}) }),
    onSuccess: (res) => {
      if (res.connector) void qc.invalidateQueries({ queryKey: connectorsKeys.list() });
    },
  });
}

/** Finish an OAuth connect with the `finish` secret from the return URL. */
export function useFinishConnectorConnect(): UseMutationResult<Connector, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<Connector, ApiError, string>({
    mutationFn: (secret) => ep.finish(secret),
    onSuccess: (c) => {
      if (c.connection) {
        qc.setQueryData(connectorsKeys.connection(c.connection.id), c);
        void qc.invalidateQueries({ queryKey: connectorsKeys.tools(c.connection.id) });
      }
    },
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.list() }),
  });
}

function patchList(data: ConnectorList | undefined, c: Connector): ConnectorList | undefined {
  return data ? { ...data, items: data.items.map((x) => (x.id === c.id ? c : x)) } : data;
}

/** Turn changes (write tools) on or off for a connection. */
export function useSetConnectorWrites(): UseMutationResult<Connector, ApiError, { id: string; writesEnabled: boolean }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<Connector, ApiError, { id: string; writesEnabled: boolean }, { prev: Connector | undefined }>({
    mutationFn: ({ id, writesEnabled }) => ep.setWrites(id, writesEnabled),
    onMutate: async ({ id, writesEnabled }) => {
      await qc.cancelQueries({ queryKey: connectorsKeys.connection(id) });
      const prev = qc.getQueryData<Connector>(connectorsKeys.connection(id));
      if (prev?.connection) qc.setQueryData(connectorsKeys.connection(id), { ...prev, connection: { ...prev.connection, writes_enabled: writesEnabled } });
      return { prev };
    },
    onError: (_e, { id }, ctx) => {
      if (ctx?.prev) qc.setQueryData(connectorsKeys.connection(id), ctx.prev);
    },
    onSuccess: (c, { id }) => {
      qc.setQueryData(connectorsKeys.connection(id), c);
      qc.setQueryData<ConnectorList>(connectorsKeys.list(), (d) => patchList(d, c));
    },
    onSettled: (_c, _e, { id }) => qc.invalidateQueries({ queryKey: connectorsKeys.tools(id) }),
  });
}

/** Switch one remote tool on or off for a connection. */
export function useSetConnectorToolEnabled(): UseMutationResult<Connector, ApiError, { id: string; tool: string; enabled: boolean }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<Connector, ApiError, { id: string; tool: string; enabled: boolean }, { prev: ConnectorToolList | undefined }>({
    mutationFn: ({ id, tool, enabled }) => ep.setToolEnabled(id, tool, enabled),
    onMutate: async ({ id, tool, enabled }) => {
      await qc.cancelQueries({ queryKey: connectorsKeys.tools(id) });
      const prev = qc.getQueryData<ConnectorToolList>(connectorsKeys.tools(id));
      const conn = qc.getQueryData<Connector>(connectorsKeys.connection(id))?.connection;
      if (prev) {
        qc.setQueryData<ConnectorToolList>(connectorsKeys.tools(id), {
          ...prev,
          items: prev.items.map((t) => (t.name === tool ? { ...t, enabled, offered: enabled && (t.read_only || !!conn?.writes_enabled) } : t)),
        });
      }
      return { prev };
    },
    onError: (_e, { id }, ctx) => {
      if (ctx?.prev) qc.setQueryData(connectorsKeys.tools(id), ctx.prev);
    },
    onSuccess: (c, { id }) => qc.setQueryData(connectorsKeys.connection(id), c),
    onSettled: (_c, _e, { id }) => qc.invalidateQueries({ queryKey: connectorsKeys.tools(id) }),
  });
}

/** Disconnect: our copy of the tokens is deleted at once; the provider is asked to revoke them. */
export function useDisconnectConnector(): UseMutationResult<ConnectorDisconnected, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorDisconnected, ApiError, string>({
    mutationFn: (id) => ep.disconnect(id),
    onSuccess: (_r, id) => {
      qc.removeQueries({ queryKey: connectorsKeys.tools(id) });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.list() }),
  });
}

/** Add your own server (when the server allows it). */
export function useCreateCustomConnector(): UseMutationResult<Connector, ApiError, ConnectorCustomRequest> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<Connector, ApiError, ConnectorCustomRequest>({
    mutationFn: (body) => ep.createCustom(body),
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.list() }),
  });
}

/** Remove your own server and its connection. */
export function useDeleteCustomConnector(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.deleteCustom(id),
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.list() }),
  });
}

// ---- admin ------------------------------------------------------------------

/** Admin: the catalog, newest first (`data.items`). */
export function useAdminConnectors(options?: InfiniteOverrides<AdminConnector>): PagedQueryResult<AdminConnector> {
  const ep = useEndpoints(connectorsEndpoints);
  return usePagedQuery<AdminConnector>(connectorsKeys.adminList(), (cursor, signal) => ep.admin.list({ limit: 50, ...(cursor ? { cursor } : {}) }, { signal }), options);
}

/** Admin: add a catalog entry (audited). */
export function useCreateAdminConnector(): UseMutationResult<AdminConnector, ApiError, AdminConnectorInput> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<AdminConnector, ApiError, AdminConnectorInput>({
    mutationFn: (body) => ep.admin.create(body),
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.all() }),
  });
}

/** Admin: change a catalog entry (audited; a new URL asks connected users to connect again). */
export function useUpdateAdminConnector(): UseMutationResult<AdminConnector, ApiError, { id: string; body: AdminConnectorInput }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<AdminConnector, ApiError, { id: string; body: AdminConnectorInput }>({
    mutationFn: ({ id, body }) => ep.admin.update(id, body),
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.all() }),
  });
}

/** Admin: delete a catalog entry and every connection to it (audited). */
export function useDeleteAdminConnector(): UseMutationResult<void, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => ep.admin.remove(id),
    onSettled: () => qc.invalidateQueries({ queryKey: connectorsKeys.all() }),
  });
}

/** Admin: check how a server signs users in before saving it. */
export function useCheckConnectorServer(): UseMutationResult<ConnectorCheckResult, ApiError, { server_url: string; auth_type: string; oauth_client_id?: string }> {
  const ep = useEndpoints(connectorsEndpoints);
  return useMutation<ConnectorCheckResult, ApiError, { server_url: string; auth_type: string; oauth_client_id?: string }>({
    mutationFn: (body) => ep.admin.check(body),
  });
}

// ---- connected platforms --------------------------------------------------------------

/** Every platform with its capabilities, the workspace's policy (orgId) and your accounts. */
export function useConnectorPlatforms(orgId?: string | null, options?: QueryOverrides<ConnectorPlatformList>): UseQueryResult<ConnectorPlatformList, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.platforms(orgId), ({ signal }) => ep.platforms(orgId, { signal }), options);
}

/** Connect an account, or reconnect / grant more on one (`connection_id`). Open `authorize_url` when it comes back. */
export function useConnectConnectorPlatform(): UseMutationResult<ConnectorPlatformChange, ApiError, ConnectorPlatformConnectInput & { provider: string }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPlatformChange, ApiError, ConnectorPlatformConnectInput & { provider: string }>({
    mutationFn: ({ provider, ...body }) => ep.connectPlatform(provider, body),
    onSuccess: (res) => {
      if (res.connection) invalidateSources(qc);
    },
  });
}

/** Connect with a personal or private token (checked against the platform first). */
export function useConnectorPlatformToken(): UseMutationResult<ConnectorPlatformChange, ApiError, ConnectorPlatformTokenInput & { provider: string }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPlatformChange, ApiError, ConnectorPlatformTokenInput & { provider: string }>({
    mutationFn: ({ provider, ...body }) => ep.platformToken(provider, body),
    onSettled: () => invalidateSources(qc),
  });
}

/** Finish a platform sign-in with the `platform_finish` secret from the return URL. */
export function useFinishConnectorPlatform(): UseMutationResult<ConnectorPlatformFinished, ApiError, { secret: string; orgId?: string | null }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPlatformFinished, ApiError, { secret: string; orgId?: string | null }>({
    mutationFn: ({ secret, orgId }) => ep.finishPlatform(secret, orgId),
    onSettled: () => invalidateSources(qc),
  });
}

/** Turn a capability on or off for one account. Turning on may answer with `authorize_url` (consent for the new scopes). */
export function useSetConnectorPlatformCapability(): UseMutationResult<
  ConnectorPlatformChange,
  ApiError,
  { connectionId: string; capability: string; enabled: boolean; client?: "web" | "mobile"; returnTo?: string; orgId?: string | null }
> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  type Vars = { connectionId: string; capability: string; enabled: boolean; client?: "web" | "mobile"; returnTo?: string; orgId?: string | null };
  return useMutation<ConnectorPlatformChange, ApiError, Vars, { prev: [readonly unknown[], ConnectorPlatformList | undefined][] }>({
    mutationFn: ({ connectionId, capability, enabled, client, returnTo, orgId }) =>
      ep.setPlatformCapability(connectionId, capability, {
        enabled,
        ...(client ? { client } : {}),
        ...(returnTo ? { return_to: returnTo } : {}),
        ...(orgId ? { org_id: orgId } : {}),
      }),
    onMutate: async ({ connectionId, capability, enabled }) => {
      await qc.cancelQueries({ queryKey: connectorsKeys.platformsAll() });
      const prev = qc.getQueriesData<ConnectorPlatformList>({ queryKey: connectorsKeys.platformsAll() });
      if (!enabled) {
        // Turning off never needs consent: show it at once.
        qc.setQueriesData<ConnectorPlatformList>({ queryKey: connectorsKeys.platformsAll() }, (d) =>
          d
            ? {
                ...d,
                items: d.items.map((p) => ({
                  ...p,
                  connections: p.connections.map((c) =>
                    c.id === connectionId ? { ...c, capabilities: c.capabilities.map((x) => (x.key === capability && x.state !== "blocked" ? { ...x, state: "off" as const } : x)) } : c,
                  ),
                })),
              }
            : d,
        );
      }
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      for (const [key, data] of ctx?.prev ?? []) qc.setQueryData(key, data);
    },
    onSuccess: (res) => {
      if (res.connection) invalidateSources(qc);
    },
  });
}

/** Disconnect one account: our tokens go at once; the platform is asked to revoke them. */
export function useDisconnectConnectorPlatform(): UseMutationResult<ConnectorPlatformDisconnected, ApiError, string> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPlatformDisconnected, ApiError, string>({
    mutationFn: (id) => ep.disconnectPlatform(id),
    onSettled: () => invalidateSources(qc),
  });
}

/** A workspace's connection policy (every member may read it; owners and admins change it). */
export function useConnectorPolicy(orgId: string | null | undefined, options?: QueryOverrides<ConnectorPolicyView>): UseQueryResult<ConnectorPolicyView, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.policy(orgId ?? ""), ({ signal }) => ep.policy(orgId ?? "", { signal }), options, !!orgId);
}

/** Change a workspace's decision for one platform (`*` = the default). Audited. */
export function useSetConnectorPolicy(): UseMutationResult<ConnectorPolicyView, ApiError, { orgId: string; provider: string; body: ConnectorPolicyInput }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPolicyView, ApiError, { orgId: string; provider: string; body: ConnectorPolicyInput }>({
    mutationFn: ({ orgId, provider, body }) => ep.setPolicy(orgId, provider, body),
    onSuccess: (v, { orgId }) => qc.setQueryData(connectorsKeys.policy(orgId), v),
    onSettled: () => invalidateSources(qc),
  });
}

/** Put a platform back on the workspace default. Audited. */
export function useResetConnectorPolicy(): UseMutationResult<ConnectorPolicyView, ApiError, { orgId: string; provider: string }> {
  const ep = useEndpoints(connectorsEndpoints);
  const qc = useQueryClient();
  return useMutation<ConnectorPolicyView, ApiError, { orgId: string; provider: string }>({
    mutationFn: ({ orgId, provider }) => ep.resetPolicy(orgId, provider),
    onSuccess: (v, { orgId }) => qc.setQueryData(connectorsKeys.policy(orgId), v),
    onSettled: () => invalidateSources(qc),
  });
}

/** Staff: each platform's setup on this server. */
export function useAdminConnectorPlatforms(options?: QueryOverrides<{ items: ConnectorPlatformSetup[]; next_cursor: string | null }>): UseQueryResult<{ items: ConnectorPlatformSetup[]; next_cursor: string | null }, ApiError> {
  const ep = useEndpoints(connectorsEndpoints);
  return useApiQuery(connectorsKeys.adminPlatforms(), ({ signal }) => ep.admin.platforms({ signal }), options);
}
