import { CalendarDays, CalendarCog } from "lucide-react";
import type { HomeTile, NavItem, SessionMount, SettingsSection } from "../registry";
import { CalendarZoneSync } from "./ZoneSync";

/** Main nav: the calendar. */
export const calendarNav: NavItem = { href: "/calendar", label: "Calendar", icon: CalendarDays };

/** Home tile: the same destination (a separate name: `scaffold add` skips a line already in the file). */
export const calendarHomeTile: HomeTile = calendarNav;

/** Settings → Calendars: time zone, working hours, reminders, other calendars, the iCal feed. */
export const calendarSettings: SettingsSection = { href: "/settings/calendars", label: "Calendars", icon: CalendarCog };

/** Session mount: saves the device's zone the first time someone is signed in (times in invitations and reminders). */
export const calendarZoneSync: SessionMount = CalendarZoneSync;
