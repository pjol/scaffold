import type { Metadata } from "next";
import { Suspense } from "react";
import { GuestRsvpScreen } from "@/features/calendar/ManageBookingScreen";

export const metadata: Metadata = { title: "Invitation", robots: { index: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return (
    <Suspense>
      <GuestRsvpScreen token={token} />
    </Suspense>
  );
}
