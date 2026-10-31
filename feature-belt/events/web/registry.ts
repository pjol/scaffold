import { CalendarDays, CalendarSync } from "lucide-react";
import type { HomeTile, NavItem, SettingsSection } from "../registry";

/** Main nav: the events list. */
export const eventsNav: NavItem = { href: "/events", label: "Events", icon: CalendarDays };

/** Home tile: the same destination (a separate name, because `scaffold add` skips a line already in the file). */
export const eventsHomeTile: HomeTile = eventsNav;

/** Admin tab: every event, with staff cancel. */
export const eventsAdminNav: NavItem = { href: "/admin/events", label: "Events", icon: CalendarDays };

/** Settings → Calendar: the personal calendar feed URL. */
export const eventsSettings: SettingsSection = { href: "/settings/calendar", label: "Calendar", icon: CalendarSync };
