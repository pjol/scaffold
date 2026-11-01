import type { Metadata } from "next";
import { AdminOrgScreen } from "@/features/orgs/AdminOrgsScreens";

export const metadata: Metadata = { title: "Organization" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminOrgScreen id={id} />;
}
