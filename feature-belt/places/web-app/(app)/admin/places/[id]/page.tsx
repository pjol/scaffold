import type { Metadata } from "next";
import { AdminPlaceScreen } from "@/features/places/admin/AdminPlaceScreen";

export const metadata: Metadata = { title: "Place" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <AdminPlaceScreen id={id} />;
}
