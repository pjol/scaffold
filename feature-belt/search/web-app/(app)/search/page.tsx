import type { Metadata } from "next";
import { Suspense } from "react";
import { SearchScreen } from "@/features/search/SearchScreen";

export const metadata: Metadata = { title: "Search" };

export default function Page() {
  return (
    <Suspense>
      <SearchScreen />
    </Suspense>
  );
}
