import type { Metadata } from "next";
import { Suspense } from "react";
import { BasketsScreen } from "@/features/baskets/BasketsScreen";

export const metadata: Metadata = { title: "Baskets" };

export default function Page() {
  return (
    <Suspense>
      <BasketsScreen />
    </Suspense>
  );
}
