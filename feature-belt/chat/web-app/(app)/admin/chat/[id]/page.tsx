import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminConversationScreen } from "@/features/chat";

export const metadata: Metadata = { title: "Conversation" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <Suspense>
      <AdminConversationScreen id={id} />
    </Suspense>
  );
}
