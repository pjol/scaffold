import type { Metadata } from "next";
import { Suspense } from "react";
import { MilestonesScreen } from "@/features/repos/LabelsScreen";

export const metadata: Metadata = { title: "Milestones" };

export default function Page() {
  return (
    <Suspense>
      <MilestonesScreen />
    </Suspense>
  );
}
