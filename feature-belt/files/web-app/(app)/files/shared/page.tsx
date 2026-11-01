import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesSectionScreen } from "@/features/files/FilesScreens";

export const metadata: Metadata = { title: "Shared folders" };

export default function Page() {
  return (
    <Suspense>
      <FilesSectionScreen section="shared" />
    </Suspense>
  );
}
