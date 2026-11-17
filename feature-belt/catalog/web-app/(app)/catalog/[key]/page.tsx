import type { Metadata } from "next";
import { Suspense } from "react";
import { CatalogItemScreen } from "@/features/catalog/ItemScreen";

export const metadata: Metadata = { title: "Catalog item" };

export default async function Page({ params }: { params: Promise<{ key: string }> }) {
  const { key } = await params;
  return (
    <Suspense>
      <CatalogItemScreen itemKey={decodeURIComponent(key)} />
    </Suspense>
  );
}
