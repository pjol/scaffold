import type { Metadata } from "next";
import { ConnectionDetailScreen } from "@/features/connectors/ConnectionDetailScreen";

export const metadata: Metadata = { title: "Connection" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ConnectionDetailScreen id={id} />;
}
