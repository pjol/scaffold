import { Rss, UserRound, Users } from "lucide-react";
import type { AccountMenuItem, HomeTile, NavItem, SettingsSection } from "../registry";

/** Main nav: the following feed. */
export const socialNav: NavItem = { href: "/following", label: "Following", icon: Rss };

/** Home tile: find people (a separate constant: the CLI never inserts the same line twice in one file). */
export const socialHomeTile: HomeTile = { href: "/people", label: "People", icon: Users };

/** Settings → Public profile: handle, bio, links, privacy. */
export const socialSettings: SettingsSection = { href: "/settings/public-profile", label: "Public profile", icon: UserRound };

/** Account menu → Your profile (`/u/me` resolves to the caller's handle). */
export const socialAccountMenu: AccountMenuItem = { id: "social-profile", href: "/u/me", label: "Your profile", icon: UserRound };
