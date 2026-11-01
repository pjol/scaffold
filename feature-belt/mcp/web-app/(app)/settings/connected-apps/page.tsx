import type { Metadata } from "next";
import { ConnectedAppsScreen } from "@/features/mcp/ConnectedAppsScreen";

export const metadata: Metadata = { title: "Connected apps" };

export default function Page() {
  return <ConnectedAppsScreen />;
}
