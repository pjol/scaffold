import type { Metadata } from "next";
import { Suspense } from "react";
import { MeetScreen } from "@/features/calls";

export const metadata: Metadata = { title: "Meeting", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ code: string }> }) {
  const { code } = await params;
  return (
    <Suspense>
      <MeetScreen code={code.toLowerCase()} />
    </Suspense>
  );
}
