"use client";

import {
  calendarDeviceZone,
  calendarZoneLabel,
  calendarZoneOptions,
  useCalendarDeviceZoneSave,
  type CalendarColor,
  type CalendarOccurrence,
  type CalendarResponse,
} from "@scaffoldapp/shared";
import { Badge, Select, cn } from "@scaffoldapp/ui-web";
import { useMemo } from "react";

/** Token classes per event colour: a soft fill with strong text (chips), a solid dot. */
export const CALENDAR_COLOR_CLASSES: Record<CalendarColor, { chip: string; dot: string; ring: string }> = {
  primary: { chip: "bg-primary-soft text-primary-strong", dot: "bg-primary", ring: "ring-primary" },
  accent: { chip: "bg-accent-soft text-accent-strong", dot: "bg-accent", ring: "ring-accent" },
  success: { chip: "bg-success-soft text-success-strong", dot: "bg-success", ring: "ring-success" },
  warning: { chip: "bg-warning-soft text-warning-strong", dot: "bg-warning", ring: "ring-warning" },
  danger: { chip: "bg-danger-soft text-danger-strong", dot: "bg-danger", ring: "ring-danger" },
  info: { chip: "bg-info-soft text-info-strong", dot: "bg-info", ring: "ring-info" },
  neutral: { chip: "bg-sunken text-fg", dot: "bg-fg-muted", ring: "ring-border-strong" },
};

export const CALENDAR_COLOR_NAMES: Record<CalendarColor, string> = {
  primary: "Default",
  accent: "Accent",
  success: "Green",
  warning: "Amber",
  danger: "Red",
  info: "Blue",
  neutral: "Gray",
};

/** Classes for an occurrence chip, dimmed when declined and outlined while unanswered. */
export function occurrenceClasses(o: Pick<CalendarOccurrence, "color" | "my_response">): string {
  const c = CALENDAR_COLOR_CLASSES[o.color] ?? CALENDAR_COLOR_CLASSES.primary;
  if (o.my_response === "declined") return "bg-surface text-fg-muted line-through border border-border";
  if (o.my_response === "needs_action") return cn("bg-surface border border-dashed border-border-strong", c.chip.split(" ")[1]);
  return c.chip;
}

export function ColorDot({ color, className }: { color: CalendarColor; className?: string }) {
  return <span aria-hidden className={cn("block size-2 shrink-0 rounded-pill", CALENDAR_COLOR_CLASSES[color]?.dot, className)} />;
}

const RESPONSE_LABEL: Record<CalendarResponse, string> = { needs_action: "Awaiting reply", accepted: "Going", tentative: "Maybe", declined: "Declined" };
const RESPONSE_TONE = { needs_action: "neutral", accepted: "success", tentative: "warning", declined: "danger" } as const;

export function ResponseBadge({ response }: { response: CalendarResponse }) {
  return (
    <Badge tone={RESPONSE_TONE[response]} dot>
      {RESPONSE_LABEL[response]}
    </Badge>
  );
}

export function responseLabel(r: CalendarResponse): string {
  return RESPONSE_LABEL[r];
}

/** A time-zone picker with the current short name of each zone. */
export function ZoneSelect({ value, onChange, id, className }: { value: string; onChange: (z: string) => void; id?: string; className?: string }) {
  const options = useMemo(() => {
    const now = new Date().toISOString();
    return calendarZoneOptions([value]).map((z) => ({ value: z, label: `${z.replace(/_/g, " ")} (${calendarZoneLabel(now, z)})` }));
  }, [value]);
  return <Select options={options} value={value} onValueChange={onChange} {...(id ? { id } : {})} {...(className ? { className } : {})} />;
}

/**
 * The zone the caller's calendar is shown in: their saved zone, else the device's (saved
 * the first time they're signed in, by `calendarZoneSync` or here).
 */
export function useCalendarZone(): { zone: string; weekStart: number; ready: boolean } {
  const settings = useCalendarDeviceZoneSave();
  const device = calendarDeviceZone();
  return {
    zone: settings.data?.timezone_set ? settings.data.timezone : device,
    weekStart: settings.data?.week_start ?? 1,
    ready: !settings.isPending,
  };
}

/** Minutes → "9:00 AM" for availability editors. */
export function minutesLabel(m: number): string {
  const h = Math.floor(m / 60) % 24;
  const mm = m % 60;
  const suffix = h < 12 ? "AM" : "PM";
  const h12 = h % 12 === 0 ? 12 : h % 12;
  return m === 1440 ? "12:00 AM (next day)" : `${h12}:${String(mm).padStart(2, "0")} ${suffix}`;
}

/** Copy text, toasting the outcome. */
export async function copyText(text: string, toast: { success: (m: string) => void; error: (m: string) => void }, done = "Link copied"): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(done);
  } catch {
    toast.error("Couldn't copy. Select the text and copy it yourself.");
  }
}

/** Save text as a file through the browser's download flow. */
export function downloadText(text: string, filename: string, type = "text/calendar"): void {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
