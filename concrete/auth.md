# Auth

Clerk is the default identity provider on every tier; the backend owns users, roles and status. Auth is enforced at the router level (route groups), never ad hoc in handlers.

## Flow

1. Client signs in with Clerk (web `@clerk/nextjs`, mobile `@clerk/expo`) and sends `Authorization: Bearer <session token>` on every API call (the shared HTTP client asks `AuthSession.getToken()` per request, refreshes once on 401).
2. Backend `auth.Clerk` verifies the JWT locally (cached JWKS, `azp` must be one of our web origins; native tokens have none).
3. `auth.Authenticator` loads the `users` row by `auth_subject`, provisioning it on first contact from Clerk's **verified** email (no verified email → 403 `email_unverified`).
4. The Identity (user id, email, `admin_role`, `status`, accepted legal versions) rides the request context. Route groups apply guards:

| Group | Guard |
|---|---|
| `/v1` public | none |
| `/v1` user | `Authenticator.Middleware` → user rate limit → `legal.RequireConsent` (mutations) |
| `/v1/admin` | + `RequireAdminRole("support")` + `RequireActive`; admin-only routes add `RequireAdminRole("admin")` |
| `/v1/ws` | WebSocket; token in `?token=` because browsers can't set upgrade headers |
| `/webhooks` | provider signatures only |

5. Clerk webhooks (`/webhooks/clerk`, Svix-signed, `CLERK_WEBHOOK_SECRET`) sync email/name changes and run account deletion on `user.deleted`.

## Roles and status

- `admin_role`: null · `support` (console read + moderation) · `admin` (everything, roles). `ADMIN_EMAILS` grants admin on sign-in; admins grant roles in the console (audited).
- `status`: `active` · `suspended` (can sign in and read, can't mutate — `RequireActive`) · `banned` (refused) · `deleted` (anonymised).
- App-specific roles (org membership, seller, moderator of a group) belong to the feature that owns them (e.g. `orgs`), checked by that feature's guards, never by adding columns to `users`.

## Development auth (dev-up turns it on)

The stack must run with zero third-party keys.
- Backend: `AUTH_DEV_BYPASS=true` (refused unless `APP_ENV=development`) accepts `Bearer dev:<subject>:<email>`; subjects are stored as `dev_<subject>`.
- Clients: **a Clerk publishable key wins.** With a key, Clerk is mounted and its sign-in shown (pasting test keys into `.env.dev` is how you test real Clerk locally). Without a key, `*_AUTH_DEV=true` + a non-production `*_APP_ENV` → `DevAuthProvider`: the sign-in screen shows the dev identity picker (`DEV_IDENTITIES`) and a free-form email field; Clerk's provider and proxy are not mounted at all. Neither → an honest "Sign-in isn't configured" screen. Web and mobile follow the same rule; mobile keeps dev tokens in SecureStore.
- `cmd/seed` creates the dev identities (`admin@<slug>.test` is admin) with legal accepted. `.test` is reserved, so dev mail never reaches anyone.
- Scripts and curl use the same tokens: `curl -H "Authorization: Bearer dev:sam:sam@<slug>.test" …`.

## Web (Next.js)

- `web/proxy.ts` (Next 16 renamed middleware): `clerkMiddleware()` when a publishable key is set; protects nothing by path (the API is the authority) except redirecting signed-out users away from `/(app)` routes to `/sign-in`.
- `<ClerkProvider appearance={clerkAppearance}>` themed from tokens; a `ClerkBridge` adapts `useAuth`/`useUser` into the shared `AuthSession`. Screens only call `useAuthSession()`.
- Sign-in at `/sign-in/[[...sign-in]]` (Clerk `<SignIn>` or the dev picker). Email code or passkey by default; social logins only on request (then Sign in with Apple is required on iOS, compliance.md).
- After sign-in: `useMe()`; if `consent.required`, show the consent screen before anything else.

## Mobile (Expo)

- `@clerk/expo` (not the deprecated `@clerk/clerk-expo`) with `tokenCache` from `expo-secure-store`.
- Custom sign-in screens (email code, passkey, Apple via `expo-apple-authentication` native sheet when social login is on). No web-view auth.
- Production: add both `<scheme>:///` and `<scheme>://` to Clerk's mobile redirect allowlist; register the iOS bundle id and Android package + SHA-256 in Clerk for passkeys.
- On sign-out: unregister the push token, clear the query cache and any per-user offline storage (never share cached data between accounts on one device).

## Swapping providers

Implement `auth.Provider` (`Subject`, `Profile`, `DeleteUser`) for the new provider (e.g. Privy when embedded wallets are needed, or an in-house issuer), add a matching `AuthSession` bridge on web and mobile, and keep everything else. `users.auth_subject` holds whatever the provider calls the user. Never key app data on provider ids outside `users`.

## Agent credentials

AI agents act for a user through one door: the agent endpoint (`/mcp`, the `mcp` feature), where every call is a tool run as the user, marked `auth.WithAgent`, and gated by the user's AI agent scope (`app.AgentPolicy`). The in-app assistant runs tools in-process the same way and holds no credential.
- Every credential minted for an agent (MCP OAuth access/refresh tokens, personal API keys) starts with `auth.AgentCredentialPrefix` (`agt_<slug>_<kind>_…`). `Authenticator.Middleware` refuses it with 401 `agent_credential` before any provider sees it, so it never opens a user route, an upload, the WebSocket or another feature's API.
- OAuth access tokens are audience-bound to the agent endpoint (RFC 8707 `resource`); the endpoint refuses tokens for any other audience and refuses people's session tokens.
- Minting credentials, consenting to a client and changing agent scopes are user routes, so only a person's session reaches them; no agent tool can create, list or widen credentials or scopes.

## Rules

- The API is the only authority: hiding a button is UX, the guard is security.
- Never trust a user id, role or email from a request body; use the Identity.
- Never log tokens. Never put the Clerk secret in `NEXT_PUBLIC_*`.
- Dev bypass must stay impossible outside development: config validation refuses it, and client helpers require a non-production env.
