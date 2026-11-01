import type { Metadata } from "next";
import type { ReactNode } from "react";
import { AdminLayout } from "@/screens/admin/AdminLayout";

export const metadata: Metadata = { title: "Admin" };

export default function Layout({ children }: { children: ReactNode }) {
  return <AdminLayout>{children}</AdminLayout>;
}
