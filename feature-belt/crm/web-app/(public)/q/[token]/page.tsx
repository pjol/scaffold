import type { Metadata } from "next";
import { CrmPublicQuoteScreen } from "@/features/crm/PublicQuoteScreen";

export const metadata: Metadata = { title: "Quote", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <CrmPublicQuoteScreen token={token} />;
}
