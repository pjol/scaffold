import type { Metadata } from "next";
import { Suspense } from "react";
import { TradeScreen } from "@/features/exchange/TradeScreen";

export async function generateMetadata({ params }: { params: Promise<{ symbol: string }> }): Promise<Metadata> {
  const { symbol } = await params;
  return { title: decodeURIComponent(symbol).toUpperCase() };
}

export default async function Page({ params }: { params: Promise<{ symbol: string }> }) {
  const { symbol } = await params;
  return (
    <Suspense>
      <TradeScreen symbol={decodeURIComponent(symbol)} />
    </Suspense>
  );
}
