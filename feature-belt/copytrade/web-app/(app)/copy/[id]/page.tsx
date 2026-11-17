import type { Metadata } from "next";
import { Suspense } from "react";
import { CopyDetailScreen } from "@/features/copytrade/CopyDetailScreen";

export const metadata: Metadata = { title: "Copy" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <CopyDetailScreen id={id} />
    </Suspense>
  );
}
