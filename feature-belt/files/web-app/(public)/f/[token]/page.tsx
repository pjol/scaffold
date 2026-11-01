import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesPublicScreen } from "@/features/files/PublicScreen";

export const metadata: Metadata = { title: "Shared file", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return (
    <Suspense>
      <FilesPublicScreen token={token} />
    </Suspense>
  );
}
