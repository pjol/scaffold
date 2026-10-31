import type { Metadata } from "next";
import { Suspense } from "react";
import { AdminWalletsScreen } from "@/features/wallets/screens/AdminWalletsScreen";

export const metadata: Metadata = { title: "Wallets" };

export default function Page() {
  return (
    <Suspense>
      <AdminWalletsScreen />
    </Suspense>
  );
}
