import type { Metadata } from "next";
import { Suspense } from "react";
import { FilesSectionScreen } from "@/features/files/FilesScreens";

export const metadata: Metadata = { title: "My drive" };

export default function Page() {
  return (
    <Suspense>
      <FilesSectionScreen section="my" />
    </Suspense>
  );
}
