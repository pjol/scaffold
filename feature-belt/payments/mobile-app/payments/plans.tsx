import { errorMessage, formatMinor, isPaymentsUnconfigured, isSubscriptionLive, priceInterval, useCheckout, useEntitlements, usePrices, useSubscription, type CatalogPrice } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, InfoTip, Screen, Skeleton, Text, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { Store, Tags } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";
import { PaymentsUnavailable } from "@/features/payments/PaymentsUnavailable";
import { MOBILE_CHECKOUT_ENABLED, openHostedPage } from "@/features/payments/store-rules";

type Holding = "current" | "covered" | "owned" | null;

function PriceCard({ price, holding, busy, onBuy }: { price: CatalogPrice; holding: Holding; busy: boolean; onBuy: () => void }) {
  const theme = useTheme();
  const every = priceInterval(price);
  const recurring = price.kind === "recurring";
  return (
    <Card style={{ gap: theme.space[4] }}>
      <View style={{ gap: theme.space[2] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
          <TitleSm style={{ flexShrink: 1 }}>{price.label || price.key}</TitleSm>
          {price.entitlement_days !== null ? <InfoTip content={`${price.entitlement_days} days of access from the day you pay.`} /> : null}
          {holding === "current" || holding === "owned" ? <Badge tone="success">{holding === "current" ? "Current plan" : "Owned"}</Badge> : null}
        </View>
        {price.amount_minor === null ? (
          <Text tone="muted">Priced at checkout</Text>
        ) : (
          <View style={{ flexDirection: "row", alignItems: "baseline", gap: theme.space[1] }}>
            <Text role="headline" weight={600} tabular>
              {formatMinor(price.amount_minor, price.currency)}
            </Text>
            <Text role="label" tone="muted">
              {every ? `/ ${every}` : "one time"}
            </Text>
          </View>
        )}
      </View>
      {holding === "current" || holding === "owned" ? null : holding === "covered" ? (
        <Button variant="secondary" onPress={() => router.push("/payments/billing")} fullWidth>
          Change plan
        </Button>
      ) : (
        <Button loading={busy} onPress={onBuy} fullWidth>
          {recurring ? "Subscribe" : "Buy"}
        </Button>
      )}
    </Card>
  );
}

/**
 * Plans: catalog prices with hosted checkout in the in-app browser, only when the
 * store rules allow it (EXPO_PUBLIC_PAYMENTS_MOBILE_CHECKOUT=on for physical goods and
 * services; see store-rules.ts). A 402 `payment_required` opens this screen with
 * `?entitlement=`.
 */
export default function PlansScreen() {
  const theme = useTheme();
  const { entitlement } = useLocalSearchParams<{ entitlement?: string }>();
  const prices = usePrices(entitlement || undefined, { enabled: MOBILE_CHECKOUT_ENABLED });
  const held = useEntitlements({ enabled: MOBILE_CHECKOUT_ENABLED });
  const sub = useSubscription({ enabled: MOBILE_CHECKOUT_ENABLED });
  const live = isSubscriptionLive(sub.data?.subscription) ? sub.data?.subscription : null;
  const holding = (p: CatalogPrice): Holding => {
    if (p.kind === "recurring") {
      if (live?.price_key === p.key) return "current";
      return live && p.entitlement && live.entitlement === p.entitlement ? "covered" : null;
    }
    const has = !!p.entitlement && !!held.data?.some((e) => e.key === p.entitlement && e.active_until === null);
    return has && p.entitlement_days === null ? "owned" : null;
  };
  const checkout = useCheckout();
  const [buying, setBuying] = useState<string | null>(null);

  if (!MOBILE_CHECKOUT_ENABLED) {
    // Digital goods must use the stores' own purchase flow inside the app; this
    // module doesn't do IAP, so the app offers no purchase here (store-rules.ts).
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "Plans" }} />
        <EmptyState icon={Store} title="Purchases aren't available in the app" />
      </Screen>
    );
  }

  const buy = (p: CatalogPrice) => {
    setBuying(p.key);
    checkout.mutate(
      { price_key: p.key, success_path: "/payments/return?status=success", cancel_path: "/payments/return?status=cancel" },
      {
        onSuccess: async (res) => {
          try {
            await openHostedPage(res.url);
          } finally {
            setBuying(null);
            router.push({ pathname: "/payments/checkout/[id]", params: { id: res.checkout_id } });
          }
        },
        onError: (e) => {
          setBuying(null);
          toast.error(errorMessage(e, "Couldn't open checkout. Try again."));
        },
      },
    );
  };

  return (
    <Screen edges={["bottom"]} gap={theme.space[4]}>
      <Stack.Screen options={{ title: "Plans" }} />
      {prices.isPending ? (
        <>
          <Skeleton height={theme.space[32]} />
          <Skeleton height={theme.space[32]} />
        </>
      ) : prices.isError ? (
        isPaymentsUnconfigured(prices.error) ? (
          <PaymentsUnavailable />
        ) : (
          <ErrorState title="Couldn't load plans" info={prices.error.message} onRetry={() => void prices.refetch()} retrying={prices.isRefetching} />
        )
      ) : prices.data.length === 0 ? (
        <EmptyState icon={Tags} title="No plans available yet" />
      ) : (
        prices.data.map((p) => (
          <PriceCard key={p.key} price={p} holding={holding(p)} busy={buying === p.key} onBuy={() => buy(p)} />
        ))
      )}
    </Screen>
  );
}
