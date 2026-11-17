import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesRequestScreen } from "@/features/files/RequestScreen";

export const metadata: Metadata = { title: "Send files", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return (
    <Suspense>
      <FilesRequestScreen token={token} />
    </Suspense>
  );
}
