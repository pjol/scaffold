import { Bookmark, MapPin } from "lucide-react-native";
import type { ColorValue } from "react-native";
import type { HomeEntry } from "@/features/registry";

/** Home row: saved places and your submissions (the map has its own tab). */
export const placesHome: HomeEntry = { id: "places", title: "Saved places", icon: Bookmark, href: "/places/saved" };

/** Bottom tab icon for the Places tab. */
export function PlacesTabIcon({ color, size }: { color: ColorValue; size: number }) {
  return <MapPin color={color} size={size} strokeWidth={2} />;
}
