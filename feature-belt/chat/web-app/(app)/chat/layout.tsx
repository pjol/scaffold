import type { Metadata } from "next";
import type { ReactNode } from "react";
import { ChatLayout } from "@/features/chat";

export const metadata: Metadata = { title: "Chat" };

export default function Layout({ children }: { children: ReactNode }) {
  return <ChatLayout>{children}</ChatLayout>;
}
