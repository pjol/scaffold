/**
 * Where installed features plug into the app shell. `scaffold add` inserts each line above
 * the matching anchor comment below (anchors are API: keep them, and never write an
 * anchor's full marker anywhere else in this file, or the CLI inserts there). Names match
 * web's registry.
 *
 *   anchor           example line                               list
 *   imports          import { chatHome } from "./chat/registry";  (imports)
 *   home             chatHome,                                   homeEntries: rows on Home
 *   settings         chatSettings,                               settingsSections: groups in Settings
 *   public-routes    "invites/[token]",                          publicRoutes: reachable signed out
 *   consent-routes   "support/new",                              consentRoutes: reachable before consent / while blocked
 *   support-links    supportTicketLink,                          supportLinks: consent, blocked and Support screens
 *   session          PushLifecycle,                              sessionMounts: rendered once while signed in
 *
 * A feature's own `src/features/<name>/registry.tsx` exports the objects:
 *
 *   export const chatHome: HomeEntry = { id: "chat", title: "Messages", icon: MessageCircle, href: "/chat", useBadge: useUnreadChatCount };
 *   export const chatSettings: SettingsSection = { id: "chat", items: [{ id: "chat", title: "Chat", icon: MessageCircle, href: "/chat/settings" }] };
 *
 * Settings renders: Profile · Appearance · <these sections, in order> · Account · Legal · Support.
 * Once any section has a `group`, it renders headed groups instead: You (Profile, Appearance,
 * its sections, Account) · Workspace · AI agents · Developer · sections without a group (each
 * under its own `title`, untitled ones under "Other") · Legal · Support.
 * A feature that needs a bottom tab wires `app/(tabs)/_layout.tsx` instead (5 tabs at most).
 */
import type { Href } from "expo-router";
import type { LucideIcon } from "lucide-react-native";
import type { ComponentType, ReactNode } from "react";
// scaffold:imports

export interface HomeEntry {
  /** Stable id (the feature name). */
  id: string;
  /** Sentence case. No subtitle: context goes in `info`. */
  title: string;
  icon: LucideIcon;
  /** Route the row opens. */
  href: Href;
  /** InfoTip text when the title needs context. */
  info?: string;
  /** Hook returning a badge (unread count); called on every render of the row. */
  useBadge?: () => number | string | undefined;
}

export interface SettingsItem {
  id: string;
  title: string;
  icon: LucideIcon;
  href: Href;
  info?: string;
  /** Show only to support/admin staff. */
  staffOnly?: boolean;
  useBadge?: () => number | string | undefined;
}

/** Settings headings, in the order Settings shows them (labels: app/(tabs)/settings.tsx). Same ids as web. */
export type SettingsGroup = "you" | "workspace" | "agents" | "developer";

export interface SettingsSection {
  id: string;
  /** Group heading; omit for a single self-explanatory row. Not shown when `group` is set. */
  title?: string;
  /** The heading the items join: "you" (personal preferences), "workspace" (org, teams), "agents" (AI agents, connected services), "developer" (API keys, git access). */
  group?: SettingsGroup;
  items: SettingsItem[];
}

/** What a ⋮ menu is about: a whole feature section, or one resource in it. */
export interface ResourceMenuContext {
  /** Module name ("docs", "files"): agent scopes key on it. */
  feature: string;
  /** Access resource kind ("docs.page") and id, when the menu is for one resource. */
  kind?: string;
  id?: string;
  /** Resource title: the sheet's heading and dialog copy. */
  title?: string;
  orgId?: string;
}

export interface ResourceMenuChoice {
  id: string;
  label: string;
  checked?: boolean;
  /** InfoTip text explaining the option. */
  hint?: string;
  disabled?: boolean;
  onSelect: () => void;
}

export interface ResourceMenuAction {
  id: string;
  label: string;
  icon: LucideIcon;
  /** Short current value shown at the end of the row ("Read"). */
  hint?: string;
  /** Runs after the sheet closes. */
  onSelect?: () => void;
  /** A second sheet of options instead of onSelect. */
  choices?: ResourceMenuChoice[];
  destructive?: boolean;
  disabled?: boolean;
}

/**
 * Rows a feature adds to every ⋮ menu (`ResourceMenu`, src/components/resource): return no
 * actions where you don't apply; `element` renders beside the menu (a sheet an action
 * opens). The hook is called on every render of a menu, in registry order.
 */
export interface ResourceMenuProvider {
  id: string;
  useActions: (ctx: ResourceMenuContext) => { actions: ResourceMenuAction[]; element?: ReactNode };
}

export const resourceMenuProviders: readonly ResourceMenuProvider[] = [
  // scaffold:resource-menu
];

export const homeEntries: HomeEntry[] = [
  // scaffold:home
];

export const settingsSections: SettingsSection[] = [
  // scaffold:settings
];

/**
 * Routes reachable signed out, one per line, written like their file path under app/
 * without the extension: "(auth)", "legal", "invites/[token]". A route also covers
 * everything beneath it. Every other route needs a signed-in, consented user; the root
 * navigator sends others to sign-in and resumes the route afterwards. Public screens must
 * work without a token (public API routes only).
 */
export const publicRoutes: readonly string[] = [
  "(auth)",
  "legal",
  "+not-found",
  // scaffold:public-routes
];

/**
 * Routes reachable while the user must still accept the Terms (or the account is blocked),
 * same form as `publicRoutes`: support and help screens. A `supportLinks` route must be
 * listed here to open from the consent and blocked screens.
 */
export const consentRoutes: readonly string[] = [
  "consent",
  "legal",
  // scaffold:consent-routes
];

/** A way to get help: an app path ("/support/new") or an external URL (https:, mailto:). */
export interface SupportLink {
  id: string;
  title: string;
  icon: LucideIcon;
  href: string;
}

/** Shown on the consent screen, the blocked-account screen and Settings → Support, after the support email. */
export const supportLinks: readonly SupportLink[] = [
  // scaffold:support-links
];

/**
 * Components rendered once inside the API layer while signed in (session work such as
 * push permission, token sync, tap handling, the app badge). They render nothing visible
 * or a fixed overlay; react-query, auth, router and toast are available. Undo their work
 * in a `registerSignOutHook` (src/lib/session.ts).
 */
export const sessionMounts: readonly ComponentType[] = [
  // scaffold:session
];

function matches(routes: readonly string[], segments: readonly string[]): boolean {
  return routes.some((route) => {
    const parts = route.split("/").filter(Boolean);
    return parts.length > 0 && parts.length <= segments.length && parts.every((part, i) => part === segments[i]);
  });
}

/** Whether the current route segments (`useSegments()`) fall under a public route. */
export function isPublicRoute(segments: readonly string[]): boolean {
  return segments.length === 0 || matches(publicRoutes, segments);
}

/** Whether the current route segments are reachable before consent (or while blocked). */
export function isConsentRoute(segments: readonly string[]): boolean {
  return matches(consentRoutes, segments);
}
