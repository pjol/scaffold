import type { Metadata } from "next";
import { Suspense } from "react";
import { EditScreen } from "@/features/repos/EditScreen";

export const metadata: Metadata = { title: "Edit file" };

export default function Page() {
  return (
    <Suspense>
      <EditScreen />
    </Suspense>
  );
}
