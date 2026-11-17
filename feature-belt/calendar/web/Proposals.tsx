"use client";

import {
  calendarDateKey,
  calendarFromWall,
  calendarMinutesOfDay,
  errorMessage,
  useAnswerCalendarProposal,
  useProposeCalendarTime,
  useRespondCalendarEvent,
  useWithdrawCalendarProposal,
  type CalendarAttendee,
  type CalendarEventDetail,
  type CalendarResponse,
} from "@scaffoldapp/shared";
import { Button, Dialog, Field, Input, SegmentedControl, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, Check, X } from "lucide-react";
import { useState } from "react";

const pad = (n: number) => String(n).padStart(2, "0");
const hhmm = (m: number) => `${pad(Math.floor(m / 60) % 24)}:${pad(m % 60)}`;
const toMin = (t: string) => {
  const [h, m] = t.split(":").map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
};

/** "Tue, Nov 3 · 3:00 – 3:30 PM" in zone. */
export function proposalLabel(start: string, end: string, zone: string): string {
  const day = new Date(start).toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric", timeZone: zone });
  const t = (iso: string) => new Date(iso).toLocaleTimeString("en-US", { hour: "numeric", minute: "2-digit", timeZone: zone });
  return `${day} · ${t(start)} – ${t(end)}`;
}

/** The guest's reply: Yes / Maybe / No, a note to the organizer, and Propose a new time. */
export function ReplyBox({ event: e, zone }: { event: CalendarEventDetail; zone: string }) {
  const respond = useRespondCalendarEvent();
  const withdraw = useWithdrawCalendarProposal();
  // Guests see only their own note and proposal (the server leaves out others').
  const me = e.attendees.find((a) => a.note) ?? null;
  const mine = e.attendees.find((a) => a.proposed_start) ?? null;
  const [note, setNote] = useState(me?.note ?? "");
  const [noteOpen, setNoteOpen] = useState(!!me?.note);
  const [proposing, setProposing] = useState(false);
  const reply = (response: Exclude<CalendarResponse, "needs_action">, withNote?: string) =>
    respond.mutate(
      { id: e.event_id, response, ...(withNote !== undefined ? { note: withNote } : {}) },
      { onSuccess: () => toast.success(withNote !== undefined ? "Reply and note sent" : "Reply sent"), onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply.")) },
    );
  const canPropose = !e.recurring && !e.all_day;
  return (
    <div className="flex flex-col gap-3">
      <span className="type-label font-semibold text-fg">Going?{e.recurring ? " (all events in the series)" : ""}</span>
      <SegmentedControl
        aria-label="Your reply"
        value={e.my_response === "needs_action" || !e.my_response ? undefined : e.my_response}
        onValueChange={(v) => reply(v)}
        options={[
          { value: "accepted", label: "Yes" },
          { value: "tentative", label: "Maybe" },
          { value: "declined", label: "No" },
        ]}
      />
      {noteOpen ? (
        <div className="flex flex-col gap-2">
          <TextArea value={note} onChange={(ev) => setNote(ev.target.value)} maxLength={500} minRows={2} placeholder="Note to the organizer" aria-label="Note to the organizer" />
          <Button
            size="sm"
            variant="secondary"
            className="self-start"
            loading={respond.isPending}
            disabled={!e.my_response || e.my_response === "needs_action"}
            onClick={() => e.my_response && e.my_response !== "needs_action" && reply(e.my_response, note.trim())}
          >
            Send note
          </Button>
        </div>
      ) : null}
      <div className="flex flex-wrap gap-2">
        {!noteOpen ? (
          <Button size="sm" variant="ghost" onClick={() => setNoteOpen(true)}>
            Add a note
          </Button>
        ) : null}
        {canPropose ? (
          <Button size="sm" variant="ghost" leftIcon={<CalendarClock />} onClick={() => setProposing(true)}>
            Propose a new time
          </Button>
        ) : null}
      </div>
      {mine?.proposed_start && mine.proposed_end ? (
        <div className="flex flex-wrap items-center gap-2 rounded-md bg-sunken px-3 py-2 type-caption text-fg">
          <span className="min-w-0 flex-1">You proposed {proposalLabel(mine.proposed_start, mine.proposed_end, zone)}</span>
          <Button size="sm" variant="ghost" loading={withdraw.isPending} onClick={() => withdraw.mutate(e.event_id, { onSuccess: () => toast.success("Proposal withdrawn"), onError: (err) => toast.error(errorMessage(err)) })}>
            Withdraw
          </Button>
        </div>
      ) : null}
      {proposing ? <ProposeDialog event={e} zone={zone} onClose={() => setProposing(false)} /> : null}
    </div>
  );
}

function ProposeDialog({ event: e, zone, onClose }: { event: CalendarEventDetail; zone: string; onClose: () => void }) {
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
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title="Propose a new time"
      info="The organizer sees your proposal and can move the event to it. Your reply stays as it is."
      size="sm"
      footer={
        <div className="flex w-full justify-end gap-2">
          <Button variant="ghost" size="auto" onClick={onClose}>
            Cancel
          </Button>
          <Button size="auto" loading={propose.isPending} onClick={() => void submit()}>
            Send proposal
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Day" error={error ?? undefined}>
          <Input type="date" value={date} onChange={(ev) => ev.target.value && setDate(ev.target.value)} />
        </Field>
        <div className="flex gap-2">
          <Field label="Starts" className="flex-1">
            <Input
              type="time"
              step={900}
              value={start}
              onChange={(ev) => {
                const v = ev.target.value;
                if (!v) return;
                setStart(v);
                setEnd(hhmm(Math.min(toMin(v) + len, 1439)));
              }}
            />
          </Field>
          <Field label="Ends" className="flex-1">
            <Input type="time" step={900} value={end} onChange={(ev) => ev.target.value && setEnd(ev.target.value)} />
          </Field>
        </div>
        <Field label="Note">
          <TextArea value={note} onChange={(ev) => setNote(ev.target.value)} maxLength={500} minRows={2} placeholder="Why this time works better" />
        </Field>
      </div>
    </Dialog>
  );
}

/** A guest's note and proposed time under their row; the organizer accepts it or keeps the time. */
export function GuestExtras({ event: e, attendee: a, zone }: { event: CalendarEventDetail; attendee: CalendarAttendee; zone: string }) {
  const answer = useAnswerCalendarProposal();
  if (!a.note && !a.proposed_start) return null;
  return (
    <div className="ml-11 flex flex-col gap-2">
      {a.note ? <p className="whitespace-pre-wrap break-words type-caption text-fg-muted">“{a.note}”</p> : null}
      {a.proposed_start && a.proposed_end ? (
        <div className="flex flex-wrap items-center gap-2 rounded-md bg-sunken px-3 py-2">
          <span className="min-w-0 flex-1 type-caption text-fg">Proposed {proposalLabel(a.proposed_start, a.proposed_end, zone)}</span>
          {e.can_edit ? (
            <>
              <Button
                size="sm"
                leftIcon={<Check />}
                loading={answer.isPending && answer.variables?.accept === true}
                onClick={() =>
                  answer.mutate(
                    { id: e.event_id, attendeeId: a.id, accept: true },
                    { onSuccess: () => toast.success("Moved to the proposed time. Guests were told."), onError: (err) => toast.error(errorMessage(err, "Couldn't move the event.")) },
                  )
                }
              >
                Accept
              </Button>
              <Button
                size="sm"
                variant="ghost"
                leftIcon={<X />}
                loading={answer.isPending && answer.variables?.accept === false}
                onClick={() => answer.mutate({ id: e.event_id, attendeeId: a.id, accept: false }, { onSuccess: () => toast.success("Kept the current time"), onError: (err) => toast.error(errorMessage(err)) })}
              >
                Keep time
              </Button>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
