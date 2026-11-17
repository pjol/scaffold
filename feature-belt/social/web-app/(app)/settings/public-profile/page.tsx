import type { Metadata } from "next";
import { ProfileSettingsScreen } from "@/features/social/ProfileSettingsScreen";

export const metadata: Metadata = { title: "Public profile" };

export default function Page() {
  return <ProfileSettingsScreen />;
}
