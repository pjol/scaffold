import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminKeysScreen } from "@/features/mcp/AdminKeysScreen";

export const metadata: Metadata = { title: "API keys" };

export default function Page() {
  return (
    <Suspense>
      <AdminKeysScreen />
    </Suspense>
  );
}
