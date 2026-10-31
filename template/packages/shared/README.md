# @scaffoldapp/shared

The client contract for web and mobile: DTOs, API client, auth session, realtime and react-query hooks, legal documents. Consumed as TypeScript source. Library code must run in browsers and Hermes (`pnpm check` enforces it).

## What's exported

| Subpath | Contents |
|---|---|
| `/types` | Core DTOs, snake_case, matching the Go structs: `Me`, `Consent`, `ClientConfig`, `Upload`, `AdminUser`, `AuditEntry`, `Page<T>`, `ApiErrorCode`, `RealtimeEvent`, `RealtimeEventMap` |
| `/api` | `createApiClient({ baseUrl, getToken, platform, version?, onUnauthorized? })` → `config, me, updateMe, acceptConsents, exportAccount, deleteAccount, presignUpload, upload, admin.{overview, users, user, updateUser, deleteUser, audit}`, plus `http`; `ApiError`, `isApiError`, `hasErrorCode`, `isConsentRequired`, `isNetworkError`, `errorMessage`; `uploadFile`, `newIdempotencyKey`, `endpointsFor`, `auditData` |
| `/realtime` | `createRealtimeClient({ url, getToken })`, `toWebSocketUrl(apiBase)`, `topic(kind, id)` |
| `/hooks` | `ApiProvider({ api, realtime, queryClient })`, `createQueryClient`, `queryKeys`, `useApiQuery`, `usePagedQuery`, `useConfig`, `useMe`, `useUpdateMe`, `useConsentGate`, `useAcceptConsents`, `useExportAccount`, `useDeleteAccount`, `useUpload`, admin hooks, `useTopic`, `useRealtimeEvent`, `useEndpoints`, `registerRealtimeHandler`, `bindAppStateLifecycle`, `bindVisibilityLifecycle` |
| `/auth` | `AuthSession`, `AuthSessionProvider`, `useAuthSession`, `DevAuthProvider({ storage })`, `useDevAuth().signInAs`, `isDevAuthEnabled(flag, nodeEnv, appEnv)` |
| `/dev` | `DEV_IDENTITIES` (same as `backend/internal/auth/dev.go`), `devToken` |
| `/legal` | `TERMS_OF_SERVICE`, `PRIVACY_POLICY`, `TERMS_VERSION`, `PRIVACY_VERSION`, `applyLegalPlaceholders`, Markdown export |

## Adding a feature

Put everything in `src/<feature>/` and add `export * from "./<feature>";` above the `// scaffold:exports` line in `src/index.ts`. No core file changes:

- Types: `src/<feature>/types.ts`, snake_case, same commit as the Go structs.
- Endpoints: `export const chatEndpoints = (http: HttpClient) => ({ … })`; hooks call `useEndpoints(chatEndpoints)`.
- Keys: a local factory (`chatKeys.all()` = `["chat"]`); queries go through `useApiQuery` / `usePagedQuery`.
- Realtime: type the payload with `declare module "../types" { interface RealtimeEventMap { "chat.message": ChatMessage } }`, then at module level in the hooks file `registerRealtimeHandler("chat.message", (qc, e) => …)` and `registerReconnectRefetch(chatKeys.all())`. Subscribe in screens with `useTopic(topic("chat", id))`.

## Commands

- `pnpm check`: portability scan (`scripts/portability.mjs`) and legal version check against `backend/internal/legal/legal.go`.
- `pnpm --filter @scaffoldapp/shared legal:export`: write `docs/legal/terms.md` and `privacy.md`. Once they exist, `check` fails when they are stale.
- Legal text is a template: fill the placeholders (`[Company legal name]`, `[Company address]`, `[Contact email]`, `[Governing jurisdiction]`, `[Hosting provider]`) and have counsel review before launch. Changing a document means bumping its version here and in Go.
