import type { Metadata } from "next";
import { AccountScreen } from "@/screens/settings/AccountScreen";

export const metadata: Metadata = { title: "Account" };

export default function Page() {
  return <AccountScreen />;
}
