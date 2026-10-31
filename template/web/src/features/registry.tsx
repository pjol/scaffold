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
 *   SettingsSection  { href, label, icon }  a page under /settings/<name> the feature ships
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
 *
 * Where each list shows:
 *   featureNav        side nav (≥ 1024 px) and bottom tab bar (< 1024 px; overflow beyond
 *                     5 tabs moves to the account menu)
 *   homeTiles         tiles on Home (`/`); its own constant (see below)
 *   adminNav          the admin side tabs (support and admin only; the API guards it)
 *   settingsSections  between Appearance and Account in Settings
 *   accountMenuItems  the avatar menu in the top bar, above Settings and Sign out
 *   sessionMounts     inside the data providers, while signed in
 *   headerActions     top bar, before the account menu
 *   consentRoutes     AppFrame shows these routes instead of the consent screen
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
import type { ComponentType } from "react";
// scaffold:imports

export interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
  match?: "exact" | "prefix";
  useBadge?: () => number | undefined;
}

export type HomeTile = Omit<NavItem, "match">;

export interface SettingsSection {
  href: string;
  label: string;
  icon: LucideIcon;
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

/** Extra settings pages between Appearance and Account. */
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

/** Help links shown wherever the support email is. */
export const supportLinks: readonly SupportLink[] = [
  // scaffold:support-links
];
