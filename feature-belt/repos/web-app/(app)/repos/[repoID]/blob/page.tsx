import type { Metadata } from "next";
import { Suspense } from "react";
import { FileScreen } from "@/features/repos/FileScreen";

export const metadata: Metadata = { title: "File" };

export default function Page() {
  return (
    <Suspense>
      <FileScreen />
    </Suspense>
  );
}
