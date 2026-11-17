import { Stack } from "expo-router";
import { PortfolioScreen } from "@/features/exchange/PortfolioScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Portfolio" }} />
      <PortfolioScreen />
    </>
  );
}
