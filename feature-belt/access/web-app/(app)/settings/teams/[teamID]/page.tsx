import type { Metadata } from "next";
import { TeamScreen } from "@/features/access/TeamScreen";

export const metadata: Metadata = { title: "Team" };

export default async function Page({ params }: { params: Promise<{ teamID: string }> }) {
  const { teamID } = await params;
  return <TeamScreen teamId={teamID} />;
}
