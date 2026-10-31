import type { Metadata } from "next";
import { PlaceFormScreen } from "@/features/places/PlaceForm";

export const metadata: Metadata = { title: "Add a place" };

export default function Page() {
  return <PlaceFormScreen />;
}
