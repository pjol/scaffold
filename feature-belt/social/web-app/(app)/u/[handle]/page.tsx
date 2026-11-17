import type { Metadata } from "next";
import { Suspense } from "react";
import { ProfileScreen } from "@/features/social/ProfileScreen";

export async function generateMetadata({ params }: { params: Promise<{ handle: string }> }): Promise<Metadata> {
  const { handle } = await params;
  return { title: handle === "me" ? "Your profile" : `@${decodeURIComponent(handle)}` };
}

export default async function Page({ params }: { params: Promise<{ handle: string }> }) {
  const { handle } = await params;
  return (
    <Suspense>
      <ProfileScreen handle={handle} />
    </Suspense>
  );
}
