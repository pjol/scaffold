import type { Metadata } from "next";
import { Suspense } from "react";
import { AuditScreen } from "@/screens/admin/AuditScreen";

export const metadata: Metadata = { title: "Audit log" };

export default function AuditPage() {
  return (
    <Suspense>
      <AuditScreen />
    </Suspense>
  );
}
