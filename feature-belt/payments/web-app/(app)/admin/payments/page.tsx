import type { Metadata } from "next";
import { Suspense } from "react";
import { CustomersScreen } from "@/features/payments/admin/CustomersScreen";

export const metadata: Metadata = { title: "Payments" };

export default function Page() {
  return (
    <Suspense>
      <CustomersScreen />
    </Suspense>
  );
}
