import { Stack } from "expo-router";
import { SearchScreen } from "@/features/search/SearchScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Search" }} />
      <SearchScreen />
    </>
  );
}
