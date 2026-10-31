"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import type { AdminAuditListParams, AdminUserListParams } from "../api/params";
import { ADMIN_LIVE_TOPIC } from "../realtime/topics";
import type { AdminOverview, AdminUser, AuditEntry, UpdateUserRequest } from "../types";
import { useApi } from "./context";
import { useApiQuery, usePagedQuery, useTopic, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "./internal";
import { listKeyParams, queryKeys } from "./keys";

/** Subscribe to the `admin:live` staff feed while mounted (features publish their admin events there). */
export function useAdminLive(enabled = true): void {
  useTopic(enabled ? ADMIN_LIVE_TOPIC : null);
}

/** `GET /v1/admin/overview`: user counts for the console home. */
export function useAdminOverview(options?: QueryOverrides<AdminOverview>): UseQueryResult<AdminOverview, ApiError> {
  const api = useApi();
  return useApiQuery(queryKeys.admin.overview(), ({ signal }) => api.admin.overview({ signal }), options);
}

/** `GET /v1/admin/users?q=&status=` as an infinite list (`data.items`). */
export function useAdminUsers(params: AdminUserListParams = {}, options?: InfiniteOverrides<AdminUser>): PagedQueryResult<AdminUser> {
  const api = useApi();
  return usePagedQuery<AdminUser>(
    queryKeys.admin.users.list(listKeyParams(params)),
    (cursor, signal) => api.admin.users({ ...params, cursor }, { signal }),
    { placeholderData: (prev) => prev, ...options },
  );
}

/** `GET /v1/admin/users/{id}`. Pass null to disable. */
export function useAdminUser(id: string | null | undefined, options?: QueryOverrides<AdminUser>): UseQueryResult<AdminUser, ApiError> {
  const api = useApi();
  return useApiQuery(queryKeys.admin.users.detail(id ?? ""), ({ signal }) => api.admin.user(id ?? "", { signal }), options, !!id);
}

/** `PATCH /v1/admin/users/{id}`: status (active, suspended, banned) and, for admins, role. */
export function useUpdateUser(): UseMutationResult<AdminUser, ApiError, { id: string } & UpdateUserRequest> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation<AdminUser, ApiError, { id: string } & UpdateUserRequest>({
    mutationFn: ({ id, ...body }) => api.admin.updateUser(id, body),
    onSuccess: (user) => {
      qc.setQueryData(queryKeys.admin.users.detail(user.id), user);
    },
    onSettled: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: queryKeys.admin.users.all() }),
        qc.invalidateQueries({ queryKey: queryKeys.admin.overview() }),
        qc.invalidateQueries({ queryKey: queryKeys.admin.audit.all() }),
      ]);
    },
  });
}

/** `DELETE /v1/admin/users/{id}` (admin role): deletes the account. Irreversible. */
export function useDeleteUser(): UseMutationResult<void, ApiError, string> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.admin.deleteUser(id),
    onSettled: async (_data, _error, id) => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: queryKeys.admin.users.detail(id) }),
        qc.invalidateQueries({ queryKey: queryKeys.admin.users.all() }),
        qc.invalidateQueries({ queryKey: queryKeys.admin.overview() }),
        qc.invalidateQueries({ queryKey: queryKeys.admin.audit.all() }),
      ]);
    },
  });
}

/** `GET /v1/admin/audit?target_type=&target_id=` as an infinite list. Decode `data` with `auditData`. */
export function useAdminAudit(params: AdminAuditListParams = {}, options?: InfiniteOverrides<AuditEntry>): PagedQueryResult<AuditEntry> {
  const api = useApi();
  return usePagedQuery<AuditEntry>(queryKeys.admin.audit.list(listKeyParams(params)), (cursor, signal) => api.admin.audit({ ...params, cursor }, { signal }), options);
}
