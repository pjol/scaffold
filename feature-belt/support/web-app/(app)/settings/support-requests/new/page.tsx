import type { Metadata } from "next";
import { NewTicketScreen } from "@/features/support/NewTicketScreen";

export const metadata: Metadata = { title: "New request" };

export default function Page() {
  return <NewTicketScreen />;
}
