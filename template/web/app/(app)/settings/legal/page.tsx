import type { Metadata } from "next";
import { LegalScreen } from "@/screens/settings/LegalScreen";

export const metadata: Metadata = { title: "Legal" };

export default function Page() {
  return <LegalScreen />;
}
