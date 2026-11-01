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
} from "./types";

/** Query keys; `all()` invalidates everything connectors cache. */
export const connectorsKeys = {
  all: () => ["connectors"] as const,
  list: () => ["connectors", "list"] as const,
  connection: (id: string) => ["connectors", "connection", id] as const,
  tools: (id: string) => ["connectors", "tools", id] as const,
  admin: () => ["connectors", "admin"] as const,
  adminList: () => ["connectors", "admin", "list"] as const,
};

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
