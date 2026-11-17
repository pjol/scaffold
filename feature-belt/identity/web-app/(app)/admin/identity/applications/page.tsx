import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminBadgeApplicationsScreen } from "@/features/identity/admin/AdminBadgeApplicationsScreen";

export const metadata: Metadata = { title: "Badge applications" };

export default function Page() {
  return (
    <Suspense>
      <AdminBadgeApplicationsScreen />
    </Suspense>
  );
}
