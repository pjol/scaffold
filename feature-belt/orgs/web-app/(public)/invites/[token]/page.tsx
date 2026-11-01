import type { Metadata } from "next";
import { InvitePreviewScreen } from "@/features/orgs/InviteScreens";

export const metadata: Metadata = { title: "Invitation", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <InvitePreviewScreen token={token} />;
}
