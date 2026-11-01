import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminAgentsScreen } from "@/features/agents/AdminAgentsScreen";

export const metadata: Metadata = { title: "AI agents" };

export default function Page() {
  return (
    <Suspense>
      <AdminAgentsScreen />
    </Suspense>
  );
}
