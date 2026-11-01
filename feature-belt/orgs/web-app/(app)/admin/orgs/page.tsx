import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminOrgsScreen } from "@/features/orgs/AdminOrgsScreens";

export const metadata: Metadata = { title: "Organizations" };

export default function Page() {
  return (
    <Suspense>
      <AdminOrgsScreen />
    </Suspense>
  );
}
