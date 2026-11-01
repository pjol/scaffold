import { MapPin, MapPinned } from "lucide-react";
import type { HomeTile, NavItem } from "@/features/registry";

/** Main nav and Home tile: the map and list. */
export const placesNav: NavItem = { href: "/places", label: "Places", icon: MapPin };
export const placesHome: HomeTile = placesNav;
/** Admin tab: the review queue. */
export const placesAdminNav: NavItem = { href: "/admin/places", label: "Places", icon: MapPinned };
