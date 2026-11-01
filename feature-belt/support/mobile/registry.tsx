import { MessagesSquare } from "lucide-react-native";
import type { SettingsSection, SupportLink } from "../registry";

/** Settings → Support requests (the core Support row keeps the contact email). */
export const supportSettings: SettingsSection = {
  id: "support",
  items: [{ id: "support-requests", title: "Support requests", icon: MessagesSquare, href: "/settings/support-requests" }],
};

/** Shown on the consent, blocked and Support screens (the route is a consent route). */
export const supportRequestsLink: SupportLink = { id: "support-requests", title: "Support requests", icon: MessagesSquare, href: "/settings/support-requests" };
