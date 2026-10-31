import type { Metadata } from "next";
import { SupportPageScreen } from "@/screens/public/SupportPageScreen";

export const metadata: Metadata = { title: "Support" };

export default function SupportPage() {
  return <SupportPageScreen />;
}
