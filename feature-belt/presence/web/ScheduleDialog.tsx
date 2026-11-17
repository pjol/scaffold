"use client";

import {
  PRESENCE_CLOCK_OPTIONS,
  PRESENCE_WEEKDAYS,
  errorMessage,
  presenceClockLabel,
  presenceDefaultSchedule,
  presenceDeviceZone,
  useMyPresence,
  useSetPresenceSchedule,
  type PresenceQuietHours,
  type PresenceSchedule,
} from "@scaffoldapp/shared";
import { Button, Dialog, InfoTip, Select, Switch, toast } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";
import { openPresenceDialog } from "./store";

const startOptions = PRESENCE_CLOCK_OPTIONS.filter((t) => t !== "24:00").map((t) => ({ value: t, label: presenceClockLabel(t) }));
const endOptions = PRESENCE_CLOCK_OPTIONS.map((t) => ({ value: t, label: t === "24:00" ? "Midnight" : presenceClockLabel(t) }));
const night: PresenceQuietHours = { start: "22:00", end: "08:00" };

/** Mon–Sun order for display (the schedule stores Sunday first). */
const order = [1, 2, 3, 4, 5, 6, 0] as const;

/** Notification schedule: quiet hours per weekday in your zone; pushes and emails wait until they end. */
export function ScheduleDialog({ open }: { open: boolean }) {
  const mine = useMyPresence();
  const save = useSetPresenceSchedule();
  const [draft, setDraft] = useState<PresenceSchedule>(() => presenceDefaultSchedule());

  useEffect(() => {
    if (!open) return;
    setDraft(mine.data?.schedule ?? presenceDefaultSchedule());
    // Start from the saved schedule each time the dialog opens.
  }, [open]);

  const close = () => openPresenceDialog(null);
  const zone = presenceDeviceZone();
  const setDay = (i: number, w: PresenceQuietHours | null) => setDraft((d) => ({ ...d, days: d.days.map((x, j) => (j === i ? w : x)) }));
  const none = draft.days.every((d) => !d);
  const submit = (body: PresenceSchedule | null, done: string) =>
    save.mutate(body, {
      onSuccess: () => {
        toast.success(done);
        close();
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't save your schedule.")),
    });

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !o && close()}
      title="Notification schedule"
      size="lg"
      info="During quiet hours you get no push notifications or emails, and people see Do Not Disturb. Everything still arrives in the app."
      footer={
        <>
          {mine.data?.schedule ? (
            <Button variant="ghost" loading={save.isPending && save.variables === null} onClick={() => submit(null, "Schedule removed.")}>
              Remove schedule
            </Button>
          ) : null}
          <Button disabled={draft.enabled && none} loading={save.isPending && save.variables !== null} onClick={() => submit({ ...draft, time_zone: draft.time_zone || zone }, "Schedule saved.")}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-6">
        <Switch checked={draft.enabled} onCheckedChange={(enabled) => setDraft((d) => ({ ...d, enabled }))} label="Pause notifications on a schedule" />
        <ul className="flex flex-col gap-2" aria-label="Quiet hours by day">
          {order.map((i) => {
            const w = draft.days[i] ?? null;
            const day = PRESENCE_WEEKDAYS[i];
            return (
              <li key={day} className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <Switch
                  className="w-36 shrink-0"
                  checked={!!w}
                  disabled={!draft.enabled}
                  onCheckedChange={(on) => setDay(i, on ? night : null)}
                  label={day}
                />
                {w ? (
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <Select aria-label={`${day} from`} value={w.start} disabled={!draft.enabled} onValueChange={(start) => setDay(i, { ...w, start })} options={startOptions} className="min-w-0 flex-1" />
                    <span className="type-label text-fg-muted">to</span>
                    <Select aria-label={`${day} until`} value={w.end} disabled={!draft.enabled} onValueChange={(end) => setDay(i, { ...w, end })} options={endOptions} className="min-w-0 flex-1" />
                  </span>
                ) : (
                  <span className="type-label text-fg-muted">No quiet hours</span>
                )}
              </li>
            );
          })}
        </ul>
        <p className="flex items-center gap-2 type-label text-fg-muted">
          Times in {draft.time_zone || zone}
          <InfoTip label="About the time zone" content="An end earlier than the start runs into the next morning. Use this device's zone to move the schedule when you travel." />
          {draft.time_zone && draft.time_zone !== zone ? (
            <Button variant="ghost" size="sm" onClick={() => setDraft((d) => ({ ...d, time_zone: zone }))}>
              Use {zone}
            </Button>
          ) : null}
        </p>
      </div>
    </Dialog>
  );
}
