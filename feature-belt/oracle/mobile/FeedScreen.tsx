import {
  ORACLE_STATUS_INFO,
  errorMessage,
  formatOracleDuration,
  formatOraclePct,
  formatOraclePrice,
  oracleBarsSeries,
  oracleChangePct,
  useNow,
  useOracleFeed,
  useOracleHistory,
  type OracleSeriesPoint,
} from "@scaffoldapp/shared";
import { Badge, Caption, EmptyState, ErrorState, Headline, InfoTip, Label, ListGroup, ListRow, Screen, SegmentedControl, Skeleton, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack, useLocalSearchParams } from "expo-router";
import { ScanSearch } from "lucide-react-native";
import { useMemo, useState } from "react";
import { RefreshControl, View } from "react-native";
import { OracleObservationInfo, OraclePriceChart, OracleStatusBadge, OracleVerdictBadge, oracleRelative } from "./components";

type Range = "1d" | "30d" | "90d" | "1y";
const RANGES: { value: Range; label: string }[] = [
  { value: "1d", label: "1D" },
  { value: "30d", label: "30D" },
  { value: "90d", label: "90D" },
  { value: "1y", label: "1Y" },
];
const RANGE_DAYS: Record<Exclude<Range, "1d">, number> = { "30d": 30, "90d": 90, "1y": 365 };

/** One feed: price, status, chart, venues or parts, and its latest screened prints. */
export function OracleFeedScreen() {
  const theme = useTheme();
  const { key } = useLocalSearchParams<{ key: string }>();
  const feedKey = (key ?? "").toUpperCase();
  const now = useNow(30_000);
  const [range, setRange] = useState<Range>("30d");
  const feed = useOracleFeed(feedKey || null);
  const history = useOracleHistory(range === "1d" || !feedKey ? null : feedKey, range === "1d" ? 30 : RANGE_DAYS[range]);
  const d = feed.data;
  const series: OracleSeriesPoint[] = useMemo(() => {
    if (!d) return [];
    if (range === "1d") return d.recent.map((p) => ({ t: Date.parse(p.at), v: Number(p.price) }));
    return oracleBarsSeries(history.data?.items ?? []);
  }, [d, range, history.data]);

  if (feed.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: feedKey }} />
        <Skeleton height={32} width="60%" />
        <Skeleton height={180} />
        <Skeleton height={120} />
      </Screen>
    );
  }
  if (feed.isError || !d) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: feedKey }} />
        {feed.error?.status === 404 ? (
          <EmptyState icon={ScanSearch} title="No such price" />
        ) : (
          <ErrorState title="Couldn't load this price" info={errorMessage(feed.error)} onRetry={() => void feed.refetch()} retrying={feed.isRefetching} />
        )}
      </Screen>
    );
  }
  const f = d.feed;
  const first = series[0]?.v;
  const change = first ? oracleChangePct(String(first), f.price) : null;

  return (
    <Screen edges={["bottom"]} refreshControl={<RefreshControl refreshing={feed.isRefetching} onRefresh={() => void feed.refetch()} />}>
      <Stack.Screen options={{ title: f.key }} />
      <View style={{ gap: theme.space[2] }}>
        <Title>{f.name}</Title>
        <View style={{ flexDirection: "row", alignItems: "center", flexWrap: "wrap", gap: theme.space[2] }}>
          <Headline tabular>{formatOraclePrice(f.price)}</Headline>
          <Label tone="muted">{f.quote}</Label>
          <OracleStatusBadge status={f.status} />
          <InfoTip content={ORACLE_STATUS_INFO[f.status]} label="What the status means" />
        </View>
        <Caption tabular>
          Target {formatOraclePrice(f.target)} · moves at most {d.params.max_move_bps_per_min} bps/min · stale after {formatOracleDuration(d.params.stale_after_s)} · updated{" "}
          {oracleRelative(f.updated_at, now)}
        </Caption>
      </View>

      <View style={{ gap: theme.space[3] }}>
        <SegmentedControl accessibilityLabel="Range" options={RANGES} value={range} onValueChange={setRange} />
        {range !== "1d" && history.isPending ? (
          <Skeleton height={180} />
        ) : range !== "1d" && history.isError ? (
          <ErrorState title="Couldn't load the history" info={errorMessage(history.error)} onRetry={() => void history.refetch()} retrying={history.isRefetching} />
        ) : series.length < 2 ? (
          <EmptyState title={range === "1d" ? "No prices in the last day" : "No history yet"} />
        ) : (
          <View style={{ gap: theme.space[2] }}>
            <Label tone="muted" tabular>
              {formatOraclePct(change)} over {RANGES.find((r) => r.value === range)?.label}
            </Label>
            <OraclePriceChart points={series} label={`${f.name} price, ${formatOraclePct(change)}`} />
          </View>
        )}
      </View>

      {f.kind === "basket" ? (
        <ListGroup title="Parts" info="The basket's price is the sum of quantity × price over its parts.">
          {d.parts.map((p) => (
            <ListRow
              key={p.feed_id}
              title={`${p.quantity} × ${p.name}`}
              trailing={
                <Text role="label" tabular>
                  {formatOraclePrice(p.price)}
                </Text>
              }
              onPress={() => router.push({ pathname: "/oracle/[key]", params: { key: p.key } })}
            />
          ))}
        </ListGroup>
      ) : (
        <ListGroup title="Venues" info="Each venue's latest print and the health of its last fetch.">
          {d.sources.map((s) => (
            <ListRow
              key={s.venue}
              title={s.venue}
              info={s.note || undefined}
              trailing={
                <View style={{ flexDirection: "row", alignItems: "center", gap: theme.space[2] }}>
                  <Text role="label" tabular>
                    {formatOraclePrice(s.last_price)}
                  </Text>
                  <Badge tone={s.ok ? "success" : "warning"} size="sm" dot>
                    {s.ok ? "OK" : "Failing"}
                  </Badge>
                </View>
              }
            />
          ))}
        </ListGroup>
      )}

      <ListGroup title="Observations" info="Every print, scored for manipulation. Accepted prints count fully, downweighted ones partly, rejected ones not at all.">
        {d.observations.length === 0 ? (
          <EmptyState title="No observations yet" />
        ) : (
          d.observations.slice(0, 30).map((o) => (
            <ListRow
              key={o.id}
              title={`${formatOraclePrice(o.raw_price)} · ${o.venue}`}
              info={<OracleObservationInfo o={o} />}
              trailing={<OracleVerdictBadge verdict={o.verdict} />}
            />
          ))
        )}
      </ListGroup>
    </Screen>
  );
}
