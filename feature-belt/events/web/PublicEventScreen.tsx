"use client";

import { eventsEndpoints, formatEventRange, isApiError, useAuthSession, useEndpoints, usePublicEvent } from "@scaffoldapp/shared";
import { Badge, Banner, Button, Card, EmptyState, ErrorState, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { CalendarClock, CalendarPlus, CalendarX, LogIn, MapPin, UserRound, Video } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { signInHref } from "@/lib/redirect";
import { CoverImage, hasEnded } from "./parts";

function Row({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-h-control items-start gap-3 type-body text-fg">
      <span className="flex size-6 shrink-0 items-center justify-center text-fg-muted [&_svg]:size-5" aria-hidden>
        {icon}
      </span>
      <div className="min-w-0 flex-1 break-words">{children}</div>
    </div>
  );
}

/** /e/<id>: a public event for anyone with the link. Time, place and host; never who is going. */
export function PublicEventScreen({ id }: { id: string }) {
  const event = usePublicEvent(id);
  const api = useEndpoints(eventsEndpoints);
  const auth = useAuthSession();
  const appPath = `/events/${id}`;

  const body = (() => {
    if (event.isPending) {
      return (
        <SkeletonGroup label="Loading event" className="flex flex-col gap-section">
          <Skeleton className="aspect-video w-full" />
          <Skeleton shape="text" className="h-8 w-2/3" />
          <Skeleton shape="text" className="h-5 w-1/2" />
        </SkeletonGroup>
      );
    }
    if (event.isError) {
      return isApiError(event.error) && event.error.status === 404 ? (
        <Card>
          <EmptyState icon={<CalendarX />} title="This event isn't available" />
        </Card>
      ) : (
        <Card>
          <ErrorState title="Couldn't load this event" info={event.error.message} onRetry={() => void event.refetch()} retrying={event.isFetching} />
        </Card>
      );
    }
    const e = event.data;
    const cancelled = e.status === "cancelled";
    const ended = hasEnded(e);
    return (
      <>
        {e.cover_url ? <CoverImage src={e.cover_url} alt="" priority /> : null}
        <div className="flex flex-col gap-4">
          <h1 className="type-title text-fg lg:type-display">{e.title}</h1>
          {cancelled ? <Banner tone="danger">This event was cancelled.</Banner> : ended ? <Banner tone="neutral">This event has ended.</Banner> : null}
          <div className="flex flex-col gap-2">
            <Row icon={<CalendarClock />}>{formatEventRange(e.starts_at, e.ends_at, e.timezone)}</Row>
            {e.location_text ? <Row icon={<MapPin />}>{e.location_text}</Row> : null}
            {e.online_url ? (
              <Row icon={<Video />}>
                <a href={e.online_url} target="_blank" rel="noopener noreferrer" className="break-all text-primary-strong underline underline-offset-2">
                  {e.online_url}
                </a>
              </Row>
            ) : null}
            <Row icon={<UserRound />}>Hosted by {e.host.display_name || "a deleted account"}</Row>
          </div>
          {e.is_full && !cancelled && !ended ? (
            <span>
              <Badge tone="warning">Full</Badge>
            </span>
          ) : null}
        </div>
        {e.details ? <p className="max-w-prose whitespace-pre-wrap break-words type-body text-fg">{e.details}</p> : null}
        <div className="flex flex-wrap gap-2">
          {!cancelled && !ended ? (
            auth.isLoaded && auth.isSignedIn ? (
              <Button asChild size="auto">
                <Link href={appPath}>RSVP</Link>
              </Button>
            ) : (
              <Button asChild size="auto" leftIcon={<LogIn />}>
                <Link href={signInHref(appPath)}>Sign in to RSVP</Link>
              </Button>
            )
          ) : null}
          <Button asChild variant="secondary" size="auto" leftIcon={<CalendarPlus />}>
            <a href={api.public.icsUrl(e.id)} download>
              Add to calendar
            </a>
          </Button>
        </div>
      </>
    );
  })();

  return <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">{body}</div>;
}
