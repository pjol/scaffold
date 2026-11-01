import type { Metadata } from "next";
import type { ReactNode } from "react";
import { FilesLayout } from "@/features/files/FilesLayout";

export const metadata: Metadata = { title: "Files" };

export default function Layout({ children }: { children: ReactNode }) {
  return <FilesLayout>{children}</FilesLayout>;
}
