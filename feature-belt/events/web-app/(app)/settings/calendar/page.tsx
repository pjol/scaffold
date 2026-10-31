import type { Metadata } from "next";
import { CalendarFeedScreen } from "@/features/events/CalendarFeedScreen";

export const metadata: Metadata = { title: "Calendar" };

export default function Page() {
  return <CalendarFeedScreen />;
}
