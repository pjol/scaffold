import type { Metadata } from "next";
import { ReputationScreen } from "@/features/reputation/ReputationScreen";

export const metadata: Metadata = { title: "Reputation" };

export default function Page() {
  return <ReputationScreen />;
}
