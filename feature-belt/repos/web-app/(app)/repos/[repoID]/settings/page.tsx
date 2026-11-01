import type { Metadata } from "next";
import { Suspense } from "react";
import { RepoSettingsScreen } from "@/features/repos/SettingsScreens";

export const metadata: Metadata = { title: "Repository settings" };

export default function Page() {
  return (
    <Suspense>
      <RepoSettingsScreen />
    </Suspense>
  );
}
