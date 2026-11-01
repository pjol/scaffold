import { orgsNounTitle } from "@scaffoldapp/shared";
import type { Metadata } from "next";
import { AdminOrgScreen } from "@/features/orgs/AdminOrgsScreens";

// A function so the title reads the app's noun (configureOrgsNoun) per request.
export function generateMetadata(): Metadata {
  return { title: orgsNounTitle() };
}

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminOrgScreen id={id} />;
}
