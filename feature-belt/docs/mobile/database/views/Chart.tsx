import { docsFormatComputed, docsOptionHue, docsTimeZone, errorMessage, useDocsChart, type DocsChartData, type DocsOptionColor, type DocsProperty } from "@scaffoldapp/shared";
import { Chip, ErrorState, Skeleton, Text, useTheme, type NativeTheme } from "@scaffoldapp/ui-native";
import { useMemo, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import Svg, { Circle, G, Line, Path, Rect, Text as SvgText } from "react-native-svg";
import type { ViewCtx } from "../context";

/** Series hues in a fixed order (validated for colour-vision separation in light mode); later ones fold into "Other". */
const ORDER = ["blue", "orange", "purple", "yellow", "pink", "green", "red", "brown"] as const;

function hueColor(theme: NativeTheme, color: DocsOptionColor | undefined, index: number): string {
  const hue = docsOptionHue(color);
  if (hue) return theme.swatches[hue].fg;
  return theme.swatches[ORDER[index % ORDER.length]!].fg;
}

interface Series {
  key: string;
  label: string;
  color: string;
}

interface Point {
  key: string;
  label: string;
  value: number;
  color: string;
  parts: number[];
}

/** Clean axis steps: 0, 1, 2, 5 × 10^n. */
function niceMax(v: number): { max: number; step: number } {
  if (v <= 0) return { max: 1, step: 1 };
  const raw = v / 4;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const n = raw / mag;
  const step = (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * mag;
  return { max: Math.ceil(v / step) * step, step };
}

function compact(v: number): string {
  return new Intl.NumberFormat(undefined, { notation: Math.abs(v) >= 10_000 ? "compact" : "standard", maximumFractionDigits: 1 }).format(v);
}

/** Rows chart data into points and series with colours (series beyond 8 add up as "Other"). */
function shape(theme: NativeTheme, data: DocsChartData): { points: Point[]; series: Series[] } {
  const visible = data.series.filter((s) => !s.hidden);
  const keep = visible.slice(0, ORDER.length);
  const rest = visible.slice(ORDER.length);
  const series: Series[] = keep.map((s, i) => ({ key: s.key, label: s.label, color: hueColor(theme, s.color, i) }));
  if (rest.length) series.push({ key: "__other", label: "Other", color: theme.swatches.gray.fg });
  const points = data.points.map((p, i) => {
    const parts = series.length
      ? series.map((s) => (s.key === "__other" ? rest.reduce((a, r) => a + (p.series?.[r.key] ?? 0), 0) : (p.series?.[s.key] ?? 0)))
      : [p.value];
    return { key: p.key, label: p.label, value: p.value, color: series.length ? "" : p.color ? hueColor(theme, p.color, i) : theme.colors.primary, parts };
  });
  return { points, series };
}

function Legend({ series }: { series: Series[] }) {
  const s = useTheme().space;
  if (series.length < 2) return null;
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[3] }}>
      {series.map((x) => (
        <View key={x.key} style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
          <View style={{ width: s[3], height: s[2], borderRadius: 2, backgroundColor: x.color }} />
          <Text role="caption" tone="muted">
            {x.label}
          </Text>
        </View>
      ))}
    </View>
  );
}

function ValuesTable({ points, series, fmt }: { points: Point[]; series: Series[]; fmt: (v: number) => string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const cols = series.length > 1 ? series : [];
  return (
    <ScrollView horizontal>
      <View style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md }}>
        <View style={{ flexDirection: "row", backgroundColor: c.sunken }}>
          <Text role="label" tone="muted" weight={600} style={{ width: 144, padding: s[2] }}>
            Group
          </Text>
          {cols.map((x) => (
            <Text key={x.key} role="label" tone="muted" weight={600} numberOfLines={1} style={{ width: 96, padding: s[2], textAlign: "right" }}>
              {x.label}
            </Text>
          ))}
          <Text role="label" tone="muted" weight={600} style={{ width: 96, padding: s[2], textAlign: "right" }}>
            Total
          </Text>
        </View>
        {points.map((p) => (
          <View key={p.key} style={{ flexDirection: "row", borderTopWidth: 1, borderTopColor: c.border }}>
            <Text role="label" numberOfLines={2} style={{ width: 144, padding: s[2] }}>
              {p.label}
            </Text>
            {cols.map((x, i) => (
              <Text key={x.key} role="label" tabular style={{ width: 96, padding: s[2], textAlign: "right" }}>
                {fmt(p.parts[i] ?? 0)}
              </Text>
            ))}
            <Text role="label" tabular weight={600} style={{ width: 96, padding: s[2], textAlign: "right" }}>
              {fmt(p.value)}
            </Text>
          </View>
        ))}
      </View>
    </ScrollView>
  );
}

/** Columns or bars from a baseline: 4 dp rounded data end, square at the base. */
function barPath(x: number, y: number, w: number, h: number, horizontal: boolean, round: boolean): string {
  const r = round ? Math.min(4, (horizontal ? h : w) / 2, horizontal ? w : h) : 0;
  if (horizontal) return `M${x} ${y} H${x + w - r} Q${x + w} ${y} ${x + w} ${y + r} V${y + h - r} Q${x + w} ${y + h} ${x + w - r} ${y + h} H${x} Z`;
  return `M${x} ${y + h} V${y + r} Q${x} ${y} ${x + r} ${y} H${x + w - r} Q${x + w} ${y} ${x + w} ${y + r} V${y + h} Z`;
}

function Columns({ points, series, fmt, width, showValues, selected, onSelect }: { points: Point[]; series: Series[]; fmt: (v: number) => string; width: number; showValues: boolean; selected: string | null; onSelect: (k: string) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const H = 200;
  const left = 40;
  const bottom = 40;
  const band = Math.max(36, (width - left) / Math.max(points.length, 1));
  const bar = Math.min(24, band * 0.6);
  const total = left + band * points.length;
  const { max, step } = niceMax(Math.max(...points.map((p) => p.value), 0));
  const y = (v: number) => H - bottom - ((H - bottom - 12) * v) / max;
  const ticksY = Array.from({ length: Math.round(max / step) + 1 }, (_, i) => i * step);
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <Svg width={Math.max(total, width)} height={H}>
        {ticksY.map((t) => (
          <G key={t}>
            <Line x1={left} x2={Math.max(total, width)} y1={y(t)} y2={y(t)} stroke={c.border} strokeWidth={1} />
            <SvgText x={left - 6} y={y(t) + 4} fontSize={11} fill={c.textMuted} textAnchor="end">
              {compact(t)}
            </SvgText>
          </G>
        ))}
        {points.map((p, i) => {
          const cx = left + band * i + band / 2;
          let acc = 0;
          const segs = p.parts.map((v, si) => {
            const y0 = y(acc);
            acc += v;
            const y1 = y(acc);
            const last = si === p.parts.length - 1 || p.parts.slice(si + 1).every((x) => x === 0);
            const hgt = Math.max(0, y0 - y1 - (si > 0 && v > 0 ? 2 : 0));
            return v > 0 ? <Path key={si} d={barPath(cx - bar / 2, y1, bar, hgt, false, last)} fill={series[si]?.color ?? p.color} opacity={selected && selected !== p.key ? 0.45 : 1} /> : null;
          });
          return (
            <G key={p.key} onPress={() => onSelect(p.key)}>
              <Rect x={cx - band / 2} y={0} width={band} height={H} fill="transparent" />
              {segs}
              {showValues || selected === p.key ? (
                <SvgText x={cx} y={y(p.value) - 6} fontSize={11} fill={c.text} textAnchor="middle" fontWeight="600">
                  {fmt(p.value)}
                </SvgText>
              ) : null}
              <SvgText x={cx} y={H - bottom + 16} fontSize={11} fill={c.textMuted} textAnchor="middle">
                {p.label.length > 10 ? `${p.label.slice(0, 9)}…` : p.label}
              </SvgText>
            </G>
          );
        })}
        <Line x1={left} x2={Math.max(total, width)} y1={y(0)} y2={y(0)} stroke={c.borderStrong} strokeWidth={1} />
      </Svg>
    </ScrollView>
  );
}

function Bars({ points, series, fmt, width, showValues, selected, onSelect }: { points: Point[]; series: Series[]; fmt: (v: number) => string; width: number; showValues: boolean; selected: string | null; onSelect: (k: string) => void }) {
  const s = useTheme().space;
  const max = Math.max(...points.map((p) => p.value), 0) || 1;
  const track = Math.max(80, width - 120 - 56);
  return (
    <View style={{ gap: s[2] }}>
      {points.map((p) => (
        <Pressable key={p.key} onPress={() => onSelect(p.key)} accessibilityRole="button" accessibilityLabel={`${p.label}: ${fmt(p.value)}`} style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: s[8] }}>
          <Text role="label" numberOfLines={2} style={{ width: 120 }}>
            {p.label}
          </Text>
          <Svg width={track} height={20}>
            {(() => {
              let acc = 0;
              return p.parts.map((v, si) => {
                const x0 = (acc / max) * track;
                acc += v;
                const w = Math.max(0, (v / max) * track - (si > 0 && v > 0 ? 2 : 0));
                const last = si === p.parts.length - 1 || p.parts.slice(si + 1).every((x) => x === 0);
                return v > 0 ? <Path key={si} d={barPath(x0 + (si > 0 ? 2 : 0), 0, w, 20, true, last)} fill={series[si]?.color ?? p.color} opacity={selected && selected !== p.key ? 0.45 : 1} /> : null;
              });
            })()}
          </Svg>
          {showValues || selected === p.key || points.length <= 12 ? (
            <Text role="label" tabular weight={600} numberOfLines={1} style={{ width: 56 }}>
              {fmt(p.value)}
            </Text>
          ) : null}
        </Pressable>
      ))}
    </View>
  );
}

function Lines({ points, series, fmt, width, selected, onSelect }: { points: Point[]; series: Series[]; fmt: (v: number) => string; width: number; selected: string | null; onSelect: (k: string) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const H = 200;
  const left = 40;
  const bottom = 40;
  const lines = series.length ? series.map((x, i) => ({ color: x.color, values: points.map((p) => p.parts[i] ?? 0) })) : [{ color: theme.colors.primary, values: points.map((p) => p.value) }];
  const top = Math.max(...lines.flatMap((l) => l.values), 0);
  const { max, step } = niceMax(top);
  const band = Math.max(40, (width - left - 16) / Math.max(points.length - 1, 1));
  const total = left + 16 + band * Math.max(points.length - 1, 1);
  const x = (i: number) => left + 8 + band * i;
  const y = (v: number) => H - bottom - ((H - bottom - 12) * v) / max;
  const ticksY = Array.from({ length: Math.round(max / step) + 1 }, (_, i) => i * step);
  const sel = points.findIndex((p) => p.key === selected);
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false}>
      <Svg width={Math.max(total, width)} height={H}>
        {ticksY.map((t) => (
          <G key={t}>
            <Line x1={left} x2={Math.max(total, width)} y1={y(t)} y2={y(t)} stroke={c.border} strokeWidth={1} />
            <SvgText x={left - 6} y={y(t) + 4} fontSize={11} fill={c.textMuted} textAnchor="end">
              {compact(t)}
            </SvgText>
          </G>
        ))}
        {sel >= 0 ? <Line x1={x(sel)} x2={x(sel)} y1={8} y2={H - bottom} stroke={c.borderStrong} strokeWidth={1} /> : null}
        {lines.map((l, li) => (
          <G key={li}>
            <Path d={l.values.map((v, i) => `${i ? "L" : "M"}${x(i)} ${y(v)}`).join(" ")} stroke={l.color} strokeWidth={2} fill="none" strokeLinejoin="round" strokeLinecap="round" />
            {l.values.map((v, i) => (
              <Circle key={i} cx={x(i)} cy={y(v)} r={i === sel || points.length <= 12 ? 4 : 0} fill={l.color} stroke={c.surface} strokeWidth={2} />
            ))}
          </G>
        ))}
        {points.map((p, i) => (
          <G key={p.key} onPress={() => onSelect(p.key)}>
            <Rect x={x(i) - band / 2} y={0} width={band} height={H} fill="transparent" />
            <SvgText x={x(i)} y={H - bottom + 16} fontSize={11} fill={c.textMuted} textAnchor="middle">
              {p.label.length > 10 ? `${p.label.slice(0, 9)}…` : p.label}
            </SvgText>
          </G>
        ))}
        {sel >= 0 ? (
          <SvgText x={x(sel)} y={y(points[sel]!.value) - 10} fontSize={11} fill={c.text} textAnchor="middle" fontWeight="600">
            {fmt(points[sel]!.value)}
          </SvgText>
        ) : null}
      </Svg>
    </ScrollView>
  );
}

function arc(cx: number, cy: number, r: number, a0: number, a1: number): string {
  const p = (a: number) => [cx + r * Math.sin(a), cy - r * Math.cos(a)] as const;
  const [x0, y0] = p(a0);
  const [x1, y1] = p(a1);
  return `M${x0} ${y0} A${r} ${r} 0 ${a1 - a0 > Math.PI ? 1 : 0} 1 ${x1} ${y1}`;
}

function Donut({ points, fmt, selected, onSelect }: { points: Point[]; fmt: (v: number) => string; selected: string | null; onSelect: (k: string) => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const sum = points.reduce((a, p) => a + Math.max(p.value, 0), 0);
  const size = 180;
  const r = 70;
  const gap = points.length > 1 ? 2 / r : 0;
  let a = 0;
  const sel = points.find((p) => p.key === selected);
  return (
    <View style={{ alignItems: "center", gap: s[3] }}>
      <View>
        <Svg width={size} height={size}>
          <Circle cx={size / 2} cy={size / 2} r={r} stroke={c.sunken} strokeWidth={20} fill="none" />
          {sum > 0
            ? points.map((p, i) => {
                const share = Math.max(p.value, 0) / sum;
                const a0 = a;
                a += share * Math.PI * 2;
                if (share <= 0) return null;
                const full = share >= 0.9999;
                return full ? (
                  <Circle key={p.key} cx={size / 2} cy={size / 2} r={r} stroke={p.color || hueColor(theme, undefined, i)} strokeWidth={20} fill="none" onPress={() => onSelect(p.key)} />
                ) : (
                  <Path key={p.key} d={arc(size / 2, size / 2, r, a0 + gap / 2, a - gap / 2)} stroke={p.color || hueColor(theme, undefined, i)} strokeWidth={selected === p.key ? 24 : 20} fill="none" onPress={() => onSelect(p.key)} />
                );
              })
            : null}
        </Svg>
        <View style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }} pointerEvents="none">
          <Text role="title" tabular>
            {fmt(sel ? sel.value : sum)}
          </Text>
          <Text role="caption" tone="muted" numberOfLines={1} style={{ maxWidth: 110 }}>
            {sel ? sel.label : "Total"}
          </Text>
        </View>
      </View>
      <View style={{ alignSelf: "stretch", gap: s[1] }}>
        {points.map((p, i) => (
          <Pressable key={p.key} onPress={() => onSelect(p.key)} accessibilityRole="button" accessibilityLabel={`${p.label}: ${fmt(p.value)}`} style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: s[8] }}>
            <View style={{ width: s[3], height: s[3], borderRadius: 3, backgroundColor: p.color || hueColor(theme, undefined, i) }} />
            <Text role="label" numberOfLines={1} style={{ flex: 1 }}>
              {p.label}
            </Text>
            <Text role="label" tabular weight={600}>
              {fmt(p.value)}
            </Text>
            <Text role="caption" tone="muted" tabular style={{ width: 44, textAlign: "right" }}>
              {sum ? `${Math.round((Math.max(p.value, 0) / sum) * 100)}%` : ""}
            </Text>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

const TYPE_LABEL = { bar: "Column chart", horizontal_bar: "Bar chart", line: "Line chart", donut: "Donut chart" } as const;

/** Chart: the view's rows counted or summed per group, drawn natively; a values table for reading exact numbers. */
export function ChartView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const s = theme.space;
  const [width, setWidth] = useState(0);
  const [table, setTable] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const params = useMemo(() => ({ view: ctx.view.id, tz: docsTimeZone() }), [ctx.view.id]);
  const chart = useDocsChart(ctx.token ? null : ctx.db.id, params);
  const cfg = ctx.config.chart ?? { type: "bar" as const };
  const yProp: DocsProperty | undefined = cfg.y && cfg.y !== "count" ? ctx.schema.find((p) => p.id === cfg.y_property) : undefined;
  const fmt = (v: number) => (yProp ? docsFormatComputed(v, yProp) : new Intl.NumberFormat().format(v));
  const shaped = useMemo(() => (chart.data ? shape(theme, chart.data) : null), [chart.data, theme]);
  if (ctx.token) {
    return (
      <Text role="label" tone="muted">
        Charts show in the app for people with access.
      </Text>
    );
  }
  if (chart.isPending) return <Skeleton height={200} />;
  if (chart.isError) return <ErrorState title="Couldn't load the chart." info={errorMessage(chart.error)} onRetry={() => void chart.refetch()} compact />;
  if (!shaped || !shaped.points.length) {
    return (
      <Text role="label" tone="muted">
        {cfg.x ? "No rows to chart." : "Choose what to group the chart by in the view's layout."}
      </Text>
    );
  }
  const { points, series } = shaped;
  const summary = `${TYPE_LABEL[cfg.type]}, ${points.length} ${points.length === 1 ? "group" : "groups"}: ${points
    .slice(0, 8)
    .map((p) => `${p.label} ${fmt(p.value)}`)
    .join(", ")}${points.length > 8 ? ", and more" : ""}.`;
  const sel = points.find((p) => p.key === selected);
  const pick = (k: string) => setSelected((cur) => (cur === k ? null : k));
  return (
    <View style={{ gap: s[3] }} onLayout={(e) => setWidth(Math.floor(e.nativeEvent.layout.width))}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Text role="caption" tone="muted" style={{ flex: 1 }}>
          {`${chart.data?.total ?? 0} ${chart.data?.total === 1 ? "row" : "rows"}`}
        </Text>
        <Chip size="sm" selected={table} onPress={() => setTable((t) => !t)}>
          Table
        </Chip>
      </View>
      {table ? (
        <ValuesTable points={points} series={series} fmt={fmt} />
      ) : width ? (
        <View accessible accessibilityRole="image" accessibilityLabel={summary} style={{ gap: s[3] }}>
          {cfg.type !== "donut" ? <Legend series={series} /> : null}
          {cfg.type === "horizontal_bar" ? (
            <Bars points={points} series={series} fmt={fmt} width={width} showValues={!!cfg.show_values} selected={selected} onSelect={pick} />
          ) : cfg.type === "line" ? (
            <Lines points={points} series={series} fmt={fmt} width={width} selected={selected} onSelect={pick} />
          ) : cfg.type === "donut" ? (
            <Donut points={points} fmt={fmt} selected={selected} onSelect={pick} />
          ) : (
            <Columns points={points} series={series} fmt={fmt} width={width} showValues={!!cfg.show_values} selected={selected} onSelect={pick} />
          )}
          {sel && cfg.type !== "donut" ? <Selection point={sel} series={series} fmt={fmt} /> : null}
        </View>
      ) : null}
    </View>
  );
}

function Selection({ point, series, fmt }: { point: Point; series: Series[]; fmt: (v: number) => string }) {
  const theme = useTheme();
  const s = theme.space;
  return (
    <View style={{ gap: s[1], padding: s[3], borderRadius: theme.radius.md, backgroundColor: theme.colors.sunken }} accessibilityLiveRegion="polite">
      <Text role="label" weight={600}>{`${point.label}: ${fmt(point.value)}`}</Text>
      {series.length > 1
        ? series.map((x, i) =>
            point.parts[i] ? (
              <View key={x.key} style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
                <View style={{ width: s[2], height: s[2], borderRadius: 2, backgroundColor: x.color }} />
                <Text role="caption" style={{ flex: 1 }}>
                  {x.label}
                </Text>
                <Text role="caption" tabular>
                  {fmt(point.parts[i] ?? 0)}
                </Text>
              </View>
            ) : null,
          )
        : null}
    </View>
  );
}

