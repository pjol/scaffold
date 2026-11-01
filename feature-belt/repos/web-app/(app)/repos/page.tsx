import type { Metadata } from "next";
import { Suspense } from "react";
import { ReposListScreen } from "@/features/repos/ReposScreens";

export const metadata: Metadata = { title: "Repositories" };

export default function Page() {
  return (
    <Suspense>
      <ReposListScreen />
    </Suspense>
  );
}
