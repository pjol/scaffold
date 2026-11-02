"use client";

import {
  CALLS_ACCESS_LABELS,
  callsHistoryTitle,
  errorMessage,
  formatCallsDuration,
  formatCallsWhen,
  parseCallsCode,
  useCallsHistory,
  useCallsLinks,
  useCallsPlatformMeetings,
  useCallsTimeZone,
  useCallsUpcoming,
  useCancelCallsMeeting,
  useCreateCallsMeeting,
  useActiveOrg,
  useMe,
  type CallsHistoryItem,
  type CallsMeeting,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, Input, Menu, MenuItem, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { CalendarClock, CalendarPlus, Copy, ExternalLink, Info, Link2, LogIn, Pencil, Phone, PhoneIncoming, PhoneMissed, Plus, Trash2, Video } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen, Section } from "@/components/shell/PageHeader";
import { copyMeetingLink, MeetingLinkDialog, ScheduleMeetingDialog, StartCallDialog } from "./MeetingDialogs";
import { PlatformMeetingRow } from "./PlatformMeetings";
import { callsScheduleAction, callsSourceLink } from "./seams";

/**
 * `/calls`: Start call (ring people, or start alone and share the link), New meeting (a link
 * to share, or a scheduled meeting), join by code, upcoming meetings, history and your links.
 */
export function CallsScreen() {
  const router = useRouter();
  const { org } = useActiveOrg();
  const create = useCreateCallsMeeting();
  const [calling, setCalling] = useState(false);
  const [scheduling, setScheduling] = useState(false);
  const [created, setCreated] = useState<CallsMeeting | null>(null);

  // The app's Schedule action (setCallsScheduleAction), else our own dialog.
  const schedule = () => {
    const action = callsScheduleAction();
    if (action) action({ navigate: (href) => router.push(href), orgId: org?.id ?? null });
    else setScheduling(true);
  };

  const newLink = () =>
    create.mutate(
      { access: "open", org_id: org?.id ?? null },
      {
        onSuccess: setCreated,
        onError: (e) => toast.error(errorMessage(e, "Couldn't create a meeting. Try again.")),
      },
    );

  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen
        title="Calls"
        action={
          <>
            <Button variant="secondary" leftIcon={<Phone />} onClick={() => setCalling(true)}>
              Start call
            </Button>
            <Menu
              trigger={
                <Button leftIcon={<Plus />} loading={create.isPending}>
                  New meeting
                </Button>
              }
            >
              <MenuItem icon={<Link2 />} onSelect={newLink}>
                Get a link to share
              </MenuItem>
              <MenuItem icon={<CalendarPlus />} onSelect={schedule}>
                Schedule a meeting
              </MenuItem>
            </Menu>
            <ResourceMenu feature="calls" title="Calls" />
          </>
        }
      >
        <JoinByCode />
        <UpcomingSection />
        <HistorySection />
        <LinksSection />
      </Screen>
      <StartCallDialog open={calling} onOpenChange={setCalling} />
      <ScheduleMeetingDialog open={scheduling} onOpenChange={setScheduling} />
      <MeetingLinkDialog meeting={created} onClose={() => setCreated(null)} />
    </div>
  );
}

function JoinByCode() {
  const router = useRouter();
  const [value, setValue] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const code = parseCallsCode(value);
    if (!code) return setProblem("Enter a code like abc-defg-hij, or paste the link.");
    router.push(`/meet/${code}`);
  };
  return (
    <form onSubmit={submit} className="flex max-w-form flex-col gap-2" aria-label="Join with a code">
      <div className="flex gap-2">
        <Input
          value={value}
          onChange={(e) => (setValue(e.target.value), setProblem(null))}
          placeholder="Enter a code or link"
          aria-label="Meeting code or link"
          aria-invalid={!!problem}
          wrapperClassName="flex-1"
          leadingIcon={<LogIn />}
        />
        <Button type="submit" variant="secondary" disabled={!value.trim()}>
          Join
        </Button>
      </div>
      {problem && <p className="type-label text-danger-strong">{problem}</p>}
    </form>
  );
}

function ListSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2" aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <SkeletonRow key={i} />
      ))}
    </div>
  );
}

function UpcomingSection() {
  const upcoming = useCallsUpcoming();
  // Meet, Teams and Zoom meetings on the person's calendars, in time order with the app's own.
  const platform = useCallsPlatformMeetings("upcoming");
  const external = platform.data?.items ?? [];
  const rows = [
    ...(upcoming.data?.items ?? []).map((m) => ({ at: m.starts_at ?? "", key: `app:${m.id}`, node: <UpcomingRow key={`app:${m.id}`} meeting={m} /> })),
    ...external.map((m) => ({ at: m.starts_at, key: `ext:${m.id}`, node: <PlatformMeetingRow key={`ext:${m.id}`} meeting={m} /> })),
  ].sort((a, b) => a.at.localeCompare(b.at));
  return (
    <Section title="Upcoming">
      {upcoming.isPending ? (
        <ListSkeleton rows={2} />
      ) : upcoming.isError ? (
        <ErrorState compact title="Couldn't load upcoming meetings" info={upcoming.error.message} onRetry={() => void upcoming.refetch()} retrying={upcoming.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<CalendarClock />} title="Nothing scheduled" />
      ) : (
        <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
          {rows.map((r) => r.node)}
          <li>
            <LoadMore hasNextPage={upcoming.hasNextPage} isFetchingNextPage={upcoming.isFetchingNextPage} fetchNextPage={upcoming.fetchNextPage} />
          </li>
        </ul>
      )}
    </Section>
  );
}

/**
 * Copy link, edit, cancel: the ⋮ actions on a meeting the viewer may host. A meeting another
 * feature made (`source`) is edited and cancelled there: "Open in …" when the app links it.
 */
function meetingActions(m: CallsMeeting, isHost: boolean, navigate: (href: string) => void, onEdit?: () => void, onCancel?: () => void) {
  const copy = { id: "copy", label: "Copy link", icon: Copy, onSelect: () => void copyMeetingLink(m.join_url) };
  if (m.source) {
    const link = callsSourceLink(m.source);
    return [copy, ...(link ? [{ id: "source", label: link.label, icon: ExternalLink, onSelect: () => navigate(link.href) }] : [])];
  }
  return [
    copy,
    ...(isHost && onEdit ? [{ id: "edit", label: "Edit", icon: Pencil, onSelect: onEdit }] : []),
    ...(isHost && onCancel ? [{ id: "cancel", label: m.starts_at ? "Cancel meeting" : "Delete link", icon: Trash2, onSelect: onCancel, destructive: true }] : []),
  ];
}

function UpcomingRow({ meeting: m }: { meeting: CallsMeeting }) {
  const me = useMe();
  const router = useRouter();
  const zone = useCallsTimeZone();
  const isHost = me.data?.id === m.host.id;
  const [editing, setEditing] = useState(false);
  const [canceling, setCanceling] = useState(false);
  const cancel = useCancelCallsMeeting();
  const actions = meetingActions(m, isHost, (href) => router.push(href), () => setEditing(true), () => setCanceling(true));
  const startsSoon = !!m.starts_at && Date.parse(m.starts_at) - Date.now() < 10 * 60 * 1000;
  return (
    <li className="flex min-h-row items-center gap-3 px-4 py-3">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-primary-soft text-primary-strong">
        <CalendarClock className="size-5" aria-hidden />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex min-w-0 items-center gap-2">
          <span className="truncate type-body font-semibold text-fg">{m.title || "Meeting"}</span>
          {m.live_since && (
            <Badge tone="success" dot>
              Live
            </Badge>
          )}
        </span>
        <span className="line-clamp-2 break-words type-label text-fg-muted">
          {m.starts_at ? formatCallsWhen(m.starts_at, zone) : ""} · {isHost ? CALLS_ACCESS_LABELS[m.access] : `Host: ${m.host.display_name}`}
        </span>
      </div>
      <Button asChild size="sm" variant={startsSoon || m.live_since ? "primary" : "secondary"}>
        <Link href={`/meet/${m.code}`}>Join</Link>
      </Button>
      <ResourceMenu feature="calls" kind="calls.meeting" id={m.id} title={m.title || "Meeting"} actions={actions} {...(m.org_id ? { orgId: m.org_id } : {})} />
      <ScheduleMeetingDialog open={editing} onOpenChange={setEditing} meeting={m} />
      <ConfirmDialog
        open={canceling}
        onOpenChange={setCanceling}
        title={`Cancel ${m.title || "this meeting"}?`}
        info="The link stops working and invited people are told."
        confirmLabel="Cancel meeting"
        cancelLabel="Keep it"
        tone="danger"
        onConfirm={() => cancel.mutateAsync(m.id).then(() => toast.success("Meeting canceled"))}
        errorText={(e) => errorMessage(e, "Couldn't cancel. Try again.")}
      />
    </li>
  );
}

function HistorySection() {
  const history = useCallsHistory();
  const platform = useCallsPlatformMeetings("recent");
  const items = history.data?.items ?? [];
  // Past Meet, Teams and Zoom meetings (last 14 days) among the calls loaded so far.
  const oldest = history.hasNextPage && items.length > 0 ? items[items.length - 1]!.started_at : "";
  const rows = [
    ...items.map((h) => ({ at: h.started_at, node: <HistoryRow key={`${h.type}:${h.id}`} item={h} /> })),
    ...(platform.data?.items ?? []).filter((m) => m.starts_at >= oldest).map((m) => ({ at: m.starts_at, node: <PlatformMeetingRow key={`ext:${m.id}`} meeting={m} past /> })),
  ].sort((a, b) => b.at.localeCompare(a.at));
  return (
    <Section title="Recent">
      {history.isPending ? (
        <ListSkeleton rows={4} />
      ) : history.isError ? (
        <ErrorState compact title="Couldn't load your calls" info={history.error.message} onRetry={() => void history.refetch()} retrying={history.isFetching} />
      ) : rows.length === 0 ? (
        <EmptyState compact icon={<Video />} title="No calls yet" />
      ) : (
        <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
          {rows.map((r) => r.node)}
          <li>
            <LoadMore hasNextPage={history.hasNextPage} isFetchingNextPage={history.isFetchingNextPage} fetchNextPage={history.fetchNextPage} />
          </li>
        </ul>
      )}
    </Section>
  );
}

/** A call in your history: who, when, how long; missed calls in red with Call back. */
export function HistoryRow({ item }: { item: CallsHistoryItem }) {
  const me = useMe();
  const router = useRouter();
  const zone = useCallsTimeZone();
  const [calling, setCalling] = useState(false);
  const missed = item.type === "missed";
  const title = callsHistoryTitle(item, me.data?.id);
  const others = item.participants.filter((p) => p.user?.id !== me.data?.id);
  const Icon = missed ? PhoneMissed : item.meeting.kind === "call" ? PhoneIncoming : Video;
  const when = formatCallsWhen(item.started_at, zone);
  const meta = missed ? `${item.ring_status === "declined" ? "Declined" : "Missed"} · ${when}` : item.live ? `Live now · ${when}` : `${when} · ${formatCallsDuration(item.duration_seconds)}`;
  const actions = [
    ...(!missed ? [{ id: "details", label: "Details", icon: Info, onSelect: () => router.push(`/calls/${item.id}`) }] : []),
    { id: "join", label: item.live ? "Join" : "Open meeting", icon: Video, onSelect: () => router.push(`/meet/${item.meeting.code}`) },
    ...(missed && item.caller ? [{ id: "callback", label: `Call ${item.caller.display_name}`, icon: Phone, onSelect: () => setCalling(true) }] : []),
  ];
  const body = (
    <>
      <span className={missed ? "flex size-10 shrink-0 items-center justify-center rounded-md bg-danger-soft text-danger-strong" : "flex size-10 shrink-0 items-center justify-center rounded-md bg-sunken text-fg-muted"}>
        <Icon className="size-5" aria-hidden />
      </span>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className={missed ? "truncate type-body font-semibold text-danger-strong" : "truncate type-body font-semibold text-fg"}>{title}</span>
        <span className="line-clamp-2 break-words type-label text-fg-muted">{meta}</span>
      </span>
      <span className="hidden -space-x-2 sm:flex" aria-hidden>
        {others.slice(0, 4).map((p, i) => (
          <AppAvatar key={p.user?.id ?? `${p.name}-${i}`} name={p.name} src={p.user?.avatar_url ?? null} size="sm" ring />
        ))}
      </span>
    </>
  );
  return (
    <li className="flex min-h-row items-center gap-2 pr-2">
      {missed ? (
        <div className="flex min-w-0 flex-1 items-center gap-3 px-4 py-3">{body}</div>
      ) : (
        <Link href={`/calls/${item.id}`} className="flex min-w-0 flex-1 items-center gap-3 rounded-md px-4 py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
          {body}
        </Link>
      )}
      {item.live && (
        <Button asChild size="sm">
          <Link href={`/meet/${item.meeting.code}`}>Join</Link>
        </Button>
      )}
      <ResourceMenu feature="calls" kind="calls.meeting" id={item.meeting.id} title={title} actions={actions} />
      {missed && item.caller ? (
        <StartCallDialog open={calling} onOpenChange={setCalling} initial={[item.caller]} />
      ) : null}
    </li>
  );
}

function LinksSection() {
  const links = useCallsLinks();
  if (links.isPending || links.isError || links.data.items.length === 0) return null;
  return (
    <Section title="Your meeting links" info="Links you created without a time. They work until you delete them.">
      <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
        {links.data.items.map((m) => (
          <LinkRow key={m.id} meeting={m} />
        ))}
        <li>
          <LoadMore hasNextPage={links.hasNextPage} isFetchingNextPage={links.isFetchingNextPage} fetchNextPage={links.fetchNextPage} />
        </li>
      </ul>
    </Section>
  );
}

function LinkRow({ meeting: m }: { meeting: CallsMeeting }) {
  const router = useRouter();
  const [deleting, setDeleting] = useState(false);
  const cancel = useCancelCallsMeeting();
  const actions = meetingActions(m, true, (href) => router.push(href), undefined, () => setDeleting(true));
  return (
    <li className="flex min-h-row items-center gap-3 px-4 py-3">
      <span className="flex size-10 shrink-0 items-center justify-center rounded-md bg-sunken text-fg-muted">
        <Link2 className="size-5" aria-hidden />
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate type-body font-semibold text-fg">{m.title || m.code}</span>
        <span className="truncate type-label text-fg-muted">
          {m.title ? `${m.code} · ` : ""}
          {CALLS_ACCESS_LABELS[m.access]}
        </span>
      </div>
      <Button asChild size="sm" variant="secondary">
        <Link href={`/meet/${m.code}`}>Join</Link>
      </Button>
      <ResourceMenu feature="calls" kind="calls.meeting" id={m.id} title={m.title || m.code} actions={actions} />
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title="Delete this meeting link?"
        info="Anyone who has it can't join anymore."
        confirmLabel="Delete link"
        tone="danger"
        onConfirm={() => cancel.mutateAsync(m.id).then(() => toast.success("Link deleted"))}
        errorText={(e) => errorMessage(e, "Couldn't delete. Try again.")}
      />
    </li>
  );
}
