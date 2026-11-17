import type { Metadata } from "next";
import { AdminBadgeApplicationScreen } from "@/features/identity/admin/AdminBadgeApplicationScreen";

export const metadata: Metadata = { title: "Badge application" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminBadgeApplicationScreen id={id} />;
}
