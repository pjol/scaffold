import { calendarDateKey, calendarFromWall, calendarMinutesOfDay, errorMessage, useProposeCalendarTime, type CalendarEventDetail } from "@scaffoldapp/shared";
import { Button, Field, Sheet, TextArea, toast, useTheme } from "@scaffoldapp/ui-native";
import { useEffect, useState } from "react";
import { View } from "react-native";
import { CalendarDateTimeRow } from "./parts";

const pad = (n: number) => String(n).padStart(2, "0");
const hhmm = (m: number) => `${pad(Math.floor(m / 60) % 24)}:${pad(m % 60)}`;
const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
};

/** A note to the organizer, sent with the guest's reply. */
export function NoteSheet({ open, onClose, initial, onSend }: { open: boolean; onClose: () => void; initial: string; onSend: (note: string) => void }) {
  const theme = useTheme();
  const [note, setNote] = useState(initial);
  useEffect(() => {
    if (open) setNote(initial);
  }, [open, initial]);
  return (
    <Sheet open={open} onClose={onClose} title="Note to the organizer">
      <View style={{ gap: theme.space[4] }}>
        <TextArea value={note} onChangeText={setNote} maxLength={500} placeholder="Running late, will join at 10:15" accessibilityLabel="Note to the organizer" />
        <Button fullWidth onPress={() => onSend(note.trim())}>
          Send note
        </Button>
      </View>
    </Sheet>
  );
}

/** Propose a new time for a one-off event: day, start, end and a note. */
export function ProposeSheet({ open, onClose, event: e, zone }: { open: boolean; onClose: () => void; event: CalendarEventDetail; zone: string }) {
  const theme = useTheme();
  const propose = useProposeCalendarTime();
  const len = Math.round((new Date(e.ends_at).getTime() - new Date(e.starts_at).getTime()) / 60_000);
  const [date, setDate] = useState(calendarDateKey(e.starts_at, zone));
  const [start, setStart] = useState(hhmm(calendarMinutesOfDay(e.starts_at, zone)));
  const [end, setEnd] = useState(hhmm(calendarMinutesOfDay(e.ends_at, zone)));
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    const s = calendarFromWall(date, toMin(start), zone);
    const en = calendarFromWall(date, toMin(end), zone);
    if (en <= s) {
      setError("The end must be after the start.");
      return;
    }
    try {
      await propose.mutateAsync({ id: e.event_id, starts_at: s, ends_at: en, note: note.trim() });
      toast.success("Proposal sent to the organizer");
      onClose();
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't send the proposal."));
    }
  };
  return (
    <Sheet open={open} onClose={onClose} title="Propose a new time" snapPoints={["80%"]}>
      <View style={{ gap: theme.space[4] }}>
        <Field label="Starts" {...(error ? { error } : {})}>
          <CalendarDateTimeRow
            label="Starts"
            date={date}
            time={start}
            onChange={(d, t) => {
              setDate(d);
              setStart(t);
              setEnd(hhmm(Math.min(toMin(t) + len, 1439)));
            }}
          />
        </Field>
        <Field label="Ends">
          <CalendarDateTimeRow label="Ends" date={date} time={end} onChange={(_d, t) => setEnd(t)} />
        </Field>
        <Field label="Note">
          <TextArea value={note} onChangeText={setNote} maxLength={500} placeholder="Why this time works better" />
        </Field>
        <Button fullWidth loading={propose.isPending} onPress={() => void submit()}>
          Send proposal
        </Button>
      </View>
    </Sheet>
  );
}
