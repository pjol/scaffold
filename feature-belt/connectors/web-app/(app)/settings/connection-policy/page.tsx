import type { Metadata } from "next";
import { ConnectionPolicyScreen } from "@/features/connectors/PolicyScreen";

export const metadata: Metadata = { title: "Connection policy" };

export default function Page() {
  return <ConnectionPolicyScreen />;
}
