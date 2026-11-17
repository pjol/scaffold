import type { Metadata } from "next";
import { RoomsScreen } from "@/features/calendar/RoomsScreen";

export const metadata: Metadata = { title: "Rooms" };

export default function Page() {
  return <RoomsScreen />;
}
