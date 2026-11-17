import type { Metadata } from "next";
import { PublicPageScreen } from "@/features/docs";
import { docsPublicMetadata } from "@/features/docs/publicMeta";

export async function generateMetadata({ params }: { params: Promise<{ token: string }> }): Promise<Metadata> {
  const { token } = await params;
  return docsPublicMetadata(token);
}

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <PublicPageScreen token={token} />;
}
