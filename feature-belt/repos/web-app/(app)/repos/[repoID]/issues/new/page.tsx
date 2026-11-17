import type { Metadata } from "next";
import { Suspense } from "react";
import { NewIssueScreen } from "@/features/repos/NewIssueScreen";

export const metadata: Metadata = { title: "New issue" };

export default function Page() {
  return (
    <Suspense>
      <NewIssueScreen />
    </Suspense>
  );
}
