"use client";

import { Card, EmptyState, SkeletonGroup, SkeletonRow } from "@scaffoldapp/ui-web";
import { WalletMinimal } from "lucide-react";

/** No chains in WALLETS_CHAINS: the honest state instead of empty balances. */
export function WalletsNotConfigured() {
  return (
    <Card padding="compact">
      <EmptyState
        icon={<WalletMinimal />}
        title="Wallets aren't set up"
        info="This app has no blockchain networks configured yet, so there's nothing to show. Contact support if you expected to see your wallets here."
      />
    </Card>
  );
}

/** Skeleton rows matching the wallet lists: in a card, or `bare` inside one. */
export function RowsSkeleton({ rows = 3, label = "Loading", bare = false }: { rows?: number; label?: string; bare?: boolean }) {
  const list = Array.from({ length: rows }, (_, i) => <SkeletonRow key={i} className="px-4" />);
  return (
    <SkeletonGroup label={label} className="flex flex-col">
      {bare ? (
        list
      ) : (
        <Card padding="none" className="p-1">
          {list}
        </Card>
      )}
    </SkeletonGroup>
  );
}

/** A short asset badge for list rows: "ETH", "USDC". */
export function AssetMark({ symbol }: { symbol: string }) {
  return (
    <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-pill bg-primary-soft type-caption font-semibold text-primary-strong">
      {symbol.slice(0, 4)}
    </span>
  );
}
