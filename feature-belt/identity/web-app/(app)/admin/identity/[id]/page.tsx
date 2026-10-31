import type { Metadata } from "next";
import { AdminVerificationScreen } from "@/features/identity/admin/AdminVerificationScreen";

export const metadata: Metadata = { title: "Verification" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminVerificationScreen id={id} />;
}
