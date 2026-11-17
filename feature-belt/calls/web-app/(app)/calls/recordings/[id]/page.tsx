import type { Metadata } from "next";
import { RecordingScreen } from "@/features/calls";

export const metadata: Metadata = { title: "Recording" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <RecordingScreen id={id} />;
}
