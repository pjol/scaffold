import type { Metadata } from "next";
import { Suspense } from "react";
import { CrmProductsScreen } from "@/features/crm/ProductsScreen";

export const metadata: Metadata = { title: "Products" };

export default function Page() {
  return (
    <Suspense>
      <CrmProductsScreen />
    </Suspense>
  );
}
