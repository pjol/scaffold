"use client";

import { emptyWeek, WEEKDAYS, type PlaceHours, type Weekday } from "@scaffoldapp/shared";
import { Button, Checkbox, Field, IconButton, Input } from "@scaffoldapp/ui-web";
import { Plus, X } from "lucide-react";

const MAX_PER_DAY = 4;

/** Device time zone, the default for new hours. */
export function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "";
  } catch {
    return "";
  }
}

/** New hours: Monday to Friday 09:00–17:00. */
export function defaultHours(): PlaceHours {
  const weekly = emptyWeek();
  for (const d of ["mon", "tue", "wed", "thu", "fri"] as Weekday[]) weekly[d] = [{ open: "09:00", close: "17:00" }];
  return { timezone: deviceTimeZone(), note: "", weekly };
}

/** Weekly hours: per day closed or 1 to 4 openings; past-midnight closes allowed. */
export function HoursEditor({ value, onChange }: { value: PlaceHours; onChange: (next: PlaceHours) => void }) {
  const setDay = (day: Weekday, list: PlaceHours["weekly"][Weekday]) => onChange({ ...value, weekly: { ...value.weekly, [day]: list } });
  return (
    <div className="flex flex-col gap-4">
      <ul className="flex flex-col gap-3">
        {WEEKDAYS.map((d) => {
          const list = value.weekly[d.key];
          const open = list.length > 0;
          return (
            <li key={d.key} className="flex flex-col gap-2 sm:flex-row sm:items-start sm:gap-4">
              <div className="flex min-h-control items-center sm:w-36">
                <Checkbox
                  label={d.label}
                  checked={open}
                  onCheckedChange={(on) => setDay(d.key, on ? [{ open: "09:00", close: "17:00" }] : [])}
                />
              </div>
              {open ? (
                <div className="flex flex-1 flex-col gap-2">
                  {list.map((iv, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Input
                        type="time"
                        aria-label={`${d.label} opens`}
                        value={iv.open}
                        onChange={(e) => setDay(d.key, list.map((x, j) => (j === i ? { ...x, open: e.target.value } : x)))}
                        wrapperClassName="w-36"
                      />
                      <span className="type-label text-fg-muted" aria-hidden>
                        to
                      </span>
                      <Input
                        type="time"
                        aria-label={`${d.label} closes`}
                        value={iv.close === "24:00" ? "23:59" : iv.close}
                        onChange={(e) => setDay(d.key, list.map((x, j) => (j === i ? { ...x, close: e.target.value } : x)))}
                        wrapperClassName="w-36"
                      />
                      {list.length > 1 ? (
                        <IconButton label={`Remove ${d.label} opening`} icon={<X />} size="sm" onClick={() => setDay(d.key, list.filter((_, j) => j !== i))} />
                      ) : null}
                    </div>
                  ))}
                  {list.length < MAX_PER_DAY ? (
                    <Button variant="ghost" size="sm" leftIcon={<Plus />} className="self-start" onClick={() => setDay(d.key, [...list, { open: "18:00", close: "22:00" }])}>
                      Add opening
                    </Button>
                  ) : null}
                </div>
              ) : (
                <span className="flex min-h-control items-center type-label text-fg-muted">Closed</span>
              )}
            </li>
          );
        })}
      </ul>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Time zone" info="IANA name, like America/Los_Angeles. Open-now badges use it.">
          <Input value={value.timezone} onChange={(e) => onChange({ ...value, timezone: e.target.value })} placeholder="America/Los_Angeles" autoComplete="off" />
        </Field>
        <Field label="Note">
          <Input value={value.note} maxLength={200} onChange={(e) => onChange({ ...value, note: e.target.value })} placeholder="Closed on public holidays" />
        </Field>
      </div>
    </div>
  );
}
