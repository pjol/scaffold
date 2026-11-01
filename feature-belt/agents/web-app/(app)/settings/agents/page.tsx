import type { Metadata } from "next";
import { AgentsSettingsScreen } from "@/features/agents/AgentsSettingsScreen";

export const metadata: Metadata = { title: "AI agents" };

export default function Page() {
  return <AgentsSettingsScreen />;
}
