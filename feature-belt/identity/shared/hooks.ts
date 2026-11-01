"use client";

import { keepPreviousData, useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import { identityEndpoints } from "./api";
import type { AdminVerification, AdminVerificationDetail, AdminVerificationListParams, IdentityStartResult, IdentityStatus } from "./types";

/** Query keys (`identityKeys.all()` invalidates everything). */
export const identityKeys = {
  all: () => ["identity"] as const,
  status: () => ["identity", "status"] as const,
  badge: (userId: string) => ["identity", "badge", userId] as const,
  admin: {
    all: () => ["identity", "admin"] as const,
    list: (params: Record<string, unknown>) => ["identity", "admin", "list", params] as const,
    detail: (id: string) => ["identity", "admin", "detail", id] as const,
  },
};

// The server publishes the caller's new status on their user topic.
registerRealtimeHandler("identity.status", (qc, e) => {
  qc.setQueryData(identityKeys.status(), e.data);
  void qc.invalidateQueries({ queryKey: ["identity", "badge"] });
});
registerRealtimeHandler("identity.submitted", (qc) => {
  void qc.invalidateQueries({ queryKey: identityKeys.admin.all() });
});
registerReconnectRefetch(identityKeys.status());

/** The caller's verification status. */
export function useIdentityStatus(options?: QueryOverrides<IdentityStatus>): UseQueryResult<IdentityStatus, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.status(), ({ signal }) => api.status({ signal }), options);
}

/** `POST /v1/identity/start`: open `flow.hosted_url` (Persona) or show the upload form (manual). */
export function useStartVerification(): UseMutationResult<IdentityStartResult, ApiError, { redirect_uri?: string | undefined } | void> {
  const api = useEndpoints(identityEndpoints);
  const qc = useQueryClient();
  return useMutation<IdentityStartResult, ApiError, { redirect_uri?: string | undefined } | void>({
    mutationFn: (body) => api.start(body ?? {}),
    onSuccess: (res) => qc.setQueryData(identityKeys.status(), res.status),
    onError: () => void qc.invalidateQueries({ queryKey: identityKeys.status() }),
  });
}

/** `POST /v1/identity/documents` (manual mode). */
export function useSubmitIdentityDocuments(): UseMutationResult<IdentityStatus, ApiError, { document_key: string; selfie_key: string }> {
  const api = useEndpoints(identityEndpoints);
  const qc = useQueryClient();
  return useMutation<IdentityStatus, ApiError, { document_key: string; selfie_key: string }>({
    mutationFn: (body) => api.submitDocuments(body),
    onSuccess: (status) => qc.setQueryData(identityKeys.status(), status),
  });
}

/** Whether a user is verified (public badge, cached 5 min). Pass null to disable. */
export function useIdentityBadge(userId: string | null | undefined, options?: QueryOverrides<boolean>): UseQueryResult<boolean, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.badge(userId ?? ""), async ({ signal }) => (await api.badge(userId ?? "", { signal })).verified, { staleTime: 5 * 60_000, ...options }, !!userId);
}

// ---- staff ------------------------------------------------------------------------

/** The review queue (no status) or one status, as an infinite list. */
export function useAdminVerifications(params: AdminVerificationListParams = {}, options?: InfiniteOverrides<AdminVerification>): PagedQueryResult<AdminVerification> {
  const api = useEndpoints(identityEndpoints);
  return usePagedQuery<AdminVerification>(identityKeys.admin.list(listKeyParams(params)), (cursor, signal) => api.admin.list({ ...params, cursor }, { signal }), {
    placeholderData: keepPreviousData,
    ...options,
  });
}

/** One attempt with 5-minute photo links; refreshed before the links expire. Each load is audited. */
export function useAdminVerification(id: string | null | undefined, options?: QueryOverrides<AdminVerificationDetail>): UseQueryResult<AdminVerificationDetail, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.admin.detail(id ?? ""), ({ signal }) => api.admin.get(id ?? "", { signal }), {
    staleTime: 4 * 60_000,
    refetchOnWindowFocus: false,
    ...options,
  }, !!id);
}

function decision<V>(fn: (api: ReturnType<typeof identityEndpoints>, v: V) => Promise<AdminVerification>) {
  return function useDecision(): UseMutationResult<AdminVerification, ApiError, V> {
    const api = useEndpoints(identityEndpoints);
    const qc = useQueryClient();
    return useMutation<AdminVerification, ApiError, V>({
      mutationFn: (v) => fn(api, v),
      onSuccess: (v) => qc.removeQueries({ queryKey: identityKeys.admin.detail(v.id) }),
      onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: identityKeys.admin.list({}) }), qc.invalidateQueries({ queryKey: identityKeys.admin.all() }), qc.invalidateQueries({ queryKey: ["admin", "audit"] })]),
    });
  };
}

/** Approve a submitted attempt (deletes its photos). */
export const useApproveVerification = decision<string>((api, id) => api.admin.approve(id));
/** Reject a submitted attempt or revoke a verified one; the user sees `reason`. */
export const useRejectVerification = decision<{ id: string; reason: string }>((api, { id, reason }) => api.admin.reject(id, reason));
