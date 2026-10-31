import type { Metadata } from "next";
import { Suspense } from "react";
import { InboxScreen } from "@/features/notifications/InboxScreen";

export const metadata: Metadata = { title: "Inbox" };

export default function Page() {
  return (
    <Suspense>
      <InboxScreen />
    </Suspense>
  );
}
