import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminCopytradeScreen } from "@/features/copytrade/AdminCopytradeScreen";

export const metadata: Metadata = { title: "Copy trading" };

export default function Page() {
  return (
    <Suspense>
      <AdminCopytradeScreen />
    </Suspense>
  );
}
