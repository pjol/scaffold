import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminReputationScreen } from "@/features/reputation/AdminReputationScreen";

export const metadata: Metadata = { title: "Reputation" };

export default function Page() {
  return (
    <Suspense>
      <AdminReputationScreen />
    </Suspense>
  );
}
