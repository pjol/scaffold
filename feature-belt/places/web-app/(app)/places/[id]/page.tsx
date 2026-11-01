import type { Metadata } from "next";
import { PlaceDetailScreen } from "@/features/places/PlaceDetailScreen";

export const metadata: Metadata = { title: "Place" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <PlaceDetailScreen id={id} />;
}
