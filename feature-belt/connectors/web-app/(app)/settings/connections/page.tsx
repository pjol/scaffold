import type { Metadata } from "next";
import { Suspense } from "react";
import { ConnectionsScreen } from "@/features/connectors/ConnectionsScreen";

export const metadata: Metadata = { title: "Connections" };

export default function Page() {
  return (
    <Suspense>
      <ConnectionsScreen />
    </Suspense>
  );
}
