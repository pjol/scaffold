import { CRM_BILLING_PERIOD_LABEL, crmMoney, crmPercent, useCrmLineItems, useCrmQuotes, type CrmDeal } from "@scaffoldapp/shared";
import { Badge, ListGroup, ListRow, Skeleton, Text, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { FileText, Package } from "lucide-react-native";
import { View } from "react-native";

const QUOTE_TONE: Record<string, BadgeTone> = { draft: "neutral", published: "info", accepted: "success", declined: "danger", expired: "warning" };
const QUOTE_LABEL: Record<string, string> = { draft: "Draft", published: "Published", accepted: "Accepted", declined: "Declined", expired: "Expired" };

/** A deal's line items (read) and quotes (open one to view it or share its link). */
export function DealCommerce({ deal }: { deal: CrmDeal }) {
  const theme = useTheme();
  const items = useCrmLineItems(deal.id);
  const quotes = useCrmQuotes(deal.id);
  const money = (n: number) => crmMoney(n, items.data?.currency ?? deal.currency, { cents: true });
  return (
    <View style={{ gap: theme.space[4] }}>
      <ListGroup title="Line items">
        {items.isPending ? (
          <Skeleton height={theme.tap.lg} />
        ) : items.isError ? (
          <ListRow title="Couldn't load line items" />
        ) : items.data.items.length === 0 ? (
          <ListRow title="No line items" />
        ) : (
          <>
            {items.data.items.map((li) => {
              const facts = [`${li.quantity} × ${money(li.unit_price)}`, li.discount_bp ? `${crmPercent(li.discount_bp)} off` : li.discount_amount ? `${money(li.discount_amount)} off` : "", li.tax_bp ? `Tax ${crmPercent(li.tax_bp)}` : ""].filter(Boolean);
              return (
                <ListRow
                  key={li.id}
                  title={li.name}
                  leading={<Package size={20} color={theme.colors.textMuted} />}
                  info={facts.join(" · ")}
                  trailing={
                    <Text role="label" weight={600} tabular>
                      {money(li.total)}
                      {li.billing === "recurring" && li.billing_period ? ` ${CRM_BILLING_PERIOD_LABEL[li.billing_period].per}` : ""}
                    </Text>
                  }
                />
              );
            })}
            <ListRow title="Total" trailing={<Text role="label" weight={600} tabular>{money(items.data.totals.total)}</Text>} />
            {items.data.totals.contract_value !== items.data.totals.total ? (
              <ListRow title="Contract value" trailing={<Text role="label" weight={600} tabular>{money(items.data.totals.contract_value)}</Text>} />
            ) : null}
          </>
        )}
      </ListGroup>
      <ListGroup title="Quotes">
        {quotes.isPending ? (
          <Skeleton height={theme.tap.lg} />
        ) : quotes.isError ? (
          <ListRow title="Couldn't load quotes" />
        ) : quotes.data.items.length === 0 ? (
          <ListRow title="No quotes" />
        ) : (
          quotes.data.items.map((q) => {
            const k = q.expired && q.status === "published" ? "expired" : q.status;
            return (
              <ListRow
                key={q.id}
                title={`${q.number} · ${q.title}`}
                leading={<FileText size={20} color={theme.colors.textMuted} />}
                trailing={<Badge tone={QUOTE_TONE[k] ?? "neutral"}>{QUOTE_LABEL[k] ?? k}</Badge>}
                onPress={() => router.push(`/crm/quotes/${q.id}`)}
              />
            );
          })
        )}
      </ListGroup>
    </View>
  );
}
