import type { Metadata } from "next";
import { Suspense } from "react";
import { AgentsSuggestionsScreen } from "@/features/agents/SuggestionsScreen";

export const metadata: Metadata = { title: "Agent suggestions" };

export default function Page() {
  return (
    <Suspense>
      <AgentsSuggestionsScreen />
    </Suspense>
  );
}
