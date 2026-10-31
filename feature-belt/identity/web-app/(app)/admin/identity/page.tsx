import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminVerificationsScreen } from "@/features/identity/admin/AdminVerificationsScreen";

export const metadata: Metadata = { title: "Verification" };

export default function Page() {
  return (
    <Suspense>
      <AdminVerificationsScreen />
    </Suspense>
  );
}
