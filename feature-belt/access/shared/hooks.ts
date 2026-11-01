"use client";

import { keepPreviousData, useMutation, useQueryClient, type InfiniteData, type QueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { accessEndpoints } from "./api";
import type {
  AccessAddGrantsRequest,
  AccessCreateLinkRequest,
  AccessCreateTeamRequest,
  AccessDirectoryEntry,
  AccessGeneralRequest,
  AccessLevel,
  AccessLink,
  AccessLinkTarget,
  AccessMyPermissions,
  AccessPermissionPolicy,
  AccessSetPermissionRequest,
  AccessSharing,
  AccessTeam,
  AccessTeamMember,
  AccessTeamRole,
  AccessUpdateTeamRequest,
} from "./types";

/** Query keys. `accessKeys.all()` invalidates everything this feature caches. */
export const accessKeys = {
  all: () => ["access"] as const,
  resource: (kind: string, id: string) => ["access", "resource", kind, id] as const,
  link: (token: string) => ["access", "link", token] as const,
  directory: (orgId: string, q: string, types: string) => ["access", "directory", orgId, q, types] as const,
  orgTeams: (orgId: string) => ["access", "teams", orgId] as const,
  teams: (orgId: string, q: string, mine: boolean) => ["access", "teams", orgId, { q, mine }] as const,
  team: (teamId: string) => ["access", "team", teamId] as const,
  teamMembers: (teamId: string) => ["access", "team", teamId, "members"] as const,
  myPermissions: (orgId: string) => ["access", "my-permissions", orgId] as const,
  permissions: (orgId: string) => ["access", "permissions", orgId] as const,
};

// ---- sharing -------------------------------------------------------------------

/**
 * How a resource is shared, for anyone who can view it (404 otherwise). `my_level`
 * gates edit controls; `grants` and `links` are filled only when `can_manage`.
 */
export function useAccessResource(kind: string, id: string | null | undefined, options?: QueryOverrides<AccessSharing>): UseQueryResult<AccessSharing, ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(accessKeys.resource(kind, id ?? ""), ({ signal }) => a.sharing(kind, id ?? "", { signal }), { retry: retryUnlessClientError, ...options }, !!id);
}

function retryUnlessClientError(count: number, error: ApiError): boolean {
  return error.status >= 500 || error.status === 0 ? count < 2 : false;
}

/** Writes answer with the fresh sharing state (nothing when you removed your own access). */
function storeSharing(qc: QueryClient, kind: string, id: string) {
  return (data: AccessSharing | undefined) => {
    if (data) qc.setQueryData(accessKeys.resource(kind, id), data);
    else qc.removeQueries({ queryKey: accessKeys.resource(kind, id) });
  };
}

/** Restricted, or everyone in the org at view/comment/edit. */
export function useAccessSetGeneral(kind: string, id: string): UseMutationResult<AccessSharing, ApiError, AccessGeneralRequest> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessSharing, ApiError, AccessGeneralRequest>({ mutationFn: (body) => a.setGeneral(kind, id, body), onSuccess: storeSharing(qc, kind, id) });
}

/** Inherit the parent's access, or stop ("restricted"); you keep manage. */
export function useAccessSetInherit(kind: string, id: string): UseMutationResult<AccessSharing, ApiError, boolean> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessSharing, ApiError, boolean>({ mutationFn: (inherits) => a.setInherit(kind, id, inherits), onSuccess: storeSharing(qc, kind, id) });
}

/** Share with people and teams (or change their level). */
export function useAccessAddGrants(kind: string, id: string): UseMutationResult<AccessSharing, ApiError, AccessAddGrantsRequest> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessSharing, ApiError, AccessAddGrantsRequest>({ mutationFn: (body) => a.addGrants(kind, id, body), onSuccess: storeSharing(qc, kind, id) });
}

export function useAccessUpdateGrant(kind: string, id: string): UseMutationResult<AccessSharing, ApiError, { grantId: string; level: AccessLevel }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessSharing, ApiError, { grantId: string; level: AccessLevel }>({
    mutationFn: ({ grantId, level }) => a.updateGrant(kind, id, grantId, level),
    onSuccess: storeSharing(qc, kind, id),
  });
}

/** Remove someone's access (or your own: resolves to undefined when that ends your access). Optimistic. */
export function useAccessRemoveGrant(kind: string, id: string): UseMutationResult<AccessSharing | undefined, ApiError, string, { prev: AccessSharing | undefined }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  const key = accessKeys.resource(kind, id);
  return useMutation<AccessSharing | undefined, ApiError, string, { prev: AccessSharing | undefined }>({
    mutationFn: (grantId) => a.removeGrant(kind, id, grantId),
    onMutate: async (grantId) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<AccessSharing>(key);
      if (prev) qc.setQueryData<AccessSharing>(key, { ...prev, grants: prev.grants.filter((g) => g.id !== grantId) });
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSuccess: storeSharing(qc, kind, id),
  });
}

/** Hand the resource to another member; the previous owner keeps manage. */
export function useAccessTransferOwner(kind: string, id: string): UseMutationResult<AccessSharing, ApiError, string> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessSharing, ApiError, string>({ mutationFn: (userId) => a.transferOwner(kind, id, userId), onSuccess: storeSharing(qc, kind, id) });
}

/** Resolves to the new link with `url` and `token`; show the URL now, it can't be read again. */
export function useAccessCreateLink(kind: string, id: string): UseMutationResult<AccessLink, ApiError, AccessCreateLinkRequest> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessLink, ApiError, AccessCreateLinkRequest>({
    mutationFn: (body) => a.createLink(kind, id, body),
    onSettled: () => qc.invalidateQueries({ queryKey: accessKeys.resource(kind, id) }),
  });
}

export function useAccessRevokeLink(kind: string, id: string): UseMutationResult<void, ApiError, string> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  const key = accessKeys.resource(kind, id);
  return useMutation<void, ApiError, string>({
    mutationFn: (linkId) => a.revokeLink(kind, id, linkId),
    onMutate: (linkId) => {
      qc.setQueryData<AccessSharing>(key, (prev) => (prev ? { ...prev, links: prev.links.filter((l) => l.id !== linkId) } : prev));
    },
    onSettled: () => qc.invalidateQueries({ queryKey: key }),
  });
}

/** Public: what a link opens (works signed out). */
export function useAccessLink(token: string | null | undefined, options?: QueryOverrides<AccessLinkTarget>): UseQueryResult<AccessLinkTarget, ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(accessKeys.link(token ?? ""), ({ signal }) => a.resolveLink(token ?? "", { signal }), { retry: false, ...options }, !!token);
}

// ---- directory -----------------------------------------------------------------

/**
 * Teams then people in an org matching `query` (debounced 200 ms; keeps the previous
 * results while typing). "@des" matches team handles only.
 */
export function useAccessDirectory(
  orgId: string | null | undefined,
  query: string,
  options: { types?: "user" | "team" | "user,team"; limit?: number; enabled?: boolean } = {},
): UseQueryResult<AccessDirectoryEntry[], ApiError> {
  const a = useEndpoints(accessEndpoints);
  const q = useDebouncedValue(query.trim(), 200);
  const types = options.types ?? "user,team";
  return useApiQuery<AccessDirectoryEntry[]>(
    accessKeys.directory(orgId ?? "", q, types),
    ({ signal }) => a.directory(orgId ?? "", { q, types, limit: options.limit ?? 8 }, { signal }).then((r) => r.items),
    { placeholderData: (prev) => prev, staleTime: 30_000 },
    !!orgId && (options.enabled ?? true),
  );
}

// ---- teams ---------------------------------------------------------------------

/** Teams in an org by name; `q` filters by name or handle, `mine` keeps your teams. */
export function useAccessTeams(orgId: string | null | undefined, params: { q?: string; mine?: boolean } = {}, options?: InfiniteOverrides<AccessTeam>): PagedQueryResult<AccessTeam> {
  const a = useEndpoints(accessEndpoints);
  const q = params.q?.trim() ?? "";
  const mine = params.mine ?? false;
  return usePagedQuery<AccessTeam>(
    accessKeys.teams(orgId ?? "", q, mine),
    (cursor, signal) => a.teams(orgId ?? "", { cursor, q, mine, limit: 50 }, { signal }),
    { placeholderData: keepPreviousData, ...options },
    !!orgId,
  );
}

/** Your teams in an org (one page of 100; for mentions and filters). */
export function useAccessMyTeams(orgId: string | null | undefined): UseQueryResult<AccessTeam[], ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(
    [...accessKeys.orgTeams(orgId ?? ""), "mine-all"],
    ({ signal }) => a.teams(orgId ?? "", { mine: true, limit: 100 }, { signal }).then((p) => p.items),
    { staleTime: 60_000 },
    !!orgId,
  );
}

export function useAccessTeam(teamId: string | null | undefined, options?: QueryOverrides<AccessTeam>): UseQueryResult<AccessTeam, ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(accessKeys.team(teamId ?? ""), ({ signal }) => a.team(teamId ?? "", { signal }), { retry: retryUnlessClientError, ...options }, !!teamId);
}

export function useAccessTeamMembers(teamId: string | null | undefined, options?: InfiniteOverrides<AccessTeamMember>): PagedQueryResult<AccessTeamMember> {
  const a = useEndpoints(accessEndpoints);
  return usePagedQuery<AccessTeamMember>(accessKeys.teamMembers(teamId ?? ""), (cursor, signal) => a.teamMembers(teamId ?? "", { cursor, limit: 50 }, { signal }), options, !!teamId);
}

function putTeam(qc: QueryClient, team: AccessTeam) {
  qc.setQueryData(accessKeys.team(team.id), team);
  return qc.invalidateQueries({ queryKey: accessKeys.orgTeams(team.org_id) });
}

export function useAccessCreateTeam(orgId: string): UseMutationResult<AccessTeam, ApiError, AccessCreateTeamRequest> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessTeam, ApiError, AccessCreateTeamRequest>({ mutationFn: (body) => a.createTeam(orgId, body), onSuccess: (t) => putTeam(qc, t) });
}

export function useAccessUpdateTeam(teamId: string): UseMutationResult<AccessTeam, ApiError, AccessUpdateTeamRequest> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessTeam, ApiError, AccessUpdateTeamRequest>({ mutationFn: (body) => a.updateTeam(teamId, body), onSuccess: (t) => putTeam(qc, t) });
}

/** Deletes the team with its memberships, grants and permission overrides. */
export function useAccessDeleteTeam(): UseMutationResult<void, ApiError, { teamId: string; orgId: string }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { teamId: string; orgId: string }>({
    mutationFn: ({ teamId }) => a.deleteTeam(teamId),
    onSuccess: (_d, { teamId, orgId }) => {
      qc.removeQueries({ queryKey: accessKeys.team(teamId) });
      return qc.invalidateQueries({ queryKey: accessKeys.orgTeams(orgId) });
    },
  });
}

function invalidateTeam(qc: QueryClient, teamId: string, orgId?: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: accessKeys.team(teamId) }),
    orgId ? qc.invalidateQueries({ queryKey: accessKeys.orgTeams(orgId) }) : undefined,
  ]);
}

export function useAccessAddTeamMembers(teamId: string): UseMutationResult<{ added: number; items: AccessTeamMember[] }, ApiError, { userIds: string[]; role?: AccessTeamRole }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<{ added: number; items: AccessTeamMember[] }, ApiError, { userIds: string[]; role?: AccessTeamRole }>({
    mutationFn: ({ userIds, role }) => a.addTeamMembers(teamId, userIds, role),
    onSettled: () => invalidateTeam(qc, teamId, qc.getQueryData<AccessTeam>(accessKeys.team(teamId))?.org_id),
  });
}

export function useAccessUpdateTeamMember(teamId: string): UseMutationResult<AccessTeamMember, ApiError, { userId: string; role: AccessTeamRole }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessTeamMember, ApiError, { userId: string; role: AccessTeamRole }>({
    mutationFn: ({ userId, role }) => a.updateTeamMember(teamId, userId, role),
    onSettled: () => invalidateTeam(qc, teamId),
  });
}

/** Remove a member, or leave with your own id. Optimistic. */
export function useAccessRemoveTeamMember(teamId: string): UseMutationResult<void, ApiError, string> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (userId) => a.removeTeamMember(teamId, userId),
    onMutate: (userId) => {
      qc.setQueryData<InfiniteData<Page<AccessTeamMember>>>(accessKeys.teamMembers(teamId), (data) =>
        data ? { ...data, pages: data.pages.map((p) => ({ ...p, items: p.items.filter((m) => m.user_id !== userId) })) } : data,
      );
    },
    onSettled: () => invalidateTeam(qc, teamId, qc.getQueryData<AccessTeam>(accessKeys.team(teamId))?.org_id),
  });
}

// ---- permissions ---------------------------------------------------------------

/** Which org permissions you hold, for gating UI (the API checks again). */
export function useAccessMyPermissions(orgId: string | null | undefined, options?: QueryOverrides<AccessMyPermissions>): UseQueryResult<AccessMyPermissions, ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(accessKeys.myPermissions(orgId ?? ""), ({ signal }) => a.myPermissions(orgId ?? "", { signal }), { staleTime: 60_000, ...options }, !!orgId);
}

/** Whether you hold `key` in the org: undefined while loading or without an org. */
export function useAccessPermission(orgId: string | null | undefined, key: string): boolean | undefined {
  const q = useAccessMyPermissions(orgId);
  return q.data ? q.data.permissions[key] === true : undefined;
}

/** Org admins: every permission with its roles, teams and people. */
export function useAccessPermissions(orgId: string | null | undefined, options?: QueryOverrides<AccessPermissionPolicy[]>): UseQueryResult<AccessPermissionPolicy[], ApiError> {
  const a = useEndpoints(accessEndpoints);
  return useApiQuery(accessKeys.permissions(orgId ?? ""), ({ signal }) => a.permissions(orgId ?? "", { signal }).then((r) => r.items), options, !!orgId);
}

function putPolicy(qc: QueryClient, orgId: string, policy: AccessPermissionPolicy) {
  qc.setQueryData<AccessPermissionPolicy[]>(accessKeys.permissions(orgId), (list) => list?.map((p) => (p.key === policy.key ? policy : p)));
  return qc.invalidateQueries({ queryKey: accessKeys.myPermissions(orgId) });
}

/** Replace who holds a permission (roles, teams, people). Optimistic for the roles. */
export function useAccessSetPermission(orgId: string): UseMutationResult<AccessPermissionPolicy, ApiError, { key: string } & AccessSetPermissionRequest, { prev: AccessPermissionPolicy[] | undefined }> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  const key = accessKeys.permissions(orgId);
  return useMutation<AccessPermissionPolicy, ApiError, { key: string } & AccessSetPermissionRequest, { prev: AccessPermissionPolicy[] | undefined }>({
    mutationFn: ({ key: k, ...body }) => a.setPermission(orgId, k, body),
    onMutate: async ({ key: k, roles }) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<AccessPermissionPolicy[]>(key);
      qc.setQueryData<AccessPermissionPolicy[]>(key, (list) => list?.map((p) => (p.key === k ? { ...p, roles, customized: true } : p)));
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
    onSuccess: (policy) => putPolicy(qc, orgId, policy),
  });
}

export function useAccessResetPermission(orgId: string): UseMutationResult<AccessPermissionPolicy, ApiError, string> {
  const a = useEndpoints(accessEndpoints);
  const qc = useQueryClient();
  return useMutation<AccessPermissionPolicy, ApiError, string>({ mutationFn: (k) => a.resetPermission(orgId, k), onSuccess: (policy) => putPolicy(qc, orgId, policy) });
}

// ---- realtime ------------------------------------------------------------------

registerRealtimeHandler("access.changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: accessKeys.resource(e.data.kind, e.data.id) });
});
registerRealtimeHandler("access.teams_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: accessKeys.orgTeams(e.data.org_id) });
  void qc.invalidateQueries({ queryKey: accessKeys.team(e.data.team_id) });
  void qc.invalidateQueries({ queryKey: ["access", "directory", e.data.org_id] });
});
registerRealtimeHandler("access.team_members_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: accessKeys.team(e.data.team_id) });
  void qc.invalidateQueries({ queryKey: accessKeys.orgTeams(e.data.org_id) });
  void qc.invalidateQueries({ queryKey: accessKeys.myPermissions(e.data.org_id) });
});
registerRealtimeHandler("access.permissions_changed", (qc, e) => {
  void qc.invalidateQueries({ queryKey: accessKeys.myPermissions(e.data.org_id) });
  void qc.invalidateQueries({ queryKey: accessKeys.permissions(e.data.org_id) });
});
registerReconnectRefetch(accessKeys.all());
