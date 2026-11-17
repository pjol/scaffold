import { EXCHANGE_INTERVALS, formatExchangePrice, useExchangeCandles, type ExchangeCandleInterval, type ExchangeMarket } from "@scaffoldapp/shared";
import { ErrorState, IconButton, SegmentedControl, Skeleton, Spinner, Text, useTheme } from "@scaffoldapp/ui-native";
import { ChevronLeft, ChevronRight } from "lucide-react-native";
import { useEffect, useMemo, useState } from "react";
import { View, type LayoutChangeEvent } from "react-native";
import Svg, { G, Line, Rect } from "react-native-svg";

const LABELS: Record<ExchangeCandleInterval, string> = { "1m": "1m", "5m": "5m", "15m": "15m", "1h": "1h", "4h": "4h", "1d": "1D" };
const BAR = 8; // px per candle slot
const HEIGHT = 220;
const VOL = 40;

/**
 * Candles and volume for one market. The newest candle updates live; Earlier and Later
 * page through history, loading older pages from the server as needed.
 */
export function CandleChart({ market }: { market: ExchangeMarket }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const [interval, setIv] = useState<ExchangeCandleInterval>("5m");
  const data = useExchangeCandles(market.symbol, interval);
  const [width, setWidth] = useState(0);
  const [offset, setOffset] = useState(0); // candles hidden at the right edge
  useEffect(() => setOffset(0), [interval]);

  const slots = Math.max(10, Math.floor(width / BAR));
  const all = data.candles;
  const end = Math.max(0, all.length - offset);
  const start = Math.max(0, end - slots);
  const view = all.slice(start, end);
  const atOldest = start === 0;
  const { fetchOlder, hasOlder } = data;
  useEffect(() => {
    if (atOldest && hasOlder && offset > 0) fetchOlder();
  }, [atOldest, hasOlder, offset]); // eslint-disable-line react-hooks/exhaustive-deps

  const geo = useMemo(() => {
    if (view.length === 0) return null;
    let lo = Infinity;
    let hi = -Infinity;
    let vmax = 0;
    for (const k of view) {
      lo = Math.min(lo, Number(k.l));
      hi = Math.max(hi, Number(k.h));
      vmax = Math.max(vmax, Number(k.v));
    }
    const span = hi - lo || hi * 0.01 || 1;
    const top = HEIGHT - VOL - 8;
    const y = (p: number) => 4 + ((hi - p) / span) * (top - 4);
    return { lo, hi, vmax, y, top };
  }, [view]);

  const onLayout = (e: LayoutChangeEvent) => setWidth(e.nativeEvent.layout.width);
  const pageBack = () => {
    if (atOldest && data.hasOlder) data.fetchOlder();
    setOffset((o) => Math.min(o + Math.floor(slots / 2), Math.max(0, all.length - slots)));
  };
  const last = view[view.length - 1];

  return (
    <View style={{ gap: s[3] }}>
      <SegmentedControl<ExchangeCandleInterval>
        accessibilityLabel="Candle interval"
        options={EXCHANGE_INTERVALS.map((v) => ({ value: v, label: LABELS[v] }))}
        value={interval}
        onValueChange={setIv}
      />
      <View onLayout={onLayout} style={{ height: HEIGHT }} accessibilityRole="image" accessibilityLabel={`${market.name} price chart, ${LABELS[interval]} candles`}>
        {data.isPending ? (
          <Skeleton height={HEIGHT} />
        ) : data.isError ? (
          <ErrorState compact title="Couldn't load the chart" info={data.error?.message} onRetry={data.refetch} />
        ) : !geo || width === 0 ? (
          <View style={{ flex: 1, alignItems: "center", justifyContent: "center" }}>
            <Text role="label" tone="muted">
              No trading history yet
            </Text>
          </View>
        ) : (
          <>
            <Svg width={width} height={HEIGHT}>
              <Line x1={0} x2={width} y1={geo.top + 4} y2={geo.top + 4} stroke={c.border} strokeWidth={1} />
              {view.map((k, i) => {
                const x = i * BAR + BAR / 2 + (slots - view.length) * BAR;
                const o = Number(k.o);
                const cl = Number(k.c);
                const up = cl >= o;
                const color = up ? c.success : c.danger;
                const yTop = geo.y(Math.max(o, cl));
                const yBot = geo.y(Math.min(o, cl));
                const vh = geo.vmax > 0 ? (Number(k.v) / geo.vmax) * (VOL - 4) : 0;
                return (
                  <G key={k.t}>
                    <Line x1={x} x2={x} y1={geo.y(Number(k.h))} y2={geo.y(Number(k.l))} stroke={color} strokeWidth={1} />
                    <Rect x={x - BAR * 0.35} y={yTop} width={BAR * 0.7} height={Math.max(1, yBot - yTop)} fill={color} opacity={k.backfill ? 0.5 : 1} />
                    {vh > 0 && <Rect x={x - BAR * 0.35} y={HEIGHT - vh} width={BAR * 0.7} height={vh} fill={up ? c.successSoft : c.dangerSoft} />}
                  </G>
                );
              })}
            </Svg>
            <View pointerEvents="none" style={{ position: "absolute", left: 0, top: 0 }}>
              <Text role="caption" tone="muted" tabular>
                {formatExchangePrice(String(geo.hi), market.tick_size)}
              </Text>
            </View>
            <View pointerEvents="none" style={{ position: "absolute", left: 0, top: geo.top - theme.type.caption.lineHeight }}>
              <Text role="caption" tone="muted" tabular>
                {formatExchangePrice(String(geo.lo), market.tick_size)}
              </Text>
            </View>
          </>
        )}
      </View>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <IconButton label="Earlier" icon={ChevronLeft} variant="ghost" onPress={pageBack} />
        <View style={{ flex: 1, alignItems: "center" }}>
          {data.isFetchingOlder ? (
            <Spinner />
          ) : last ? (
            <Text role="caption" tone="muted" tabular>
              {new Date(view[0]!.t).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })} –{" "}
              {offset === 0 ? "now" : new Date(last.t).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })}
            </Text>
          ) : null}
        </View>
        <IconButton label="Later" icon={ChevronRight} variant="ghost" onPress={() => setOffset((o) => Math.max(0, o - Math.floor(slots / 2)))} />
      </View>
    </View>
  );
}
