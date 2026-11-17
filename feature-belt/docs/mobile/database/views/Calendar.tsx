import { docsLocalDateTime, docsTitle, type DocsDateValue, type DocsProperty, type DocsRow } from "@scaffoldapp/shared";
import { Button, Chip, Text, useTheme } from "@scaffoldapp/ui-native";
import { Plus } from "lucide-react-native";
import { useMemo, useState } from "react";
import { Pressable, View } from "react-native";
import type { ViewCtx } from "../context";
import { addDays, dayOf, longDayLabel, todayYmd } from "../dates";
import { MonthGrid } from "../MonthGrid";
import { RowValues } from "./List";

/** A row's days on a date-ish property: start and end (YYYY-MM-DD), or null. */
export function rowSpan(row: DocsRow, p: DocsProperty): { start: string; end: string } | null {
  if (p.type === "created_time") return { start: dayOf(docsLocalDateTime(row.created_at)), end: dayOf(docsLocalDateTime(row.created_at)) };
  if (p.type === "last_edited_time") return { start: dayOf(docsLocalDateTime(row.updated_at)), end: dayOf(docsLocalDateTime(row.updated_at)) };
  const v = row.props[p.id] as DocsDateValue | null | undefined;
  if (!v || typeof v !== "object" || Array.isArray(v) || typeof v.start !== "string") return null;
  const start = dayOf(v.start);
  const end = v.end ? dayOf(v.end) : start;
  return { start, end: end < start ? start : end };
}

/** Calendar: a month grid marking rows on their days, and the chosen day's rows below. */
export function CalendarView({ ctx }: { ctx: ViewCtx }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const dateProp = ctx.schema.find((p) => p.id === ctx.config.date_property);
  const [day, setDay] = useState(todayYmd());
  const [month, setMonth] = useState({ year: Number(day.slice(0, 4)), month: Number(day.slice(5, 7)) - 1 });
  const byDay = useMemo(() => {
    const m = new Map<string, DocsRow[]>();
    if (!dateProp) return m;
    for (const r of ctx.rows) {
      const span = rowSpan(r, dateProp);
      if (!span) continue;
      let d = span.start;
      for (let i = 0; i < 62 && d <= span.end; i++) {
        m.set(d, [...(m.get(d) ?? []), r]);
        d = addDays(d, 1);
      }
    }
    return m;
  }, [ctx.rows, dateProp]);
  if (!dateProp) {
    return (
      <Text role="label" tone="muted">
        Choose a date property for this calendar in the view's layout.
      </Text>
    );
  }
  const undated = ctx.rows.filter((r) => !rowSpan(r, dateProp)).length;
  const today = todayYmd();
  const list = byDay.get(day) ?? [];
  const settable = dateProp.type === "date";
  return (
    <View style={{ gap: s[4] }}>
      <MonthGrid
        start={day}
        month={month}
        onMonthChange={setMonth}
        onPressDay={setDay}
        cellHeight={56}
        headerEnd={
          <Chip
            size="sm"
            onPress={() => {
              setDay(today);
              setMonth({ year: Number(today.slice(0, 4)), month: Number(today.slice(5, 7)) - 1 });
            }}
          >
            Today
          </Chip>
        }
        dayHint={(d) => {
          const n = byDay.get(d)?.length ?? 0;
          return n ? `${n} ${n === 1 ? "row" : "rows"}` : undefined;
        }}
        renderDay={(d) => {
          const n = byDay.get(d)?.length ?? 0;
          if (!n) return null;
          return (
            <View style={{ flexDirection: "row", gap: s[0.5] }}>
              {Array.from({ length: Math.min(n, 3) }, (_, i) => (
                <View key={i} style={{ width: s[1.5], height: s[1.5], borderRadius: s[1], backgroundColor: c.primary }} />
              ))}
            </View>
          );
        }}
      />
      <View style={{ gap: s[2] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="titleSm" accessibilityRole="header" style={{ flex: 1 }}>
            {longDayLabel(day)}
          </Text>
          {ctx.canAdd && settable ? (
            <Button variant="ghost" size="md" leftIcon={Plus} onPress={() => ctx.createRow({ [dateProp.id]: { start: day } })}>
              New
            </Button>
          ) : null}
        </View>
        {list.length === 0 ? (
          <Text role="label" tone="muted">
            Nothing on this day.
          </Text>
        ) : (
          list.map((r) => (
            <Pressable
              key={r.id}
              accessibilityRole="link"
              accessibilityLabel={`Open ${docsTitle(r.title)}`}
              onPress={() => ctx.openRow(r.id)}
              style={({ pressed }) => ({ gap: s[1], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
            >
              <Text role="label" weight={600} numberOfLines={2}>
                {r.icon ? `${r.icon} ` : ""}
                {docsTitle(r.title)}
              </Text>
              <RowValues ctx={ctx} row={r} />
            </Pressable>
          ))
        )}
        {undated ? (
          <Text role="caption" tone="muted">
            {`${undated} ${undated === 1 ? "row has" : "rows have"} no ${dateProp.name.toLowerCase()}.`}
          </Text>
        ) : null}
      </View>
    </View>
  );
}
