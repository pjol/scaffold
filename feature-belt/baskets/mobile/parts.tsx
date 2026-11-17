import { BASKETS_STATUS_LABEL, formatBasketsWeight, type BasketsLine, type BasketsPriceStatus, type BasketsSummary } from "@scaffoldapp/shared";
import { Badge, Text, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { Pressable, View } from "react-native";

/** A basket's status when it isn't simply trading. */
export function BasketStatusBadge({ basket }: { basket: Pick<BasketsSummary, "status"> }) {
  if (basket.status === "active") return null;
  return (
    <Badge tone={basket.status === "archived" ? "neutral" : "warning"} size="sm">
      {BASKETS_STATUS_LABEL[basket.status]}
    </Badge>
  );
}

const priceLabels: Partial<Record<BasketsPriceStatus, string>> = { stale: "Stale", seed: "Not live", off: "Off" };

/** The oracle's status of a price when it isn't live. */
export function PriceStatusBadge({ status }: { status: BasketsPriceStatus | undefined }) {
  const label = status ? priceLabels[status] : undefined;
  if (!label) return null;
  return (
    <Badge tone={status === "stale" || status === "off" ? "warning" : "neutral"} size="sm">
      {label}
    </Badge>
  );
}

/** "Gold · Silver +2". */
export function topLinesText(b: Pick<BasketsSummary, "top_lines" | "line_count">): string {
  const more = b.line_count - b.top_lines.length;
  return b.top_lines.join(" · ") + (more > 0 ? ` +${more}` : "");
}

/** One basket in a list: name, symbol and largest lines; entries on the right. */
export function BasketRow({ b }: { b: BasketsSummary }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${b.name}, ${b.symbol}, ${b.entries} entries`}
      onPress={() => router.push({ pathname: "/baskets/[symbol]", params: { symbol: b.symbol } })}
      style={({ pressed }) => ({
        flexDirection: "row",
        alignItems: "center",
        gap: s[3],
        paddingVertical: s[3],
        minHeight: theme.tap.xl,
        borderBottomWidth: 1,
        borderBottomColor: theme.colors.border,
        backgroundColor: pressed ? theme.colors.sunken : "transparent",
      })}
    >
      <View style={{ flex: 1, minWidth: 0, gap: s[0.5] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="label" weight={600} numberOfLines={1} style={{ flexShrink: 1 }}>
            {b.name}
          </Text>
          <BasketStatusBadge basket={b} />
        </View>
        <Text role="caption" tone="muted" numberOfLines={1}>
          {b.symbol} · {topLinesText(b)}
        </Text>
      </View>
      <View style={{ alignItems: "flex-end" }}>
        <Text role="caption" tone="muted">
          Entries
        </Text>
        <Text role="label" tabular>
          {b.entries}
        </Text>
      </View>
    </Pressable>
  );
}

/** A basket's lines: quantity × market, price and weight; a press opens the market. */
export function BasketLines({ lines, money }: { lines: BasketsLine[]; money: (v: string | null) => string }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View>
      {lines.map((l) => (
        <Pressable
          key={l.symbol}
          accessibilityRole="button"
          accessibilityLabel={`${l.quantity} ${l.name}, ${formatBasketsWeight(l.weight)} of the basket`}
          onPress={() => router.push({ pathname: "/markets/[symbol]", params: { symbol: l.symbol } })}
          style={({ pressed }) => ({
            flexDirection: "row",
            alignItems: "center",
            gap: s[3],
            paddingVertical: s[2],
            minHeight: theme.tap.base,
            borderBottomWidth: 1,
            borderBottomColor: theme.colors.border,
            backgroundColor: pressed ? theme.colors.sunken : "transparent",
          })}
        >
          <Text role="label" tone="muted" tabular style={{ minWidth: s[10], textAlign: "right" }}>
            {l.quantity}×
          </Text>
          <View style={{ flex: 1, minWidth: 0, gap: s[0.5] }}>
            <Text role="label" numberOfLines={1}>
              {l.name}
            </Text>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Text role="caption" tone="muted" tabular numberOfLines={1}>
                {l.symbol} · {money(l.price)}
              </Text>
              <PriceStatusBadge status={l.price_status} />
            </View>
          </View>
          <Text role="label" tabular>
            {formatBasketsWeight(l.weight)}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}
