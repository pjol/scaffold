import type { Metadata } from "next";
import { CustomerScreen } from "@/features/payments/admin/CustomerScreen";

export const metadata: Metadata = { title: "Customer" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <CustomerScreen userId={id} />;
}
