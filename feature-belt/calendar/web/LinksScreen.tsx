"use client";

import { errorMessage, formatCalendarDuration, orgsNoun, useCalendarLinks, useDeleteCalendarLink, useUpdateCalendarLink, type CalendarBookingLink } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, IconButton, Skeleton, SkeletonGroup, Stagger, StaggerItem, Switch, toast } from "@scaffoldapp/ui-web";
import { Copy, ExternalLink, Link2, Lock, Pause, Pencil, Play, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { ColorDot, copyText } from "./parts";

function LinkRow({ link, onDelete }: { link: CalendarBookingLink; onDelete: (l: CalendarBookingLink) => void }) {
  const router = useRouter();
  const update = useUpdateCalendarLink();
  const setActive = (active: boolean) =>
    update.mutate(
      { id: link.id, body: { active } },
      { onSuccess: () => toast.success(active ? "Link is on" : "Link paused"), onError: (e) => toast.error(errorMessage(e, "Couldn't change the link.")) },
    );
  const actions = [
    { id: "copy", label: "Copy link", icon: Copy, onSelect: () => void copyText(link.url, toast) },
    { id: "open", label: "Open booking page", icon: ExternalLink, onSelect: () => window.open(link.url, "_blank", "noopener") },
    { id: "edit", label: "Edit", icon: Pencil, onSelect: () => router.push(`/calendar/links/${link.id}`) },
    { id: "toggle", label: link.active ? "Pause" : "Turn on", icon: link.active ? Pause : Play, onSelect: () => setActive(!link.active) },
    { id: "delete", label: "Delete", icon: Trash2, destructive: true, onSelect: () => onDelete(link) },
  ];
  return (
    <Card padding="compact" className="flex flex-col gap-3 sm:flex-row sm:items-center">
      <Link href={`/calendar/links/${link.id}`} className="flex min-w-0 flex-1 flex-col gap-1 rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
        <span className="flex min-w-0 items-center gap-2">
          <ColorDot color={link.color} />
          <span className="truncate type-title-sm text-fg">{link.title}</span>
        </span>
        <span className="flex flex-wrap items-center gap-2 type-label text-fg-muted">
          {link.durations.map(formatCalendarDuration).join(", ")}
          <span aria-hidden>·</span>
          <span className="truncate">/book/{link.slug}</span>
        </span>
        <span className="flex flex-wrap items-center gap-2 pt-1">
          {link.audience === "internal" ? (
            <Badge tone="info" icon={<Lock />}>
              {link.org_name ? `${link.org_name} only` : "Members only"}
            </Badge>
          ) : (
            <Badge tone="neutral">Anyone</Badge>
          )}
          {link.upcoming_count > 0 ? <Badge tone="primary">{link.upcoming_count} upcoming</Badge> : null}
          {!link.active ? <Badge tone="warning">Paused</Badge> : null}
        </span>
      </Link>
      <div className="flex items-center gap-2 self-end sm:self-center">
        <Switch checked={link.active} onCheckedChange={setActive} label={<span className="sr-only">Accept bookings on {link.title}</span>} />
        <IconButton label="Copy link" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyText(link.url, toast)} />
        <ResourceMenu feature="calendar" kind="calendar.booking_link" id={link.id} title={link.title} actions={actions} />
      </div>
    </Card>
  );
}

/** /calendar/links: your booking links; copy, pause, edit or delete them. */
export function LinksScreen() {
  const links = useCalendarLinks();
  const remove = useDeleteCalendarLink();
  const [deleting, setDeleting] = useState<CalendarBookingLink | null>(null);
  const rows = links.data?.items ?? [];
  return (
    <div className="mx-auto flex w-full max-w-content flex-col gap-section px-gutter py-section">
      <PageHeader
        title="Booking links"
        info={`Share a link so people can pick a time from your free slots. Members-only links need a signed-in member of the chosen ${orgsNoun()}; others work for anyone, without an account.`}
        action={
          <Button asChild size="auto" leftIcon={<Plus />}>
            <Link href="/calendar/links/new">New link</Link>
          </Button>
        }
      />
      {links.isPending ? (
        <SkeletonGroup label="Loading booking links" className="flex flex-col gap-4">
          {[0, 1].map((i) => (
            <Skeleton key={i} className="h-24" />
          ))}
        </SkeletonGroup>
      ) : links.isError ? (
        <Card>
          <ErrorState compact title="Couldn't load your booking links" info={links.error.message} onRetry={() => void links.refetch()} retrying={links.isFetching} />
        </Card>
      ) : rows.length === 0 ? (
        <Card>
          <EmptyState
            compact
            icon={<Link2 />}
            title="No booking links yet"
            action={
              <Button asChild variant="secondary" size="auto" leftIcon={<Plus />}>
                <Link href="/calendar/links/new">Create a booking link</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        <>
          <Stagger as="ul" className="flex flex-col gap-4" aria-label="Booking links">
            {rows.map((l) => (
              <StaggerItem as="li" key={l.id}>
                <LinkRow link={l} onDelete={setDeleting} />
              </StaggerItem>
            ))}
          </Stagger>
          <LoadMore hasNextPage={links.hasNextPage} isFetchingNextPage={links.isFetchingNextPage} fetchNextPage={links.fetchNextPage} />
        </>
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={`Delete “${deleting?.title ?? ""}”?`}
        tone="danger"
        confirmLabel="Delete link"
        onConfirm={async () => {
          if (!deleting) return;
          await remove.mutateAsync(deleting.id);
          toast.success("Link deleted");
          setDeleting(null);
        }}
        errorText={(e) => errorMessage(e, "Couldn't delete the link. Try again.")}
      >
        <p className="type-body text-fg">
          The booking page stops working.{deleting && deleting.upcoming_count > 0 ? ` Its ${deleting.upcoming_count} upcoming bookings stay on your calendar; cancel them from Bookings if needed.` : ""}
        </p>
      </ConfirmDialog>
    </div>
  );
}
