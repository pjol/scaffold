"use client";

import {
  errorMessage,
  formatEventRange,
  isApiError,
  useCancelEvent,
  useClearRsvp,
  useDisableInviteLink,
  useEvent,
  useEventAttendees,
  useEventIcs,
  useEventTopic,
  useMe,
  useRemoveCoHost,
  useRotateInviteLink,
  useRsvp,
  type EventItem,
  type RsvpChoice,
  type RsvpStatus,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  IconButton,
  InfoTip,
  ListRow,
  Menu,
  MenuItem,
  MenuSeparator,
  Sheet,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  Tab,
  TabPanel,
  Tabs,
  TabsList,
  toast,
} from "@scaffoldapp/ui-web";
import { Ban, CalendarClock, CalendarPlus, CalendarX, Check, Copy, Ellipsis, Globe, Link2, Lock, MapPin, Pencil, RefreshCw, Unlink, Users, UsersRound, Video } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { CoverImage, downloadText, goingText, hasEnded, icsFileName, RSVP_LABEL, VISIBILITY_INFO, VISIBILITY_LABEL } from "./parts";

const CHOICES: { value: RsvpChoice; label: string }[] = [
  { value: "going", label: "Going" },
  { value: "maybe", label: "Maybe" },
  { value: "declined", label: "Can't go" },
];

function inviteUrl(code: string): string {
  return `${window.location.origin}/events/join?code=${encodeURIComponent(code)}`;
}

async function copy(text: string, done: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(done);
  } catch {
    toast.error("Couldn't copy. Select the link and copy it yourself.");
  }
}

function DetailSkeleton() {
  return (
    <SkeletonGroup label="Loading event" className="flex flex-col gap-section">
      <Skeleton className="aspect-video w-full" />
      <Skeleton shape="text" className="h-8 w-2/3" />
      <div className="flex flex-col gap-3">
        <Skeleton shape="text" className="h-5 w-1/2" />
        <Skeleton shape="text" className="h-5 w-1/3" />
      </div>
      <Skeleton className="h-24" />
    </SkeletonGroup>
  );
}

function InfoRow({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-control items-start gap-3 type-body text-fg">
      <span className="flex size-6 shrink-0 items-center justify-center text-fg-muted [&_svg]:size-5" aria-hidden>
        {icon}
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </div>
  );
}

/** Going / Maybe / Can't go, the waitlist state, and withdrawing. */
function RsvpPanel({ event }: { event: EventItem }) {
  const rsvp = useRsvp();
  const clear = useClearRsvp();
  const mine = event.my_rsvp?.status ?? null;
  const pending = rsvp.isPending ? rsvp.variables?.status : null;

  const choose = (status: RsvpChoice) => {
    if (status === mine) return;
    rsvp.mutate(
      { id: event.id, status },
      {
        onSuccess: (e) => {
          if (e.my_rsvp?.status === "waitlisted" && status === "going") toast.success(`It's full. You're number ${e.my_rsvp.waitlist_position ?? 1} on the waitlist.`);
          else toast.success(status === "going" ? "You're going" : status === "maybe" ? "Marked as maybe" : "Marked as can't go");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save your RSVP. Try again.")),
      },
    );
  };

  return (
    <Card className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <h2 className="type-title-sm text-fg">Are you going?</h2>
        {event.capacity ? <InfoTip content={`${event.capacity} spots. When it's full, Going puts you on the waitlist and you move up automatically when a spot opens.`} /> : null}
      </div>
      {mine === "waitlisted" ? (
        <Banner
          tone="warning"
          action={
            <Button variant="ghost" size="sm" loading={clear.isPending} onClick={() => clear.mutate(event.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't leave the waitlist. Try again.")) })}>
              Leave waitlist
            </Button>
          }
        >
          You're number {event.my_rsvp?.waitlist_position ?? 1} on the waitlist.
        </Banner>
      ) : null}
      <div role="radiogroup" aria-label="Your RSVP" className="grid grid-cols-1 gap-2 sm:grid-cols-3">
        {CHOICES.map((c) => {
          const selected = mine === c.value || (c.value === "going" && mine === "waitlisted");
          return (
            <Button
              key={c.value}
              role="radio"
              aria-checked={selected}
              variant={selected ? "primary" : "secondary"}
              size="auto"
              leftIcon={selected ? <Check /> : undefined}
              loading={pending === c.value}
              disabled={rsvp.isPending || clear.isPending}
              onClick={() => choose(c.value)}
            >
              {c.label}
            </Button>
          );
        })}
      </div>
      {mine && mine !== "waitlisted" ? (
        <Button
          variant="ghost"
          size="sm"
          className="self-start"
          loading={clear.isPending}
          onClick={() => clear.mutate(event.id, { onSuccess: () => toast.success("RSVP removed"), onError: (e) => toast.error(errorMessage(e, "Couldn't remove your RSVP. Try again.")) })}
        >
          Remove my RSVP
        </Button>
      ) : null}
    </Card>
  );
}

function AttendeeList({ eventId, status }: { eventId: string; status: RsvpStatus }) {
  const list = useEventAttendees(eventId, status);
  const rows = list.data?.items ?? [];
  if (list.isPending) {
    return (
      <SkeletonGroup label="Loading people" className="flex flex-col">
        <SkeletonRow />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    );
  }
  if (list.isError) return <ErrorState compact title="Couldn't load this list" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />;
  if (rows.length === 0) return <EmptyState compact icon={<Users />} title={status === "waitlisted" ? "No one waiting" : `No one marked ${RSVP_LABEL[status].toLowerCase()}`} />;
  return (
    <>
      <ul className="flex flex-col">
        {rows.map((a) => (
          <li key={a.user.id}>
            <ListRow
              title={a.user.display_name || "Unnamed"}
              leading={<AppAvatar name={a.user.display_name || "?"} src={a.user.avatar_url} size="sm" />}
              trailing={a.waitlist_position ? <Badge tone="neutral">#{a.waitlist_position}</Badge> : undefined}
            />
          </li>
        ))}
      </ul>
      <LoadMore hasNextPage={list.hasNextPage} isFetchingNextPage={list.isFetchingNextPage} fetchNextPage={list.fetchNextPage} />
    </>
  );
}

function AttendeesSheet({ event, open, onOpenChange }: { event: EventItem; open: boolean; onOpenChange: (o: boolean) => void }) {
  const [tab, setTab] = useState<RsvpStatus>("going");
  const tabs: { value: RsvpStatus; label: string; count?: number }[] = [
    { value: "going", label: "Going", count: event.going_count },
    { value: "maybe", label: "Maybe", count: event.maybe_count },
    ...(event.waitlist_count > 0 || event.capacity ? [{ value: "waitlisted" as const, label: "Waitlist", count: event.waitlist_count }] : []),
    ...(event.host_role ? [{ value: "declined" as const, label: "Can't go" }] : []),
  ];
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title="Who's coming" info="Only hosts and people who RSVP'd see this list.">
      <Tabs value={tab} onValueChange={(v) => setTab(v as RsvpStatus)}>
        <TabsList aria-label="RSVP">
          {tabs.map((t) => (
            <Tab key={t.value} value={t.value} {...(t.count !== undefined ? { count: t.count } : {})}>
              {t.label}
            </Tab>
          ))}
        </TabsList>
        {tabs.map((t) => (
          <TabPanel key={t.value} value={t.value} className="pt-3">
            {tab === t.value ? <AttendeeList eventId={event.id} status={t.value} /> : null}
          </TabPanel>
        ))}
      </Tabs>
    </Sheet>
  );
}

function HostsSection({ event, meId }: { event: EventItem; meId: string | undefined }) {
  const remove = useRemoveCoHost();
  const creator = event.host_role === "creator";
  return (
    <Section title="Hosts">
      <Card padding="none" className="p-1">
        <ul className="flex flex-col">
          <li>
            <ListRow title={event.host.display_name || "Deleted user"} leading={<AppAvatar name={event.host.display_name || "?"} src={event.host.avatar_url} size="sm" />} trailing={<Badge tone="neutral">Creator</Badge>} />
          </li>
          {event.co_hosts.map((h) => {
            const self = h.id === meId;
            const canRemove = creator || self;
            return (
              <li key={h.id}>
                <ListRow
                  title={h.display_name || "Unnamed"}
                  leading={<AppAvatar name={h.display_name || "?"} src={h.avatar_url} size="sm" />}
                  trailing={
                    canRemove ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={remove.isPending && remove.variables?.userId === h.id}
                        onClick={() =>
                          remove.mutate(
                            { id: event.id, userId: h.id },
                            {
                              onSuccess: () => toast.success(self ? "You're no longer a co-host" : `${h.display_name || "They"} removed as co-host`),
                              onError: (e) => toast.error(errorMessage(e, "Couldn't remove the co-host. Try again.")),
                            },
                          )
                        }
                      >
                        {self ? "Step down" : "Remove"}
                      </Button>
                    ) : (
                      <Badge tone="neutral">Co-host</Badge>
                    )
                  }
                />
              </li>
            );
          })}
        </ul>
      </Card>
    </Section>
  );
}

function HostMenu({ event, onCancel }: { event: EventItem; onCancel: () => void }) {
  const rotate = useRotateInviteLink();
  const disable = useDisableInviteLink();
  const creator = event.host_role === "creator";
  const live = event.status === "scheduled";

  const newLink = () =>
    rotate.mutate(event.id, {
      onSuccess: (link) => void copy(inviteUrl(link.code), "New invite link copied"),
      onError: (e) => toast.error(errorMessage(e, "Couldn't make an invite link. Try again.")),
    });

  return (
    <Menu align="end" trigger={<IconButton label="Event actions" icon={<Ellipsis />} variant="secondary" />}>
      {live ? (
        <MenuItem icon={<Pencil />} asChild>
          <Link href={`/events/${event.id}/edit`}>Edit</Link>
        </MenuItem>
      ) : null}
      {live && event.invite_code ? (
        <MenuItem icon={<Copy />} onSelect={() => void copy(inviteUrl(event.invite_code ?? ""), "Invite link copied")}>
          Copy invite link
        </MenuItem>
      ) : null}
      {live ? (
        <MenuItem icon={event.invite_code ? <RefreshCw /> : <Link2 />} onSelect={newLink} disabled={rotate.isPending}>
          {event.invite_code ? "New invite link" : "Create invite link"}
        </MenuItem>
      ) : null}
      {live && event.invite_code ? (
        <MenuItem
          icon={<Unlink />}
          onSelect={() =>
            disable.mutate(event.id, { onSuccess: () => toast.success("Invite link turned off"), onError: (e) => toast.error(errorMessage(e, "Couldn't turn off the link. Try again.")) })
          }
        >
          Turn off invite link
        </MenuItem>
      ) : null}
      {creator && live ? (
        <>
          <MenuSeparator />
          <MenuItem icon={<Ban />} destructive onSelect={onCancel}>
            Cancel event
          </MenuItem>
        </>
      ) : null}
    </Menu>
  );
}

/** One event: cover, time and place, RSVP, who's coming (hosts and attendees), host actions and calendar export. */
export function EventDetailScreen({ id }: { id: string }) {
  const me = useMe();
  const event = useEvent(id);
  useEventTopic(id);
  const ics = useEventIcs();
  const cancel = useCancelEvent();
  const [attendeesOpen, setAttendeesOpen] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);

  const wrap = (children: ReactNode) => <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">{children}</div>;

  if (event.isPending) return wrap(<DetailSkeleton />);
  if (event.isError) {
    const gone = isApiError(event.error) && event.error.status === 404;
    return wrap(
      gone ? (
        <Card>
          <EmptyState
            icon={<CalendarX />}
            title="This event isn't available"
            action={
              <Button asChild variant="secondary" size="auto">
                <Link href="/events">All events</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        <Card>
          <ErrorState title="Couldn't load this event" info={event.error.message} onRetry={() => void event.refetch()} retrying={event.isFetching} />
        </Card>
      ),
    );
  }

  const e = event.data;
  const ended = hasEnded(e);
  const cancelled = e.status === "cancelled";
  const seesAttendees = !!e.host_role || e.my_rsvp?.status === "going" || e.my_rsvp?.status === "maybe" || e.my_rsvp?.status === "waitlisted";

  const addToCalendar = () =>
    ics.mutate(e.id, {
      onSuccess: (text) => downloadText(text, icsFileName(e.title), "text/calendar"),
      onError: (err) => toast.error(errorMessage(err, "Couldn't create the calendar file. Try again.")),
    });

  return wrap(
    <>
      <div className="flex flex-col gap-4">
        <PageHeader title={e.title} action={e.host_role ? <HostMenu event={e} onCancel={() => setConfirmCancel(true)} /> : undefined} />
        {cancelled ? <Banner tone="danger">This event was cancelled.</Banner> : ended ? <Banner tone="neutral">This event has ended.</Banner> : null}
      </div>
      <div className="flex flex-col gap-section lg:grid lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start lg:gap-12">
        <div className="flex min-w-0 flex-col gap-section">
          {e.cover_url ? <CoverImage src={e.cover_url} alt="" priority /> : null}
          <div className="flex flex-col gap-2">
            <InfoRow icon={<CalendarClock />}>{formatEventRange(e.starts_at, e.ends_at, e.timezone)}</InfoRow>
            {e.location_text ? (
              <InfoRow icon={<MapPin />}>
                <span className="break-words">{e.location_text}</span>
              </InfoRow>
            ) : null}
            {e.online_url ? (
              <InfoRow icon={<Video />}>
                <a href={e.online_url} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                  {e.online_url}
                </a>
              </InfoRow>
            ) : null}
            <InfoRow icon={e.visibility === "invite" ? <Lock /> : e.visibility === "public" ? <Globe /> : <UsersRound />}>
              <span className="inline-flex items-center gap-2">
                {VISIBILITY_LABEL[e.visibility]}
                <InfoTip content={VISIBILITY_INFO[e.visibility]} />
              </span>
            </InfoRow>
          </div>
          {e.details ? <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{e.details}</p> : null}
          {!cancelled && !ended ? <RsvpPanel event={e} /> : null}
        </div>

        <div className="flex min-w-0 flex-col gap-section">
          <Button variant="secondary" size="auto" leftIcon={<CalendarPlus />} loading={ics.isPending} onClick={addToCalendar} className="self-start">
            Add to calendar
          </Button>
          <Section
            title="Who's coming"
            info={seesAttendees ? undefined : "Only hosts and people who RSVP'd see who's coming."}
            action={
              seesAttendees ? (
                <Button variant="ghost" size="sm" onClick={() => setAttendeesOpen(true)}>
                  See all
                </Button>
              ) : undefined
            }
          >
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="success">{goingText(e)}</Badge>
              {e.maybe_count > 0 ? <Badge tone="info">{e.maybe_count} maybe</Badge> : null}
              {e.waitlist_count > 0 ? <Badge tone="warning">{e.waitlist_count} on the waitlist</Badge> : null}
              {e.is_full && !cancelled ? <Badge tone="warning">Full</Badge> : null}
            </div>
          </Section>
          <HostsSection event={e} meId={me.data?.id} />
        </div>
      </div>

      {seesAttendees ? <AttendeesSheet event={e} open={attendeesOpen} onOpenChange={setAttendeesOpen} /> : null}
      <ConfirmDialog
        open={confirmCancel}
        onOpenChange={setConfirmCancel}
        title="Cancel this event?"
        tone="danger"
        confirmLabel="Cancel event"
        cancelLabel="Keep event"
        onConfirm={async () => {
          await cancel.mutateAsync(e.id);
          toast.success("Event cancelled. Everyone who RSVP'd was told.");
        }}
        errorText={(err) => errorMessage(err, "Couldn't cancel the event. Try again.")}
      >
        <p className="type-body text-fg">Everyone who RSVP'd gets a notice. This can't be undone.</p>
      </ConfirmDialog>
    </>,
  );
}
