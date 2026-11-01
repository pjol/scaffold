"use client";

import {
  calendarDeviceZone,
  calendarEndpoints,
  calendarStartOfMonth,
  calendarTodayKey,
  errorMessage,
  formatCalendarRange,
  isApiError,
  useCalendarGuestInvitation,
  useCalendarManagedBooking,
  useCalendarManageSlots,
  useCancelManagedCalendarBooking,
  useEndpoints,
  useRescheduleCalendarBooking,
  useRespondCalendarGuest,
  type CalendarResponse,
  type CalendarSlot,
} from "@scaffoldapp/shared";
import { Banner, Button, Card, ConfirmDialog, EmptyState, ErrorState, SegmentedControl, Skeleton, SkeletonGroup, TextArea, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, CalendarX, Clock, MapPin, Repeat, UserRound, Video } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useMemo, useState, type ReactNode } from "react";
import { AddToCalendar } from "./PublicBookingScreen";
import { SlotPicker, monthWindow } from "./SlotPicker";

function Row({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-start gap-3 type-body text-fg">
      <span className="flex size-6 shrink-0 items-center justify-center text-fg-muted [&_svg]:size-5" aria-hidden>
        {icon}
      </span>
      <div className="min-w-0 flex-1 break-words">{children}</div>
    </div>
  );
}

const frame = (children: ReactNode) => <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">{children}</div>;

/** /book/manage/<token>: the booker's own page to reschedule or cancel, within the host's rules. */
export function ManageBookingScreen({ token }: { token: string }) {
  const booking = useCalendarManagedBooking(token);
  const api = useEndpoints(calendarEndpoints);
  const [zone, setZone] = useState(calendarDeviceZone);
  const [moving, setMoving] = useState(false);
  const [month, setMonth] = useState(() => calendarStartOfMonth(calendarTodayKey(calendarDeviceZone())));
  const [slot, setSlot] = useState<CalendarSlot | null>(null);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [reason, setReason] = useState("");
  const win = useMemo(() => monthWindow(month, zone, 0), [month, zone]);
  const slots = useCalendarManageSlots(token, win.from, win.to, moving);
  const reschedule = useRescheduleCalendarBooking(token);
  const cancel = useCancelManagedCalendarBooking(token);

  if (booking.isPending) {
    return frame(
      <SkeletonGroup label="Loading booking" className="flex max-w-form flex-col gap-4">
        <Skeleton shape="text" className="h-8 w-2/3" />
        <Skeleton className="h-32" />
      </SkeletonGroup>,
    );
  }
  if (booking.isError) {
    return frame(
      <Card className="max-w-form">
        {isApiError(booking.error) && booking.error.status === 404 ? (
          <EmptyState icon={<CalendarX />} title="This booking link doesn't work any more" />
        ) : (
          <ErrorState title="Couldn't load this booking" info={booking.error.message} onRetry={() => void booking.refetch()} retrying={booking.isFetching} />
        )}
      </Card>,
    );
  }
  const b = booking.data;
  const cancelled = b.status === "cancelled";
  const past = new Date(b.ends_at).getTime() < Date.now();
  const range = (z: string) => formatCalendarRange({ starts_at: b.starts_at, ends_at: b.ends_at, all_day: false, start_date: null, end_date: null }, z);

  return frame(
    <div className="flex flex-col gap-section">
      <Card className="max-w-form">
        <div className="flex flex-col gap-6">
          <h1 className="break-words type-title text-fg">
            {b.title} with {b.host.display_name}
          </h1>
          {cancelled ? (
            <Banner tone="danger">
              {b.cancelled_by === "booker" ? "You cancelled this booking." : `${b.host.display_name} cancelled this booking.`}
              {b.cancel_reason && b.cancelled_by !== "booker" ? ` ${b.cancel_reason}` : ""}
            </Banner>
          ) : past ? (
            <Banner tone="neutral">This meeting has ended.</Banner>
          ) : null}
          <div className="flex flex-col gap-3">
            <Row icon={<Clock />}>{range(zone)}</Row>
            <Row icon={b.location_type === "video" ? <Video /> : <MapPin />}>
              {b.conference_url && !cancelled ? (
                <a href={b.conference_url} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                  Join video call
                </a>
              ) : (
                b.location_label
              )}
            </Row>
            <Row icon={<UserRound />}>
              Booked by {b.booker_name} ({b.booker_email})
            </Row>
          </div>
          {!cancelled && !past ? (
            <AddToCalendar icsUrl={api.manage.icsUrl(token)} title={`${b.title} with ${b.host.display_name}`} start={b.starts_at} end={b.ends_at} location={b.conference_url ?? b.location_label} />
          ) : null}
          <div className="flex flex-wrap gap-2">
            {b.can_reschedule && !moving ? (
              <Button size="auto" variant="secondary" leftIcon={<CalendarClock />} onClick={() => setMoving(true)}>
                Reschedule
              </Button>
            ) : null}
            {b.can_cancel ? (
              <Button size="auto" variant="danger-ghost" onClick={() => setCancelOpen(true)}>
                Cancel booking
              </Button>
            ) : null}
            {cancelled && b.link_slug ? (
              <Button asChild size="auto">
                <Link href={`/book/${b.link_slug}`}>Book another time</Link>
              </Button>
            ) : null}
          </div>
          {!cancelled && !past && !b.can_cancel && !b.can_reschedule ? (
            <p className="type-label text-fg-muted">Changes close {b.manage_cutoff >= 60 ? `${Math.round(b.manage_cutoff / 60)} hours` : `${b.manage_cutoff} minutes`} before the start. Reply to your confirmation email to reach {b.host.display_name}.</p>
          ) : null}
        </div>
      </Card>

      {moving ? (
        <Card>
          <div className="flex flex-col gap-4">
            <h2 className="type-title-sm text-fg">Pick a new time</h2>
            <SlotPicker
              zone={zone}
              onZoneChange={setZone}
              month={month}
              onMonthChange={setMonth}
              slots={slots.data?.slots ?? []}
              loading={slots.isFetching}
              error={slots.isError ? errorMessage(slots.error) : null}
              onRetry={() => void slots.refetch()}
              selected={slot?.starts_at ?? null}
              onSelect={setSlot}
              actionLabel={reschedule.isPending ? "Moving…" : "Move booking"}
              onConfirm={() => {
                if (!slot) return;
                reschedule.mutate(
                  { starts_at: slot.starts_at, timezone: zone },
                  {
                    onSuccess: () => {
                      toast.success("Booking moved. A new invite is on its way.");
                      setMoving(false);
                      setSlot(null);
                    },
                    onError: (e) => {
                      toast.error(errorMessage(e, "Couldn't move the booking."));
                      void slots.refetch();
                    },
                  },
                );
              }}
            />
            <Button variant="ghost" size="auto" className="self-start" onClick={() => setMoving(false)}>
              Keep the current time
            </Button>
          </div>
        </Card>
      ) : null}

      <ConfirmDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        title="Cancel this booking?"
        tone="danger"
        confirmLabel="Cancel booking"
        cancelLabel="Keep it"
        onConfirm={async () => {
          await cancel.mutateAsync(reason);
          toast.success("Booking cancelled");
        }}
        errorText={(e) => errorMessage(e, "Couldn't cancel. Try again.")}
      >
        <div className="flex flex-col gap-3">
          <p className="type-body text-fg">{b.host.display_name} is told, and the time frees up.</p>
          <TextArea value={reason} onChange={(e) => setReason(e.target.value)} placeholder={`Note to ${b.host.display_name} (optional)`} maxLength={500} aria-label="Note" />
        </div>
      </ConfirmDialog>
    </div>,
  );
}

/** /calendar/rsvp/<token>: an emailed guest replies. A reply in the link is preselected and confirmed with a tap (link scanners never answer for you). */
export function GuestRsvpScreen({ token }: { token: string }) {
  const params = useSearchParams();
  const fromLink = params.get("response");
  const invite = useCalendarGuestInvitation(token);
  const respond = useRespondCalendarGuest(token);
  const [choice, setChoice] = useState<Exclude<CalendarResponse, "needs_action"> | undefined>(
    fromLink === "accepted" || fromLink === "tentative" || fromLink === "declined" ? fromLink : undefined,
  );
  const zone = calendarDeviceZone();

  if (invite.isPending) {
    return frame(
      <SkeletonGroup label="Loading invitation" className="flex max-w-form flex-col gap-4">
        <Skeleton shape="text" className="h-8 w-2/3" />
        <Skeleton className="h-32" />
      </SkeletonGroup>,
    );
  }
  if (invite.isError) {
    return frame(
      <Card className="max-w-form">
        {isApiError(invite.error) && invite.error.status === 404 ? (
          <EmptyState icon={<CalendarX />} title="This invitation isn't available any more" />
        ) : (
          <ErrorState title="Couldn't load the invitation" info={invite.error.message} onRetry={() => void invite.refetch()} retrying={invite.isFetching} />
        )}
      </Card>,
    );
  }
  const g = invite.data;
  const answered = g.response !== "needs_action" ? g.response : null;
  return frame(
    <Card className="max-w-form">
      <div className="flex flex-col gap-6">
        <h1 className="break-words type-title text-fg">{g.title}</h1>
        <div className="flex flex-col gap-3">
          <Row icon={<Clock />}>{formatCalendarRange(g, zone)}</Row>
          {g.repeats ? <Row icon={<Repeat />}>{g.repeats}</Row> : null}
          {g.location ? <Row icon={<MapPin />}>{g.location}</Row> : null}
          {g.conference_url ? (
            <Row icon={<Video />}>
              <a href={g.conference_url} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                Join video call
              </a>
            </Row>
          ) : null}
          <Row icon={<UserRound />}>Invited by {g.organizer}</Row>
        </div>
        {g.description ? <p className="whitespace-pre-wrap break-words type-body text-fg">{g.description}</p> : null}
        <div className="flex flex-col gap-3">
          <span className="type-label font-semibold text-fg">{answered ? "Your reply" : `Going${g.name ? `, ${g.name}` : ""}?`}</span>
          <SegmentedControl
            aria-label="Your reply"
            value={choice ?? answered ?? undefined}
            onValueChange={(v) => setChoice(v)}
            options={[
              { value: "accepted", label: "Yes" },
              { value: "tentative", label: "Maybe" },
              { value: "declined", label: "No" },
            ]}
          />
          {choice && choice !== answered ? (
            <Button
              size="auto"
              className="self-start"
              loading={respond.isPending}
              onClick={() =>
                respond.mutate(choice, {
                  onSuccess: () => toast.success("Reply sent"),
                  onError: (e) => toast.error(errorMessage(e, "Couldn't send your reply. Try again.")),
                })
              }
            >
              Send reply
            </Button>
          ) : answered ? (
            <p className="type-label text-fg-muted">{g.organizer} can see your reply.</p>
          ) : null}
        </div>
      </div>
    </Card>,
  );
}
