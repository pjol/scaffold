import { CRM_BILLING_PERIOD_LABEL, CRM_QUOTE_STATUS_LABEL, crmMoney, crmPercent, useCrmQuote } from "@scaffoldapp/shared";
import { Badge, Button, EmptyState, ErrorState, Headline, ListGroup, ListRow, Screen, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack } from "expo-router";
import { FileX, Share2 } from "lucide-react-native";
import { Share, View } from "react-native";
import { CrmGate } from "./parts";

/** A quote, read only: lines, totals, status; share its link (a person's action) when published. */
export function CrmQuoteScreen({ id }: { id: string }) {
  return <CrmGate>{() => <QuoteView id={id} />}</CrmGate>;
}

function QuoteView({ id }: { id: string }) {
  const theme = useTheme();
  const quote = useCrmQuote(id);
  if (quote.isPending) {
    return (
      <Screen edges={["bottom"]} gap={theme.space[4]}>
        <Stack.Screen options={{ title: "Quote" }} />
        <Skeleton height={theme.tap.lg * 2} />
        <Skeleton height={theme.tap.lg * 4} />
      </Screen>
    );
  }
  if (quote.isError) {
    return (
      <Screen edges={["bottom"]} centered>
        <Stack.Screen options={{ title: "Quote" }} />
        {quote.error.status === 404 ? <EmptyState icon={FileX} title="Quote not found" /> : <ErrorState title="Couldn't load the quote" info={quote.error.message} onRetry={() => void quote.refetch()} />}
      </Screen>
    );
  }
  const q = quote.data;
  const money = (n: number) => crmMoney(n, q.currency, { cents: true });
  const k = q.expired && q.status === "published" ? "expired" : q.status;
  const share = () => {
    if (!q.url) return;
    Share.share({ message: `${q.title} (${q.number}): ${q.url}`, url: q.url }).catch(() => toast.error("Couldn't open sharing."));
  };
  return (
    <Screen edges={["bottom"]} gap={theme.space[4]}>
      <Stack.Screen options={{ title: q.number }} />
      <View style={{ gap: theme.space[2] }}>
        <Headline>{q.title}</Headline>
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2], alignItems: "center" }}>
          <Badge tone={k === "accepted" ? "success" : k === "declined" ? "danger" : k === "expired" ? "warning" : k === "published" ? "info" : "neutral"}>{CRM_QUOTE_STATUS_LABEL[k]}</Badge>
          <Text tone="muted">{q.deal.name}</Text>
        </View>
      </View>
      {q.url ? (
        <Button variant="secondary" leftIcon={Share2} onPress={share}>
          Share link
        </Button>
      ) : null}
      <ListGroup title="Details">
        {q.contact ? <ListRow title="Prepared for" trailing={<Text role="label">{q.contact.name}</Text>} /> : null}
        <ListRow title="Valid until" trailing={<Text role="label">{new Date(q.expires_at).toLocaleDateString()}</Text>} />
        {q.status !== "draft" ? <ListRow title="Views" trailing={<Text role="label">{String(q.view_count)}</Text>} /> : null}
        {q.signer_name ? <ListRow title={q.status === "accepted" ? "Accepted by" : "Declined by"} trailing={<Text role="label">{q.signer_name}</Text>} /> : null}
      </ListGroup>
      <ListGroup title="Lines">
        {q.items.length === 0 ? (
          <ListRow title="No lines" />
        ) : (
          q.items.map((it) => (
            <ListRow
              key={it.id}
              title={it.name}
              info={[`${it.quantity} × ${money(it.unit_price)}`, it.discount_bp ? `${crmPercent(it.discount_bp)} off` : "", it.tax_bp ? `Tax ${crmPercent(it.tax_bp)}` : ""].filter(Boolean).join(" · ")}
              trailing={
                <Text role="label" weight={600} tabular>
                  {money(it.total)}
                  {it.billing === "recurring" && it.billing_period ? ` ${CRM_BILLING_PERIOD_LABEL[it.billing_period].per}` : ""}
                </Text>
              }
            />
          ))
        )}
      </ListGroup>
      <ListGroup title="Totals">
        <ListRow title="Subtotal" trailing={<Text role="label" tabular>{money(q.totals.subtotal)}</Text>} />
        {q.totals.discount ? <ListRow title="Discount" trailing={<Text role="label" tabular>{`−${money(q.totals.discount)}`}</Text>} /> : null}
        {q.totals.tax ? <ListRow title="Tax" trailing={<Text role="label" tabular>{money(q.totals.tax)}</Text>} /> : null}
        <ListRow title="Total" trailing={<Text role="label" weight={600} tabular>{money(q.totals.total)}</Text>} />
        {q.totals.contract_value !== q.totals.total ? <ListRow title="Contract value" trailing={<Text role="label" weight={600} tabular>{money(q.totals.contract_value)}</Text>} /> : null}
      </ListGroup>
      {q.terms ? (
        <View style={{ gap: theme.space[1] }}>
          <Text role="label" weight={600}>
            Terms
          </Text>
          <Text tone="muted">{q.terms}</Text>
        </View>
      ) : null}
    </Screen>
  );
}
