import type { Metadata } from "next";
import { Suspense } from "react";
import { FeedScreen } from "@/features/feed/FeedScreen";

export const metadata: Metadata = { title: "Feed" };

export default function Page() {
  return (
    <Suspense>
      <FeedScreen />
    </Suspense>
  );
}
