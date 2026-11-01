/**
 * Query keys for core data. Prefix keys (`all`) are for invalidation:
 *
 * ```ts
 * queryClient.invalidateQueries({ queryKey: queryKeys.admin.users.all() })
 * ```
 *
 * Features keep their own key factory next to their hooks (`chatKeys.all()` = `["chat"]`).
 */
export const queryKeys = {
  config: () => ["config"] as const,
  me: () => ["me"] as const,
  admin: {
    all: () => ["admin"] as const,
    overview: () => ["admin", "overview"] as const,
    users: {
      all: () => ["admin", "users"] as const,
      list: (params: Record<string, unknown> = {}) => ["admin", "users", "list", params] as const,
      detail: (id: string) => ["admin", "users", "detail", id] as const,
    },
    audit: {
      all: () => ["admin", "audit"] as const,
      list: (params: Record<string, unknown> = {}) => ["admin", "audit", "list", params] as const,
    },
  },
} as const;

/** Strip pagination fields and empty members from list params (stable keys). */
export function listKeyParams(params: object | undefined): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (!params) return out;
  for (const [k, v] of Object.entries(params)) {
    if (k === "cursor" || v === undefined || v === null || v === "") continue;
    out[k] = v;
  }
  return out;
}
