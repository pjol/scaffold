import type { Metadata } from "next";
import { Suspense } from "react";
import { BranchesScreen } from "@/features/repos/BranchesScreen";

export const metadata: Metadata = { title: "Branches" };

export default function Page() {
  return (
    <Suspense>
      <BranchesScreen />
    </Suspense>
  );
}
