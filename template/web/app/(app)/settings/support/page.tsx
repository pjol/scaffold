import type { Metadata } from "next";
import { SupportScreen } from "@/screens/settings/SupportScreen";

export const metadata: Metadata = { title: "Support" };

export default function Page() {
  return <SupportScreen />;
}
