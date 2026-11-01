import type { Metadata } from "next";
import { AssistantScreen } from "@/features/assistant/AssistantScreen";

export const metadata: Metadata = { title: "Assistant" };

/** /assistant/new starts a conversation; /assistant/<id> opens one. */
export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AssistantScreen conversationId={id} />;
}
