import { useLocalSearchParams } from "expo-router";
import { TradeScreen } from "@/features/exchange/TradeScreen";

export default function Route() {
  const { symbol } = useLocalSearchParams<{ symbol: string }>();
  return <TradeScreen symbol={symbol ?? ""} />;
}
