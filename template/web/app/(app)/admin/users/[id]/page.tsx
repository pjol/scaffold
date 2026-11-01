import type { Metadata } from "next";
import { UserDetailScreen } from "@/screens/admin/UserDetailScreen";

export const metadata: Metadata = { title: "User" };

export default async function UserPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <UserDetailScreen id={id} />;
}
