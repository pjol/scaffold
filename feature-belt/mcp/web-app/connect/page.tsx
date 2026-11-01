import type { Metadata } from "next";
import { Suspense } from "react";
import { ConnectScreen } from "@/features/mcp/ConnectScreen";

export const metadata: Metadata = { title: "Connect an app", robots: { index: false } };

export default function Page() {
  return (
    <Suspense>
      <ConnectScreen />
    </Suspense>
  );
}
