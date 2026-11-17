import { Rss, UserRound, Users } from "lucide-react-native";
import type { ColorValue } from "react-native";
import type { HomeEntry, SettingsSection } from "@/features/registry";

/** Tab icon for the Following tab. */
export function SocialTabIcon({ color, size }: { focused: boolean; color: ColorValue; size: number }) {
  return <Rss color={color as string} size={size} strokeWidth={2} />;
}

/** Home → People (search and suggestions). */
export const socialHome: HomeEntry = { id: "social", title: "People", icon: Users, href: "/people" };

/** Settings → Public profile, with a row for viewing your own profile. */
export const socialSettings: SettingsSection = {
  id: "social",
  items: [
    { id: "social-profile", title: "Public profile", icon: UserRound, href: "/settings/public-profile", info: "Handle, bio, links, and who sees what." },
    { id: "social-view", title: "Your profile", icon: Users, href: "/u/me" },
  ],
};
