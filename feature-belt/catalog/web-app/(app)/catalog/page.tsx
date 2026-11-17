import type { Metadata } from "next";
import { Suspense } from "react";
import { CatalogScreen } from "@/features/catalog/CatalogScreen";

export const metadata: Metadata = { title: "Catalog" };

export default function Page() {
  return (
    <Suspense>
      <CatalogScreen />
    </Suspense>
  );
}
