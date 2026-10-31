import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminQueueScreen } from "@/features/support/AdminQueueScreen";

export const metadata: Metadata = { title: "Support" };

export default function Page() {
  return (
    <Suspense>
      <AdminQueueScreen />
    </Suspense>
  );
}
