import type { Metadata } from "next";
import { IdentityScreen } from "@/features/identity/IdentityScreen";

export const metadata: Metadata = { title: "Verification" };

export default function Page() {
  return <IdentityScreen />;
}
