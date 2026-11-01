import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminUsageScreen } from "@/features/assistant/AdminUsageScreen";

export const metadata: Metadata = { title: "AI usage" };

export default function Page() {
  return (
    <Suspense>
      <AdminUsageScreen />
    </Suspense>
  );
}
