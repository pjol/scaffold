import { Video } from "lucide-react";
import type { HomeTile, NavItem, SessionMount } from "../registry";
import { CallsRinger } from "./CallsRinger";

/** Side nav / tab bar entry. */
export const callsNav: NavItem = { href: "/calls", label: "Calls", icon: Video };

/** Home tile (same destination; a separate constant so the CLI can wire both lines). */
export const callsHome: HomeTile = { href: "/calls", label: "Calls", icon: Video };

/** Incoming-call overlay and ringtone, mounted while signed in. */
export const callsRinger: SessionMount = CallsRinger;
