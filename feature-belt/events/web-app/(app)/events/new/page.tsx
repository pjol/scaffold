import type { Metadata } from "next";
import { EventFormScreen } from "@/features/events/EventFormScreen";

export const metadata: Metadata = { title: "New event" };

export default function Page() {
  return <EventFormScreen />;
}
