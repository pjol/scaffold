import type { Metadata } from "next";
import { PublicEventScreen } from "@/features/events/PublicEventScreen";

export const metadata: Metadata = { title: "Event" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <PublicEventScreen id={id} />;
}
