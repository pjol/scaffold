import type { Metadata } from "next";
import { WalletsSettingsScreen } from "@/features/wallets/screens/WalletsSettingsScreen";

export const metadata: Metadata = { title: "Wallets" };

export default function Page() {
  return <WalletsSettingsScreen />;
}
