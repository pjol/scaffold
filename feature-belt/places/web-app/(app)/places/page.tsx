import type { Metadata } from "next";
import { Suspense } from "react";
import { PlacesScreen } from "@/features/places/PlacesScreen";

export const metadata: Metadata = { title: "Places" };

export default function Page() {
  return (
    <Suspense>
      <PlacesScreen />
    </Suspense>
  );
}
