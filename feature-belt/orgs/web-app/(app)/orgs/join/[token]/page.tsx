import { orgsNoun } from "@scaffoldapp/shared";
import type { Metadata } from "next";
import { AcceptInviteScreen } from "@/features/orgs/InviteScreens";

// A function so the title reads the app's noun (configureOrgsNoun) per request.
export function generateMetadata(): Metadata {
  return { title: `Join ${orgsNoun()}` };
}

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <AcceptInviteScreen token={token} />;
}
