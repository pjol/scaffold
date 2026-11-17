import { REPUTATION_STATUS_LABEL, reputationTierLabel, useReputationSummary, type ReputationStatus } from "@scaffoldapp/shared";
import { Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { View } from "react-native";
import Svg, { Circle } from "react-native-svg";

export interface ReputationRingProps {
  /** Null unless rated. */
  score: number | null | undefined;
  status: ReputationStatus;
  tier: string | null | undefined;
  /** Diameter in dp. @default 40 */
  size?: number;
  /** The tier (or status) under the ring. */
  showLabel?: boolean;
}

/** A score as a ring filled to score/100, coloured by tier; unrated and under review draw a dashed ring with "–". */
export function ReputationRing({ score, status, tier, size = 40, showLabel }: ReputationRingProps) {
  const theme = useTheme();
  const c = theme.colors;
  const stroke = Math.max(3, Math.round(size / 12));
  const r = (size - stroke) / 2;
  const circ = 2 * Math.PI * r;
  const rated = status === "rated" && typeof score === "number";
  const frac = rated ? Math.max(0, Math.min(100, score)) / 100 : 0;
  const tierColors: Record<string, string> = { building: c.textMuted, steady: c.info, strong: c.success, elite: c.primary };
  const tierColor = tierColors[tier ?? ""] ?? c.primary;
  const label = rated ? reputationTierLabel(tier) : REPUTATION_STATUS_LABEL[status];
  const role = size >= 72 ? "headline" : size >= 48 ? "titleSm" : "caption";
  return (
    <View
      accessible
      accessibilityRole="image"
      accessibilityLabel={rated ? `Reputation ${score} of 100, ${label}` : `Reputation: ${label.toLowerCase()}`}
      style={{ alignItems: "center", gap: theme.space[1] }}
    >
      <View style={{ width: size, height: size, alignItems: "center", justifyContent: "center" }}>
        <Svg width={size} height={size} style={{ position: "absolute", transform: [{ rotate: "-90deg" }] }}>
          <Circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            stroke={status === "under_review" ? c.warning : c.borderStrong}
            strokeWidth={stroke}
            {...(rated ? {} : { strokeDasharray: `${stroke} ${stroke}` })}
          />
          {rated && (
            <Circle
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              stroke={tierColor}
              strokeWidth={stroke}
              strokeLinecap="round"
              strokeDasharray={`${circ} ${circ}`}
              strokeDashoffset={circ * (1 - frac)}
            />
          )}
        </Svg>
        <Text role={role} weight={600} tabular tone={rated ? "default" : "muted"} maxFontSizeMultiplier={1.5}>
          {rated ? String(score) : "–"}
        </Text>
      </View>
      {showLabel ? (
        <Text role="caption" tone={status === "under_review" ? "warning" : "muted"}>
          {label}
        </Text>
      ) : null}
    </View>
  );
}

/** A person's ring, fetched by id (lists prime the cache with `useReputationSummaries`). Nothing when it can't load. */
export function ReputationBadge({ userId, size = 32, showLabel }: { userId: string; size?: number; showLabel?: boolean }) {
  const q = useReputationSummary(userId);
  if (q.isPending) return <Skeleton shape="circle" width={size} height={size} />;
  if (q.isError) return null;
  return <ReputationRing score={q.data.score} status={q.data.status} tier={q.data.tier} size={size} showLabel={!!showLabel} />;
}
