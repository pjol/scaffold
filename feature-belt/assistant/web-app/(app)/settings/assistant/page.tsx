import type { Metadata } from "next";
import { AssistantSettingsScreen } from "@/features/assistant/AssistantSettingsScreen";

export const metadata: Metadata = { title: "Assistant" };

export default function Page() {
  return <AssistantSettingsScreen />;
}
