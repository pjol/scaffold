import type { Metadata } from "next";
import { ProfileScreen } from "@/screens/settings/ProfileScreen";

export const metadata: Metadata = { title: "Profile" };

export default function Page() {
  return <ProfileScreen />;
}
