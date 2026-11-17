import type { Metadata } from "next";
import { Suspense } from "react";
import { LabelsScreen } from "@/features/repos/LabelsScreen";

export const metadata: Metadata = { title: "Labels" };

export default function Page() {
  return (
    <Suspense>
      <LabelsScreen />
    </Suspense>
  );
}
