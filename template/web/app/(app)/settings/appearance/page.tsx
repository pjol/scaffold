import type { Metadata } from "next";
import { AppearanceScreen } from "@/screens/settings/AppearanceScreen";

export const metadata: Metadata = { title: "Appearance" };

export default function Page() {
  return <AppearanceScreen />;
}
