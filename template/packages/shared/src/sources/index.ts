/**
 * Connected platforms, as every feature presents them. Content in a feature is either the
 * app's own (source type "app") or comes
 * live from a person's connected account on an outside platform. Features return sources
 * in these shapes and put a SourceRef on every row, so screens can badge, filter and group
 * connected content the same way everywhere (ui-web / ui-native: ProviderIcon, SourceBadge,
 * SourceFilter, ConnectPrompt). Backend counterpart: app.Connections / app.Capability.
 */

import { buildQueryString } from "../api/query-string";

/** Platform ids known to the connections registry; apps may add more. */
export type SourceProvider =
  | "google"
  | "microsoft"
  | "github"
  | "gitlab"
  | "notion"
  | "hubspot"
  | "salesforce"
  | "zoom"
  | (string & {});

/** Where one row came from. Rows without a source are the app's own. */
export type SourceRef =
  | { type: "app" }
  | {
      type: "connected";
      provider: SourceProvider;
      /** The connection (one person's account on the platform). */
      connection_id: string;
      /** Display name of the account: sam@acme.com, acme-org, Acme. */
      account: string;
      /** Product within the platform when it matters: "drive", "sharepoint", "calendar". */
      product?: string;
      /** Opens the item on the platform, when it has a page there. */
      url?: string;
    };

/** One place a feature can show content from, for sidebars and filters. */
export interface FeatureSource {
  /** "app" for the app's own content; the connection id otherwise. */
  id: string;
  type: "app" | "connected";
  provider?: SourceProvider;
  product?: string;
  account?: string;
  /** What people see: "Google Drive", "Notion · Acme". */
  label: string;
  status?: "active" | "needs_reauth" | "blocked";
  capability?: string;
  /** App path to reconnect or grant more access, when status isn't active. */
  fix_url?: string;
}

/** A platform capability the person could still connect for this feature. */
export interface ConnectionOffer {
  capability: string;
  provider: SourceProvider;
  label: string;
  connect_url: string;
}

/** What a feature's `GET /v1/<feature>/sources` returns. */
export interface FeatureSources {
  sources: FeatureSource[];
  offers: ConnectionOffer[];
}

/** Product names for providers, for badges and copy (whitelabel apps keep these factual). */
export const SOURCE_PROVIDER_LABELS: Record<string, string> = {
  google: "Google",
  microsoft: "Microsoft",
  github: "GitHub",
  gitlab: "GitLab",
  notion: "Notion",
  hubspot: "HubSpot",
  salesforce: "Salesforce",
  zoom: "Zoom",
};

/** The source a row belongs to, treating a missing source as the app's own. */
export function sourceOf(row: { source?: SourceRef | null }): SourceRef {
  return row.source ?? { type: "app" };
}

/** True when a row comes from a connected platform. */
export function isConnectedSource(row: { source?: SourceRef | null }): boolean {
  return row.source?.type === "connected";
}

/** Product names within a platform, for badges and copy ("Google Drive"). Keys: `<provider>.<product>`. */
export const SOURCE_PRODUCT_LABELS: Record<string, string> = {
  "google.drive": "Google Drive",
  "google.docs": "Google Docs",
  "google.sheets": "Google Sheets",
  "google.calendar": "Google Calendar",
  "google.meet": "Google Meet",
  "google.people": "Google Contacts",
  "microsoft.onedrive": "OneDrive",
  "microsoft.sharepoint": "SharePoint",
  "microsoft.outlook": "Outlook",
  "microsoft.calendar": "Outlook Calendar",
  "microsoft.teams": "Microsoft Teams",
  "zoom.meetings": "Zoom",
};

/** What people call a source: the product when it has its own name, else the platform ("Google Drive", "GitHub"). */
export function sourceProductLabel(provider: string, product?: string | null): string {
  if (product) {
    const named = SOURCE_PRODUCT_LABELS[`${provider}.${product}`];
    if (named) return named;
  }
  return SOURCE_PROVIDER_LABELS[provider] ?? provider.charAt(0).toUpperCase() + provider.slice(1);
}

/** "Google Drive · sam@acme.com" for a connected source; the app's name for the app's own. */
export function sourceLabel(source: SourceRef | null | undefined, appName: string): string {
  if (!source || source.type !== "connected") return appName;
  const name = sourceProductLabel(source.provider, source.product);
  return source.account ? `${name} · ${source.account}` : name;
}

/**
 * Error codes the backend uses for connected-platform failures (Go: app.Connection*). Show the fix by
 * code: `connection_needs_auth` and `connection_scope_missing` link to Settings → Connections
 * (`connectionFixPath`), `connection_blocked` says the workspace doesn't allow it.
 */
export const CONNECTION_ERROR_CODES = [
  "connection_needs_auth",
  "connection_scope_missing",
  "connection_blocked",
  "connection_not_found",
  "connection_unavailable",
  "connection_rate_limited",
  "connection_host_refused",
  "connection_response_too_large",
] as const;
export type ConnectionErrorCode = (typeof CONNECTION_ERROR_CODES)[number];

/** True for a connected-platform error code. */
export function isConnectionErrorCode(code: string | null | undefined): code is ConnectionErrorCode {
  return !!code && (CONNECTION_ERROR_CODES as readonly string[]).includes(code);
}

/** The app path that fixes a connection for a capability, returning to `returnTo` afterwards. */
export function connectionFixPath(capability?: string | null, returnTo?: string | null): string {
  const safeReturn = returnTo && returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : undefined;
  return `/settings/connections${buildQueryString({ capability: capability || undefined, return_to: safeReturn })}`;
}
