import type { Metadata } from "next";
import { ModerationCaseScreen } from "@/features/moderation/ModerationCaseScreen";

export const metadata: Metadata = { title: "Moderation case" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ModerationCaseScreen id={id} />;
}
