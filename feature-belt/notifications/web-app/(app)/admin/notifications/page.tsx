import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminNotificationsScreen } from "@/features/notifications/AdminNotificationsScreen";

export const metadata: Metadata = { title: "Notifications" };

export default function Page() {
  return (
    <Suspense>
      <AdminNotificationsScreen />
    </Suspense>
  );
}
