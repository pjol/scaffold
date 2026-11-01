import { Video } from "lucide-react-native";
import type { ComponentType } from "react";
import type { HomeEntry } from "../registry";
import { CallsRinger } from "./CallsRinger";

/** Home row: meetings, calls and history. */
export const callsHome: HomeEntry = { id: "calls", title: "Calls", icon: Video, href: "/calls" };

/** Session mount: opens the incoming-call screen when a call rings for you. */
export const callsSession: ComponentType = CallsRinger;
