import { CalendarDays, CalendarSync } from "lucide-react-native";
import type { HomeEntry, SettingsSection } from "../registry";

/** Home → Events. */
export const eventsHome: HomeEntry = { id: "events", title: "Events", icon: CalendarDays, href: "/events" };

/** Settings → Calendar feed: the secret URL calendar apps subscribe to. */
export const eventsSettings: SettingsSection = {
  id: "events",
  items: [
    {
      id: "calendar-feed",
      title: "Calendar feed",
      icon: CalendarSync,
      href: "/settings/calendar-feed",
      info: "A private link that puts events you host or RSVP'd to in your calendar app.",
    },
  ],
};
