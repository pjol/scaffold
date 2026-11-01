import { getLegalDocument, type LegalDocumentKind } from "@scaffoldapp/shared/legal";
import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalPageScreen } from "@/screens/public/LegalPageScreen";

const KINDS: readonly LegalDocumentKind[] = ["terms", "privacy"];
const isKind = (v: string): v is LegalDocumentKind => (KINDS as readonly string[]).includes(v);

export const dynamicParams = false;

export function generateStaticParams() {
  return KINDS.map((doc) => ({ doc }));
}

export async function generateMetadata({ params }: { params: Promise<{ doc: string }> }): Promise<Metadata> {
  const { doc } = await params;
  return isKind(doc) ? { title: getLegalDocument(doc).title } : {};
}

export default async function LegalPage({ params }: { params: Promise<{ doc: string }> }) {
  const { doc } = await params;
  if (!isKind(doc)) notFound();
  return <LegalPageScreen kind={doc} />;
}
