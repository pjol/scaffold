import { Stack } from "expo-router";

/** Signed-out screens. More (sign-up, recovery) go in this folder. */
export default function AuthLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
