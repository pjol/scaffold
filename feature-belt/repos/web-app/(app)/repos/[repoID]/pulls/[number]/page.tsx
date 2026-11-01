import type { Metadata } from "next";
import { Suspense } from "react";
import { PullScreen } from "@/features/repos/PullScreen";

export const metadata: Metadata = { title: "Pull request" };

export default async function Page({ params }: { params: Promise<{ number: string }> }) {
  const { number } = await params;
  return (
    <Suspense>
      <PullScreen number={Number(number) || 0} />
    </Suspense>
  );
}
