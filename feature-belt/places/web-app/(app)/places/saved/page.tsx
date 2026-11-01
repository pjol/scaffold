import type { Metadata } from "next";
import { Suspense } from "react";
import { YourPlacesScreen } from "@/features/places/YourPlacesScreen";

export const metadata: Metadata = { title: "Your places" };

export default function Page() {
  return (
    <Suspense>
      <YourPlacesScreen />
    </Suspense>
  );
}
