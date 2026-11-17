import type { Metadata } from "next";
import { Suspense } from "react";
import { LeadersScreen } from "@/features/copytrade/LeadersScreen";

export const metadata: Metadata = { title: "Find traders" };

export default function Page() {
  return (
    <Suspense>
      <LeadersScreen />
    </Suspense>
  );
}
