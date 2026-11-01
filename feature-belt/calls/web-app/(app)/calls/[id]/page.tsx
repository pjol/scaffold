import type { Metadata } from "next";
import { CallDetailScreen } from "@/features/calls";

export const metadata: Metadata = { title: "Call" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <CallDetailScreen id={id} />;
}
