import { exchangeSign, formatExchangeMoney, formatExchangePct, useExchangeConfig, type ExchangeIndexStatus, type ExchangeMarket } from "@scaffoldapp/shared";
import { Badge, Text, useTheme } from "@scaffoldapp/ui-native";
import { useMemo, type ReactNode } from "react";
import { View, type StyleProp, type ViewStyle } from "react-native";
import Svg, { Polyline } from "react-native-svg";

/** The exchange's quote currency and a money formatter for it. */
export function useExchangeMoney() {
  const config = useExchangeConfig();
  const currency = config.data?.quote_currency ?? "USD";
  const decimals = config.data?.quote_decimals ?? 2;
  return useMemo(
    () => ({
      currency,
      decimals,
      config: config.data,
      money: (amount: string | null | undefined, signed = false) => formatExchangeMoney(amount, currency, decimals, signed),
    }),
    [currency, decimals, config.data],
  );
}

/** Text tone for a signed decimal. */
export function signTone(value: string | null | undefined): "success" | "danger" | "muted" {
  const s = exchangeSign(value);
  return s > 0 ? "success" : s < 0 ? "danger" : "muted";
}

export function ChangeText({ pct, role = "caption" }: { pct: string | null | undefined; role?: "caption" | "label" }) {
  return (
    <Text role={role} tone={signTone(pct)} tabular>
      {formatExchangePct(pct)}
    </Text>
  );
}

const indexLabels: Record<ExchangeIndexStatus, string> = { none: "No price", seed: "Seed price", live: "Live", manual: "Manual price", stale: "Stale" };

export function IndexStatusBadge({ status }: { status: ExchangeIndexStatus }) {
  if (status === "live") return null;
  return (
    <Badge size="sm" tone={status === "stale" || status === "none" ? "warning" : "neutral"}>
      {indexLabels[status]}
    </Badge>
  );
}

export function MarketStatusBadge({ market }: { market: Pick<ExchangeMarket, "status"> }) {
  if (market.status === "active") return null;
  return (
    <Badge size="sm" tone={market.status === "halted" ? "danger" : "warning"}>
      {market.status === "halted" ? "Paused" : "Reduce only"}
    </Badge>
  );
}

/** A tiny line of recent closes; colour follows the change over the line. */
export function Sparkline({ closes, width = 72, height = 28, label }: { closes: readonly string[] | undefined; width?: number; height?: number; label: string }) {
  const theme = useTheme();
  const line = useMemo(() => {
    const ys = (closes ?? []).map(Number).filter((n) => Number.isFinite(n));
    if (ys.length < 2) return null;
    const lo = Math.min(...ys);
    const span = Math.max(...ys) - lo || 1;
    return {
      up: (ys[ys.length - 1] ?? 0) >= (ys[0] ?? 0),
      points: ys.map((y, i) => `${((i / (ys.length - 1)) * width).toFixed(1)},${(height - 1 - ((y - lo) / span) * (height - 2)).toFixed(1)}`).join(" "),
    };
  }, [closes, width, height]);
  if (!line) return <View style={{ width, height }} />;
  return (
    <Svg width={width} height={height} accessibilityLabel={label}>
      <Polyline points={line.points} fill="none" stroke={line.up ? theme.colors.success : theme.colors.danger} strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
    </Svg>
  );
}

/** A label above a value. */
export function Stat({ label, children, style }: { label: string; children: ReactNode; style?: StyleProp<ViewStyle> }) {
  const theme = useTheme();
  return (
    <View style={[{ gap: theme.space[0.5], minWidth: 0 }, style]}>
      <Text role="caption" tone="muted">
        {label}
      </Text>
      {typeof children === "string" ? (
        <Text role="label" tabular numberOfLines={1}>
          {children}
        </Text>
      ) : (
        children
      )}
    </View>
  );
}

/** Two stats a row. */
export function StatGrid({ children }: { children: ReactNode[] }) {
  const theme = useTheme();
  const rows: ReactNode[][] = [];
  children.forEach((c, i) => {
    if (i % 2 === 0) rows.push([]);
    rows[rows.length - 1]!.push(c);
  });
  return (
    <View style={{ gap: theme.space[4] }}>
      {rows.map((r, i) => (
        <View key={i} style={{ flexDirection: "row", gap: theme.space[4] }}>
          {r.map((c, j) => (
            <View key={j} style={{ flex: 1, minWidth: 0 }}>
              {c}
            </View>
          ))}
          {r.length === 1 && <View style={{ flex: 1 }} />}
        </View>
      ))}
    </View>
  );
}
