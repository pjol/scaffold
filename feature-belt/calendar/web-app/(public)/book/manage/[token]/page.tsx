import type { Metadata } from "next";
import { ManageBookingScreen } from "@/features/calendar/ManageBookingScreen";

export const metadata: Metadata = { title: "Your booking", robots: { index: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <ManageBookingScreen token={token} />;
}
