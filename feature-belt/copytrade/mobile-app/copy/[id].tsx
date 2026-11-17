import { Stack, useLocalSearchParams } from "expo-router";
import { CopyDetailScreen } from "@/features/copytrade/CopyDetailScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return (
    <>
      <Stack.Screen options={{ title: "Copy" }} />
      <CopyDetailScreen id={id ?? ""} />
    </>
  );
}
