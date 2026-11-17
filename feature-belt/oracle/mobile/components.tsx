import {
  ORACLE_STATUS_LABEL,
  ORACLE_VERDICT_LABEL,
  formatOraclePremium,
  formatOraclePrice,
  oracleAreaPath,
  oraclePath,
  oracleScale,
  oracleStatusTone,
  oracleVerdictTone,
  type OracleObservation,
  type OracleSeriesPoint,
  type OracleStatus,
  type OracleVerdict,
} from "@scaffoldapp/shared";
import { Badge, Caption, Text, useTheme } from "@scaffoldapp/ui-native";
import { useState } from "react";
import { View } from "react-native";
import Svg, { Path } from "react-native-svg";

export function OracleStatusBadge({ status }: { status: OracleStatus }) {
  return (
    <Badge tone={oracleStatusTone(status)} size="sm" dot>
      {ORACLE_STATUS_LABEL[status]}
    </Badge>
  );
}

export function OracleVerdictBadge({ verdict }: { verdict: OracleVerdict }) {
  return (
    <Badge tone={oracleVerdictTone(verdict)} size="sm">
      {ORACLE_VERDICT_LABEL[verdict]}
    </Badge>
  );
}

/** "3 min ago", "2 h ago", "4 d ago". */
export function oracleRelative(iso: string | null | undefined, now: number): string {
  if (!iso) return "—";
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86_400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86_400)} d ago`;
}

/** The price line in the primary colour over a soft area, with the high and low. */
export function OraclePriceChart({ points, label }: { points: OracleSeriesPoint[]; label: string }) {
  const theme = useTheme();
  const [width, setWidth] = useState(0);
  const height = 180;
  const { xy, min, max } = oracleScale(points, Math.max(width, 1), height, 8);
  const line = oraclePath(xy);
  const area = oracleAreaPath(xy, height);
  const fmt = (v: number) => v.toLocaleString("en-US", { maximumFractionDigits: v >= 100 ? 2 : 6 });
  return (
    <View style={{ gap: theme.space[1] }} accessible accessibilityRole="image" accessibilityLabel={label}>
      <Caption tabular>High {fmt(max)}</Caption>
      <View style={{ height }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
        {width > 0 && xy.length > 1 ? (
          <Svg width={width} height={height}>
            <Path d={area} fill={theme.colors.primarySoft} />
            <Path d={line} fill="none" stroke={theme.colors.primary} strokeWidth={2} strokeLinejoin="round" />
          </Svg>
        ) : null}
      </View>
      <Caption tabular>Low {fmt(min)}</Caption>
    </View>
  );
}

/** The ⓘ content of an observation row: score, weight, model and every reason. */
export function OracleObservationInfo({ o }: { o: OracleObservation }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[1] }}>
      <Text role="label" tabular>
        Score {o.score.toFixed(2)} · weight {o.weight} · {o.model}
      </Text>
      {o.premium !== 0 ? (
        <Text role="label" tabular>
          Premium {formatOraclePremium(o.premium)} removed: {formatOraclePrice(o.price)}
        </Text>
      ) : null}
      {o.reasons.map((r, i) => (
        <Text key={i} role="label">
          {r}
        </Text>
      ))}
    </View>
  );
}
