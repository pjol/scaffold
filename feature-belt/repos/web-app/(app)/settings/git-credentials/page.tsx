import type { Metadata } from "next";
import { GitCredentialsScreen } from "@/features/repos/SettingsScreens";

export const metadata: Metadata = { title: "Git credentials" };

export default function Page() {
  return <GitCredentialsScreen />;
}
