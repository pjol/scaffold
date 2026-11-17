import type { Metadata } from "next";
import { LeaderSettingsScreen } from "@/features/copytrade/LeaderSettingsScreen";

export const metadata: Metadata = { title: "Copy trading" };

export default function Page() {
  return <LeaderSettingsScreen />;
}
