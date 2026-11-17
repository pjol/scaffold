import type { Metadata } from "next";
import { PublicFormScreen } from "@/features/docs";

export const metadata: Metadata = { title: "Form", robots: { index: false, follow: false }, referrer: "no-referrer" };

export default async function Page({ params }: { params: Promise<{ token: string }> }) {
  const { token } = await params;
  return <PublicFormScreen token={token} />;
}
