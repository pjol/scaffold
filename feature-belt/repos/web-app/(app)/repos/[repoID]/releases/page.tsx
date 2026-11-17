import type { Metadata } from "next";
import { ReleasesScreen } from "@/features/repos/ReleasesScreen";

export const metadata: Metadata = { title: "Releases" };

export default function Page() {
  return <ReleasesScreen />;
}
