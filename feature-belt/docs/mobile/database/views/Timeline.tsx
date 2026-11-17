import { docsTitle, type DocsProperty, type DocsRow } from "@scaffoldapp/shared";
import { SegmentedControl, Text, useTheme } from "@scaffoldapp/ui-native";
import { useMemo, useRef, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import Svg, { Path } from "react-native-svg";
import type { ViewCtx } from "../context";
import { addDays, daysBetween, shortDayLabel, todayYmd } from "../dates";
import { rowSpan } from "./Calendar";

type Zoom = "day" | "week" | "month" | "quarter" | "year";
const PX: Record<Zoom, number> = { day: 44, week: 18, month: 7, quarter: 2.5, year: 1 };
const ZOOMS: { value: Zoom; label: string }[] = [
  { value: "day", label: "Day" },
  { value: "week", label: "Week" },
  { value: "month", label: "Month" },
  { value: "quarter", label: "Quarter" },
  { value: "year", label: "Year" },
];
const LANE = 44;
const HEAD = 32;

/** Start and end days from the start property and the end property (or the start's own end). */
function span(row: DocsRow, startProp: DocsProperty, endProp: DocsProperty | undefined): { start: string; end: string } | null {
  const a = rowSpan(row, startProp);
  if (!a) return null;
  if (endProp) {
    const b = rowSpan(row, endProp);
    if (b) return { start: a.start, end: b.end < a.start ? a.start : b.end };
  }
  return a;
}

function ticks(from: string, days: number, zoom: Zoom): { day: string; label: string }[] {
  const out: { day: string; label: string }[] = [];
  for (let i = 0; i <= days; i++) {
    const d = addDays(from, i);
    const dom = Number(d.slice(8, 10));
    const month = Number(d.slice(5, 7));
    if (zoom === "day") out.push({ day: d, label: dom === 1 || i === 0 ? shortDayLabel(d) : String(dom) });
    else if (zoom === "week" && new Date(`${d}T00:00:00Z`).getUTCDay() === 1) out.push({ day: d, label: shortDayLabel(d) });
    else if ((zoom === "month" || zoom === "quarter") && dom === 1 && (zoom === "month" || (month - 1) % 3 === 0))
      out.push({ day: d, label: new Intl.DateTimeFormat(undefined, { month: "short", ...(month === 1 || i < 31 ? { year: "numeric" as const } : {}), timeZone: "UTC" }).format(new Date(`${d}T00:00:00Z`)) });
    else if (zoom === "year" && dom === 1 && month === 1) out.push({ day: d, label: d.slice(0, 4) });
  }
  return out;
}

/** Timeline: dated bars on a scrollable time axis (zoom day → year), dependency arrows, undated rows below. */
export function TimelineView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const startProp = ctx.schema.find((p) => p.id === ctx.config.date_property);
  const endProp = ctx.schema.find((p) => p.id === ctx.config.end_property);
  const depProp = ctx.schema.find((p) => p.id === ctx.config.timeline?.dependency);
  const seeded = ctx.config.timeline?.zoom;
  const [zoom, setZoom] = useState<Zoom>(seeded && seeded !== "hours" ? seeded : seeded === "hours" ? "day" : "month");
  const scroller = useRef<ScrollView>(null);
  const scrolled = useRef(false);
  const today = todayYmd();
  const dated = useMemo(() => {
    if (!startProp) return [];
    return ctx.rows.map((r) => ({ row: r, span: span(r, startProp, endProp) })).filter((x): x is { row: DocsRow; span: { start: string; end: string } } => !!x.span);
  }, [ctx.rows, startProp, endProp]);
  if (!startProp) {
    return (
      <Text role="label" tone="muted">
        Choose a date property for this timeline in the view's layout.
      </Text>
    );
  }
  const px = PX[zoom];
  const lo = dated.reduce((m, x) => (x.span.start < m ? x.span.start : m), today);
  const hi = dated.reduce((m, x) => (x.span.end > m ? x.span.end : m), today);
  const pad = zoom === "day" ? 3 : zoom === "week" ? 7 : zoom === "month" ? 21 : 60;
  const from = addDays(lo, -pad);
  const days = Math.min(daysBetween(from, addDays(hi, pad)), 366 * 4);
  const width = Math.max(days * px, 320);
  const xOf = (d: string) => Math.max(0, Math.min(daysBetween(from, d), days)) * px;
  const lanes = new Map(dated.map((x, i) => [x.row.id, i]));
  const undated = ctx.rows.filter((r) => !lanes.has(r.id));
  const editableDates = startProp.type === "date";
  const arrows: string[] = [];
  if (depProp) {
    for (const { row, span: sp } of dated) {
      const blockers = Array.isArray(row.props[depProp.id]) ? (row.props[depProp.id] as string[]) : [];
      for (const b of blockers) {
        const bi = lanes.get(b);
        if (bi === undefined) continue;
        const bx = xOf(dated[bi]!.span.end) + px;
        const by = HEAD + bi * LANE + LANE / 2;
        const ax = xOf(sp.start);
        const ay = HEAD + lanes.get(row.id)! * LANE + LANE / 2;
        const mid = Math.max(bx + 6, Math.min(ax - 6, bx + 12));
        arrows.push(`M${bx} ${by} H${mid} V${ay} H${ax - 2} M${ax - 7} ${ay - 4} L${ax - 2} ${ay} L${ax - 7} ${ay + 4}`);
      }
    }
  }
  const tickList = ticks(from, days, zoom);
  return (
    <View style={{ gap: s[3] }}>
      <SegmentedControl<Zoom> options={ZOOMS} value={zoom} onValueChange={setZoom} accessibilityLabel="Zoom" />
      {dated.length === 0 ? (
        <Text role="label" tone="muted">
          {`No rows have a ${startProp.name.toLowerCase()} yet.`}
        </Text>
      ) : (
        <ScrollView
          ref={scroller}
          horizontal
          showsHorizontalScrollIndicator={false}
          onContentSizeChange={() => {
            if (scrolled.current) return;
            scrolled.current = true;
            scroller.current?.scrollTo({ x: Math.max(0, xOf(today) - 48), animated: false });
          }}
          style={{ borderWidth: 1, borderColor: c.border, borderRadius: theme.radius.md }}
        >
          <View style={{ width, height: HEAD + dated.length * LANE + s[2] }}>
            {tickList.map((t) => (
              <View key={t.day} style={{ position: "absolute", left: xOf(t.day), top: 0, bottom: 0, borderLeftWidth: 1, borderLeftColor: c.border }}>
                <Text role="caption" tone="muted" numberOfLines={1} style={{ position: "absolute", left: s[1], top: s[1.5], width: 96 }}>
                  {t.label}
                </Text>
              </View>
            ))}
            <View
              style={{ position: "absolute", left: xOf(today) + px / 2, top: 0, bottom: 0, width: 2, backgroundColor: c.accent }}
              accessible
              accessibilityLabel="Today"
            />
            {arrows.length ? (
              <Svg width={width} height={HEAD + dated.length * LANE} style={{ position: "absolute", left: 0, top: 0 }} pointerEvents="none">
                {arrows.map((d, i) => (
                  <Path key={i} d={d} stroke={c.textMuted} strokeWidth={1.5} fill="none" strokeLinecap="round" strokeLinejoin="round" />
                ))}
              </Svg>
            ) : null}
            {dated.map(({ row, span: sp }, i) => {
              const left = xOf(sp.start);
              const w = Math.max((daysBetween(sp.start, sp.end) + 1) * px, 20);
              const narrow = w < 96;
              const label = docsTitle(row.title);
              const blocked = depProp && Array.isArray(row.props[depProp.id]) ? (row.props[depProp.id] as string[]).filter((b) => lanes.has(b)).length : 0;
              return (
                <View key={row.id} style={{ position: "absolute", top: HEAD + i * LANE + s[1.5], left, flexDirection: "row", alignItems: "center", gap: s[2] }}>
                  <Pressable
                    accessibilityRole="button"
                    accessibilityLabel={`${label}, ${shortDayLabel(sp.start)}${sp.end !== sp.start ? ` to ${shortDayLabel(sp.end)}` : ""}${blocked ? `, blocked by ${blocked}` : ""}`}
                    accessibilityHint={editableDates && ctx.canEditRow(row) ? "Opens the row. Long press to change its dates." : "Opens the row."}
                    onPress={() => ctx.openRow(row.id)}
                    onLongPress={editableDates && ctx.canEditRow(row) ? () => ctx.editValue(row, startProp) : undefined}
                    style={({ pressed }) => ({
                      width: w,
                      height: LANE - s[3],
                      justifyContent: "center",
                      paddingHorizontal: s[2],
                      borderRadius: theme.radius.sm,
                      borderWidth: 1,
                      borderColor: c.primary,
                      backgroundColor: pressed ? c.primary : c.primarySoft,
                    })}
                  >
                    {narrow ? null : (
                      <Text role="caption" weight={600} numberOfLines={1} style={{ color: c.primaryStrong }}>
                        {label}
                      </Text>
                    )}
                  </Pressable>
                  {narrow ? (
                    <Text role="caption" weight={600} numberOfLines={1} style={{ maxWidth: 200 }}>
                      {label}
                    </Text>
                  ) : null}
                </View>
              );
            })}
          </View>
        </ScrollView>
      )}
      {undated.length ? (
        <View style={{ gap: s[1] }}>
          <Text role="label" tone="muted" weight={600}>
            {`No ${startProp.name.toLowerCase()}`}
          </Text>
          {undated.map((r) => (
            <Pressable
              key={r.id}
              accessibilityRole="button"
              accessibilityLabel={`${docsTitle(r.title)}, no date`}
              accessibilityHint={editableDates && ctx.canEditRow(r) ? "Sets its dates." : "Opens the row."}
              onPress={() => (editableDates && ctx.canEditRow(r) ? ctx.editValue(r, startProp) : ctx.openRow(r.id))}
              style={({ pressed }) => ({ minHeight: theme.tap.sm, justifyContent: "center", paddingHorizontal: s[2], borderRadius: theme.radius.sm, backgroundColor: pressed ? c.sunken : "transparent" })}
            >
              <Text role="label" numberOfLines={1}>
                {r.icon ? `${r.icon} ` : ""}
                {docsTitle(r.title)}
              </Text>
            </Pressable>
          ))}
        </View>
      ) : null}
    </View>
  );
}
