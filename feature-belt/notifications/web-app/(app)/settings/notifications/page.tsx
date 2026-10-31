import type { Metadata } from "next";
import { NotificationSettingsScreen } from "@/features/notifications/NotificationSettingsScreen";

export const metadata: Metadata = { title: "Notifications" };

export default function Page() {
  return <NotificationSettingsScreen />;
}
