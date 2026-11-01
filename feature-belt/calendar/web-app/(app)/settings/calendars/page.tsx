import type { Metadata } from "next";
import { CalendarSettingsScreen } from "@/features/calendar/SettingsScreen";

export const metadata: Metadata = { title: "Calendars" };

export default function Page() {
  return <CalendarSettingsScreen />;
}
