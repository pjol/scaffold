# Mobile

Expo app in `mobile/` (`@<slug>/mobile`): SDK 57, expo-router with typed routes, New Architecture (the only one), Hermes, dev builds. Components come from `packages/ui-native`. The template is the reference for every rule here; read the file before changing a pattern.

## Shape

```
mobile/
  app.config.ts      name, scheme, bundle id/package, icons, splash, privacy manifest, blocked permissions, plugins (anchor `scaffold:plugins`)
  plugins/           config plugins features ship (`<name>.js`), listed in app.config.ts
  eas.json           development · development-simulator · preview · production
  metro.config.js    monorepo: watches the workspace root, resolves the hoisted node_modules
  app/               routes, thin: _layout.tsx (providers + gates), (auth)/sign-in, consent,
                     (tabs)/{_layout,index,settings}, settings/{profile,appearance,account},
                     blocked, legal/[doc], support, +not-found, +native-intent
  src/lib/           env, auth (Clerk/dev bridge), api, session (sign-out), storage, preferences, version, legal
  src/components/    boot screens, update gate, legal links, modal close
  src/features/      registry.tsx + one folder per installed feature
  assets/images/     committed PNGs; `pnpm --filter @<slug>/mobile icons` redraws them from the palette
```

## Providers and gates

`app/_layout.tsx`: the splash holds until fonts (`useAppFonts`) and the saved appearance load, then `GestureHandlerRootView` › `SafeAreaProvider` › `ThemeProvider` (palette + System/Light/Dark through `Appearance.setColorScheme`) › `AuthRoot` › `ApiLayer` › `RootNavigator` + `ToastHost`.

- **AuthRoot**: Clerk when `EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY` is set; otherwise `DevAuthProvider` (SecureStore) when `EXPO_PUBLIC_AUTH_DEV=true` in a non-production env; otherwise a "Sign-in isn't configured" screen. Screens call only `useAuthSession()`.
- **ApiLayer**: `createApiClient` (platform, `X-Client-Version`), realtime only while signed in, `bindAppStateLifecycle`, cache cleared when the user changes, full sign-out when the API refuses a refreshed token. Renders the features' `sessionMounts` once while signed in (push lifecycle, badges); session work never hides inside a Home row's badge hook.
- **Gates, in order**: (1) update: `/v1/config` `min_app_version[platform]` newer than the build → unpassable `UpdateRequired` with a store link (the splash waits ≤3 s for config; a late answer still gates); (2) auth: `Stack.Protected` leaves only `(auth)`, `legal`, `+not-found`; (3) consent: `me.consent.required` → `consent`; an account the API refuses (`account_suspended` on `/v1/me`: banned or deleted) → `blocked` (support email, `supportLinks`, sign out). `consentRoutes` stay reachable in both. Signed in and `me` loading → `BootLoading`; any other failure → `BootError` (retry, sign out). Suspended accounts (read-only) see a Banner on Home pointing to Support.
- Routes features add are signed-in and consented through the redirect effect in `RootNavigator`. Public routes are listed in `publicRoutes` (`src/features/registry.tsx`, anchor `scaffold:public-routes`, one route per line in file-path form: `"invites/[token]",`); a public screen works without a token.
- **Resume after the gates**: a link that arrives while sign-in or consent is pending (`app/+native-intent.tsx` sees every system link) or a route the gate redirected from is remembered (`src/lib/pending-route.ts`: this app's scheme or plain paths only, never the gate screens, 30-minute expiry, kept across an app kill) and opened once every gate is passed. A public screen that sends the user to sign in calls `rememberRoute(useUnstableGlobalHref())` first.

## Navigation

- **Tabs**: `expo-router/js-tabs` with ui-native `TabBar`; 5 at most, labelled icons, Settings last. Features insert `<Tabs.Screen>` above `{/* scaffold:tabs */}` (imports above `// scaffold:imports`). Tab screens use `Screen edges={["top"]}` and `useScrollToTop(ref)`.
- **Stacks**: the root Stack shows native headers (themed, title from options). Pushed screens use `Screen edges={["bottom"]}`; their title is set in `_layout.tsx` or with `<Stack.Screen options>` in the file.
- **Modals and sheets**: `presentation: "modal"` on iOS, `slide_from_bottom` on Android, always with `HeaderClose`. Create/edit flows and pickers use `Sheet`; start a system picker from `onDismissed`, never while the sheet is closing.
- **Deep links**: `<slug>://<path>` opens the route; behind a closed gate it opens after sign-in and consent. Notifications and emails carry app paths.
- **Registry** (`src/features/registry.tsx`, names match web's): `homeEntries` (`scaffold:home`) and `settingsSections` (`scaffold:settings`) with entries `{ id, title, icon, href, info?, useBadge?, staffOnly? }`; `publicRoutes` (`scaffold:public-routes`) and `consentRoutes` (`scaffold:consent-routes`), route strings in file-path form; `supportLinks` (`scaffold:support-links`) `{ id, title, icon, href }`, href an app path or an external URL, shown on Settings → Support, consent and blocked (a link to an app route also lists it in `consentRoutes`); `sessionMounts` (`scaffold:session`) `ComponentType[]`. Every inserted line must be unique in the file (one constant per entry). Settings order: Profile · Appearance · feature sections · Account · Legal · Support · Sign out. No admin on mobile: staff see a note.

## Data and session

- Data only through `@scaffoldapp/shared` hooks; every async view has skeleton, empty and error states.
- Env: `EXPO_PUBLIC_*` read as literals in `src/lib/env.ts` (no `extra`). Nothing secret in the app.
- Sign-out is one path, `useSignOut()`: hooks from `registerSignOutHook` (token still valid, 5 s cap each; the notifications feature unregisters push here) → `session.signOut()` → cancel and clear the query cache → `clearUserStorage()`. Account deletion runs it after `DELETE /v1/me`.
- Storage: SecureStore via `readJson`/`writeJson`, small values. Device keys (appearance, palette, dev identity) survive sign-out; per-user data goes only in `userStorage` or `userDirectory()`, wiped on sign-out.

## Permissions

- Ask at the moment of use, never at launch; a denial leaves a working fallback and a Settings nudge. Never require push or location.
- Each permission has a purpose string in the plugin options naming the app and the reason. Camera and photos are worded for every feature that takes photos (`Allow Scaffold App to use your camera to take photos you choose to share.`), so features offer the camera without config edits; any other permission ships with its own string in the same change.
- Photos go through the system picker (`pickPhotos`), which needs no library permission. `android.blockedPermissions` drops `READ_MEDIA_*`, storage, `RECORD_AUDIO` and `SYSTEM_ALERT_WINDOW`; check the merged manifest before each release.

## Dev builds

- Dev builds only (`expo-dev-client`); Expo Go can't load the native modules (Clerk passkeys, secure store). Rebuild after any native change: `pnpm --filter @<slug>/mobile exec expo run:ios` (Xcode ≥ 26.4 for SDK 57) or `eas build --profile development-simulator`.
- dev-up runs Metro on base+3 with `--host localhost` (`--lan`/`--tunnel` for devices) and `--ios`/`--android` opens the installed build via `<scheme>://expo-development-client/?url=…`.
- `ios/` and `android/` are generated and gitignored. Never commit a prebuild; native config lives in `app.config.ts` and config plugins. A feature that needs native config (deployment target, permission strings, map keys) ships `mobile/plugins/<name>.js` and wires `"./plugins/<name>",` above `scaffold:plugins`; rebuild the dev build after installing it.

## EAS, versions, updates

- Profiles: `development` (dev client, internal), `development-simulator`, `preview` (internal), `production`. All auto-increment with `appVersionSource: "remote"`: EAS owns build numbers; never set `buildNumber`/`versionCode` or `autoIncrement: false` (iOS keeps the old binary on a duplicate build). Each profile reads `EXPO_PUBLIC_*` from its EAS environment (`eas env:create`).
- Marketing version: `VERSION` in `app.config.ts`; bump it for each store release.
- Forced update: raise `MOBILE_MIN_VERSION_IOS`/`_ANDROID` only after the fixed build is live in the store.
- **No OTA without a production build.** `expo-updates` is not installed; changes ship as new binaries. Adding OTA later needs a production build carrying the same runtime first.

## Clerk

- `@clerk/expo` 4 (never the deprecated `@clerk/clerk-expo`), `tokenCache` from `@clerk/expo/token-cache`, custom screens, no web views: email code (an unknown email becomes a sign-up verified by the same code) and passkeys (`signIn.passkey` → `finalize`; Settings → Account → Add a passkey).
- Clerk instance: email code on, passkeys on, sign-up asks for nothing beyond email.
- Production: Native applications → add `<scheme>:///` and `<scheme>://` to the mobile redirect allowlist; register the iOS Team ID + bundle id and the Android package + SHA-256 (EAS credentials) for passkeys. `app.config.ts` adds `webcredentials:<frontend-api>`, decoded from the publishable key.
- Any social login → Sign in with Apple too (compliance.md): add the `@clerk/expo` plugin and `expo-apple-authentication`.
- `buffer` is a direct dependency because `@clerk/expo-passkeys` requires it without declaring it.

## Store submission

`compliance.md` is the checklist. Mobile specifics the template covers: privacy manifest (UserDefaults `CA92.1`, FileTimestamp, SystemBootTime, DiskSpace; `NSPrivacyTracking: false`), purpose strings, deletion in 3 taps (Settings → Account → Delete account), data export to the share sheet, support email, `ITSAppUsesNonExemptEncryption: false`, iPhone only (`supportsTablet: false`; turning it on means iPad screenshots and layouts). Bundle id and scheme are permanent once a store record exists: rename before the first submission.

## Platform notes

- Android 16 (targetSdk 36) is always edge-to-edge: `Screen` puts insets on content padding, `TabBar` pads the bottom inset, `expo-system-ui` paints the root background, predictive back is on.
- `expo-maps` (places feature) needs iOS 17: the places config plugin raises the deployment target.
- Upgrade the SDK with `npx expo install --check`, then expo-doctor; move to 58 once Expo Go 58 ships (stack.md).

## Verification

`pnpm typecheck` and `pnpm check` at the root; in `mobile/`: `npx expo config --type public`, `npx expo-doctor` (only a local Xcode version check may fail), `npx expo export --platform ios` and `--platform android` into a temp dir (proves Metro resolves the monorepo; delete the output). Then on a dev build: light and dark, largest Dynamic Type, a 375-wide phone, sign-in → consent → tabs → profile photo → export → delete account.
