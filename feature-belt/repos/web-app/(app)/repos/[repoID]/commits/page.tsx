import type { Metadata } from "next";
import { Suspense } from "react";
import { CommitsScreen } from "@/features/repos/history";

export const metadata: Metadata = { title: "Commits" };

export default function Page() {
  return (
    <Suspense>
      <CommitsScreen />
    </Suspense>
  );
}
