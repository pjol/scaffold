import type { Metadata } from "next";
import { Suspense } from "react";
import { SearchScreen } from "@/features/repos/history";

export const metadata: Metadata = { title: "Search code" };

export default function Page() {
  return (
    <Suspense>
      <SearchScreen />
    </Suspense>
  );
}
