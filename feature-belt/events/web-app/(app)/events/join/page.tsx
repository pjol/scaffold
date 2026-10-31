import type { Metadata } from "next";
import { Suspense } from "react";
import { JoinEventScreen } from "@/features/events/JoinEventScreen";

export const metadata: Metadata = { title: "Join event" };

export default function Page() {
  return (
    <Suspense>
      <JoinEventScreen />
    </Suspense>
  );
}
