import type { Metadata } from "next";
import { Suspense } from "react";
import { UploadScreen } from "@/features/repos/EditScreen";

export const metadata: Metadata = { title: "Upload files" };

export default function Page() {
  return (
    <Suspense>
      <UploadScreen />
    </Suspense>
  );
}
