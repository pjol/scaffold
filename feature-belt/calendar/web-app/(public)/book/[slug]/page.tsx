import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { PublicBookingScreen } from "@/features/calendar/PublicBookingScreen";

export const metadata: Metadata = { title: "Book a time", robots: { index: false } };

export default async function Page({ params, searchParams }: { params: Promise<{ slug: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { slug } = await params;
  // `?embed=1` (what the embed code loads) opens the frameless page other sites may embed.
  if ((await searchParams)["embed"] === "1") redirect(`/book/${encodeURIComponent(slug)}/embed`);
  return <PublicBookingScreen slug={slug} />;
}
