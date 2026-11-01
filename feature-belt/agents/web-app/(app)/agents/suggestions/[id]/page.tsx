import type { Metadata } from "next";
import { AgentsSuggestionScreen } from "@/features/agents/SuggestionScreen";

export const metadata: Metadata = { title: "Agent suggestion" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AgentsSuggestionScreen id={id} />;
}
