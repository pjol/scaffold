import type { Metadata } from "next";
import { TicketListScreen } from "@/features/support/TicketListScreen";

export const metadata: Metadata = { title: "Support requests" };

export default function Page() {
  return <TicketListScreen />;
}
