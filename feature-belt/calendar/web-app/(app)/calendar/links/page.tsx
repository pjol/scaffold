import type { Metadata } from "next";
import { LinksScreen } from "@/features/calendar/LinksScreen";

export const metadata: Metadata = { title: "Booking links" };

export default function Page() {
  return <LinksScreen />;
}
