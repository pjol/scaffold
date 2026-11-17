import type { Metadata } from "next";
import { InsightsScreen } from "@/features/repos/InsightsScreen";

export const metadata: Metadata = { title: "Insights" };

export default function Page() {
  return <InsightsScreen />;
}
