import { useLocalSearchParams } from "expo-router";
import { PlaceDetailScreen } from "@/features/places/PlaceDetailScreen";

export default function Route() {
  const { id } = useLocalSearchParams<{ id: string }>();
  return <PlaceDetailScreen id={id} />;
}
