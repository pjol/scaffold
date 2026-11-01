import DateTimePicker, { DateTimePickerAndroid, type DateTimePickerEvent } from "@react-native-community/datetimepicker";
import { calendarDeviceZone, useCalendarDeviceZoneSave, type CalendarColor, type CalendarResponse } from "@scaffoldapp/shared";
import { Text, useTheme, type BadgeTone } from "@scaffoldapp/ui-native";
import { Platform, Pressable, View } from "react-native";

type Theme = ReturnType<typeof useTheme>;

/** Fill and text colours for an event colour (tokens only). */
export function calendarColors(theme: Theme, color: CalendarColor): { bg: string; fg: string; dot: string } {
  const c = theme.colors;
  switch (color) {
    case "accent":
      return { bg: c.accentSoft, fg: c.accentStrong, dot: c.accent };
    case "success":
      return { bg: c.successSoft, fg: c.successStrong, dot: c.success };
    case "warning":
      return { bg: c.warningSoft, fg: c.warningStrong, dot: c.warning };
    case "danger":
      return { bg: c.dangerSoft, fg: c.dangerStrong, dot: c.danger };
    case "info":
      return { bg: c.infoSoft, fg: c.infoStrong, dot: c.info };
    case "neutral":
      return { bg: c.sunken, fg: c.text, dot: c.textMuted };
    default:
      return { bg: c.primarySoft, fg: c.primaryStrong, dot: c.primary };
  }
}

export const RESPONSE_LABEL: Record<CalendarResponse, string> = { needs_action: "Awaiting reply", accepted: "Going", tentative: "Maybe", declined: "Declined" };
export const RESPONSE_TONE: Record<CalendarResponse, BadgeTone> = { needs_action: "neutral", accepted: "success", tentative: "warning", declined: "danger" };

/** The calendar's zone: the saved one, else the device's (saved the first time they're signed in). */
export function useCalendarZone(): { zone: string; weekStart: number; ready: boolean } {
  const settings = useCalendarDeviceZoneSave();
  const device = calendarDeviceZone();
  return { zone: settings.data?.timezone_set ? settings.data.timezone : device, weekStart: settings.data?.week_start ?? 1, ready: !settings.isPending };
}

const pad = (n: number) => String(n).padStart(2, "0");

/** A local Date holding wall-clock `date` + `time` (pickers show device-local values). */
function wallDate(date: string, time: string): Date {
  const [y, mo, d] = date.split("-").map(Number);
  const [h, mi] = time.split(":").map(Number);
  return new Date(y ?? 2026, (mo ?? 1) - 1, d ?? 1, h ?? 0, mi ?? 0);
}

const dayFormat = new Intl.DateTimeFormat("en-US", { weekday: "short", month: "short", day: "numeric", year: "numeric" });
const timeFormat = new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" });

/** Date and (unless dateOnly) time pickers for wall-clock values (`YYYY-MM-DD`, `HH:mm`). */
export function CalendarDateTimeRow({ date, time, onChange, label, dateOnly }: { date: string; time: string; onChange: (date: string, time: string) => void; label: string; dateOnly?: boolean }) {
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
        {!dateOnly ? <DateTimePicker value={value} mode="time" display="compact" minuteInterval={5} onChange={handle("time")} accentColor={c.primary} themeVariant={theme.mode} /> : null}
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
      {!dateOnly ? field("time", timeFormat.format(value)) : null}
    </View>
  );
}

/** A small round colour marker. */
export function CalendarDot({ color }: { color: string }) {
  const theme = useTheme();
  return <View style={{ width: theme.space[2], height: theme.space[2], borderRadius: theme.radius.pill, backgroundColor: color }} />;
}
