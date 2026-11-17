import type { Metadata } from "next";
import { Suspense } from "react";
import { CopiesScreen } from "@/features/copytrade/CopiesScreen";

export const metadata: Metadata = { title: "Copy trading" };

export default function Page() {
  return (
    <Suspense>
      <CopiesScreen />
    </Suspense>
  );
}
