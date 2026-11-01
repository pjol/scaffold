import type { Metadata } from "next";
import { Suspense } from "react";
import { CompareScreen } from "@/features/repos/CompareScreen";

export const metadata: Metadata = { title: "Compare" };

export default function Page() {
  return (
    <Suspense>
      <CompareScreen />
    </Suspense>
  );
}
