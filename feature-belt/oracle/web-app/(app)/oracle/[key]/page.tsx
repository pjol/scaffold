import type { Metadata } from "next";
import { Suspense } from "react";
import { OracleFeedScreen } from "@/features/oracle/FeedScreen";

export async function generateMetadata({ params }: { params: Promise<{ key: string }> }): Promise<Metadata> {
  const { key } = await params;
  return { title: decodeURIComponent(key).toUpperCase() };
}

export default async function Page({ params }: { params: Promise<{ key: string }> }) {
  const { key } = await params;
  return (
    <Suspense>
      <OracleFeedScreen feedKey={decodeURIComponent(key).toUpperCase()} />
    </Suspense>
  );
}
