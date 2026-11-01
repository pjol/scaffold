import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminFilesScreen } from "@/features/files/AdminFilesScreen";

export const metadata: Metadata = { title: "Public files" };

export default function Page() {
  return (
    <Suspense>
      <AdminFilesScreen />
    </Suspense>
  );
}
