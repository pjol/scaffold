"use client";

import { callsPeopleNames, formatCallsDuration, formatCallsTime, formatCallsWhen, useCallsCall, useCallsTimeZone, useMe } from "@scaffoldapp/shared";
import { Badge, Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup, SkeletonRow } from "@scaffoldapp/ui-web";
import { ArrowLeft, Copy, Video } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Screen, Section } from "@/components/shell/PageHeader";
import { copyMeetingLink } from "./MeetingDialogs";

/** `/calls/[id]`: one call (a session): when, how long, and everyone's time in it. */
export function CallDetailScreen({ id }: { id: string }) {
  const call = useCallsCall(id);
  const me = useMe();
  const router = useRouter();
  const zone = useCallsTimeZone();

  let body;
  if (call.isPending) {
    body = (
      <SkeletonGroup label="Loading call" className="flex flex-col gap-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-24 w-full" />
        {Array.from({ length: 3 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </SkeletonGroup>
    );
  } else if (call.isError) {
    body =
      call.error.status === 404 ? (
        <EmptyState icon={<Video />} title="Call not found" action={<Button asChild variant="secondary"><Link href="/calls">Back to calls</Link></Button>} />
      ) : (
        <ErrorState title="Couldn't load the call" info={call.error.message} onRetry={() => void call.refetch()} retrying={call.isFetching} />
      );
  } else {
    const c = call.data;
    const title = c.meeting.title || `Call with ${callsPeopleNames(c.participants, me.data?.id)}`;
    const joinUrl = typeof window !== "undefined" ? `${window.location.origin}/meet/${c.meeting.code}` : `/meet/${c.meeting.code}`;
    body = (
      <Screen
        title={title}
        action={
          <>
            {c.can_join && (
              <Button asChild variant={c.live ? "primary" : "secondary"} leftIcon={<Video />}>
                <Link href={`/meet/${c.meeting.code}`}>{c.live ? "Join" : "Start again"}</Link>
              </Button>
            )}
            <ResourceMenu
              feature="calls"
              kind="calls.meeting"
              id={c.meeting.id}
              title={title}
              actions={[{ id: "copy", label: "Copy link", icon: Copy, onSelect: () => void copyMeetingLink(joinUrl) }]}
            />
          </>
        }
      >
        <Card className="grid gap-4 sm:grid-cols-3">
          <Fact label="Started" value={formatCallsWhen(c.started_at, zone)} />
          <Fact label="Duration" value={c.live ? "Live now" : formatCallsDuration(c.duration_seconds)} />
          <Fact label="Host" value={c.host.display_name} />
        </Card>
        <Section title={`People (${c.participants.length})`}>
          <ul className="flex flex-col divide-y divide-border rounded-lg border border-border bg-surface">
            {c.participants.map((p) => (
              <li key={p.identity} className="flex min-h-row items-center gap-3 px-4 py-3">
                <AppAvatar name={p.name} src={p.user?.avatar_url ?? null} size="md" />
                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate type-body font-semibold text-fg">
                      {p.name}
                      {p.user?.id === me.data?.id ? " (you)" : ""}
                    </span>
                    {p.guest && <Badge>Guest</Badge>}
                    {p.user?.id === c.host.id && <Badge tone="primary">Host</Badge>}
                  </span>
                  <span className="truncate type-label text-fg-muted">
                    Joined {formatCallsTime(p.first_joined_at, zone)}
                    {p.joins > 1 ? ` · ${p.joins} times` : ""}
                  </span>
                </div>
                <span className="shrink-0 type-label tabular-nums text-fg-muted">{p.last_left_at ? formatCallsDuration(p.seconds) : "In the call"}</span>
              </li>
            ))}
          </ul>
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
