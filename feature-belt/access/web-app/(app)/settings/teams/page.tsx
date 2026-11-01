import type { Metadata } from "next";
import { Suspense } from "react";
import { TeamsScreen } from "@/features/access/TeamsScreen";

export const metadata: Metadata = { title: "Teams" };

export default function Page() {
  return (
    <Suspense>
      <TeamsScreen />
    </Suspense>
  );
}
