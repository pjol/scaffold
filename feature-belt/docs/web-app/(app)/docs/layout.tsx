import type { Metadata } from "next";
import type { ReactNode } from "react";
import { DocsLayout } from "@/features/docs";

export const metadata: Metadata = { title: "Docs" };

export default function Layout({ children }: { children: ReactNode }) {
  return <DocsLayout>{children}</DocsLayout>;
}
