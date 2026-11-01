import { isDevAuthEnabled } from "@scaffoldapp/shared/auth/env";

// Babel inlines public env values only when each `process.env.<NAME>` is spelled out literally, as here.

/** Backend origin (dev-up writes the LAN or localhost URL). Empty = the app shows a config screen. */
export const API_URL = (process.env.EXPO_PUBLIC_API_URL ?? "").trim().replace(/\/+$/, "");

/** Public web origin for links the app shares or opens (invites, events, legal pages). Empty = share app links instead. */
export const WEB_URL = (process.env.EXPO_PUBLIC_WEB_URL ?? "").trim().replace(/\/+$/, "");

/** development | staging | production. */
export const APP_ENV = process.env.EXPO_PUBLIC_APP_ENV || "production";

export const CLERK_PUBLISHABLE_KEY = (process.env.EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY ?? "").trim();

/** Seeded dev identities instead of Clerk: needs the flag and a non-production env. */
export const DEV_AUTH = isDevAuthEnabled(process.env.EXPO_PUBLIC_AUTH_DEV, process.env.NODE_ENV, process.env.EXPO_PUBLIC_APP_ENV);
