import { orgsNounTitle } from "@scaffoldapp/shared";
import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminOrgsScreen } from "@/features/orgs/AdminOrgsScreens";

// A function so the title reads the app's noun (configureOrgsNoun) per request.
export function generateMetadata(): Metadata {
  return { title: orgsNounTitle("many") };
}

export default function Page() {
  return (
    <Suspense>
      <AdminOrgsScreen />
    </Suspense>
  );
}
