import type { Metadata } from "next";
import { LedgerScreen } from "@/features/payments/admin/LedgerScreen";

export const metadata: Metadata = { title: "Ledger" };

export default function Page() {
  return <LedgerScreen />;
}
