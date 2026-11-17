"use client";

import {
  CALLS_ACCESS_LABELS,
  CALLS_RECORDS_ACCESS_LABELS,
  formatCallsDuration,
  formatCallsWhen,
  useCallsJoinInfo,
  useCallsMeetingRecordings,
  useCallsMeetingSessions,
  useCallsTimeZone,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup, SkeletonRow } from "@scaffoldapp/ui-web";
import { ArrowLeft, CalendarX, Copy, ExternalLink, NotebookText, Video } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Screen, Section } from "@/components/shell/PageHeader";
import { copyMeetingLink } from "./MeetingDialogs";
import { RecordingsList } from "./Records";
import { callsNotesLink, callsSourceLink } from "./seams";

/**
 * `/calls/meetings/[id]`: a meeting's page: details, its notes (the app's link), recordings,
 * and each call held on it with its transcript and attendance.
 */
export function MeetingScreen({ id }: { id: string }) {
  const router = useRouter();
  const zone = useCallsTimeZone();
  const info = useCallsJoinInfo(id);
  const sessions = useCallsMeetingSessions(id);
  const recordings = useCallsMeetingRecordings(id);
  const sessionItems = sessions.data?.items ?? [];

  let body;
  if (info.isPending || sessions.isPending) {
    body = (
      <SkeletonGroup label="Loading meeting" className="flex flex-col gap-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-24 w-full" />
        <SkeletonRow />
        <SkeletonRow />
      </SkeletonGroup>
    );
  } else if (info.isError && info.error.status !== 404) {
    body = <ErrorState title="Couldn't load the meeting" info={info.error.message} onRetry={() => void info.refetch()} retrying={info.isFetching} />;
  } else if (info.isError && sessionItems.length === 0) {
    body = <EmptyState icon={<CalendarX />} title="Meeting not found" action={<Button asChild variant="secondary"><Link href="/calls">Back to calls</Link></Button>} />;
  } else {
    const d = info.data;
    const m = d?.meeting ?? null;
    const title = m?.title || d?.title || "Meeting";
    const notes = callsNotesLink(m);
    const source = callsSourceLink(m?.source);
    const canJoin = d && (d.mode === "host" || d.mode === "cohost" || d.mode === "direct");
    body = (
      <Screen
        title={title}
        action={
          <>
            {d && canJoin && (
              <Button asChild variant={d.live ? "primary" : "secondary"} leftIcon={<Video />}>
                <Link href={`/meet/${d.code}`}>{d.live ? "Join" : "Start"}</Link>
              </Button>
            )}
            <ResourceMenu
              feature="calls"
              kind="calls.meeting"
              id={id}
              title={title}
              actions={[
                ...(m ? [{ id: "copy", label: "Copy link", icon: Copy, onSelect: () => void copyMeetingLink(m.join_url) }] : []),
                ...(source ? [{ id: "source", label: source.label, icon: ExternalLink, onSelect: () => router.push(source.href) }] : []),
              ]}
              {...(m?.org_id ? { orgId: m.org_id } : {})}
            />
          </>
        }
      >
        {m && (
          <Card className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Fact label="When" value={m.starts_at ? formatCallsWhen(m.starts_at, zone) : "Any time (a link)"} />
            <Fact label="Host" value={m.host.display_name} />
            <Fact label="Who can join" value={CALLS_ACCESS_LABELS[m.access]} />
            <Fact label="Records open to" value={CALLS_RECORDS_ACCESS_LABELS[m.records_access]} />
          </Card>
        )}
        {notes && (
          <Card className="flex items-center gap-3">
            <NotebookText className="size-5 shrink-0 text-primary-strong" aria-hidden />
            <span className="flex-1 type-body text-fg">{notes.label}</span>
            <Button asChild size="sm" variant="secondary">
              <Link href={notes.href}>Open</Link>
            </Button>
          </Card>
        )}
        {m && m.invitees.length > 0 && (
          <Section title={`Invited (${m.invitees.length})`}>
            <ul className="flex flex-wrap gap-3">
              {m.invitees.map((i) => (
                <li key={i.user.id} className="flex items-center gap-2 rounded-pill border border-border py-1 pr-3 pl-1">
                  <AppAvatar name={i.user.display_name} src={i.user.avatar_url} size="sm" />
                  <span className="type-label text-fg">{i.user.display_name}</span>
                  {i.role === "cohost" && <Badge tone="primary">Co-host</Badge>}
                </li>
              ))}
            </ul>
          </Section>
        )}
        <Section title="Recordings">
          {recordings.isPending ? (
            <SkeletonRow />
          ) : recordings.isError ? (
            <ErrorState compact title="Couldn't load recordings" info={recordings.error.message} onRetry={() => void recordings.refetch()} retrying={recordings.isFetching} />
          ) : (
            <RecordingsList recordings={recordings.data.items} />
          )}
        </Section>
        <Section title="Calls">
          {sessions.isError ? (
            <ErrorState compact title="Couldn't load the calls" info={sessions.error?.message ?? ""} onRetry={() => void sessions.refetch()} retrying={sessions.isFetching} />
          ) : sessionItems.length === 0 ? (
            <EmptyState compact icon={<Video />} title="No calls yet" />
          ) : (
            <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
              {sessionItems.map((x) => (
                <li key={x.id}>
                  <Link href={`/calls/${x.id}`} className="flex min-h-row items-center gap-3 px-4 py-3 hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring">
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="flex items-center gap-2">
                        <span className="truncate type-body font-semibold text-fg">{formatCallsWhen(x.started_at, zone)}</span>
                        {x.live && (
                          <Badge tone="success" dot>
                            Live
                          </Badge>
                        )}
                      </span>
                      <span className="truncate type-label text-fg-muted">
                        {[
                          x.live ? "In progress" : formatCallsDuration(x.duration_seconds),
                          `${x.participant_count} ${x.participant_count === 1 ? "person" : "people"}`,
                          x.recordings ? `${x.recordings} ${x.recordings === 1 ? "recording" : "recordings"}` : null,
                          x.transcript_lines ? "transcript" : null,
                          x.chat_messages ? "saved chat" : null,
                        ]
                          .filter(Boolean)
                          .join(" · ")}
                      </span>
                    </div>
                  </Link>
                </li>
              ))}
            </ul>
          )}
          {sessions.hasNextPage && (
            <Button variant="ghost" className="self-center" loading={sessions.isFetchingNextPage} onClick={() => void sessions.fetchNextPage()}>
              Show more
            </Button>
          )}
        </Section>
      </Screen>
    );
  }
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-4 px-gutter py-section">
      <Button variant="ghost" size="sm" leftIcon={<ArrowLeft />} className="self-start" onClick={() => router.push("/calls")}>
        Calls
      </Button>
      {body}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="type-caption text-fg-muted">{label}</span>
      <span className="type-body text-fg">{value}</span>
    </div>
  );
}
