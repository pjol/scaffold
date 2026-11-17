import type { Metadata } from "next";
import { Suspense } from "react";
import { PeopleScreen } from "@/features/social/PeopleScreen";

export const metadata: Metadata = { title: "People" };

export default function Page() {
  return (
    <Suspense>
      <PeopleScreen />
    </Suspense>
  );
}
