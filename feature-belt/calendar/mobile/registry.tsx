import { useCalendarDeviceZoneSave } from "@scaffoldapp/shared";
import { CalendarDays } from "lucide-react-native";
import type { ComponentType } from "react";
import type { HomeEntry } from "../registry";

/** Home → Calendar (agenda and day views, booking links). */
export const calendarHome: HomeEntry = { id: "calendar", title: "Calendar", icon: CalendarDays, href: "/calendar" };

/** Saves the device's time zone as the calendar zone when the person has none yet. Renders nothing. */
function CalendarZoneSync() {
  useCalendarDeviceZoneSave();
  return null;
}

/** Session mount: the device's zone, saved the first time someone is signed in (times in invitations and reminders). */
export const calendarZoneSync: ComponentType = CalendarZoneSync;
