import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminBasketsScreen } from "@/features/baskets/AdminBasketsScreen";

export const metadata: Metadata = { title: "Baskets" };

export default function Page() {
  return (
    <Suspense>
      <AdminBasketsScreen />
    </Suspense>
  );
}
