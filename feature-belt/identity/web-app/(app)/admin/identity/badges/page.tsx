import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminBadgesScreen } from "@/features/identity/admin/AdminBadgesScreen";

export const metadata: Metadata = { title: "Badges" };

export default function Page() {
  return (
    <Suspense>
      <AdminBadgesScreen />
    </Suspense>
  );
}
