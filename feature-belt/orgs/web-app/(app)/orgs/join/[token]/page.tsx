import type { Metadata } from "next";
import { AcceptInviteScreen } from "@/features/orgs/InviteScreens";

export const metadata: Metadata = { title: "Join organization" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <AcceptInviteScreen token={token} />;
}
