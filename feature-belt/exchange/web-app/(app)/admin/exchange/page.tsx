import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminExchangeScreen } from "@/features/exchange/AdminExchangeScreen";

export const metadata: Metadata = { title: "Exchange" };

export default function Page() {
  return (
    <Suspense>
      <AdminExchangeScreen />
    </Suspense>
  );
}
