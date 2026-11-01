import type { Metadata } from "next";
import { Suspense } from "react";
import { CodeScreen } from "@/features/repos/CodeScreen";

export const metadata: Metadata = { title: "Code" };

export default function Page() {
  return (
    <Suspense>
      <CodeScreen />
    </Suspense>
  );
}
