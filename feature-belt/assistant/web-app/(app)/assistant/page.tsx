import type { Metadata } from "next";
import { AssistantScreen } from "@/features/assistant/AssistantScreen";

export const metadata: Metadata = { title: "Assistant" };

export default function Page() {
  return <AssistantScreen />;
}
