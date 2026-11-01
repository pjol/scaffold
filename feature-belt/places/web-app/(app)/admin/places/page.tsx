import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminPlacesScreen } from "@/features/places/admin/AdminPlacesScreen";

export const metadata: Metadata = { title: "Places" };

export default function Page() {
  return (
    <Suspense>
      <AdminPlacesScreen />
    </Suspense>
  );
}
