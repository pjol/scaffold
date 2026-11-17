import type { Metadata } from "next";
import { Suspense } from "react";
import { OracleAdminScreen } from "@/features/oracle/AdminScreen";

export const metadata: Metadata = { title: "Oracle" };

export default function Page() {
  return (
    <Suspense>
      <OracleAdminScreen />
    </Suspense>
  );
}
