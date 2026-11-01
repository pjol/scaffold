import { orgsNounTitle } from "@scaffoldapp/shared";
import type { Metadata } from "next";
import { OrgsScreen } from "@/features/orgs/OrgsScreen";

// A function so the title reads the app's noun (configureOrgsNoun) per request.
export function generateMetadata(): Metadata {
  return { title: orgsNounTitle("many") };
}

export default function Page() {
  return <OrgsScreen />;
}
