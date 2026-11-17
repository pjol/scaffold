import { IconButton, Text, useTheme } from "@scaffoldapp/ui-native";
import { ChevronLeft, ChevronRight } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { Pressable, View } from "react-native";
import { addMonths, longDayLabel, monthLabel, monthWeeks, todayYmd, weekdayLabels } from "./dates";

export interface MonthGridProps {
  /** The month shown first (YYYY-MM-DD in it); today when empty. */
  initial?: string | undefined;
  /** Days drawn selected (a single day or a start–end range). */
  start?: string | undefined;
  end?: string | undefined;
  onPressDay: (day: string) => void;
  /** Extra content under a day number (event marks). */
  renderDay?: (day: string) => ReactNode;
  /** Accessible extra for a day ("3 rows"). */
  dayHint?: (day: string) => string | undefined;
  /** Header actions beside the month (Today…). */
  headerEnd?: ReactNode;
  /** Controlled month (calendar view). */
  month?: { year: number; month: number };
  onMonthChange?: (m: { year: number; month: number }) => void;
  cellHeight?: number;
}

/** A month of days, Sunday first, with month paging. */
export function MonthGrid({ initial, start, end, onPressDay, renderDay, dayHint, headerEnd, month: controlled, onMonthChange, cellHeight }: MonthGridProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const seed = initial && /^\d{4}-\d{2}/.test(initial) ? initial : todayYmd();
  const [own, setOwn] = useState({ year: Number(seed.slice(0, 4)), month: Number(seed.slice(5, 7)) - 1 });
  const cur = controlled ?? own;
  const setMonth = (m: { year: number; month: number }) => (onMonthChange ? onMonthChange(m) : setOwn(m));
  const weeks = monthWeeks(cur.year, cur.month);
  const today = todayYmd();
  const days = weekdayLabels("narrow");
  const lo = start && end && end < start ? end : start;
  const hi = start && end && end < start ? start : end;
  return (
    <View style={{ gap: s[2] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
        <Text role="titleSm" accessibilityRole="header" style={{ flex: 1 }}>
          {monthLabel(cur.year, cur.month)}
        </Text>
        {headerEnd}
        <IconButton label="Previous month" icon={ChevronLeft} variant="ghost" onPress={() => setMonth(addMonths(cur.year, cur.month, -1))} />
        <IconButton label="Next month" icon={ChevronRight} variant="ghost" onPress={() => setMonth(addMonths(cur.year, cur.month, 1))} />
      </View>
      <View style={{ flexDirection: "row" }} importantForAccessibility="no-hide-descendants" accessibilityElementsHidden>
        {days.map((d, i) => (
          <Text key={i} role="caption" tone="muted" align="center" style={{ flex: 1 }}>
            {d}
          </Text>
        ))}
      </View>
      {weeks.map((week) => (
        <View key={week[0]} style={{ flexDirection: "row" }}>
          {week.map((day) => {
            const inMonth = Number(day.slice(5, 7)) - 1 === cur.month;
            const selected = day === start || day === end;
            const inRange = !!lo && !!hi && day > lo && day < hi;
            const isToday = day === today;
            const hint = dayHint?.(day);
            return (
              <Pressable
                key={day}
                accessibilityRole="button"
                accessibilityLabel={`${longDayLabel(day)}${isToday ? ", today" : ""}${hint ? `, ${hint}` : ""}`}
                accessibilityState={{ selected }}
                onPress={() => onPressDay(day)}
                style={({ pressed }) => ({
                  flex: 1,
                  minHeight: cellHeight ?? theme.tap.sm,
                  alignItems: "center",
                  paddingVertical: s[1],
                  gap: s[0.5],
                  borderRadius: theme.radius.md,
                  backgroundColor: selected ? c.primarySoft : inRange ? c.sunken : pressed ? c.sunken : "transparent",
                })}
              >
                <View
                  style={{
                    minWidth: s[6],
                    height: s[6],
                    borderRadius: s[3],
                    alignItems: "center",
                    justifyContent: "center",
                    backgroundColor: selected ? c.primary : "transparent",
                    borderWidth: isToday && !selected ? 1 : 0,
                    borderColor: c.primaryStrong,
                  }}
                >
                  <Text role="label" tabular weight={selected || isToday ? 600 : 400} style={{ color: selected ? c.primaryFg : inMonth ? c.text : c.textSubtle }}>
                    {Number(day.slice(8, 10))}
                  </Text>
                </View>
                {renderDay?.(day)}
              </Pressable>
            );
          })}
        </View>
      ))}
    </View>
  );
}
