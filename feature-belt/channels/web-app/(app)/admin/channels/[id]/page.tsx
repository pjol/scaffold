import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminChannelScreen } from "@/features/channels";

export const metadata: Metadata = { title: "Channel" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <AdminChannelScreen id={id} />
    </Suspense>
  );
}
