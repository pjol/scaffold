import type { Metadata } from "next";
import { Suspense } from "react";
import { CatalogAdminScreen } from "@/features/catalog/AdminScreen";

export const metadata: Metadata = { title: "Catalog" };

export default function Page() {
  return (
    <Suspense>
      <CatalogAdminScreen />
    </Suspense>
  );
}
