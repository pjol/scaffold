import type { Metadata } from "next";
import { PublicPageScreen } from "@/features/docs";
import { docsPublicMetadata } from "@/features/docs/publicMeta";

export async function generateMetadata({ params }: { params: Promise<{ token: string; pageId: string }> }): Promise<Metadata> {
  const { token, pageId } = await params;
  return docsPublicMetadata(token, pageId);
}

export default async function Page({ params }: { params: Promise<{ token: string; pageId: string }> }) {
  const { token, pageId } = await params;
  return <PublicPageScreen token={token} pageId={pageId} />;
}
