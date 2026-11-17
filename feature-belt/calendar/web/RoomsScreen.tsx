"use client";

import {
  calendarAddDays,
  calendarDateKey,
  calendarDayStart,
  calendarMinutesOfDay,
  calendarTodayKey,
  errorMessage,
  orgsNoun,
  useActiveOrg,
  useCalendarRooms,
  useDebouncedValue,
  useDeleteCalendarRoom,
  useSaveCalendarRoom,
  type CalendarRoom,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, Input, SegmentedControl, Skeleton, SkeletonGroup, Switch, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { ChevronLeft, DoorOpen, Pencil, Plus, Projector, Share2, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { RoomShareDialog } from "./CalendarShareDialog";
import { useCalendarZone } from "./parts";

const LO = 7 * 60;
const HI = 20 * 60;
const pct = (m: number) => `${((Math.min(HI, Math.max(LO, m)) - LO) / (HI - LO)) * 100}%`;

/** /calendar/rooms: the workspace's rooms and resources with today's free/busy; people who manage rooms add and change them. */
export function RoomsScreen() {
  const { org } = useActiveOrg();
  const { zone } = useCalendarZone();
  const today = calendarTodayKey(zone);
  const [q, setQ] = useState("");
  const term = useDebouncedValue(q, 250);
  const rooms = useCalendarRooms(org ? { org_id: org.id, from: calendarDayStart(today, zone), to: calendarDayStart(calendarAddDays(today, 1), zone), q: term } : null);
  const [editing, setEditing] = useState<CalendarRoom | "new" | null>(null);
  const [sharing, setSharing] = useState<CalendarRoom | null>(null);
  const [deleting, setDeleting] = useState<CalendarRoom | null>(null);
  const del = useDeleteCalendarRoom();
  const list = rooms.data?.items ?? [];

  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <Link href="/calendar" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
        <ChevronLeft className="size-4" aria-hidden />
        Calendar
      </Link>
      <div className="flex min-h-control-lg flex-wrap items-center gap-3">
        <h1 className="min-w-0 flex-1 truncate type-title text-fg">Rooms{org ? <span className="text-fg-muted"> · {org.name}</span> : null}</h1>
        {rooms.data?.can_create ? (
          <Button size="auto" leftIcon={<Plus />} onClick={() => setEditing("new")}>
            New room
          </Button>
        ) : null}
      </div>
      {!org ? (
        <Card>
          <EmptyState icon={<DoorOpen />} title={`Rooms belong to a ${orgsNoun()}`} info={`Join or pick a ${orgsNoun()} to see its rooms.`} />
        </Card>
      ) : (
        <>
          <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search rooms" aria-label="Search rooms" className="max-w-form" />
          {rooms.isPending ? (
            <SkeletonGroup label="Loading rooms" className="flex flex-col gap-3">
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
              <Skeleton className="h-16" />
            </SkeletonGroup>
          ) : rooms.isError ? (
            <Card>
              <ErrorState title="Couldn't load rooms" info={errorMessage(rooms.error)} onRetry={() => void rooms.refetch()} retrying={rooms.isFetching} />
            </Card>
          ) : list.length === 0 ? (
            <Card>
              <EmptyState
                icon={<DoorOpen />}
                title={term ? `No rooms match “${term}”` : "No rooms yet"}
                info={rooms.data?.can_create ? "Add the rooms and shared equipment people book with their events." : `People who manage rooms in ${org.name} add them here.`}
              />
            </Card>
          ) : (
            <Card className="p-0">
              <ul className="flex flex-col divide-y divide-border">
                {list.map((r) => {
                  const Icon = r.kind === "resource" ? Projector : DoorOpen;
                  return (
                    <li key={r.id} className="flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
                      <div className="flex min-w-0 flex-1 items-start gap-3">
                        <Icon className="mt-0.5 size-5 shrink-0 text-fg-muted" aria-hidden />
                        <div className="flex min-w-0 flex-col">
                          <span className="flex items-center gap-2 truncate type-body font-semibold text-fg">
                            <span className="truncate">{r.name}</span>
                            {!r.active ? <Badge tone="neutral">Off</Badge> : null}
                          </span>
                          <span className="truncate type-caption text-fg-muted">
                            {[r.building, r.floor ? `Floor ${r.floor}` : "", r.capacity ? `${r.capacity} people` : "", ...r.features].filter(Boolean).join(" · ") || (r.kind === "resource" ? "Resource" : "Room")}
                          </span>
                        </div>
                      </div>
                      <div className="flex items-center gap-3 sm:w-80">
                        <div className="relative h-6 flex-1 overflow-hidden rounded-sm bg-surface ring-1 ring-border" role="img" aria-label={`Today: ${r.busy.length === 0 ? "free" : `${r.busy.length} ${r.busy.length === 1 ? "booking" : "bookings"}`}`}>
                          {r.busy.map((b, i) => {
                            const s = calendarDateKey(b.starts_at, zone) < today ? 0 : calendarMinutesOfDay(b.starts_at, zone);
                            const e = calendarDateKey(b.ends_at, zone) > today ? 1440 : calendarMinutesOfDay(b.ends_at, zone);
                            return <div key={i} className="absolute inset-y-1 rounded-xs bg-fg-muted" style={{ left: pct(s), width: `calc(${pct(e)} - ${pct(s)})` }} />;
                          })}
                        </div>
                        <Badge tone={r.free ? "success" : "warning"}>{r.free ? "Free today" : "Booked"}</Badge>
                        <ResourceMenu
                          feature="calendar"
                          title={r.name}
                          label={`Options for ${r.name}`}
                          actions={
                            r.can_manage
                              ? [
                                  { id: "edit", label: "Edit", icon: Pencil, onSelect: () => setEditing(r) },
                                  { id: "share", label: "Who can book…", icon: Share2, onSelect: () => setSharing(r) },
                                  { id: "delete", label: "Delete", icon: Trash2, destructive: true, onSelect: () => setDeleting(r) },
                                ]
                              : []
                          }
                        />
                      </div>
                    </li>
                  );
                })}
              </ul>
            </Card>
          )}
          <p className="type-caption text-fg-muted">Today, 7 AM to 8 PM. Book a room from an event&apos;s Rooms and resources.</p>
        </>
      )}
      {editing && org ? <RoomDialog room={editing === "new" ? null : editing} orgId={org.id} onClose={() => setEditing(null)} /> : null}
      {sharing ? <RoomShareDialog room={sharing} open onOpenChange={(o) => !o && setSharing(null)} /> : null}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.name ?? ""}”?`}
        tone="danger"
        confirmLabel="Delete room"
        onConfirm={async () => {
          if (!deleting) return;
          await del.mutateAsync(deleting.id);
          toast.success("Room deleted");
          setDeleting(null);
        }}
      >
        <p className="type-body text-fg-muted">Events that booked it lose it; the events stay.</p>
      </ConfirmDialog>
    </div>
  );
}

function RoomDialog({ room, orgId, onClose }: { room: CalendarRoom | null; orgId: string; onClose: () => void }) {
  const save = useSaveCalendarRoom();
  const [kind, setKind] = useState<"room" | "resource">(room?.kind ?? "room");
  const [name, setName] = useState(room?.name ?? "");
  const [building, setBuilding] = useState(room?.building ?? "");
  const [floor, setFloor] = useState(room?.floor ?? "");
  const [capacity, setCapacity] = useState(room?.capacity ? String(room.capacity) : "");
  const [features, setFeatures] = useState((room?.features ?? []).join(", "));
  const [description, setDescription] = useState(room?.description ?? "");
  const [active, setActive] = useState(room?.active ?? true);
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    if (!name.trim()) {
      setError("Name the room.");
      return;
    }
    const body = {
      kind,
      name: name.trim(),
      building,
      floor,
      capacity: capacity ? Math.max(1, Number(capacity) || 1) : 0,
      features: features.split(",").map((f) => f.trim()).filter(Boolean),
      description,
      active,
    };
    try {
      await save.mutateAsync(room ? { id: room.id, body } : { body: { ...body, org_id: orgId } });
      toast.success(room ? "Room saved" : "Room added");
      onClose();
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't save the room."));
    }
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={room ? "Edit room" : "New room"}
      size="sm"
      footer={
        <div className="flex w-full justify-end gap-2">
          <Button variant="ghost" size="auto" onClick={onClose}>
            Cancel
          </Button>
          <Button size="auto" loading={save.isPending} onClick={() => void submit()}>
            {room ? "Save" : "Add room"}
          </Button>
        </div>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <SegmentedControl
          aria-label="Kind"
          value={kind}
          onValueChange={setKind}
          options={[
            { value: "room", label: "Room" },
            { value: "resource", label: "Resource" },
          ]}
        />
        <Field label="Name" error={error ?? undefined} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} autoFocus placeholder={kind === "room" ? "Board room" : "Projector"} />
        </Field>
        <div className={cn("grid gap-4", kind === "room" ? "sm:grid-cols-3" : "sm:grid-cols-2")}>
          <Field label="Building">
            <Input value={building} onChange={(e) => setBuilding(e.target.value)} maxLength={100} />
          </Field>
          <Field label="Floor">
            <Input value={floor} onChange={(e) => setFloor(e.target.value)} maxLength={40} />
          </Field>
          {kind === "room" ? (
            <Field label="Seats">
              <Input type="number" min={1} max={10000} value={capacity} onChange={(e) => setCapacity(e.target.value)} />
            </Field>
          ) : null}
        </div>
        <Field label="Features" info="Comma-separated: TV, whiteboard, video conferencing.">
          <Input value={features} onChange={(e) => setFeatures(e.target.value)} maxLength={400} />
        </Field>
        <Field label="Notes">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={1000} minRows={2} />
        </Field>
        <Switch checked={active} onCheckedChange={setActive} label="Bookable" />
      </form>
    </Dialog>
  );
}
