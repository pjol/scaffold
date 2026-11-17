import type { Metadata } from "next";
import { Suspense } from "react";
import { IssueScreen } from "@/features/repos/IssueScreen";

export const metadata: Metadata = { title: "Issue" };

export default async function Page({ params }: { params: Promise<{ number: string }> }) {
  const { number } = await params;
  return (
    <Suspense>
      <IssueScreen number={Number(number) || 0} />
    </Suspense>
  );
}
