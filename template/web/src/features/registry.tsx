/**
 * Feature registry: where installed features plug into the web shell without editing
 * core files. `scaffold add` inserts one line directly above each anchor comment in this
 * file; feature agents add entries the same way by hand. Keep the anchor comments exactly
 * as they are (renaming one breaks every feature that wires into it).
 *
 * Shapes:
 *   NavItem          { href, label, icon, match?, useBadge? }
 *     href           app path the entry opens ("/chat"); the feature ships that route
 *     label          one or two words, sentence case ("Chat", "Saved places")
 *     icon           a lucide-react icon component (MessagesSquare), not an element
 *     match          "prefix" (default: active on nested routes too) or "exact"
 *     useBadge       a React hook returning a count (unread, pending) or undefined;
 *                    it is called on every render of the nav, so keep it cheap
 *                    (read a react-query cache, never fetch per call)
 *   HomeTile         { href, label, icon, useBadge? }  same fields as NavItem, minus match
 *   SettingsSection  { href, label, icon, group? }  a page under /settings/<name> the feature
 *                    ships; `group` puts it under a heading in Settings: "you" (personal
 *                    preferences), "workspace" (org, teams, permissions), "agents" (AI
 *                    agents and connected services) or "developer" (API keys, git access)
 *   AccountMenuItem  either a link { id, href, label, icon }, or a slot { id, Component }:
 *                    a component rendered inside the account menu that draws its own rows
 *                    with ui-web's MenuLabel / MenuItem / MenuSeparator (an org switcher)
 *   SessionMount     a component (no props) mounted once while signed in, inside the data
 *                    providers (realtime listeners, foreground notification toasts); it
 *                    remounts when the identity changes and renders nothing or overlays
 *   HeaderAction     { id, Component }  a top-bar control before the account menu (a bell);
 *                    keep it a 40 px IconButton-sized target with a label
 *   ConsentRoute     a regex source matched against the whole pathname ("/support(/.*)?");
 *                    those routes open while the user still has to accept the Terms
 *   SupportLink      { id, href, label, icon }  a way to get help besides the support email
 *   ResourceMenuProvider { id, useActions(ctx) → { actions, element? } }  rows a feature adds
 *                    to every ⋮ menu (`ResourceMenu`, src/components/resource): ctx names
 *                    the feature and, for one resource, its access kind and id. Return no
 *                    actions where you don't apply; `element` renders beside the menu (the
 *                    dialog an action opens). Hooks are called on every render of a menu.
 *
 * Where each list shows:
 *   featureNav        side nav (≥ 1024 px) and bottom tab bar (< 1024 px; overflow beyond
 *                     5 tabs moves to the account menu)
 *   homeTiles         tiles on Home (`/`); its own constant (see below)
 *   adminNav          the admin side tabs (support and admin only; the API guards it)
 *   settingsSections  Settings, under their group's heading (You · Workspace · AI agents ·
 *                     Developer · Other for sections without a group); while no section
 *                     has a group, one flat list between Appearance and Account
 *   accountMenuItems  the avatar menu in the top bar, above Settings and Sign out
 *   sessionMounts     inside the data providers, while signed in
 *   headerActions     top bar, before the account menu
 *   consentRoutes     AppFrame shows these routes instead of the consent screen
 *   resourceMenuProviders  every ⋮ menu, after the screen's own actions (Share…, AI agent access)
 *   supportLinks      consent screen, suspended banner, blocked-account screen (next to
 *                     the support email) and Settings → Support. A link that must work
 *                     before consent is accepted also goes in consentRoutes; one that must
 *                     work for banned accounts points at a public page (proxy public paths)
 *
 * Example (chat): the import goes above the imports anchor, each entry inside its array
 * above that array's anchor, one line each, ideally constants exported from the feature
 * folder:
 *
 *   import { chatHome, chatNav } from "./chat/nav";
 *   …
 *   export const featureNav: readonly NavItem[] = [
 *     chatNav,
 *     …anchor…
 *   ];
 *   export const homeTiles: readonly HomeTile[] = [
 *     chatHome,
 *     …anchor…
 *   ];
 *
 * where `chatNav = { href: "/chat", label: "Chat", icon: MessagesSquare, useBadge: useChatUnreadCount }`
 * and `chatHome = chatNav` (a separate name: the CLI skips a line already present in this file).
 */
import type { LucideIcon } from "lucide-react";
import type { ComponentType, ReactNode } from "react";
// scaffold:imports

export interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
  match?: "exact" | "prefix";
  useBadge?: () => number | undefined;
}

export type HomeTile = Omit<NavItem, "match">;

/** Settings headings, in the order Settings shows them (labels: screens/settings/links.ts). */
export type SettingsGroup = "you" | "workspace" | "agents" | "developer";

export interface SettingsSection {
  href: string;
  label: string;
  icon: LucideIcon;
  /** The heading it sits under; without one it goes under "Other" (once any section has a group). */
  group?: SettingsGroup;
}

export type AccountMenuItem = { id: string; href: string; label: string; icon: LucideIcon } | { id: string; Component: ComponentType };

export type SessionMount = ComponentType;

export interface HeaderAction {
  id: string;
  Component: ComponentType;
}

export interface SupportLink {
  id: string;
  href: string;
  label: string;
  icon: LucideIcon;
}

/** What a ⋮ menu is about: a whole feature section, or one resource in it. */
export interface ResourceMenuContext {
  /** Module name ("docs", "files"): agent scopes and analytics key on it. */
  feature: string;
  /** Access resource kind ("docs.page") and id, when the menu is for one resource. */
  kind?: string;
  id?: string;
  /** Resource title, for dialogs ("Share “Q3 plan”"). */
  title?: string;
  orgId?: string;
}

export interface ResourceMenuChoice {
  id: string;
  label: string;
  checked?: boolean;
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
  onSelect?: () => void;
  /** A submenu of options (web) / a second sheet (mobile) instead of onSelect. */
  choices?: ResourceMenuChoice[];
  destructive?: boolean;
  disabled?: boolean;
}

export interface ResourceMenuProvider {
  id: string;
  useActions: (ctx: ResourceMenuContext) => { actions: ResourceMenuAction[]; element?: ReactNode };
}

/** Product sections in the main navigation. */
export const featureNav: readonly NavItem[] = [
  // scaffold:nav
];

/** Tiles on Home, in order. */
export const homeTiles: readonly HomeTile[] = [
  // scaffold:home
];

/** Extra admin tabs after Overview, Users and Audit log. */
export const adminNav: readonly NavItem[] = [
  // scaffold:admin-nav
];

/** Extra settings pages: under their group's heading, or between Appearance and Account while none has a group. */
export const settingsSections: readonly SettingsSection[] = [
  // scaffold:settings
];

/** Account menu entries above Settings and Sign out. */
export const accountMenuItems: readonly AccountMenuItem[] = [
  // scaffold:account-menu
];

/** Mounted once inside the data providers while signed in. */
export const sessionMounts: readonly SessionMount[] = [
  // scaffold:session
];

/** Top-bar controls before the account menu. */
export const headerActions: readonly HeaderAction[] = [
  // scaffold:header-actions
];

/** Path patterns reachable while consent is still required. */
export const consentRoutes: readonly string[] = [
  // scaffold:consent-routes
];

/** Rows added to every ⋮ menu (ResourceMenu), in order. */
export const resourceMenuProviders: readonly ResourceMenuProvider[] = [
  // scaffold:resource-menu
];

/** Help links shown wherever the support email is. */
export const supportLinks: readonly SupportLink[] = [
  // scaffold:support-links
];
