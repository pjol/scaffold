import type { Metadata } from "next";
import { Suspense } from "react";
import { IssuesScreen } from "@/features/repos/IssuesScreen";

export const metadata: Metadata = { title: "Issues" };

export default function Page() {
  return (
    <Suspense>
      <IssuesScreen />
    </Suspense>
  );
}
