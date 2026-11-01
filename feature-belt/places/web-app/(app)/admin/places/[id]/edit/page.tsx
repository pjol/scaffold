import type { Metadata } from "next";
import { PlaceFormScreen } from "@/features/places/PlaceForm";

export const metadata: Metadata = { title: "Edit place" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <PlaceFormScreen id={id} admin />;
}
