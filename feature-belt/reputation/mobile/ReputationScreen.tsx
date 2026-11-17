import {
  REPUTATION_STATUS_INFO,
  REPUTATION_STATUS_LABEL,
  errorMessage,
  formatReputationAmount,
  formatReputationPct,
  formatReputationWeight,
  reputationEquityScale,
  reputationLinePath,
  reputationMetricLabel,
  reputationTierLabel,
  reputationTierTone,
  useMyReputation,
  type Reputation,
  type ReputationComponent,
  type ReputationEquityPoint,
} from "@scaffoldapp/shared";
import { Badge, Caption, EmptyState, ErrorState, InfoTip, Label, ListGroup, ListRow, Screen, Skeleton, Text, TitleSm, useTheme } from "@scaffoldapp/ui-native";
import { CheckCircle2, Circle, LineChart } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { RefreshControl, View } from "react-native";
import Svg, { Line, Path } from "react-native-svg";
import { ReputationRing } from "./ReputationRing";

function Section({ title, info, children }: { title: string; info?: string; children: ReactNode }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
        <TitleSm>{title}</TitleSm>
        {info ? <InfoTip content={info} /> : null}
      </View>
      {children}
    </View>
  );
}

function ComponentItem({ c }: { c: ReputationComponent }) {
  const theme = useTheme();
  const s = theme.space;
  const metrics = Object.entries(c.metrics);
  return (
    <View style={{ gap: s[3], paddingVertical: s[4], borderBottomWidth: 1, borderBottomColor: theme.colors.border }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Label weight={600} tone={c.scored ? "default" : "muted"}>
          {c.label}
        </Label>
        <InfoTip content={c.detail} label={`How ${c.label.toLowerCase()} is scored`} />
        <View style={{ flex: 1 }} />
        <Label tabular>{c.scored ? `${c.points.toFixed(1)} pts` : "Not scored"}</Label>
      </View>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
        <View style={{ flex: 1, height: s[2], borderRadius: theme.radius.pill, backgroundColor: theme.colors.sunken, overflow: "hidden" }}>
          {c.scored ? <View style={{ width: `${Math.max(1, Math.min(100, c.value))}%`, height: "100%", backgroundColor: theme.colors.primary }} /> : null}
        </View>
        <Caption tabular>{c.scored ? `${c.value.toFixed(1)} × ${formatReputationWeight(c.weight)}` : "0%"}</Caption>
      </View>
      {metrics.map(([k, v]) => (
        <View key={k} style={{ flexDirection: "row", justifyContent: "space-between", gap: s[3] }}>
          <Caption style={{ flexShrink: 1 }}>{reputationMetricLabel(k)}</Caption>
          <Text role="caption" tabular>
            {v}
          </Text>
        </View>
      ))}
    </View>
  );
}

function EquityChart({ points }: { points: ReputationEquityPoint[] }) {
  const theme = useTheme();
  const [width, setWidth] = useState(0);
  const height = 140;
  if (points.length < 2) return <EmptyState compact icon={LineChart} title="Not enough days for a curve yet" />;
  const { xy, min, max, baseline } = reputationEquityScale(points, Math.max(width, 1), height, 6);
  const last = points[points.length - 1]!;
  return (
    <View style={{ gap: theme.space[1] }} accessible accessibilityRole="image" accessibilityLabel={`Value index over ${points.length} days, from 100 to ${last.index}`}>
      <View style={{ flexDirection: "row", justifyContent: "space-between" }}>
        <Caption tabular>High {max.toFixed(2)}</Caption>
        <Caption tabular>Now {Number(last.index).toFixed(2)}</Caption>
      </View>
      <View style={{ height }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
        {width > 0 ? (
          <Svg width={width} height={height}>
            <Line x1={0} x2={width} y1={baseline} y2={baseline} stroke={theme.colors.borderStrong} strokeDasharray="4 4" />
            <Path d={reputationLinePath(xy)} fill="none" stroke={theme.colors.primary} strokeWidth={2} strokeLinejoin="round" />
          </Svg>
        ) : null}
      </View>
      <View style={{ flexDirection: "row", justifyContent: "space-between" }}>
        <Caption tabular>Low {min.toFixed(2)}</Caption>
        <Caption tabular>{points[0]!.day}</Caption>
      </View>
    </View>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  const theme = useTheme();
  return (
    <View style={{ width: "50%", gap: theme.space[1], paddingRight: theme.space[3] }}>
      <Caption>{label}</Caption>
      <Text role="titleSm" tabular>
        {value}
      </Text>
    </View>
  );
}

function Detail({ rep }: { rep: Reputation }) {
  const theme = useTheme();
  const s = theme.space;
  const rated = rep.status === "rated" && rep.score !== null;
  const total = rep.components.reduce((n, c) => n + (c.scored ? c.points : 0), 0);
  const st = rep.stats;
  return (
    <>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[4] }}>
        <ReputationRing score={rep.score} status={rep.status} tier={rep.tier} size={80} />
        <View style={{ flex: 1, gap: s[2] }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Text role="titleSm" tabular>{rated ? `${rep.score} of 100` : REPUTATION_STATUS_LABEL[rep.status]}</Text>
            <InfoTip content={REPUTATION_STATUS_INFO[rep.status]} label="About this score" />
          </View>
          {rated && rep.tier ? (
            <Badge tone={reputationTierTone(rep.tier)} style={{ alignSelf: "flex-start" }}>
              {reputationTierLabel(rep.tier)}
            </Badge>
          ) : null}
          {rep.status === "under_review" ? (
            <Badge tone="warning" dot style={{ alignSelf: "flex-start" }}>
              Score hidden
            </Badge>
          ) : null}
        </View>
      </View>

      <Section
        title="How the score is made"
        info="Each part scores 0 to 100. A part's weight is its share of the parts that have data; parts without data give their weight to the rest. The score is the sum of the points, rounded."
      >
        <View>
          {rep.components.map((c) => (
            <ComponentItem key={c.key} c={c} />
          ))}
          <View style={{ flexDirection: "row", justifyContent: "space-between", paddingTop: s[4] }}>
            <Label weight={600}>Total</Label>
            <Label tabular>
              {total.toFixed(1)} pts{rated ? ` → ${rep.score}` : ""}
            </Label>
          </View>
        </View>
      </Section>

      <Section title="Requirements" info="A score shows once all of these are met.">
        <ListGroup>
          {rep.requirements.map((q) => (
            <ListRow
              key={q.key}
              title={q.label}
              leading={q.met ? <CheckCircle2 size={20} color={theme.colors.successStrong} /> : <Circle size={20} color={theme.colors.textMuted} />}
              trailing={<Label tabular>{q.progress}</Label>}
              accessibilityHint={q.met ? "Met" : "Not met"}
            />
          ))}
        </ListGroup>
      </Section>

      <Section title="Last 90 days" info="Returns are time-weighted: money put in or taken out never counts as a gain or a loss, so a deposit can't hide a drawdown.">
        <View style={{ flexDirection: "row", flexWrap: "wrap", rowGap: s[4] }}>
          <Stat label="Return, 30 days" value={formatReputationPct(st.return_30d_pct)} />
          <Stat label="Return, 90 days" value={formatReputationPct(st.return_90d_pct)} />
          <Stat label="Gain, 30 days" value={formatReputationAmount(st.gain_30d)} />
          <Stat label="Max drawdown" value={formatReputationPct(st.max_drawdown_90d_pct, false)} />
          <Stat label="Volume, 30 days" value={formatReputationAmount(st.volume_30d)} />
          <Stat label="Activity, 30 days" value={String(st.activity_30d)} />
          <Stat label="Active days" value={String(st.active_days_90d)} />
          <Stat label="Incidents" value={String(st.incidents_90d)} />
        </View>
        <EquityChart points={st.equity} />
      </Section>
    </>
  );
}

/** Your reputation: the score, every component with its weight and inputs, requirements, stats and the 90-day curve. */
export function ReputationScreen() {
  const theme = useTheme();
  const rep = useMyReputation();
  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={rep.isRefetching} onRefresh={() => void rep.refetch()} />}>
      {rep.isPending ? (
        <View style={{ gap: theme.space[6] }} accessibilityLabel="Loading your reputation" accessibilityRole="progressbar">
          <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[4] }}>
            <Skeleton shape="circle" width={80} height={80} />
            <Skeleton height={24} width="40%" />
          </View>
          <Skeleton height={280} />
          <Skeleton height={180} />
        </View>
      ) : rep.isError ? (
        <ErrorState title="Couldn't load your reputation" info={errorMessage(rep.error)} onRetry={() => void rep.refetch()} retrying={rep.isRefetching} />
      ) : (
        <Detail rep={rep.data} />
      )}
    </Screen>
  );
}
