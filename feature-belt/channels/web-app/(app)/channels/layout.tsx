import type { Metadata } from "next";
import type { ReactNode } from "react";
import { WorkspaceLayout } from "@/features/channels";

export const metadata: Metadata = { title: "Channels" };

export default function Layout({ children }: { children: ReactNode }) {
  return <WorkspaceLayout>{children}</WorkspaceLayout>;
}
