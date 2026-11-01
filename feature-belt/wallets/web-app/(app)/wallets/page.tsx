import type { Metadata } from "next";
import { WalletScreen } from "@/features/wallets/screens/WalletScreen";

export const metadata: Metadata = { title: "Wallet" };

export default function Page() {
  return <WalletScreen />;
}
