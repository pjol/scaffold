"use client";

import {
  calendarDateKey,
  calendarEndpoints,
  describeCalendarRecurrence,
  errorMessage,
  formatCalendarRange,
  isApiError,
  useCalendarEvent,
  useDeleteCalendarEvent,
  useEndpoints,
  useRespondCalendarEvent,
  type CalendarEventDetail,
  type CalendarResponse,
  type CalendarScope,
} from "@scaffoldapp/shared";
import { Button, Card, ConfirmDialog, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, CalendarX, ChevronLeft, Copy, Download, Link2, MapPin, Pencil, Repeat, Trash2, UserRound, Users, Video } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { EventDialog, ScopeDialog } from "./EventDialog";
import { ColorDot, ResponseBadge, copyText, downloadText, useCalendarZone } from "./parts";

function Row({ icon, children, label }: { icon: ReactNode; children: ReactNode; label: string }) {
  return (
    <div className="flex items-start gap-3 type-body text-fg">
      <span className="flex size-6 shrink-0 items-center justify-center text-fg-muted [&_svg]:size-5" aria-label={label} role="img">
        {icon}
      </span>
      <div className="min-w-0 flex-1 break-words">{children}</div>
    </div>
  );
}

const isURL = (v: string) => /^https?:\/\/\S+$/.test(v);

/** One event (or occurrence) with guests, replies and the organizer's actions. */
export function EventDetailPanel({ id, occurrence, onEdit, onDeleted }: { id: string; occurrence: string | null; onEdit: (e: CalendarEventDetail) => void; onDeleted: () => void }) {
  const { zone } = useCalendarZone();
  const event = useCalendarEvent(id, occurrence);
  const respond = useRespondCalendarEvent();
  const remove = useDeleteCalendarEvent();
  const api = useEndpoints(calendarEndpoints);
  const router = useRouter();
  const [confirm, setConfirm] = useState<"delete" | "scope" | null>(null);
  const [reason, setReason] = useState("");

  if (event.isPending) {
    return (
      <SkeletonGroup label="Loading event" className="flex flex-col gap-4">
        <Skeleton shape="text" className="h-7 w-2/3" />
        <Skeleton shape="text" className="h-5 w-1/2" />
        <Skeleton shape="text" className="h-5 w-1/3" />
        <Skeleton className="h-24" />
      </SkeletonGroup>
    );
  }
  if (event.isError) {
    return isApiError(event.error) && event.error.status === 404 ? (
      <EmptyState compact icon={<CalendarX />} title="This event isn't on your calendar" />
    ) : (
      <ErrorState compact title="Couldn't load this event" info={event.error.message} onRetry={() => void event.refetch()} retrying={event.isFetching} />
    );
  }
  const e = event.data;
  const owner = e.role === "owner";
  const counts = e.attendees.reduce((acc, a) => ({ ...acc, [a.response]: (acc[a.response] ?? 0) + 1 }), {} as Partial<Record<CalendarResponse, number>>);
  const summary = [counts.accepted && `${counts.accepted} going`, counts.tentative && `${counts.tentative} maybe`, counts.declined && `${counts.declined} declined`, counts.needs_action && `${counts.needs_action} awaiting`]
    .filter(Boolean)
    .join(" · ");
  const otherZone = !e.all_day && e.timezone !== zone;

  const doDelete = async (scope?: CalendarScope) => {
    try {
      await remove.mutateAsync({ id: e.event_id, scope: scope ?? "all", occurrence: e.occurrence_start, reason });
      toast.success(e.booking_id ? "Booking cancelled" : "Event deleted");
      onDeleted();
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't delete the event. Try again."));
      throw err;
    }
  };

  const actions = [
    ...(owner ? [{ id: "edit", label: "Edit", icon: Pencil, onSelect: () => onEdit(e) }] : []),
    {
      id: "ics",
      label: "Download .ics",
      icon: Download,
      onSelect: () => {
        api
          .eventIcs(e.event_id)
          .then((text) => downloadText(text, `${e.title.replace(/[^\w -]+/g, "").trim() || "event"}.ics`))
          .catch((err: unknown) => toast.error(errorMessage(err, "Couldn't download the event.")));
      },
    },
    ...(e.conference_url ? [{ id: "copy-video", label: "Copy video link", icon: Copy, onSelect: () => void copyText(e.conference_url ?? "", toast) }] : []),
    ...(e.booking_id ? [{ id: "booking", label: "Open bookings", icon: Link2, onSelect: () => router.push("/calendar/bookings") }] : []),
    ...(owner
      ? [{ id: "delete", label: e.booking_id ? "Cancel booking" : "Delete", icon: Trash2, destructive: true, onSelect: () => setConfirm(e.recurring ? "scope" : "delete") }]
      : []),
  ];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start gap-3">
        <ColorDot color={e.color} className="mt-3" />
        <h2 className="min-w-0 flex-1 break-words type-title text-fg">{e.title}</h2>
        <ResourceMenu feature="calendar" kind="calendar.event" id={e.event_id} title={e.title} actions={actions} />
      </div>

      <div className="flex flex-col gap-3">
        <Row icon={<CalendarClock />} label="When">
          {formatCalendarRange(e, zone)}
          {otherZone ? <span className="block type-label text-fg-muted">{formatCalendarRange(e, e.timezone)} in {e.timezone.replace(/_/g, " ")}</span> : null}
        </Row>
        {e.recurrence ? (
          <Row icon={<Repeat />} label="Repeats">
            {describeCalendarRecurrence(e.recurrence)}
            {e.modified ? <span className="block type-label text-fg-muted">This occurrence was changed</span> : null}
          </Row>
        ) : null}
        {e.location ? (
          <Row icon={<MapPin />} label="Location">
            {isURL(e.location) ? (
              <a href={e.location} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                {e.location}
              </a>
            ) : (
              e.location
            )}
          </Row>
        ) : null}
        {e.conference_url ? (
          <Row icon={<Video />} label="Video call">
            <Button asChild size="sm" leftIcon={<Video />}>
              <a href={e.conference_url} target="_blank" rel="noopener noreferrer">
                Join video call
              </a>
            </Button>
          </Row>
        ) : null}
        <Row icon={<UserRound />} label="Organizer">
          {owner ? "You're the organizer" : `Organized by ${e.owner.display_name || "a deleted account"}`}
        </Row>
      </div>

      {e.description ? <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{e.description}</p> : null}

      {e.attendees.length > 0 ? (
        <section className="flex flex-col gap-3" aria-label="Guests">
          <div className="flex items-center gap-2 type-label text-fg-muted">
            <Users className="size-4" aria-hidden />
            <span>
              {e.attendees.length} {e.attendees.length === 1 ? "guest" : "guests"}
              {summary ? ` · ${summary}` : ""}
            </span>
          </div>
          <ul className="flex flex-col gap-2">
            {e.attendees.map((a) => (
              <li key={a.id} className="flex min-h-control items-center gap-3">
                <AppAvatar name={a.name || a.email} src={a.avatar_url} size="sm" />
                <span className="min-w-0 flex-1 truncate type-body text-fg">{a.name || a.email}</span>
                <ResponseBadge response={a.response} />
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {e.role === "attendee" ? (
        <div className="flex flex-col gap-2">
          <span className="type-label font-semibold text-fg">Going?{e.recurring ? " (all events in the series)" : ""}</span>
          <SegmentedControl
            aria-label="Your reply"
            value={e.my_response === "needs_action" || !e.my_response ? undefined : e.my_response}
            onValueChange={(v) =>
              respond.mutate(
                { id: e.event_id, response: v },
                { onSuccess: () => toast.success("Reply sent"), onError: (err) => toast.error(errorMessage(err, "Couldn't send your reply.")) },
              )
            }
            options={[
              { value: "accepted", label: "Yes" },
              { value: "tentative", label: "Maybe" },
              { value: "declined", label: "No" },
            ]}
          />
        </div>
      ) : owner ? (
        <Button variant="secondary" size="auto" leftIcon={<Pencil />} className="self-start" onClick={() => onEdit(e)}>
          Edit
        </Button>
      ) : null}

      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(o) => setConfirm(o ? "delete" : null)}
        title={e.booking_id ? "Cancel this booking?" : `Delete “${e.title}”?`}
        tone="danger"
        confirmLabel={e.booking_id ? "Cancel booking" : "Delete"}
        onConfirm={() => doDelete()}
        errorText={(err) => errorMessage(err, "Couldn't delete the event. Try again.")}
      >
        <div className="flex flex-col gap-3">
          <p className="type-body text-fg">
            {e.booking_id
              ? "The person who booked gets a cancellation email with your note and a link to book another time."
              : e.attendees.length > 0
                ? "Guests are told it's cancelled. It can't be undone."
                : "It can't be undone."}
          </p>
          {e.booking_id ? <TextArea value={reason} onChange={(ev) => setReason(ev.target.value)} placeholder="Note to the booker (optional)" maxLength={500} aria-label="Note to the booker" /> : null}
        </div>
      </ConfirmDialog>
      <ScopeDialog open={confirm === "scope"} onOpenChange={(o) => setConfirm(o ? "scope" : null)} title="Delete recurring event" confirmLabel="Delete" tone="danger" onConfirm={(s) => doDelete(s)} />
    </div>
  );
}

/** /calendar/event/<id>: an event on its own page (notification and email links land here). */
export function EventPageScreen({ id, occurrence }: { id: string; occurrence: string | null }) {
  const router = useRouter();
  const { zone } = useCalendarZone();
  const event = useCalendarEvent(id, occurrence);
  const [editing, setEditing] = useState<CalendarEventDetail | null>(null);
  const date = event.data ? calendarDateKey(event.data.starts_at, zone) : null;
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-6 px-gutter py-section">
      <Link
        href={date ? `/calendar?view=day&date=${date}` : "/calendar"}
        className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap"
      >
        <ChevronLeft className="size-4" aria-hidden />
        Calendar
      </Link>
      <Card className="max-w-form">
        <EventDetailPanel id={id} occurrence={occurrence} onEdit={setEditing} onDeleted={() => router.push("/calendar")} />
      </Card>
      {editing ? <EventDialog target={{ mode: "edit", event: editing }} zone={zone} onClose={() => setEditing(null)} /> : null}
    </div>
  );
}
