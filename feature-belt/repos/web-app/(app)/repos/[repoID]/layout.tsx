import type { ReactNode } from "react";
import { RepoLayout } from "@/features/repos/RepoLayout";

export default async function Layout({ params, children }: { params: Promise<{ repoID: string }>; children: ReactNode }) {
  const { repoID } = await params;
  return <RepoLayout repoId={repoID}>{children}</RepoLayout>;
}
