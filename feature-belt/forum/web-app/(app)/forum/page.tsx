import type { Metadata } from "next";
import { Suspense } from "react";
import { ForumHomeScreen } from "@/features/forum/ForumHomeScreen";

export const metadata: Metadata = { title: "Forum" };

export default function Page() {
  return (
    <Suspense>
      <ForumHomeScreen />
    </Suspense>
  );
}
