import { orgsNounTitle } from "@scaffoldapp/shared";
import type { Metadata } from "next";
import { Suspense } from "react";
import { OrgScreen } from "@/features/orgs/OrgScreen";

// A function so the title reads the app's noun (configureOrgsNoun) per request.
export function generateMetadata(): Metadata {
  return { title: orgsNounTitle() };
}

export default async function Page({ params }: { params: Promise<{ orgID: string }> }) {
  const { orgID } = await params;
  return (
    <Suspense>
      <OrgScreen orgId={orgID} />
    </Suspense>
  );
}
