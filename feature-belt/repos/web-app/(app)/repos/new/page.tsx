import type { Metadata } from "next";
import { Suspense } from "react";
import { NewRepoScreen } from "@/features/repos/ReposScreens";

export const metadata: Metadata = { title: "New repository" };

export default function Page() {
  return (
    <Suspense>
      <NewRepoScreen />
    </Suspense>
  );
}
