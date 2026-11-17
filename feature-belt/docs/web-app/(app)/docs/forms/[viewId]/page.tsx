import type { Metadata } from "next";
import { FormScreen } from "@/features/docs";

export const metadata: Metadata = { title: "Form" };

export default async function Page({ params }: { params: Promise<{ viewId: string }> }) {
  const { viewId } = await params;
  return <FormScreen viewId={viewId} />;
}
