import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminConnectionsScreen } from "@/features/mcp/AdminConnectionsScreen";

export const metadata: Metadata = { title: "Connected apps" };

export default function Page() {
  return (
    <Suspense>
      <AdminConnectionsScreen />
    </Suspense>
  );
}
