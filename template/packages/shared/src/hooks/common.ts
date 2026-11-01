"use client";

import { useMutation, useQueryClient, type QueryClient, type UseMutationOptions, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiClient } from "../api/client";
import type { ApiError } from "../api/errors";
import { uploadFile, type UploadInput, type UploadResult } from "../api/upload";
import type { AccountExport, ClientConfig, Me, UpdateMeRequest } from "../types";
import { useApi } from "./context";
import { useApiQuery, type QueryOverrides } from "./internal";
import { queryKeys } from "./keys";

/** `GET /v1/config` (cached 10 min; works signed out). */
export function useConfig(options?: QueryOverrides<ClientConfig>): UseQueryResult<ClientConfig, ApiError> {
  const api = useApi();
  return useApiQuery(queryKeys.config(), ({ signal }) => api.config({ signal }), { staleTime: 10 * 60_000, ...options });
}

/** `GET /v1/me`. Disable while signed out: `useMe({ enabled: session.isSignedIn })`. */
export function useMe(options?: QueryOverrides<Me>): UseQueryResult<Me, ApiError> {
  const api = useApi();
  return useApiQuery(queryKeys.me(), ({ signal }) => api.me({ signal }), options);
}

interface MeSnapshot {
  previous: Me | undefined;
}

/** Mutation options for {@link useUpdateMe}: optimistic patch + rollback (exported for tests). */
export function updateMeMutation(api: ApiClient, qc: QueryClient): UseMutationOptions<Me, ApiError, UpdateMeRequest, MeSnapshot> {
  const key = queryKeys.me();
  return {
    mutationFn: (body) => api.updateMe(body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<Me>(key);
      if (previous) {
        const next: Me = { ...previous };
        if (body.display_name !== undefined) next.display_name = body.display_name.trim();
        if (body.avatar_url !== undefined) next.avatar_url = body.avatar_url || null;
        qc.setQueryData<Me>(key, next);
      }
      return { previous };
    },
    onSuccess: (me) => {
      qc.setQueryData(key, me);
    },
    onError: (_error, _body, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
    },
  };
}

/** `PATCH /v1/me` (name, avatar): optimistic on the `me` cache, rolled back on failure. */
export function useUpdateMe(): UseMutationResult<Me, ApiError, UpdateMeRequest, MeSnapshot> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation(updateMeMutation(api, qc));
}

/**
 * `GET /v1/me/export`: resolves to the export object. The app saves it (web: a Blob
 * download named `account-export.json`; native: write a file and open the share sheet).
 */
export function useExportAccount(): UseMutationResult<AccountExport, ApiError, void> {
  const api = useApi();
  return useMutation<AccountExport, ApiError, void>({ mutationFn: () => api.exportAccount() });
}

/**
 * `DELETE /v1/me`: deletes the account. On success every cached query is dropped; call
 * `session.signOut()` next (pass it as `onSuccess` to `mutate`).
 */
export function useDeleteAccount(): UseMutationResult<void, ApiError, void> {
  const api = useApi();
  const qc = useQueryClient();
  return useMutation<void, ApiError, void>({
    mutationFn: () => api.deleteAccount(),
    onSuccess: async () => {
      await qc.cancelQueries();
      qc.removeQueries();
    },
  });
}

/** Presign + PUT a file (web Blob/File or native `{ uri }`); resolves to `{ key, public_url }`. */
export function useUpload(): UseMutationResult<UploadResult, ApiError, UploadInput> {
  const api = useApi();
  return useMutation<UploadResult, ApiError, UploadInput>({ mutationFn: (input) => uploadFile(api.http, input) });
}
