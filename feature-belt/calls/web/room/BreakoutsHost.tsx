"use client";

import {
  errorMessage,
  formatCallsCountdown,
  useBroadcastCallsBreakouts,
  useCallsBreakouts,
  useCloseCallsBreakouts,
  useCreateCallsBreakouts,
  useJoinCallsBreakout,
  useUpdateCallsBreakouts,
  type CallsBreakout,
  type CallsBreakoutPerson,
} from "@scaffoldapp/shared";
import { Avatar, Button, ErrorState, Field, IconButton, Menu, MenuItem, SegmentedControl, Select, Skeleton, SkeletonGroup, Stepper, TextArea, toast } from "@scaffoldapp/ui-web";
import { DoorOpen, LogIn, MoreVertical, Send, Timer } from "lucide-react";
import { useState } from "react";
import { useCountdown } from "./countdown";
import { useCallSession, useRoomTarget } from "./session";

const TIMERS = [0, 5, 10, 15, 20, 30, 45, 60];
// Radix Select items can't have an empty value.
const NONE = "none";

/** Hosts: open breakout rooms, move people, visit rooms, broadcast, close. */
export function BreakoutsHost() {
  const s = useCallSession();
  const q = useCallsBreakouts(s.meetingId);
  if (q.isPending) {
    return (
      <SkeletonGroup label="Loading breakout rooms" className="flex flex-col gap-3">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-32 w-full" />
      </SkeletonGroup>
    );
  }
  if (q.isError) return <ErrorState compact title="Couldn't load breakout rooms" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />;
  return q.data ? <OpenRooms b={q.data} /> : <NewRooms />;
}

function NewRooms() {
  const s = useCallSession();
  const create = useCreateCallsBreakouts(s.meetingId);
  const [count, setCount] = useState(2);
  const [assign, setAssign] = useState<"auto" | "manual" | "choose">("auto");
  const [minutes, setMinutes] = useState(15);
  return (
    <div className="flex flex-col gap-5">
      <Field label="Rooms">
        <Stepper value={count} onValueChange={setCount} min={1} max={50} label="Number of rooms" />
      </Field>
      <Field label="Who goes where" info="Auto spreads people evenly at random. By hand: you move each person. People pick: everyone chooses a room. Hosts stay in the main room and can visit any room.">
        <SegmentedControl
          value={assign}
          onValueChange={setAssign}
          options={[
            { value: "auto", label: "Auto" },
            { value: "manual", label: "By hand" },
            { value: "choose", label: "People pick" },
          ]}
          aria-label="Who goes where"
        />
      </Field>
      <Field label="Timer" info="When it runs out, everyone gets 30 seconds, then returns to the main room.">
        <Select value={String(minutes)} onValueChange={(v) => setMinutes(Number(v))} options={TIMERS.map((m) => ({ value: String(m), label: m === 0 ? "No timer" : `${m} minutes` }))} />
      </Field>
      <Button
        leftIcon={<DoorOpen />}
        loading={create.isPending}
        onClick={() =>
          create.mutate(
            { count, assign, minutes },
            { onSuccess: () => toast.success(`${count} ${count === 1 ? "room is" : "rooms are"} open`), onError: (e) => toast.error(errorMessage(e, "Couldn't open the rooms.")) },
          )
        }
      >
        Open rooms
      </Button>
    </div>
  );
}

function OpenRooms({ b }: { b: CallsBreakout }) {
  const s = useCallSession();
  const target = useRoomTarget();
  const update = useUpdateCallsBreakouts(s.meetingId);
  const close = useCloseCallsBreakouts(s.meetingId);
  const broadcast = useBroadcastCallsBreakouts(s.meetingId);
  const visit = useJoinCallsBreakout(target);
  const [message, setMessage] = useState("");
  const left = useCountdown(b.status === "closing" ? b.closing_at : b.ends_at);
  const assigned = new Set(b.rooms.flatMap((r) => r.assigned.map((p) => p.identity)));
  const unassigned = b.main_room.filter((p) => !assigned.has(p.identity) && p.identity !== s.token.identity);
  const move = (identity: string, roomId: string | null) =>
    update.mutate({ assignments: { [identity]: roomId } }, { onError: (e) => toast.error(errorMessage(e, "Couldn't move them.")) });
  const roomOptions = [{ value: NONE, label: "Not assigned" }, ...b.rooms.map((r) => ({ value: r.id, label: r.name }))];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center gap-2 rounded-md bg-sunken px-3 py-2">
        <Timer className="size-4 text-fg-muted" aria-hidden />
        <span className="flex-1 type-label text-fg" role="timer">
          {b.status === "closing" ? `Closing in ${formatCallsCountdown(left ?? 0)}` : left !== null ? `${formatCallsCountdown(left)} left` : "No timer"}
          {b.allow_choose ? " · people choose" : ""}
        </span>
        <Menu trigger={<IconButton label="Breakout room options" icon={<MoreVertical />} variant="ghost" size="sm" />}>
          {b.status === "open" && (
            <>
              <MenuItem onSelect={() => update.mutate({ minutes: 5 })}>Restart timer: 5 minutes</MenuItem>
              <MenuItem onSelect={() => update.mutate({ minutes: 0 })}>Remove the timer</MenuItem>
              <MenuItem onSelect={() => update.mutate({ allow_choose: !b.allow_choose })}>{b.allow_choose ? "Stop letting people choose" : "Let people choose a room"}</MenuItem>
            </>
          )}
          <MenuItem destructive onSelect={() => close.mutate(true, { onError: (e) => toast.error(errorMessage(e, "Couldn't close the rooms.")) })}>
            Close rooms now
          </MenuItem>
        </Menu>
      </div>
      <ul className="flex flex-col gap-3">
        {b.rooms.map((r) => (
          <li key={r.id} className="flex flex-col gap-2 rounded-lg border border-border p-3">
            <div className="flex items-center gap-2">
              <h4 className="flex-1 truncate type-body font-semibold text-fg">{r.name}</h4>
              <span className="type-caption text-fg-muted">{r.present.length} here</span>
              <Button
                size="sm"
                variant="soft"
                leftIcon={<LogIn />}
                loading={visit.isPending && visit.variables === r.id}
                disabled={b.status !== "open"}
                onClick={() => visit.mutate(r.id, { onSuccess: (t) => s.switchRoom(t), onError: (e) => toast.error(errorMessage(e, "Couldn't join that room.")) })}
              >
                Join
              </Button>
            </div>
            {r.assigned.length === 0 ? (
              <p className="type-caption text-fg-muted">Nobody assigned</p>
            ) : (
              <ul className="flex flex-col gap-1">
                {r.assigned.map((p) => (
                  <PersonRow key={p.identity} p={p} value={r.id} options={roomOptions} onMove={move} disabled={b.status !== "open"} />
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
      {unassigned.length > 0 && b.status === "open" && (
        <section className="flex flex-col gap-2" aria-label="Not assigned">
          <h4 className="type-label font-semibold text-fg">In the main room, not assigned</h4>
          <ul className="flex flex-col gap-1">
            {unassigned.map((p) => (
              <PersonRow key={p.identity} p={p} value={NONE} options={roomOptions} onMove={move} />
            ))}
          </ul>
        </section>
      )}
      {b.status === "open" && (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const text = message.trim();
            if (!text) return;
            broadcast.mutate(text, { onSuccess: () => (setMessage(""), toast.success("Message sent to every room")), onError: (err) => toast.error(errorMessage(err, "Couldn't send it.")) });
          }}
        >
          <TextArea value={message} onChange={(e) => setMessage(e.target.value)} placeholder="Message every room" aria-label="Message every room" maxLength={500} minRows={1} />
          <Button type="submit" variant="secondary" leftIcon={<Send />} className="self-end" disabled={!message.trim()} loading={broadcast.isPending}>
            Send to all rooms
          </Button>
        </form>
      )}
      {b.status === "open" && (
        <Button variant="danger" loading={close.isPending} onClick={() => close.mutate(false, { onError: (e) => toast.error(errorMessage(e, "Couldn't close the rooms.")) })}>
          Close rooms
        </Button>
      )}
    </div>
  );
}

function PersonRow({ p, value, options, onMove, disabled }: { p: CallsBreakoutPerson; value: string; options: { value: string; label: string }[]; onMove: (identity: string, roomId: string | null) => void; disabled?: boolean }) {
  return (
    <li className="flex min-h-row items-center gap-2">
      <Avatar name={p.name || "Guest"} size="sm" />
      <span className="min-w-0 flex-1 truncate type-body text-fg">
        {p.name || "Guest"}
        {!p.present && value !== NONE ? <span className="type-caption text-fg-muted"> · not here yet</span> : null}
      </span>
      <Select value={value} disabled={disabled} onValueChange={(v) => onMove(p.identity, v === NONE ? null : v)} options={options} aria-label={`Room for ${p.name || "guest"}`} className="w-36" />
    </li>
  );
}
