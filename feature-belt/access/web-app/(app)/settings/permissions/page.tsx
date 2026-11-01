import type { Metadata } from "next";
import { PermissionsScreen } from "@/features/access/PermissionsScreen";

export const metadata: Metadata = { title: "Permissions" };

export default function Page() {
  return <PermissionsScreen />;
}
