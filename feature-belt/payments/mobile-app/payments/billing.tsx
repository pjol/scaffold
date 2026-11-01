import { errorMessage, formatMinor, isPaymentsUnconfigured, useEntitlements, usePaymentHistory, usePortal, usePrices, useSubscription, type PaymentLedgerEntry } from "@scaffoldapp/shared";
import { Badge, Banner, Button, EmptyState, ErrorState, ListGroup, ListRow, Screen, Skeleton, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Receipt, Undo2 } from "lucide-react-native";
import { useState } from "react";
import { RefreshControl, View } from "react-native";
import { SUBSCRIPTION_STATUS, formatDate, subscriptionDateLine } from "@/features/payments/labels";
import { PaymentsUnavailable } from "@/features/payments/PaymentsUnavailable";
import { MOBILE_CHECKOUT_ENABLED, openHostedPage } from "@/features/payments/store-rules";

const shortDate = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });

function HistoryRow({ entry }: { entry: PaymentLedgerEntry }) {
  const theme = useTheme();
  const refund = entry.kind === "refund";
  const Icon = refund ? Undo2 : Receipt;
  return (
    <ListRow
      title={entry.description || (refund ? "Refund" : "Payment")}
      leading={<Icon size={22} color={refund ? theme.colors.successStrong : theme.colors.textMuted} strokeWidth={2} />}
      trailing={
        <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[3] }}>
          <Text tone="muted" role="label">
            {shortDate.format(new Date(entry.created_at))}
          </Text>
          <Text weight={600} tabular tone={refund ? "success" : "default"}>
            {formatMinor(entry.amount_minor, entry.currency, { signed: refund })}
          </Text>
        </View>
      }
      chevron={false}
    />
  );
}

/**
 * Settings → Billing: plan status, what the user holds and payment history. The
 * billing portal opens only where the store rules allow (see store-rules.ts).
 */
export default function BillingScreen() {
  const theme = useTheme();
  const sub = useSubscription();
  const ents = useEntitlements();
  const prices = usePrices();
  const history = usePaymentHistory();
  const portal = usePortal();
  const [refreshing, setRefreshing] = useState(false);

  const refresh = async () => {
    setRefreshing(true);
    await Promise.all([sub.refetch(), ents.refetch(), history.refetch()]);
    setRefreshing(false);
  };

  const openPortal = () =>
    portal.mutate(
      { return_path: "/payments/return?status=portal" },
      {
        onSuccess: async (res) => {
          await openHostedPage(res.url);
          void refresh();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't open billing. Try again.")),
      },
    );

  if (sub.isError && isPaymentsUnconfigured(sub.error)) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Billing" }} />
        <PaymentsUnavailable />
      </Screen>
    );
  }

  const s = sub.data?.subscription ?? null;
  const name = (key: string) => prices.data?.find((p) => p.entitlement === key)?.label ?? key;
  const rows = history.data?.items ?? [];

  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={() => void refresh()} />}>
      <Stack.Screen options={{ title: "Billing" }} />

      {sub.isPending ? (
        <Skeleton height={theme.space[24]} />
      ) : sub.isError ? (
        <ErrorState compact title="Couldn't load your plan" info={sub.error.message} onRetry={() => void sub.refetch()} retrying={sub.isRefetching} />
      ) : s ? (
        <View style={{ gap: theme.space[4] }}>
          {s.status === "past_due" || s.status === "unpaid" ? (
            <Banner
              tone="warning"
              title="Your last payment failed. Update your card to keep your plan."
              {...(MOBILE_CHECKOUT_ENABLED ? { action: { label: "Update card", onPress: openPortal } } : {})}
            />
          ) : null}
          <ListGroup title="Plan">
            <ListRow title={s.label} trailing={<Badge tone={SUBSCRIPTION_STATUS[s.status].tone}>{SUBSCRIPTION_STATUS[s.status].label}</Badge>} chevron={false} />
            {subscriptionDateLine(s) ? <ListRow title={subscriptionDateLine(s) ?? ""} chevron={false} /> : null}
            {MOBILE_CHECKOUT_ENABLED ? <ListRow title="Manage billing" onPress={openPortal} disabled={portal.isPending} /> : null}
          </ListGroup>
        </View>
      ) : (
        <EmptyState
          compact
          icon={Receipt}
          title="No plan"
          {...(MOBILE_CHECKOUT_ENABLED
            ? {
                action: (
                  <Button variant="secondary" onPress={() => router.push("/payments/plans")}>
                    See plans
                  </Button>
                ),
              }
            : {})}
        />
      )}

      {ents.data && ents.data.length > 0 ? (
        <ListGroup title="Access">
          {ents.data.map((e) => (
            <ListRow key={e.key} title={name(e.key)} trailing={<Text tone="muted">{e.active_until ? `Until ${formatDate(e.active_until)}` : "No expiry"}</Text>} chevron={false} />
          ))}
        </ListGroup>
      ) : null}

      <View style={{ gap: theme.space[3] }}>
        {history.isPending ? (
          <ListGroup title="History">
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </ListGroup>
        ) : history.isError ? (
          <ErrorState compact title="Couldn't load your history" info={history.error.message} onRetry={() => void history.refetch()} retrying={history.isRefetching} />
        ) : rows.length === 0 ? (
          <ListGroup title="History">
            <EmptyState compact icon={Receipt} title="No payments yet" />
          </ListGroup>
        ) : (
          <ListGroup title="History">
            {rows.map((e) => (
              <HistoryRow key={e.id} entry={e} />
            ))}
          </ListGroup>
        )}
        {history.hasNextPage ? (
          <Button variant="ghost" loading={history.isFetchingNextPage} onPress={() => void history.fetchNextPage()}>
            Load more
          </Button>
        ) : null}
      </View>
    </Screen>
  );
}
