import type { Metadata } from "next";
import { Suspense } from "react";
import { PullsScreen } from "@/features/repos/PullsScreen";

export const metadata: Metadata = { title: "Pull requests" };

export default function Page() {
  return (
    <Suspense>
      <PullsScreen />
    </Suspense>
  );
}
