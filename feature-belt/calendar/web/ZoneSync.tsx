"use client";

import { useCalendarDeviceZoneSave } from "@scaffoldapp/shared";

/** Saves the device's time zone as the calendar zone when the person has none yet, once the account is active and the Terms are accepted. Renders nothing. */
export function CalendarZoneSync() {
  useCalendarDeviceZoneSave();
  return null;
}
