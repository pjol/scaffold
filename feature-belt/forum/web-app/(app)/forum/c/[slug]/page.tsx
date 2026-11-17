import type { Metadata } from "next";
import { Suspense } from "react";
import { CommunityScreen } from "@/features/forum/CommunityScreen";

export const metadata: Metadata = { title: "Community" };

export default async function Page({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  return (
    <Suspense>
      <CommunityScreen slug={slug} />
    </Suspense>
  );
}
