import type { Metadata } from "next";
import { EventFormScreen } from "@/features/events/EventFormScreen";

export const metadata: Metadata = { title: "Edit event" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <EventFormScreen id={id} />;
}
