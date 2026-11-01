import type { Metadata } from "next";
import { LinkEditorScreen } from "@/features/calendar/LinkEditor";

export const metadata: Metadata = { title: "Booking link" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <LinkEditorScreen id={id} />;
}
