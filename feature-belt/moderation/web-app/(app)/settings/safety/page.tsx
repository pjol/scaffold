import type { Metadata } from "next";
import { SafetyScreen } from "@/features/moderation/SafetyScreen";

export const metadata: Metadata = { title: "Safety" };

export default function Page() {
  return <SafetyScreen />;
}
