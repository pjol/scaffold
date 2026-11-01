import { useLocalSearchParams } from "expo-router";
import { PlaceFormScreen } from "@/features/places/PlaceFormScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <PlaceFormScreen id={id} />;
}
