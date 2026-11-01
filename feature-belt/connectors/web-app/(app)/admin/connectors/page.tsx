import type { Metadata } from "next";
import { AdminConnectorsScreen } from "@/features/connectors/AdminConnectorsScreen";

export const metadata: Metadata = { title: "Connectors" };

export default function Page() {
  return <AdminConnectorsScreen />;
}
