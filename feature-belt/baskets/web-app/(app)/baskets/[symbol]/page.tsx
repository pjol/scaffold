import type { Metadata } from "next";
import { Suspense } from "react";
import { BasketScreen } from "@/features/baskets/BasketScreen";

export async function generateMetadata({ params }: { params: Promise<{ symbol: string }> }): Promise<Metadata> {
  const { symbol } = await params;
  return { title: decodeURIComponent(symbol).toUpperCase() };
}

export default async function Page({ params }: { params: Promise<{ symbol: string }> }) {
  const { symbol } = await params;
  return (
    <Suspense>
      <BasketScreen symbol={decodeURIComponent(symbol).toUpperCase()} />
    </Suspense>
  );
}
