"use client";

import { errorMessage, orgsNoun, useActiveOrg, useCalendarRooms, type CalendarEventRoom, type CalendarRoom } from "@scaffoldapp/shared";
import { Badge, Button, Chip, Dialog, EmptyState, ErrorState, Field, Input, Skeleton, cn } from "@scaffoldapp/ui-web";
import { DoorOpen, Plus, Projector, Users, X } from "lucide-react";
import { useState } from "react";

/** Rooms and resources an event books: chips, and Add room opens the list free at the event's time. */
export function RoomPicker({
  rooms,
  onChange,
  from,
  to,
  eventId,
  guests,
}: {
  rooms: CalendarEventRoom[];
  onChange: (rooms: CalendarEventRoom[]) => void;
  /** The event's time (first occurrence); null while the time is invalid. */
  from: string | null;
  to: string | null;
  eventId?: string;
  guests: number;
}) {
  const { org } = useActiveOrg();
  const [open, setOpen] = useState(false);
  if (!org) return null;
  return (
    <Field label="Rooms and resources" info={`Rooms of ${org.name}. Booking one blocks it for every occurrence; it's refused when someone has it then.`}>
      <div className="flex flex-wrap items-center gap-2">
        {rooms.map((r) => (
          <Chip key={r.id} icon={<X />} onClick={() => onChange(rooms.filter((x) => x.id !== r.id))} aria-label={`Remove ${r.name}`}>
            {r.name}
          </Chip>
        ))}
        <Button variant="ghost" size="sm" leftIcon={<Plus />} onClick={() => setOpen(true)} disabled={!from || !to}>
          Add room
        </Button>
      </div>
      {open && from && to ? (
        <RoomListDialog
          orgId={org.id}
          from={from}
          to={to}
          {...(eventId ? { eventId } : {})}
          guests={guests}
          picked={new Set(rooms.map((r) => r.id))}
          onPick={(r) => {
            onChange([...rooms, { id: r.id, name: r.name, kind: r.kind, building: r.building, capacity: r.capacity }]);
            setOpen(false);
          }}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </Field>
  );
}

function RoomListDialog({
  orgId,
  from,
  to,
  eventId,
  guests,
  picked,
  onPick,
  onClose,
}: {
  orgId: string;
  from: string;
  to: string;
  eventId?: string;
  guests: number;
  picked: Set<string>;
  onPick: (r: CalendarRoom) => void;
  onClose: () => void;
}) {
  const [q, setQ] = useState("");
  const [fits, setFits] = useState(guests > 1);
  const rooms = useCalendarRooms({ org_id: orgId, from, to, q, ...(fits && guests > 1 ? { capacity: guests } : {}), ...(eventId ? { exclude_event: eventId } : {}) });
  const list = (rooms.data?.items ?? []).filter((r) => !picked.has(r.id));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title="Add a room or resource" size="md">
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search rooms" aria-label="Search rooms" className="sm:flex-1" autoFocus />
          {guests > 1 ? (
            <Button variant={fits ? "secondary" : "ghost"} size="sm" leftIcon={<Users />} aria-pressed={fits} onClick={() => setFits((v) => !v)}>
              Fits {guests}
            </Button>
          ) : null}
        </div>
        {rooms.isPending ? (
          <div className="flex flex-col gap-2" aria-busy>
            <Skeleton className="h-12" />
            <Skeleton className="h-12" />
          </div>
        ) : rooms.isError ? (
          <ErrorState title="Couldn't load rooms" info={errorMessage(rooms.error)} onRetry={() => void rooms.refetch()} retrying={rooms.isFetching} />
        ) : list.length === 0 ? (
          <EmptyState title="No rooms" info={rooms.data?.can_create ? "Add rooms on the Rooms page." : `Your ${orgsNoun()} hasn't added rooms yet, or none match.`} />
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {list.map((r) => {
              const Icon = r.kind === "resource" ? Projector : DoorOpen;
              const usable = r.can_book && r.free !== false;
              return (
                <li key={r.id}>
                  <button
                    type="button"
                    disabled={!usable}
                    onClick={() => onPick(r)}
                    className={cn(
                      "flex min-h-12 w-full items-center gap-3 rounded-md px-2 py-2 text-left focus-visible:outline-2 focus-visible:outline-ring",
                      usable ? "hover:bg-sunken" : "cursor-not-allowed opacity-60",
                    )}
                  >
                    <Icon className="size-5 shrink-0 text-fg-muted" aria-hidden />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate type-body text-fg">{r.name}</span>
                      <span className="truncate type-caption text-fg-muted">
                        {[r.building, r.floor ? `Floor ${r.floor}` : "", r.capacity ? `${r.capacity} people` : "", ...r.features].filter(Boolean).join(" · ")}
                      </span>
                    </span>
                    {!r.can_book ? <Badge tone="neutral">Can't book</Badge> : r.free === false ? <Badge tone="danger">Busy</Badge> : <Badge tone="success">Free</Badge>}
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </Dialog>
  );
}
