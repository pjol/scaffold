import type { Metadata } from "next";
import { OrgsScreen } from "@/features/orgs/OrgsScreen";

export const metadata: Metadata = { title: "Organizations" };

export default function Page() {
  return <OrgsScreen />;
}
