import type { Metadata } from "next";
import { Suspense } from "react";
import { NewBasketScreen } from "@/features/baskets/NewBasketScreen";

export const metadata: Metadata = { title: "New basket" };

export default function Page() {
  return (
    <Suspense>
      <NewBasketScreen />
    </Suspense>
  );
}
