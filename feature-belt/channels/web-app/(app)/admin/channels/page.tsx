import type { Metadata } from "next";
import { AdminChannelsScreen } from "@/features/channels";

export const metadata: Metadata = { title: "Channels" };

export default function Page() {
  return <AdminChannelsScreen />;
}
