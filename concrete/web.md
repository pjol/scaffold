# Web

Next.js 16 App Router app in `web/` (`@<slug>/web`): React 19.2, Tailwind 4.3, Turbopack, `motion`, Radix, lucide, sonner, Clerk 7. The template's code is the reference for every rule here; read the file before changing a pattern. Next 16 differs from older versions (`proxy.ts`, promised `params`, error boundaries get `retry`): read `node_modules/next/dist/docs/` before Next-specific work.

## Shape

```
web/
  proxy.ts               Clerk middleware when a publishable key is set; otherwise passes through
  next.config.ts         transpilePackages, images.remotePatterns, agentRules off
  types/env.d.ts         every env var the app reads (dot access type-checks)
  app/
    layout.tsx           Inter (next/font → --app-face-body), ThemeScript, metadata from APP_NAME
    globals.css          @import "tailwindcss"; @import "@<slug>/tokens/theme.css"; @source ui-web
    (public)/            sign-in/[[...sign-in]] · legal/[doc] · delete-account · support (PublicFrame)
    (app)/               layout = AppFrame (gates + shell) · page = Home · settings/* · admin/*
    not-found.tsx error.tsx global-error.tsx mark.svg/route.ts
  src/
    providers.tsx        UIProvider > AuthProvider > DataProviders (ApiProvider) + Toaster
    lib/                 env, api, app (APP_NAME), redirect, url-state, format, images, download
    features/registry.tsx  the anchors features wire into
    components/          auth/ shell/ legal/ account/ admin/  (app-level building blocks)
    screens/             home/ settings/ admin/ public/  (one component per screen)
```

Route files are thin: metadata plus one screen component from `src/screens` (features: `src/features/<name>/`). `packages/ui-web` holds primitives (its README is the inventory); app-specific composition lives in `src/components`.

## Providers and auth

- `AUTH_MODE` (`src/lib/env.ts`): `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY` set → Clerk (`ClerkProvider` themed by `clerkAppearance(palette, mode)` + `ClerkBridge` → `AuthSessionProvider`). No key and `isDevAuthEnabled(NEXT_PUBLIC_AUTH_DEV, NODE_ENV, NEXT_PUBLIC_APP_ENV)` → `DevAuthProvider` (identity in localStorage `<slug>.dev-identity`). Neither → nobody is signed in and /sign-in says "Sign-in isn't configured". Clerk is never mounted without a key.
- Screens only call `useAuthSession()`; sign out through `useSignOut()` (clears the query cache, navigates).
- One API client per app (`createWebApiClient`, `NEXT_PUBLIC_API_URL`, platform `web`); its `getToken` waits up to 2.5 s for auth to load so the first request never caches a 401. One realtime client per signed-in identity; `bindVisibilityLifecycle` reconnects when the tab returns. Switching identity or signing out resets user queries (`resetQueries`, never `removeQueries` under mounted screens).
- `proxy.ts` sends signed-out visitors of app routes to /sign-in (Clerk only). `PUBLIC_PATHS` lists the public ones as regex sources matched against the whole path; a feature with public pages adds one line above `// scaffold:public-paths` (`"/invites/(.*)",`). The API stays the authority.

## Routing and gates

`(app)/layout.tsx` → `AppFrame`, in order:
1. Session loading → `FrameSkeleton`; signed out → `/sign-in?redirect_url=<path>` (`safeRedirect` allows same-app paths only).
2. `useMe()` failing on first load → `AccountBlocked` (banned, unverified email, ended session, unreachable API + Retry).
3. `me.consent.required` → `ConsentScreen` (`ConsentForm` with the shared documents, contact email filled from `/v1/config`). It submits the bundled versions; a 409 means the build is stale and the banner offers Reload.
4. The shell (`AppShell`): side nav ≥ 1024 (Home · `featureNav` · Settings · Admin for staff), bottom tabs < 1024 (Home · up to 3 features · Settings; overflow goes to the account menu), top bar (name, account menu). `me.status === "suspended"` shows a warning banner.

`(public)` pages use `PublicFrame` (name, Sign in / Open app, footer with legal links and the support address). `/delete-account` is Play's web deletion URL: steps and what is deleted and kept, signed out → sign in, signed in → the same `DeleteAccountDialog`, plus the support email. Root `error.tsx`, `(app)/error.tsx` (keeps the shell) and `global-error.tsx` (no providers; inline `tokensStylesheet()`) all offer Retry.

## Data layer

- Every read and write goes through `@<slug>/shared` hooks (`useMe`, `useUpdateMe`, `useUpload`, admin hooks, feature hooks). Never `fetch` in a screen; never a second API client.
- Query keys come from the shared factories (`queryKeys`, `<feature>Keys`); lists use `usePagedQuery` with keyset cursors and `LoadMore`.
- Realtime cache updates live in shared (`registerRealtimeHandler`, `registerReconnectRefetch`); screens subscribe with `useTopic(topic(kind, id))` and use `useRealtimeEvent` only for component-local reactions (a toast).
- Optimistic updates belong in the shared mutation (`onMutate` snapshot, rollback `onError`, server result `onSuccess`), as `useUpdateMe` does. Screens show `toast.success` / `toast.error(errorMessage(e, fallback))`.
- Filters and tabs live in the URL (`useUrlState`); wrap pages that read search params in `<Suspense>`.

## Registry (`src/features/registry.tsx`)

`scaffold add` inserts one line above each anchor. An anchor is a comment that opens with the marker (`// scaffold:nav`); keep those lines verbatim. Mentions in doc comments are ignored.

| Anchor | Inside | Shape |
|---|---|---|
| `// scaffold:imports` | top level | `import { chatNav } from "./chat/nav";` |
| `// scaffold:nav` | `featureNav: NavItem[]` | `{ href, label, icon: LucideIcon, match?, useBadge? }` |
| `// scaffold:home` | `homeTiles: HomeTile[]` | NavItem without `match` (tiles on Home) |
| `// scaffold:admin-nav` | `adminNav: NavItem[]` | admin tabs after Overview · Users · Audit log |
| `// scaffold:settings` | `settingsSections: SettingsSection[]` | `{ href, label, icon }`, shown between Appearance and Account |
| `// scaffold:account-menu` | `accountMenuItems: AccountMenuItem[]` | link `{ id, href, label, icon }` or slot `{ id, Component }` (renders its own `MenuLabel`/`MenuItem`/`MenuSeparator`, e.g. an org switcher); above Settings and Sign out |
| `// scaffold:session` | `sessionMounts: ComponentType[]` | mounted once inside the data providers while signed in (realtime listeners, notification toasts); remount on identity change |
| `// scaffold:header-actions` | `headerActions: { id, Component }[]` | top-bar controls before the account menu (a bell); 40 px labelled targets |
| `// scaffold:consent-routes` | `consentRoutes: string[]` | whole-path regex sources (`"/support(/.*)?"`) AppFrame shows instead of the consent screen |
| `// scaffold:support-links` | `supportLinks: { id, href, label, icon }[]` | consent screen, suspended banner, blocked-account screen (next to the support email), Settings → Support. Add the route to `consentRoutes` if it must open before consent; point at a public page if banned users need it |

`useBadge` (nav, home tiles, admin tabs) is a hook returning a count; it runs on every render, so read a cache, never fetch.

## Settings and admin

- Settings: side tabs ≥ 1024 (`/settings` shows Profile there), a list on phones with a back link on each section. Order: Profile (photo via `useUpload` purpose `avatar`, name) · Appearance (System/Light/Dark; palette picker when the build ships more than one) · feature sections · Account (Download my data → JSON file; Delete account: 3 taps, `requireText="DELETE"`, says what is deleted and kept, then signs out) · Legal · Support · Admin (staff).
- Admin (`/admin`, `data-density="compact"`): staff only by `me.admin_role` (the API guards every route). Overview (counts, recent actions) · Users (search, status filter, keyset list, detail: status for staff, role and delete for admins, never yourself) · Audit log (filter by target, "only this target") · `adminNav`. Every change confirms in a `ConfirmDialog` naming the person and the consequence.

## Screens, states and copy

- Page container `mx-auto w-full max-w-content px-gutter py-section`; `Screen` gives title → body 24 and `gap-section` between sections; section heading + content in one `gap-3` stack (`Section`). Forms `max-w-form`.
- Every async view: skeleton shaped like the result, `EmptyState` (title + one action), `ErrorState` (what failed + Retry). Interactive rows never contain other controls (no `info` on them).
- Copy follows writing.md: no subtitles (context in `info`), sentence case, numerals, verbs on buttons, errors say what to do. Use `APP_NAME`, never the literal.
- Tokens only; check 375 px and 1440 px, light and dark, keyboard focus, reduced motion.

## Images

`next/image` optimises only `images.remotePatterns`: Clerk (`img.clerk.com`, `images.clerk.dev`) and the public bucket (`NEXT_PUBLIC_S3_PUBLIC_URL`, the backend's `S3_PUBLIC_URL`). `src/lib/images.ts` mirrors that list and renders anything else `unoptimized` (development uploads from the API), so an unexpected host never breaks a page. Avatars go through `AppAvatar`; pass `eager` for above-the-fold ones. `qualities: [75]`.

## Performance

Route files and layouts are server components (metadata, static params); screens are client components because they read session-bound data. Static routes prerender (`○`); only `/sign-in` and dynamic ids render on demand. Keep heavy client code out of the root layout, import icons individually from `lucide-react`, and lazy-load large optional widgets (`next/dynamic`). The palette's fonts are the only web fonts.

## Verification

`pnpm typecheck`, `pnpm check`, `pnpm --filter @<slug>/web build` pass; `scaffold doctor` is clean (anchors, env declarations in `types/env.d.ts` + `scaffold.json`, no raw colours or banned words). Then on the dev-up stack (or backend + `next dev`): sign in as a member and as the admin, save the profile, switch appearance, export, open delete, accept consent as a new email, change a user's status and see it in the audit log; screenshots at 375 and 1440 in light and dark against the design.md checklist; the browser console stays clean.
