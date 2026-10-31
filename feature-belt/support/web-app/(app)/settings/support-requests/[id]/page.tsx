import type { Metadata } from "next";
import { TicketScreen } from "@/features/support/TicketScreen";

export const metadata: Metadata = { title: "Support request" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <TicketScreen id={id} />;
}
