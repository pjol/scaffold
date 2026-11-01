import type { Metadata } from "next";
import { Suspense } from "react";
import { ModerationQueueScreen } from "@/features/moderation/ModerationQueueScreen";

export const metadata: Metadata = { title: "Moderation" };

export default function Page() {
  return (
    <Suspense>
      <ModerationQueueScreen />
    </Suspense>
  );
}
