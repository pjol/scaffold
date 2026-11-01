import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminFeedScreen } from "@/features/feed/AdminFeedScreen";

export const metadata: Metadata = { title: "Feed" };

export default function Page() {
  return (
    <Suspense>
      <AdminFeedScreen />
    </Suspense>
  );
}
