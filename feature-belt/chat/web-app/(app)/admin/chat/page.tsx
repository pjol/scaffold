import type { Metadata } from "next";
import { AdminChatScreen } from "@/features/chat";

export const metadata: Metadata = { title: "Chat" };

export default function Page() {
  return <AdminChatScreen />;
}
