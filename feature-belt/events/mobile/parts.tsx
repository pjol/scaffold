import DateTimePicker, { DateTimePickerAndroid, type DateTimePickerEvent } from "@react-native-community/datetimepicker";
import { eventDateBadge, formatEventRange, type EventItem, type EventVisibility, type RsvpStatus } from "@scaffoldapp/shared";
import { Badge, Card, Text, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { MapPin, Video } from "lucide-react-native";
import { Platform, Pressable, View } from "react-native";

export const RSVP_LABEL: Record<RsvpStatus, string> = { going: "Going", maybe: "Maybe", declined: "Can't go", waitlisted: "On the waitlist" };
export const RSVP_TONE: Record<RsvpStatus, BadgeTone> = { going: "success", maybe: "info", declined: "neutral", waitlisted: "warning" };

export const VISIBILITY_LABEL: Record<EventVisibility, string> = { public: "Public", members: "Members", invite: "Invite only" };
export const VISIBILITY_INFO: Record<EventVisibility, string> = {
  public: "Anyone with the link can read it, signed in or not. Who's going stays private.",
  members: "Everyone signed in can see it and RSVP.",
  invite: "Only hosts, people you invite and people with the invite link can see it.",
};

export function hasEnded(event: Pick<EventItem, "ends_at">, now = Date.now()): boolean {
  return new Date(event.ends_at).getTime() <= now;
}

export function goingText(event: Pick<EventItem, "going_count" | "capacity">): string {
  return event.capacity ? `${event.going_count} of ${event.capacity} going` : `${event.going_count} going`;
}

/** Month and day block. */
export function DateBadge({ iso, timeZone }: { iso: string; timeZone: string }) {
  const theme = useTheme();
  const d = eventDateBadge(iso, timeZone);
  return (
    <View
      importantForAccessibility="no-hide-descendants"
      accessibilityElementsHidden
      style={{ minWidth: theme.tap.lg, minHeight: theme.tap.lg, paddingVertical: theme.space[1], borderRadius: theme.radius.md, backgroundColor: theme.colors.primarySoft, alignItems: "center", justifyContent: "center" }}
    >
      <Text role="caption" weight={600} style={{ color: theme.colors.primaryStrong }} maxFontSizeMultiplier={1.5}>
        {d.month}
      </Text>
      <Text role="titleSm" style={{ color: theme.colors.primaryStrong }} maxFontSizeMultiplier={1.5}>
        {d.day}
      </Text>
    </View>
  );
}

/** Cancelled / ended / full, the viewer's RSVP and hosting. */
export function EventBadges({ event }: { event: EventItem }) {
  const theme = useTheme();
  const ended = hasEnded(event);
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: theme.space[2] }}>
      {event.status === "cancelled" ? (
        <Badge size="sm" tone="danger">
          Cancelled
        </Badge>
      ) : ended ? (
        <Badge size="sm" tone="neutral">
          Ended
        </Badge>
      ) : event.is_full ? (
        <Badge size="sm" tone="warning">
          Full
        </Badge>
      ) : null}
      {event.my_rsvp && event.my_rsvp.status !== "declined" ? (
        <Badge size="sm" tone={RSVP_TONE[event.my_rsvp.status]} dot>
          {RSVP_LABEL[event.my_rsvp.status]}
        </Badge>
      ) : null}
      {event.host_role ? (
        <Badge size="sm" tone="primary">
          Hosting
        </Badge>
      ) : null}
    </View>
  );
}

/** A list card: date block, title, time range in the event's zone, place, counts and badges. */
export function EventCard({ event, onPress }: { event: EventItem; onPress: () => void }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const range = formatEventRange(event.starts_at, event.ends_at, event.timezone);
  return (
    <Card padding="compact" onPress={onPress} accessibilityRole="button" accessibilityLabel={`${event.title}, ${range}`}>
      <View style={{ flexDirection: "row", gap: s[4], alignItems: "flex-start" }}>
        <DateBadge iso={event.starts_at} timeZone={event.timezone} />
        <View style={{ flex: 1, gap: s[1] }}>
          <Text role="titleSm" numberOfLines={2}>
            {event.title}
          </Text>
          <Text role="label" tone="muted">
            {range}
          </Text>
          {event.location_text ? (
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[1.5] }}>
              <MapPin size={16} color={c.textMuted} strokeWidth={2} />
              <Text role="label" tone="muted" numberOfLines={1} style={{ flex: 1 }}>
                {event.location_text}
              </Text>
            </View>
          ) : event.online_url ? (
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[1.5] }}>
              <Video size={16} color={c.textMuted} strokeWidth={2} />
              <Text role="label" tone="muted">
                Online
              </Text>
            </View>
          ) : null}
          <View style={{ flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: s[2], paddingTop: s[1] }}>
            {event.status === "scheduled" ? (
              <Text role="caption" tone="muted">
                {goingText(event)}
              </Text>
            ) : null}
            <EventBadges event={event} />
          </View>
        </View>
      </View>
    </Card>
  );
}

// ───────────────────────────────────────────────────── date + time pickers ──

const pad = (n: number) => String(n).padStart(2, "0");

/** A device-local Date holding the wall-clock `date` + `time` (the pickers work in device time). */
function wallDate(date: string, time: string): Date {
  const [y, mo, d] = date.split("-").map(Number);
  const [h, mi] = time.split(":").map(Number);
  const out = new Date(y ?? 2000, (mo ?? 1) - 1, d ?? 1, h ?? 0, mi ?? 0);
  return Number.isNaN(out.getTime()) ? new Date() : out;
}

const dayFormat = new Intl.DateTimeFormat("en-US", { weekday: "short", month: "short", day: "numeric", year: "numeric" });
const timeFormat = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" });

/**
 * Date and time of day as wall-clock strings (`YYYY-MM-DD`, `HH:mm`) in whatever zone the
 * form uses. iOS shows the compact native pickers inline; Android opens the system dialogs.
 */
export function DateTimeRow({ date, time, onChange, label }: { date: string; time: string; onChange: (date: string, time: string) => void; label: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const value = wallDate(date, time);
  const apply = (mode: "date" | "time", picked: Date) => {
    if (mode === "date") onChange(`${picked.getFullYear()}-${pad(picked.getMonth() + 1)}-${pad(picked.getDate())}`, time);
    else onChange(date, `${pad(picked.getHours())}:${pad(picked.getMinutes())}`);
  };

  if (Platform.OS === "ios") {
    const handle = (mode: "date" | "time") => (_e: DateTimePickerEvent, picked?: Date) => {
      if (picked) apply(mode, picked);
    };
    return (
      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }} accessibilityLabel={label}>
        <DateTimePicker value={value} mode="date" display="compact" onChange={handle("date")} accentColor={c.primary} themeVariant={theme.mode} />
        <DateTimePicker value={value} mode="time" display="compact" onChange={handle("time")} accentColor={c.primary} themeVariant={theme.mode} />
      </View>
    );
  }

  const open = (mode: "date" | "time") =>
    DateTimePickerAndroid.open({
      value,
      mode,
      onChange: (e: DateTimePickerEvent, picked?: Date) => {
        if (e.type === "set" && picked) apply(mode, picked);
      },
    });
  const field = (mode: "date" | "time", text: string) => (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${label}: ${text}. Change ${mode}`}
      onPress={() => open(mode)}
      style={({ pressed }) => ({
        minHeight: theme.tap.base,
        paddingHorizontal: s[4],
        justifyContent: "center",
        borderRadius: theme.radius.md,
        borderWidth: 1,
        borderColor: c.borderStrong,
        backgroundColor: pressed ? c.sunken : c.surface,
        flexGrow: 1,
      })}
    >
      <Text role="body">{text}</Text>
    </Pressable>
  );
  return (
    <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
      {field("date", dayFormat.format(value))}
      {field("time", timeFormat.format(value))}
    </View>
  );
}
