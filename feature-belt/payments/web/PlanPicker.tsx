"use client";

import { formatMinor, isPaymentsUnconfigured, isSubscriptionLive, priceInterval, useEntitlements, usePrices, useSubscription, type CatalogPrice } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, Stagger, StaggerItem } from "@scaffoldapp/ui-web";
import { Tags } from "lucide-react";
import Link from "next/link";
import { CheckoutButton } from "./CheckoutButton";
import { PaymentsUnavailable } from "./PaymentsUnavailable";

function PriceAmount({ price }: { price: CatalogPrice }) {
  const every = priceInterval(price);
  if (price.amount_minor === null) return <span className="type-body text-fg-muted">Priced at checkout</span>;
  return (
    <span className="flex items-baseline gap-1">
      <span className="type-headline font-semibold text-fg tabular-nums">{formatMinor(price.amount_minor, price.currency)}</span>
      <span className="type-label text-fg-muted">{every ? `/ ${every}` : "one time"}</span>
    </span>
  );
}

type Holding = "current" | "covered" | "owned" | null;

function PriceCard({ price, holding, successPath }: { price: CatalogPrice; holding: Holding; successPath: string | undefined }) {
  const recurring = price.kind === "recurring";
  return (
    <Card className="flex h-full flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h3 className="flex items-center gap-2 type-title-sm text-fg">
          <span className="min-w-0 truncate">{price.label || price.key}</span>
          {price.entitlement_days !== null && <InfoTip content={`${price.entitlement_days} days of access from the day you pay.`} />}
          {(holding === "current" || holding === "owned") && (
            <Badge tone="success" className="ml-auto shrink-0">
              {holding === "current" ? "Current plan" : "Owned"}
            </Badge>
          )}
        </h3>
        <PriceAmount price={price} />
      </div>
      <div className="mt-auto flex">
        {holding === "current" || holding === "covered" ? (
          <Button asChild variant="secondary" size="auto" className="w-full">
            <Link href="/settings/billing">{holding === "current" ? "Manage billing" : "Change plan"}</Link>
          </Button>
        ) : holding === "owned" ? null : (
          <CheckoutButton priceKey={price.key} size="auto" className="w-full" {...(successPath ? { successPath } : {})}>
            {recurring ? "Subscribe" : "Buy"}
          </CheckoutButton>
        )}
      </div>
    </Card>
  );
}

/**
 * The plan picker: active catalog prices (optionally only those granting
 * `entitlement`), each with a checkout button. Marks what the user already holds.
 */
export function PlanPicker({ entitlement, successPath }: { entitlement?: string; successPath?: string }) {
  const prices = usePrices(entitlement);
  const held = useEntitlements();
  const sub = useSubscription();
  const live = isSubscriptionLive(sub.data?.subscription) ? sub.data?.subscription : null;
  /** current: this is the live plan · covered: another live plan grants the same thing (switch in the portal) · owned: a one-time purchase without expiry. */
  const holding = (p: CatalogPrice): Holding => {
    if (p.kind === "recurring") {
      if (live?.price_key === p.key) return "current";
      return live && p.entitlement && live.entitlement === p.entitlement ? "covered" : null;
    }
    const has = !!p.entitlement && !!held.data?.some((e) => e.key === p.entitlement && e.active_until === null);
    return has && p.entitlement_days === null ? "owned" : null;
  };

  if (prices.isPending) {
    return (
      <SkeletonGroup className="grid gap-4 sm:grid-cols-2">
        <Skeleton className="h-44" />
        <Skeleton className="h-44" />
      </SkeletonGroup>
    );
  }
  if (prices.isError) {
    if (isPaymentsUnconfigured(prices.error)) return <PaymentsUnavailable />;
    return <ErrorState title="Couldn't load plans" info={prices.error.message} onRetry={() => void prices.refetch()} retrying={prices.isFetching} />;
  }
  if (prices.data.length === 0) {
    return <EmptyState icon={<Tags />} title="No plans available yet" />;
  }
  return (
    <Stagger className="grid gap-4 sm:grid-cols-2">
      {prices.data.map((p) => (
        <StaggerItem key={p.key}>
          <PriceCard price={p} holding={holding(p)} successPath={successPath} />
        </StaggerItem>
      ))}
    </Stagger>
  );
}
