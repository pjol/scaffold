import type { Metadata } from "next";
import { PublicBookingScreen } from "@/features/calendar/PublicBookingScreen";

export const metadata: Metadata = { title: "Book a time", robots: { index: false } };

export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return <PublicBookingScreen slug={slug} />;
}
