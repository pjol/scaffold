import {
  calendarAddDays,
  calendarDayStart,
  calendarFromWall,
  calendarLayoutDay,
  calendarMinutesOfDay,
  calendarOccurrenceKey,
  calendarOnDay,
  calendarTodayKey,
  errorMessage,
  formatCalendarDayLong,
  formatCalendarTime,
  useCalendarRange,
  useNow,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, IconButton, SegmentedControl, Skeleton, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CalendarDays, ChevronLeft, ChevronRight, Link2, Plus, Repeat } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CalendarDot, calendarColors, useCalendarZone } from "./parts";

const HOUR = 56;
const AGENDA_DAYS = 14;

function openEvent(o: CalendarOccurrence) {
  router.push({ pathname: "/calendar/event/[id]", params: { id: o.event_id, ...(o.occurrence_start ? { occurrence: o.occurrence_start } : {}) } });
}

function newEvent(startsAt?: string, endsAt?: string) {
  router.push({ pathname: "/calendar/form", params: startsAt && endsAt ? { starts_at: startsAt, ends_at: endsAt } : {} });
}

/** Calendar: agenda (the next two weeks) or one day as a timeline; tap empty time to add an event. */
export function CalendarScreen() {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const { zone, ready } = useCalendarZone();
  const today = calendarTodayKey(zone);
  const [view, setView] = useState<"agenda" | "day">("agenda");
  const [date, setDate] = useState<string | null>(null);
  const day = date ?? today;
  const span = view === "day" ? 1 : AGENDA_DAYS;
  const from = ready ? calendarDayStart(day, zone) : null;
  const to = ready ? calendarDayStart(calendarAddDays(day, span), zone) : null;
  const range = useCalendarRange(from, to);
  const items = range.data?.items ?? [];

  return (
    <View style={{ flex: 1, backgroundColor: c.canvas }}>
      <Stack.Screen
        options={{
          headerRight: () => (
            <View style={{ flexDirection: "row", alignItems: "center" }}>
              <IconButton label="New event" icon={Plus} variant="ghost" onPress={() => newEvent()} />
              <ResourceMenu
                feature="calendar"
                title="Calendar"
                actions={[{ id: "links", label: "Booking links", icon: Link2, onSelect: () => router.push("/calendar/links") }]}
              />
            </View>
          ),
        }}
      />
      <View style={{ gap: s[3], paddingHorizontal: s[4], paddingTop: s[3], paddingBottom: s[2] }}>
        <SegmentedControl<"agenda" | "day">
          accessibilityLabel="View"
          options={[
            { value: "agenda", label: "Agenda" },
            { value: "day", label: "Day" },
          ]}
          value={view}
          onValueChange={setView}
        />
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
          <IconButton label="Previous" icon={ChevronLeft} variant="ghost" onPress={() => setDate(calendarAddDays(day, -span))} />
          <Text role="titleSm" style={{ flex: 1 }} numberOfLines={1} accessibilityLiveRegion="polite">
            {view === "day" ? formatCalendarDayLong(day) : day === today ? "Next 2 weeks" : `From ${formatCalendarDayLong(day)}`}
          </Text>
          <IconButton label="Next" icon={ChevronRight} variant="ghost" onPress={() => setDate(calendarAddDays(day, span))} />
          {day !== today ? (
            <Button variant="secondary" size="md" onPress={() => setDate(null)}>
              Today
            </Button>
          ) : null}
        </View>
      </View>
      {!ready || (range.isPending && !range.data) ? (
        <View style={{ padding: s[4], gap: s[3] }} accessibilityLabel="Loading calendar" accessibilityRole="progressbar">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} height={theme.tap.base} />
          ))}
        </View>
      ) : range.isError && !range.data ? (
        <View style={{ padding: s[4] }}>
          <ErrorState title="Couldn't load your calendar" info={errorMessage(range.error)} onRetry={() => void range.refetch()} retrying={range.isRefetching} />
        </View>
      ) : view === "day" ? (
        <DayTimeline day={day} zone={zone} items={items} refreshing={range.isRefetching} onRefresh={() => void range.refetch()} />
      ) : (
        <Agenda day={day} zone={zone} items={items} refreshing={range.isRefetching} onRefresh={() => void range.refetch()} />
      )}
    </View>
  );
}

function Agenda({ day, zone, items, refreshing, onRefresh }: { day: string; zone: string; items: CalendarOccurrence[]; refreshing: boolean; onRefresh: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const today = calendarTodayKey(zone);
  const groups = useMemo(
    () =>
      Array.from({ length: AGENDA_DAYS }, (_, i) => calendarAddDays(day, i))
        .map((d) => ({ d, list: calendarOnDay(items, d, zone).sort((a, b) => Number(b.all_day) - Number(a.all_day) || a.starts_at.localeCompare(b.starts_at)) }))
        .filter((g) => g.list.length > 0),
    [day, items, zone],
  );
  return (
    <ScrollView contentContainerStyle={{ padding: s[4], paddingTop: s[2], gap: s[6] }} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}>
      {groups.length === 0 ? (
        <EmptyState
          icon={CalendarDays}
          title="Nothing scheduled in these 2 weeks"
          action={
            <Button variant="secondary" leftIcon={Plus} onPress={() => newEvent()}>
              Add an event
            </Button>
          }
        />
      ) : (
        groups.map((g) => (
          <View key={g.d} style={{ gap: s[2] }}>
            <Text role="label" weight={600} style={{ color: g.d === today ? c.primaryStrong : c.textMuted }} accessibilityRole="header">
              {g.d === today ? "Today · " : ""}
              {formatCalendarDayLong(g.d)}
            </Text>
            {g.list.map((o) => {
              const col = calendarColors(theme, o.color);
              const declined = o.my_response === "declined";
              return (
                <Pressable
                  key={calendarOccurrenceKey(o)}
                  accessibilityRole="button"
                  accessibilityLabel={`${o.title}, ${o.all_day ? "all day" : `${formatCalendarTime(o.starts_at, zone)} to ${formatCalendarTime(o.ends_at, zone)}`}${declined ? ", declined" : ""}`}
                  onPress={() => openEvent(o)}
                  style={({ pressed }) => ({
                    minHeight: theme.tap.lg,
                    flexDirection: "row",
                    alignItems: "center",
                    gap: s[3],
                    paddingHorizontal: s[3],
                    paddingVertical: s[2],
                    borderRadius: theme.radius.md,
                    backgroundColor: pressed ? c.sunken : c.surface,
                    borderWidth: 1,
                    borderColor: c.border,
                  })}
                >
                  <CalendarDot color={col.dot} />
                  <View style={{ flex: 1, gap: s[0.5] }}>
                    <Text role="body" weight={600} numberOfLines={2} style={declined ? { color: c.textMuted, textDecorationLine: "line-through" } : undefined}>
                      {o.title}
                    </Text>
                    <Text role="label" tone="muted" numberOfLines={1}>
                      {o.all_day ? "All day" : `${formatCalendarTime(o.starts_at, zone)} – ${formatCalendarTime(o.ends_at, zone)}`}
                      {o.location ? ` · ${o.location}` : ""}
                    </Text>
                  </View>
                  {o.recurring ? <Repeat size={16} color={c.textSubtle} /> : null}
                </Pressable>
              );
            })}
          </View>
        ))
      )}
    </ScrollView>
  );
}

function DayTimeline({ day, zone, items, refreshing, onRefresh }: { day: string; zone: string; items: CalendarOccurrence[]; refreshing: boolean; onRefresh: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const now = useNow(60_000);
  const today = calendarTodayKey(zone);
  const ref = useRef<ScrollView>(null);
  const onDay = calendarOnDay(items, day, zone);
  const allDay = onDay.filter((o) => o.all_day);
  const placed = calendarLayoutDay(onDay.filter((o) => !o.all_day), day, zone, 30);
  const [width, setWidth] = useState(0);
  const gutter = theme.space[12];

  useEffect(() => {
    const target = day === today ? Math.max(0, calendarMinutesOfDay(Date.now(), zone) - 60) : 8 * 60;
    const t = setTimeout(() => ref.current?.scrollTo({ y: (target / 60) * HOUR, animated: false }), 0);
    return () => clearTimeout(t);
  }, [day, today, zone]);

  return (
    <View style={{ flex: 1 }}>
      {allDay.length > 0 ? (
        <View style={{ paddingHorizontal: s[4], paddingBottom: s[2], gap: s[1] }}>
          {allDay.map((o) => {
            const col = calendarColors(theme, o.color);
            return (
              <Pressable key={calendarOccurrenceKey(o)} accessibilityRole="button" accessibilityLabel={`${o.title}, all day`} onPress={() => openEvent(o)} style={{ minHeight: theme.tap.sm, justifyContent: "center", paddingHorizontal: s[3], borderRadius: theme.radius.sm, backgroundColor: col.bg }}>
                <Text role="label" weight={600} style={{ color: col.fg }} numberOfLines={1}>
                  {o.title}
                </Text>
              </Pressable>
            );
          })}
        </View>
      ) : null}
      <ScrollView ref={ref} refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />} contentContainerStyle={{ paddingBottom: s[8] }}>
        <View style={{ height: 24 * HOUR, flexDirection: "row" }}>
          <View style={{ width: gutter }}>
            {Array.from({ length: 24 }, (_, h) => (
              <Text key={h} role="caption" tone="subtle" style={{ position: "absolute", top: h * HOUR - 8, right: s[2] }} maxFontSizeMultiplier={1.3}>
                {h === 0 ? "" : `${h % 12 === 0 ? 12 : h % 12} ${h < 12 ? "AM" : "PM"}`}
              </Text>
            ))}
          </View>
          <View style={{ flex: 1 }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
            {Array.from({ length: 24 }, (_, h) => (
              <Pressable
                key={h}
                accessibilityRole="button"
                accessibilityLabel={`Add an event at ${h % 12 === 0 ? 12 : h % 12} ${h < 12 ? "AM" : "PM"}`}
                onPress={() => newEvent(calendarFromWall(day, h * 60, zone), calendarFromWall(day, h * 60 + 30, zone))}
                style={({ pressed }) => ({ height: HOUR, borderTopWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : "transparent" })}
              />
            ))}
            {placed.map((p) => {
              const o = p.item;
              const col = calendarColors(theme, o.color);
              const w = width / p.columns;
              return (
                <Pressable
                  key={calendarOccurrenceKey(o)}
                  accessibilityRole="button"
                  accessibilityLabel={`${o.title}, ${formatCalendarTime(o.starts_at, zone)} to ${formatCalendarTime(o.ends_at, zone)}`}
                  onPress={() => openEvent(o)}
                  style={{
                    position: "absolute",
                    top: (p.top / 60) * HOUR + 1,
                    height: Math.max((p.height / 60) * HOUR - 2, theme.tap.sm),
                    left: p.column * w + 2,
                    width: w - 4,
                    borderRadius: theme.radius.sm,
                    backgroundColor: o.my_response === "declined" ? c.surface : col.bg,
                    borderWidth: o.my_response === "needs_action" || o.my_response === "declined" ? 1 : 0,
                    borderColor: c.borderStrong,
                    paddingHorizontal: s[2],
                    paddingVertical: s[1],
                    overflow: "hidden",
                  }}
                >
                  <Text role="label" weight={600} numberOfLines={1} style={{ color: o.my_response === "declined" ? c.textMuted : col.fg }}>
                    {o.title}
                  </Text>
                  {p.height >= 45 ? (
                    <Text role="caption" numberOfLines={1} style={{ color: col.fg }}>
                      {formatCalendarTime(o.starts_at, zone)} – {formatCalendarTime(o.ends_at, zone)}
                    </Text>
                  ) : null}
                </Pressable>
              );
            })}
            {day === today ? (
              <View pointerEvents="none" style={{ position: "absolute", left: 0, right: 0, top: (calendarMinutesOfDay(now, zone) / 60) * HOUR, height: 2, backgroundColor: c.danger }} />
            ) : null}
          </View>
        </View>
      </ScrollView>
    </View>
  );
}
