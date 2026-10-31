import { Stack } from "expo-router";
import { IdentityScreen } from "@/features/identity/IdentityScreen";

export default function Route() {
  return (
    <>
      <Stack.Screen options={{ title: "Verification" }} />
      <IdentityScreen />
    </>
  );
}
