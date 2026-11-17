"use client";

import {
  errorMessage,
  formatCalendarRange,
  useCalendarBookings,
  useCalendarLink,
  useCancelCalendarBooking,
  useReassignCalendarBooking,
  type CalendarBooking,
  type CalendarBookingWhen,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, Tab, Tabs, TabsList, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, ChevronLeft, ListChecks, Mail, MapPin, Shuffle, Users, Video } from "lucide-react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import Link from "next/link";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { useCalendarZone } from "./parts";

const ASSIGNMENT_LABEL = { round_robin: "Round robin", collective: "Collective", reassigned: "Handed over" } as const;

/** Hand a round-robin booking to another host of its link (free then; the server checks). */
function ReassignDialog({ b, onClose }: { b: CalendarBooking; onClose: () => void }) {
  const link = useCalendarLink(b.link_id ?? undefined);
  const reassign = useReassignCalendarBooking();
  const hosts = (link.data?.hosts ?? []).filter((h) => h.active && h.user.id !== b.host.id);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title="Hand to another host" info="They must be free then. The booker gets an updated invitation; both hosts are told." size="sm">
      {link.isPending ? (
        <Skeleton className="h-24" />
      ) : link.isError ? (
        <ErrorState compact title="Couldn't load the hosts" info={errorMessage(link.error)} onRetry={() => void link.refetch()} />
      ) : hosts.length === 0 ? (
        <EmptyState compact title="No other hosts" info="Add hosts to the link first." />
      ) : (
        <ul className="flex flex-col divide-y divide-border">
          {hosts.map((h) => (
            <li key={h.user.id} className="flex min-h-12 items-center gap-3 py-2">
              <AppAvatar name={h.user.display_name} src={h.user.avatar_url} size="sm" />
              <span className="min-w-0 flex-1 truncate type-body text-fg">{h.user.display_name}</span>
              <Button
                size="sm"
                variant="secondary"
                loading={reassign.isPending && reassign.variables?.userId === h.user.id}
                onClick={() =>
                  reassign.mutate(
                    { id: b.id, userId: h.user.id },
                    { onSuccess: () => { toast.success(`Handed to ${h.user.display_name}`); onClose(); }, onError: (e) => toast.error(errorMessage(e, "Couldn't hand it over.")) },
                  )
                }
              >
                Hand over
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

function BookingCard({ b, zone, onCancel }: { b: CalendarBooking; zone: string; onCancel: (b: CalendarBooking) => void }) {
  const [open, setOpen] = useState(false);
  const [reassigning, setReassigning] = useState(false);
  const range = formatCalendarRange({ starts_at: b.starts_at, ends_at: b.ends_at, all_day: false, start_date: null, end_date: null }, zone);
  return (
    <Card padding="compact" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate type-title-sm text-fg">{b.role === "host" ? b.booker_name : `With ${b.host.display_name}`}</span>
          <span className="type-label text-fg-muted">
            {b.title}
            {b.role === "host" && b.assignment ? ` · hosted by ${b.host.display_name}` : ""}
          </span>
        </div>
        {b.assignment && b.status === "confirmed" ? <Badge tone="info">{ASSIGNMENT_LABEL[b.assignment]}</Badge> : null}
        {b.status === "cancelled" ? <Badge tone="danger">{b.cancelled_by === "host" ? "You cancelled" : b.cancelled_by === "booker" ? (b.role === "host" ? "They cancelled" : "You cancelled") : "Cancelled"}</Badge> : null}
      </div>
      <div className="flex flex-col gap-1 type-label text-fg">
        <span className="flex items-center gap-2">
          <CalendarClock className="size-4 shrink-0 text-fg-muted" aria-hidden />
          {range}
        </span>
        {b.conference_url ? (
          <span className="flex items-center gap-2">
            <Video className="size-4 shrink-0 text-fg-muted" aria-hidden />
            <a href={b.conference_url} target="_blank" rel="noopener noreferrer" className="truncate text-primary-strong underline underline-offset-2">
              Video call
            </a>
          </span>
        ) : b.location_value ? (
          <span className="flex items-center gap-2">
            <MapPin className="size-4 shrink-0 text-fg-muted" aria-hidden />
            <span className="truncate">{b.location_value}</span>
          </span>
        ) : null}
        {b.role === "host" && b.booker_email ? (
          <span className="flex items-center gap-2">
            <Mail className="size-4 shrink-0 text-fg-muted" aria-hidden />
            <a href={`mailto:${b.booker_email}`} className="truncate hover:underline">
              {b.booker_email}
            </a>
          </span>
        ) : null}
      </div>
      {b.guests.length > 0 ? (
        <span className="flex items-center gap-2 type-label text-fg">
          <Users className="size-4 shrink-0 text-fg-muted" aria-hidden />
          <span className="min-w-0 break-words">Guests: {b.guests.join(", ")}</span>
        </span>
      ) : null}
      {b.status === "cancelled" && b.cancel_reason ? <p className="type-label text-fg-muted">“{b.cancel_reason}”</p> : null}
      {open && b.answers.length > 0 ? (
        <dl className="flex flex-col gap-2 rounded-md bg-sunken p-3">
          {b.answers.map((a) => (
            <div key={a.question_id} className="flex flex-col gap-0.5">
              <dt className="type-caption text-fg-muted">{a.label}</dt>
              <dd className="whitespace-pre-wrap break-words type-label text-fg">{a.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
      <div className="flex flex-wrap gap-2">
        {b.answers.length > 0 ? (
          <Button variant="ghost" size="sm" onClick={() => setOpen((v) => !v)}>
            {open ? "Hide answers" : `Answers (${b.answers.length})`}
          </Button>
        ) : null}
        {b.event_id && b.status === "confirmed" ? (
          <Button asChild variant="ghost" size="sm">
            <Link href={`/calendar/event/${b.event_id}`}>Open event</Link>
          </Button>
        ) : null}
        {b.role === "host" && b.link_kind === "round_robin" && b.status === "confirmed" && new Date(b.starts_at).getTime() > Date.now() ? (
          <Button variant="ghost" size="sm" leftIcon={<Shuffle />} onClick={() => setReassigning(true)}>
            Hand over
          </Button>
        ) : null}
        {b.status === "confirmed" && new Date(b.ends_at).getTime() > Date.now() ? (
          <Button variant="danger-ghost" size="sm" onClick={() => onCancel(b)}>
            Cancel booking
          </Button>
        ) : null}
      </div>
      {reassigning ? <ReassignDialog b={b} onClose={() => setReassigning(false)} /> : null}
    </Card>
  );
}

/** /calendar/bookings: bookings on your links (or that you made), upcoming, past or cancelled. Tabs live in the URL. */
export function BookingsScreen() {
  const { zone } = useCalendarZone();
  const [when, setWhen] = useUrlState<CalendarBookingWhen>("when", "upcoming");
  const [role, setRole] = useUrlState<"host" | "booker">("role", "host");
  const list = useCalendarBookings(role, when);
  const cancel = useCancelCalendarBooking();
  const [cancelling, setCancelling] = useState<CalendarBooking | null>(null);
  const [reason, setReason] = useState("");
  const rows = list.data?.items ?? [];
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <div className="flex flex-col gap-2">
        <Link href="/calendar" className="flex min-h-control items-center gap-1 self-start rounded-sm pr-2 type-label font-semibold text-fg-muted hover:text-fg pointer-coarse:min-h-tap">
          <ChevronLeft className="size-4" aria-hidden />
          Calendar
        </Link>
        <PageHeader title="Bookings" />
      </div>
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <Tabs value={when} onValueChange={(v) => setWhen(v as CalendarBookingWhen)}>
            <TabsList aria-label="When">
              <Tab value="upcoming">Upcoming</Tab>
              <Tab value="past">Past</Tab>
              <Tab value="cancelled">Cancelled</Tab>
            </TabsList>
          </Tabs>
          <SegmentedControl
            fullWidth={false}
            className="self-start"
            aria-label="Whose bookings"
            value={role}
            onValueChange={(v) => setRole(v)}
            options={[
              { value: "host", label: "On my links" },
              { value: "booker", label: "I booked" },
            ]}
          />
        </div>
        {list.isPending ? (
          <SkeletonGroup label="Loading bookings" className="flex flex-col gap-4">
            {[0, 1, 2].map((i) => (
              <Skeleton key={i} className="h-28" />
            ))}
          </SkeletonGroup>
        ) : list.isError ? (
          <Card>
            <ErrorState compact title="Couldn't load bookings" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
          </Card>
        ) : rows.length === 0 ? (
          <Card>
            <EmptyState
              compact
              icon={<ListChecks />}
              title={when === "upcoming" ? "No upcoming bookings" : when === "past" ? "No past bookings" : "No cancelled bookings"}
              action={
                role === "host" && when === "upcoming" ? (
                  <Button asChild variant="secondary" size="auto">
                    <Link href="/calendar/links">Share a booking link</Link>
                  </Button>
                ) : undefined
              }
            />
          </Card>
        ) : (
          <>
            <ul className="grid gap-4 md:grid-cols-2" aria-label="Bookings">
              {rows.map((b) => (
                <li key={b.id}>
                  <BookingCard b={b} zone={zone} onCancel={setCancelling} />
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
          </>
        )}
      </div>
      <ConfirmDialog
        open={!!cancelling}
        onOpenChange={(o) => {
          if (!o) {
            setCancelling(null);
            setReason("");
          }
        }}
        title="Cancel this booking?"
        tone="danger"
        confirmLabel="Cancel booking"
        cancelLabel="Keep it"
        onConfirm={async () => {
          if (!cancelling) return;
          await cancel.mutateAsync({ id: cancelling.id, reason });
          toast.success("Booking cancelled");
          setCancelling(null);
          setReason("");
        }}
        errorText={(e) => errorMessage(e, "Couldn't cancel. Try again.")}
      >
        <div className="flex flex-col gap-3">
          <p className="type-body text-fg">
            {cancelling?.role === "host"
              ? `${cancelling.booker_name} gets a cancellation email with your note and a link to book another time.`
              : `${cancelling?.host.display_name ?? "The host"} is told, and the time frees up.`}
          </p>
          <TextArea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Note (optional)" maxLength={500} aria-label="Note" />
        </div>
      </ConfirmDialog>
    </div>
  );
}
