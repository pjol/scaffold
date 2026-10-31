import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminEventsScreen } from "@/features/events/AdminEventsScreen";

export const metadata: Metadata = { title: "Events" };

export default function Page() {
  return (
    <Suspense>
      <AdminEventsScreen />
    </Suspense>
  );
}
