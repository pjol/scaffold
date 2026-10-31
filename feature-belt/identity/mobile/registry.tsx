import { BadgeCheck } from "lucide-react-native";
import type { SettingsSection } from "@/features/registry";

/** Settings → Verification. */
export const identitySettings: SettingsSection = { id: "identity", items: [{ id: "identity", title: "Verification", icon: BadgeCheck, href: "/settings/identity" }] };
