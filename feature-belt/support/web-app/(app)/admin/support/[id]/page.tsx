import type { Metadata } from "next";
import { AdminTicketScreen } from "@/features/support/AdminTicketScreen";

export const metadata: Metadata = { title: "Support request" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminTicketScreen id={id} />;
}
