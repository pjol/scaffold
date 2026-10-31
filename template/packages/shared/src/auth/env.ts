/**
 * Server-safe auth helpers (no React, no "use client"): callable from Next's proxy,
 * server components, route handlers and native code alike. Also re-exported from
 * `@scaffoldapp/shared/auth` and `@scaffoldapp/shared`.
 */

/**
 * Whether the dev auth shim may run. Requires the explicit flag AND a non-production
 * app environment, so a stray flag can never disable real auth in production.
 *
 * `appEnv` (`NEXT_PUBLIC_APP_ENV` / `EXPO_PUBLIC_APP_ENV`) lets optimized local builds,
 * which always run with NODE_ENV=production, keep the shim (dev-up sets
 * `development`). Unset, it falls back to NODE_ENV, so production builds stay locked down.
 * The backend refuses dev tokens outside APP_ENV=development regardless.
 *
 * Pass the env values at the call site so bundlers inline them.
 */
export function isDevAuthEnabled(flag: string | undefined, nodeEnv: string | undefined, appEnv?: string): boolean {
  const env = appEnv || nodeEnv;
  return flag === "true" && env !== "production" && env !== undefined && env !== "";
}
