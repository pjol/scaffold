import { isDevAuthEnabled } from "@scaffoldapp/shared/auth/env";
import { toWebSocketUrl } from "@scaffoldapp/shared/realtime";

/**
 * Build-time environment. Next inlines NEXT_PUBLIC_* values written as literal `process.env` members, so these
 * constants are identical in proxy.ts, server components and the browser. Server-safe:
 * imports nothing client-only. Every name here is declared in scaffold.json (tier web)
 * and types/env.d.ts.
 */

/** Backend origin. */
export const API_URL = (process.env.NEXT_PUBLIC_API_URL ?? "").replace(/\/+$/, "");

/** Realtime endpoint, derived from the API origin. */
export const WS_URL = API_URL ? toWebSocketUrl(API_URL) : "";

/** development | staging | production. */
export const APP_ENV = process.env.NEXT_PUBLIC_APP_ENV || "production";

/** Clerk publishable key; unset = Clerk is never mounted. */
export const CLERK_PUBLISHABLE_KEY = process.env.NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY ?? "";

/**
 * Dev sign-in allowed: the explicit flag AND a non-production environment. Used only
 * when no Clerk key is set (see AUTH_MODE). Shared by the proxy, server and client code.
 */
export const DEV_AUTH = isDevAuthEnabled(process.env.NEXT_PUBLIC_AUTH_DEV, process.env.NODE_ENV, process.env.NEXT_PUBLIC_APP_ENV);

/**
 * Which auth provider the app mounts. A Clerk key always wins; dev sign-in runs only
 * without one (and only when DEV_AUTH); neither = sign-in isn't configured.
 */
export const AUTH_MODE: "dev" | "clerk" | "none" = CLERK_PUBLISHABLE_KEY ? "clerk" : DEV_AUTH ? "dev" : "none";

/** Public bucket origin (the backend's S3_PUBLIC_URL), for next/image. */
export const S3_PUBLIC_URL = (process.env.NEXT_PUBLIC_S3_PUBLIC_URL ?? "").replace(/\/+$/, "");
