import { Copy } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "@/features/registry";

/** Home → Copy trading (your copies; traders to copy are one tap further). */
export const copytradeHome: HomeEntry = { id: "copytrade", title: "Copy trading", icon: Copy, href: "/copy" };

/** Settings → Copy trading: offer your trades for others to copy. */
export const copytradeSettings: SettingsSection = {
  id: "copytrade",
  items: [{ id: "copytrade", title: "Copy trading", icon: Copy, href: "/settings/copy-trading", info: "Let others copy your trades." }],
};
