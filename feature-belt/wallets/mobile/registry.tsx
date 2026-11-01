import { Wallet } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "../registry";

/** Home entry: balances and activity. */
export const walletsHome: HomeEntry = { id: "wallets", title: "Wallet", icon: Wallet, href: "/wallets" };

/** Settings → Wallets: linked wallets. */
export const walletsSettings: SettingsSection = {
  id: "wallets",
  items: [{ id: "wallets", title: "Wallets", icon: Wallet, href: "/wallets/manage" }],
};
