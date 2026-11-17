import { Stack } from "expo-router";
import { CopiesScreen } from "@/features/copytrade/CopiesScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Copy trading" }} />
      <CopiesScreen />
    </>
  );
}
