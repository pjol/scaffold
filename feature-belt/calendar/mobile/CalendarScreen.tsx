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
  formatCalendarDay,
  formatCalendarDayLong,
  formatCalendarMonth,
  formatCalendarTime,
  calendarMonthGrid,
  calendarSameMonth,
  calendarStartOfMonth,
  calendarWeekdayNames,
  useCalendarCalendars,
  isCalendarConnected,
  useCalendarConnectedRange,
  useCalendarRange,
  useCalendarSources,
  useNow,
  type CalendarOccurrence,
} from "@scaffoldapp/shared";
import { Button, ConnectPrompt, EmptyState, ErrorState, IconButton, ProviderIcon, SegmentedControl, Skeleton, SourceBadge, Text, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { CalendarDays, ChevronLeft, ChevronRight, Focus, Link2, Plane, Plus, Repeat } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Pressable, RefreshControl, ScrollView, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { CalendarDot, calendarColors, useCalendarZone } from "./parts";

const HOUR = 56;
const AGENDA_DAYS = 14;

function openEvent(o: CalendarOccurrence) {
  if (isCalendarConnected(o)) {
    router.push({ pathname: "/calendar/connected", params: { connection_id: o.connection_id, calendar_id: o.calendar_id, event_id: o.external_id } });
    return;
  }
  router.push({ pathname: "/calendar/event/[id]", params: { id: o.event_id, ...(o.occurrence_start ? { occurrence: o.occurrence_start } : {}) } });
}

/** ", from Google Calendar · sam@acme.com" for screen readers; "" for the app's own events. */
function sourceText(o: CalendarOccurrence): string {
  return o.source?.type === "connected" ? `, from ${o.source.account ? `${o.source.provider} ${o.source.account}` : o.source.provider}` : "";
}

function newEvent(startsAt?: string, endsAt?: string, eventType?: "out_of_office" | "focus") {
  router.push({ pathname: "/calendar/form", params: { ...(startsAt && endsAt ? { starts_at: startsAt, ends_at: endsAt } : {}), ...(eventType ? { event_type: eventType } : {}) } });
}

type View3 = "agenda" | "day" | "3day" | "month";

/** Calendar: agenda (the next two weeks) or one day as a timeline; tap empty time to add an event. */
export function CalendarScreen() {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const { zone, weekStart, ready } = useCalendarZone();
  const today = calendarTodayKey(zone);
  const [view, setView] = useState<View3>("agenda");
  const [date, setDate] = useState<string | null>(null);
  const day = date ?? today;
  const span = view === "day" ? 1 : view === "3day" ? 3 : AGENDA_DAYS;
  const grid = useMemo(() => calendarMonthGrid(day, weekStart), [day, weekStart]);
  const first = view === "month" ? grid[0]! : day;
  const last = view === "month" ? calendarAddDays(grid[grid.length - 1]!, 1) : calendarAddDays(day, span);
  const from = ready ? calendarDayStart(first, zone) : null;
  const to = ready ? calendarDayStart(last, zone) : null;
  const calendars = useCalendarCalendars();
  const range = useCalendarRange(from, to);
  // Connected calendars' events (Google, Outlook) join the app's own; hidden calendars drop out.
  const connected = useCalendarConnectedRange(from, to);
  const sources = useCalendarSources();
  const items = useMemo(() => {
    const hidden = new Set<string>();
    for (const src of sources.data?.sources ?? []) for (const cal of src.calendars) if (cal.hidden) hidden.add(`${src.id}|${cal.id}`);
    // The app's calendars: hidden ones drop out; default-coloured events take their calendar's colour.
    const cals = new Map((calendars.data?.items ?? []).map((x) => [x.id, x]));
    const app = (range.data?.items ?? [])
      .filter((o) => !(o.calendar_id && cals.get(o.calendar_id)?.hidden))
      .map((o): CalendarOccurrence => {
        const cal = o.calendar_id ? cals.get(o.calendar_id) : undefined;
        if (o.redacted) return { ...o, color: "neutral" };
        return cal && o.color === "primary" && cal.color !== "primary" ? { ...o, color: cal.color } : o;
      });
    return [...app, ...(connected.data?.items ?? []).filter((o) => !hidden.has(`${o.connection_id}|${o.calendar_id}`))];
  }, [range.data, connected.data, sources.data, calendars.data]);
  const hasConnected = (sources.data?.sources ?? []).some((x) => x.type === "connected");

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
                actions={[
                  { id: "ooo", label: "Out of office", icon: Plane, onSelect: () => newEvent(calendarDayStart(day, zone), calendarDayStart(calendarAddDays(day, 1), zone), "out_of_office") },
                  { id: "focus", label: "Focus time", icon: Focus, onSelect: () => newEvent(calendarFromWall(day, 9 * 60, zone), calendarFromWall(day, 11 * 60, zone), "focus") },
                  { id: "links", label: "Booking links", icon: Link2, onSelect: () => router.push("/calendar/links") },
                ]}
              />
            </View>
          ),
        }}
      />
      <View style={{ gap: s[3], paddingHorizontal: s[4], paddingTop: s[3], paddingBottom: s[2] }}>
        <SegmentedControl<View3>
          accessibilityLabel="View"
          options={[
            { value: "agenda", label: "Agenda" },
            { value: "day", label: "Day" },
            { value: "3day", label: "3 days" },
            { value: "month", label: "Month" },
          ]}
          value={view}
          onValueChange={setView}
        />
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
          <IconButton label="Previous" icon={ChevronLeft} variant="ghost" onPress={() => setDate(view === "month" ? calendarStartOfMonth(day, -1) : calendarAddDays(day, -span))} />
          <Text role="titleSm" style={{ flex: 1 }} numberOfLines={1} accessibilityLiveRegion="polite">
            {view === "day"
              ? formatCalendarDayLong(day)
              : view === "month"
                ? formatCalendarMonth(day)
                : view === "3day"
                  ? `${formatCalendarDay(day)} – ${formatCalendarDay(calendarAddDays(day, 2))}`
                  : day === today
                    ? "Next 2 weeks"
                    : `From ${formatCalendarDayLong(day)}`}
          </Text>
          <IconButton label="Next" icon={ChevronRight} variant="ghost" onPress={() => setDate(view === "month" ? calendarStartOfMonth(day, 1) : calendarAddDays(day, span))} />
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
      ) : view === "day" || view === "3day" ? (
        <DayTimeline days={view === "day" ? [day] : [day, calendarAddDays(day, 1), calendarAddDays(day, 2)]} zone={zone} items={items} refreshing={range.isRefetching} onRefresh={() => void range.refetch()} />
      ) : view === "month" ? (
        <MonthGrid
          month={day}
          grid={grid}
          zone={zone}
          weekStart={weekStart}
          items={items}
          onDay={(d) => {
            setDate(d);
            setView("day");
          }}
        />
      ) : (
        <Agenda
          day={day}
          zone={zone}
          items={items}
          refreshing={range.isRefetching}
          onRefresh={() => {
            void range.refetch();
            void connected.refetch();
          }}
          footer={
            !hasConnected ? (
              <ConnectPrompt offers={sources.data?.offers} onConnect={(o) => router.push(o.connect_url.slice(1) as never)} title="Your other calendars" />
            ) : null
          }
        />
      )}
    </View>
  );
}

function Agenda({ day, zone, items, refreshing, onRefresh, footer }: { day: string; zone: string; items: CalendarOccurrence[]; refreshing: boolean; onRefresh: () => void; footer?: ReactNode }) {
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
                  accessibilityLabel={`${o.title}, ${o.all_day ? "all day" : `${formatCalendarTime(o.starts_at, zone)} to ${formatCalendarTime(o.ends_at, zone)}`}${declined ? ", declined" : ""}${sourceText(o)}`}
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
                    <SourceBadge source={o.source} style={{ alignSelf: "flex-start" }} />
                  </View>
                  {o.recurring ? <Repeat size={16} color={c.textSubtle} /> : null}
                </Pressable>
              );
            })}
          </View>
        ))
      )}
      {footer}
    </ScrollView>
  );
}

/** Month: a dot per event on each day (up to three); a day opens its timeline. */
function MonthGrid({ month, grid, zone, weekStart, items, onDay }: { month: string; grid: string[]; zone: string; weekStart: number; items: CalendarOccurrence[]; onDay: (d: string) => void }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const today = calendarTodayKey(zone);
  const names = calendarWeekdayNames(weekStart, "narrow");
  return (
    <ScrollView contentContainerStyle={{ paddingHorizontal: s[2], paddingBottom: s[8] }}>
      <View style={{ flexDirection: "row" }}>
        {names.map((n, i) => (
          <Text key={i} role="caption" tone="muted" style={{ flex: 1, textAlign: "center", paddingVertical: s[2] }}>
            {n}
          </Text>
        ))}
      </View>
      <View style={{ flexDirection: "row", flexWrap: "wrap" }}>
        {grid.map((d) => {
          const dayItems = calendarOnDay(items, d, zone);
          const inMonth = calendarSameMonth(d, month);
          const isToday = d === today;
          return (
            <Pressable
              key={d}
              accessibilityRole="button"
              accessibilityLabel={`${formatCalendarDayLong(d)}, ${dayItems.length === 0 ? "nothing scheduled" : `${dayItems.length} ${dayItems.length === 1 ? "event" : "events"}`}`}
              onPress={() => onDay(d)}
              style={({ pressed }) => ({
                width: `${100 / 7}%`,
                minHeight: theme.tap.lg + s[2],
                alignItems: "center",
                gap: s[1],
                paddingVertical: s[1],
                borderTopWidth: 1,
                borderColor: c.border,
                backgroundColor: pressed ? c.sunken : "transparent",
              })}
            >
              <View style={{ minWidth: theme.tap.sm - s[2], height: theme.tap.sm - s[2], borderRadius: theme.radius.pill, alignItems: "center", justifyContent: "center", backgroundColor: isToday ? c.primary : "transparent" }}>
                <Text role="label" weight={isToday ? 600 : 400} style={{ color: isToday ? c.primaryFg : inMonth ? c.text : c.textSubtle }}>
                  {Number(d.slice(8))}
                </Text>
              </View>
              <View style={{ flexDirection: "row", gap: s[0.5] }}>
                {dayItems.slice(0, 3).map((o) => (
                  <CalendarDot key={calendarOccurrenceKey(o)} color={calendarColors(theme, o.color).dot} />
                ))}
              </View>
            </Pressable>
          );
        })}
      </View>
    </ScrollView>
  );
}

function DayTimeline({ days, zone, items, refreshing, onRefresh }: { days: string[]; zone: string; items: CalendarOccurrence[]; refreshing: boolean; onRefresh: () => void }) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const now = useNow(60_000);
  const today = calendarTodayKey(zone);
  const ref = useRef<ScrollView>(null);
  const [width, setWidth] = useState(0);
  const gutter = theme.space[12];
  const multi = days.length > 1;
  const colWidth = width / days.length;
  const allDay = days.flatMap((d) => calendarOnDay(items, d, zone).filter((o) => o.all_day)).filter((o, i, arr) => arr.findIndex((x) => calendarOccurrenceKey(x) === calendarOccurrenceKey(o)) === i);

  useEffect(() => {
    const target = days.includes(today) ? Math.max(0, calendarMinutesOfDay(Date.now(), zone) - 60) : 8 * 60;
    const t = setTimeout(() => ref.current?.scrollTo({ y: (target / 60) * HOUR, animated: false }), 0);
    return () => clearTimeout(t);
  }, [days.join(","), today, zone]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <View style={{ flex: 1 }}>
      {allDay.length > 0 ? (
        <View style={{ paddingHorizontal: s[4], paddingBottom: s[2], gap: s[1] }}>
          {allDay.map((o) => {
            const col = calendarColors(theme, o.color);
            return (
              <Pressable key={calendarOccurrenceKey(o)} accessibilityRole="button" accessibilityLabel={`${o.title}, all day${sourceText(o)}`} onPress={() => openEvent(o)} style={{ minHeight: theme.tap.sm, justifyContent: "center", paddingHorizontal: s[3], borderRadius: theme.radius.sm, backgroundColor: col.bg }}>
                <Text role="label" weight={600} style={{ color: col.fg }} numberOfLines={1}>
                  {o.title}
                </Text>
              </Pressable>
            );
          })}
        </View>
      ) : null}
      {multi ? (
        <View style={{ flexDirection: "row", paddingLeft: gutter, paddingBottom: s[1] }}>
          {days.map((d) => (
            <Text key={d} role="caption" weight={600} style={{ flex: 1, textAlign: "center", color: d === today ? c.primaryStrong : c.textMuted }} numberOfLines={1}>
              {formatCalendarDay(d)}
            </Text>
          ))}
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
          <View style={{ flex: 1, flexDirection: "row" }} onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
            {days.map((day) => {
              const placed = calendarLayoutDay(calendarOnDay(items, day, zone).filter((o) => !o.all_day), day, zone, 30);
              return (
          <View key={day} style={{ flex: 1, borderLeftWidth: multi ? 1 : 0, borderColor: c.border }}>
            {Array.from({ length: 24 }, (_, h) => (
              <Pressable
                key={h}
                accessibilityRole="button"
                accessibilityLabel={`Add an event${multi ? ` on ${formatCalendarDay(day)}` : ""} at ${h % 12 === 0 ? 12 : h % 12} ${h < 12 ? "AM" : "PM"}`}
                onPress={() => newEvent(calendarFromWall(day, h * 60, zone), calendarFromWall(day, h * 60 + 30, zone))}
                style={({ pressed }) => ({ height: HOUR, borderTopWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : "transparent" })}
              />
            ))}
            {placed.map((p) => {
              const o = p.item;
              const col = calendarColors(theme, o.color);
              const w = colWidth / p.columns;
              return (
                <Pressable
                  key={calendarOccurrenceKey(o)}
                  accessibilityRole="button"
                  accessibilityLabel={`${o.title}, ${formatCalendarTime(o.starts_at, zone)} to ${formatCalendarTime(o.ends_at, zone)}${sourceText(o)}`}
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
                  <View style={{ flexDirection: "row", alignItems: "center", gap: s[1] }}>
                    {o.source?.type === "connected" ? <ProviderIcon provider={o.source.provider} product={o.source.product ?? null} size={12} color={col.fg} /> : null}
                    <Text role="label" weight={600} numberOfLines={1} style={{ flex: 1, color: o.my_response === "declined" ? c.textMuted : col.fg }}>
                      {o.title}
                    </Text>
                  </View>
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
              );
            })}
          </View>
        </View>
      </ScrollView>
    </View>
  );
}
