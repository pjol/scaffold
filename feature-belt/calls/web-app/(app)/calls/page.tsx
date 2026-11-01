import type { Metadata } from "next";
import { CallsScreen } from "@/features/calls";

export const metadata: Metadata = { title: "Calls" };

export default function Page() {
  return <CallsScreen />;
}
