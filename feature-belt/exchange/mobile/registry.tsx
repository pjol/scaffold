import { CandlestickChart, WalletCards } from "lucide-react-native";
import type { ColorValue } from "react-native";
import type { HomeEntry } from "@/features/registry";

/** Home → Portfolio (the markets have their own tab). */
export const exchangeHome: HomeEntry = { id: "exchange", title: "Portfolio", icon: WalletCards, href: "/portfolio" };

/** Bottom tab icon for the Markets tab. */
export function MarketsTabIcon({ color, size }: { color: ColorValue; size: number }) {
  return <CandlestickChart color={color} size={size} strokeWidth={2} />;
}
