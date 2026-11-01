import type { Metadata } from "next";
import { EventPageScreen } from "@/features/calendar/EventDetail";

export const metadata: Metadata = { title: "Event" };

export default async function Page({ params, searchParams }: { params: Promise<{ id: string }>; searchParams: Promise<{ occurrence?: string }> }) {
  const { id } = await params;
  const { occurrence } = await searchParams;
  return <EventPageScreen id={id} occurrence={occurrence ?? null} />;
}
