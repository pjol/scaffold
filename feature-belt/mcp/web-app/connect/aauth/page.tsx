import type { Metadata } from "next";
import { Suspense } from "react";
import { AAuthLinkScreen } from "@/features/mcp/AAuthLinkScreen";

export const metadata: Metadata = { title: "Link a person server", robots: { index: false } };

export default function Page() {
  return (
    <Suspense>
      <AAuthLinkScreen />
    </Suspense>
  );
}
