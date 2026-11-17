"use client";

import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryKey, type UseMutationResult, type UseQueryOptions, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import { identityEndpoints } from "./api";
import type {
  AdminVerification,
  AdminVerificationDetail,
  AdminVerificationListParams,
  IdentityAdminBadge,
  IdentityAdminBadgeApplication,
  IdentityAdminBadgeApplicationDetail,
  IdentityAdminBadgeApplicationListParams,
  IdentityAdminBadgeListParams,
  IdentityBadge,
  IdentityBadgeApplication,
  IdentityBadgeApplicationInput,
  IdentityBadgeDecision,
  IdentityBadgeGrantInput,
  IdentityBadgeKind,
  IdentityBadges,
  IdentityDomain,
  IdentityStartResult,
  IdentityStatus,
} from "./types";

/** Query keys (`identityKeys.all()` invalidates everything). */
export const identityKeys = {
  all: () => ["identity"] as const,
  status: () => ["identity", "status"] as const,
  badge: (userId: string) => ["identity", "badge", userId] as const,
  badges: () => ["identity", "badges"] as const,
  admin: {
    all: () => ["identity", "admin"] as const,
    list: (params: Record<string, unknown>) => ["identity", "admin", "list", params] as const,
    detail: (id: string) => ["identity", "admin", "detail", id] as const,
    applications: (params: Record<string, unknown>) => ["identity", "admin", "applications", params] as const,
    application: (id: string) => ["identity", "admin", "application", id] as const,
    badges: (params: Record<string, unknown>) => ["identity", "admin", "badges", params] as const,
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
// Badges, applications or domains changed (a decision, a removal, a plan starting or ending).
registerRealtimeHandler("identity.badges", (qc, e) => {
  void qc.invalidateQueries({ queryKey: identityKeys.badges() });
  void qc.invalidateQueries({ queryKey: identityKeys.badge(e.data.user_id) });
});
registerRealtimeHandler("identity.badge_application", (qc) => {
  void qc.invalidateQueries({ queryKey: ["identity", "admin", "applications"] });
});
registerReconnectRefetch(identityKeys.status());
registerReconnectRefetch(identityKeys.badges());

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

/** Whether a user's identity is verified (public badge, cached 5 min). Pass null to disable. */
export function useIdentityBadge(userId: string | null | undefined, options?: QueryOverrides<boolean>): UseQueryResult<boolean, ApiError> {
  const api = useEndpoints(identityEndpoints);
  // Shares the cache entry of useIdentityUserBadge (the whole badge) and selects `verified`.
  return useQuery<IdentityBadge, ApiError, boolean, QueryKey>({
    staleTime: 5 * 60_000,
    ...(options as unknown as Omit<UseQueryOptions<IdentityBadge, ApiError, boolean, QueryKey>, "queryKey" | "queryFn">),
    queryKey: identityKeys.badge(userId ?? ""),
    queryFn: ({ signal }) => api.badge(userId ?? "", { signal }),
    select: (b) => b.verified,
    enabled: !!userId && (options?.enabled ?? true),
  });
}

/** A user's public badge: ID verification and their highest verification badge (cached 5 min). Pass null to disable. */
export function useIdentityUserBadge(userId: string | null | undefined, options?: QueryOverrides<IdentityBadge>): UseQueryResult<IdentityBadge, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.badge(userId ?? ""), ({ signal }) => api.badge(userId ?? "", { signal }), { staleTime: 5 * 60_000, ...options }, !!userId);
}

// ---- verification badges -----------------------------------------------------------

/** The caller's badges, paid badge, applications and domain proofs. */
export function useIdentityBadges(options?: QueryOverrides<IdentityBadges>): UseQueryResult<IdentityBadges, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.badges(), ({ signal }) => api.badges({ signal }), options);
}

function badgesMutation<V, R>(fn: (api: ReturnType<typeof identityEndpoints>, v: V) => Promise<R>) {
  return function useBadgesMutation(): UseMutationResult<R, ApiError, V> {
    const api = useEndpoints(identityEndpoints);
    const qc = useQueryClient();
    return useMutation<R, ApiError, V>({
      mutationFn: (v) => fn(api, v),
      onSettled: () => qc.invalidateQueries({ queryKey: identityKeys.badges() }),
    });
  };
}

/** Apply for a reviewed badge. */
export const useApplyForIdentityBadge = badgesMutation<IdentityBadgeApplicationInput, IdentityBadgeApplication>((api, body) => api.applyForBadge(body));
/** Send an application again after staff asked for more. */
export const useResubmitIdentityBadgeApplication = badgesMutation<{ id: string; body: IdentityBadgeApplicationInput }, IdentityBadgeApplication>((api, { id, body }) =>
  api.resubmitBadgeApplication(id, body),
);
/** Withdraw an open application. */
export const useWithdrawIdentityBadgeApplication = badgesMutation<string, IdentityBadgeApplication>((api, id) => api.withdrawBadgeApplication(id));
/** Start a domain proof; the result carries the TXT record to add. */
export const useAddIdentityDomain = badgesMutation<string, IdentityDomain>((api, domain) => api.addBadgeDomain(domain));
/** Look the domain's TXT record up (422 `domain_unverified` says what was found). */
export const useCheckIdentityDomain = badgesMutation<string, IdentityDomain>((api, domain) => api.checkBadgeDomain(domain));
/** Remove a domain proof. */
export const useRemoveIdentityDomain = badgesMutation<string, void>((api, domain) => api.removeBadgeDomain(domain));

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

// ---- staff: badges -------------------------------------------------------------------

/** Badge applications: the review queue (no status) or one status, as an infinite list. */
export function useIdentityAdminBadgeApplications(
  params: IdentityAdminBadgeApplicationListParams = {},
  options?: InfiniteOverrides<IdentityAdminBadgeApplication>,
): PagedQueryResult<IdentityAdminBadgeApplication> {
  const api = useEndpoints(identityEndpoints);
  return usePagedQuery<IdentityAdminBadgeApplication>(identityKeys.admin.applications(listKeyParams(params)), (cursor, signal) => api.admin.badgeApplications({ ...params, cursor }, { signal }), {
    placeholderData: keepPreviousData,
    ...options,
  });
}

/** One application with the applicant's ID verification and current badges. */
export function useIdentityAdminBadgeApplication(id: string | null | undefined, options?: QueryOverrides<IdentityAdminBadgeApplicationDetail>): UseQueryResult<IdentityAdminBadgeApplicationDetail, ApiError> {
  const api = useEndpoints(identityEndpoints);
  return useApiQuery(identityKeys.admin.application(id ?? ""), ({ signal }) => api.admin.badgeApplication(id ?? "", { signal }), options, !!id);
}

/** Approve, ask for more, or reject an application. */
export function useDecideIdentityBadgeApplication(): UseMutationResult<IdentityAdminBadgeApplicationDetail, ApiError, { id: string; decision: IdentityBadgeDecision }> {
  const api = useEndpoints(identityEndpoints);
  const qc = useQueryClient();
  return useMutation<IdentityAdminBadgeApplicationDetail, ApiError, { id: string; decision: IdentityBadgeDecision }>({
    mutationFn: ({ id, decision }) => api.admin.decideBadgeApplication(id, decision),
    onSuccess: (a) => qc.setQueryData(identityKeys.admin.application(a.id), a),
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ["identity", "admin", "applications"] }), qc.invalidateQueries({ queryKey: ["identity", "admin", "badges"] }), qc.invalidateQueries({ queryKey: ["admin", "audit"] })]),
  });
}

/** Badge rows (active, ended or held), most recently changed first. */
export function useIdentityAdminBadges(params: IdentityAdminBadgeListParams = {}, options?: InfiniteOverrides<IdentityAdminBadge>): PagedQueryResult<IdentityAdminBadge> {
  const api = useEndpoints(identityEndpoints);
  return usePagedQuery<IdentityAdminBadge>(identityKeys.admin.badges(listKeyParams(params)), (cursor, signal) => api.admin.badges({ ...params, cursor }, { signal }), {
    placeholderData: keepPreviousData,
    ...options,
  });
}

function badgeAdminMutation<V>(fn: (api: ReturnType<typeof identityEndpoints>, v: V) => Promise<IdentityAdminBadge>) {
  return function useBadgeAdminMutation(): UseMutationResult<IdentityAdminBadge, ApiError, V> {
    const api = useEndpoints(identityEndpoints);
    const qc = useQueryClient();
    return useMutation<IdentityAdminBadge, ApiError, V>({
      mutationFn: (v) => fn(api, v),
      onSettled: (b) =>
        Promise.all([
          qc.invalidateQueries({ queryKey: ["identity", "admin", "badges"] }),
          qc.invalidateQueries({ queryKey: ["admin", "audit"] }),
          b ? qc.invalidateQueries({ queryKey: identityKeys.badge(b.user.id) }) : undefined,
        ]),
    });
  };
}

/** Grant a badge (admins), or lift a staff removal of the paid badge (kind paid). */
export const useGrantIdentityBadge = badgeAdminMutation<IdentityBadgeGrantInput>((api, body) => api.admin.grantBadge(body));
/** Remove a badge; the person sees `reason`. Removing the paid badge holds it against billing. */
export const useRevokeIdentityBadge = badgeAdminMutation<{ userId: string; kind: IdentityBadgeKind; reason: string }>((api, { userId, kind, reason }) =>
  api.admin.revokeBadge(userId, kind, reason),
);
