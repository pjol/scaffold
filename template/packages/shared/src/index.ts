/**
 * @scaffoldapp/shared: the client contract shared by web and mobile.
 *
 * - `types`     canonical DTOs for the core API (also at `@scaffoldapp/shared/types`)
 * - `api`       `createApiClient`, `ApiError` + helpers, uploads, `endpointsFor`
 * - `realtime`  `createRealtimeClient` (reconnecting, ref-counted WebSocket), topics
 * - `hooks`     `ApiProvider`, react-query hooks, `registerRealtimeHandler`, lifecycle binders
 * - `auth`      `useAuthSession` (Clerk bridge or dev shim), `DevAuthProvider`
 * - `dev`       `DEV_IDENTITIES`, the seeded dev sign-ins
 * - `legal`     Terms and Privacy as structured data (`TERMS_VERSION`, `PRIVACY_VERSION`)
 * - `utils`     `generateId`, `stableStringify`, `computeBackoff`
 *
 * Server-safe subpaths (no React): `/api`, `/realtime`, `/types`, `/legal`, `/dev`, `/auth/env`, `/utils`.
 * Features add `src/<feature>/` and one export line above the anchor below.
 */
export * from "./types";
export * from "./api";
export * from "./realtime";
export * from "./hooks";
export * from "./auth";
export * from "./dev";
export * from "./legal";
export * from "./utils";
export * from "./sources";
// scaffold:exports
