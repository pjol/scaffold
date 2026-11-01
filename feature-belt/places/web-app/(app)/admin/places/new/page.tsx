import type { Metadata } from "next";
import { PlaceFormScreen } from "@/features/places/PlaceForm";

export const metadata: Metadata = { title: "Add place" };

export default function Page() {
  return <PlaceFormScreen admin />;
}
