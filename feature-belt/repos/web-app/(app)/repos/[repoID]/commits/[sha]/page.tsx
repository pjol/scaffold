import type { Metadata } from "next";
import { CommitScreen } from "@/features/repos/history";

export const metadata: Metadata = { title: "Commit" };

export default async function Page({ params }: { params: Promise<{ sha: string }> }) {
  const { sha } = await params;
  return <CommitScreen sha={sha} />;
}
