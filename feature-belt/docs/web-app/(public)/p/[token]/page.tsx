import type { Metadata } from "next";
import { PublicPageScreen } from "@/features/docs";

export const metadata: Metadata = { title: "Shared page", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <PublicPageScreen token={token} />;
}
