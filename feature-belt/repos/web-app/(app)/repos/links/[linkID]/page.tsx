import { RepoLinkScreen } from "@/features/repos";

export default async function Page({ params }: { params: Promise<{ linkID: string }> }) {
  const { linkID } = await params;
  return <RepoLinkScreen linkId={linkID} />;
}
