import type { Metadata } from "next";
import { Suspense } from "react";
import { SubmitScreen } from "@/features/forum/SubmitScreen";

export const metadata: Metadata = { title: "New post" };

export default function Page() {
  return (
    <Suspense>
      <SubmitScreen />
    </Suspense>
  );
}
