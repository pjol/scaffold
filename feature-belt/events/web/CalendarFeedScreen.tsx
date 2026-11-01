"use client";

import { errorMessage, useCalendarFeed, useDeleteCalendarFeed, useRotateCalendarFeed } from "@scaffoldapp/shared";
import { Banner, Button, Card, ConfirmDialog, ErrorState, IconButton, Input, ListRow, Skeleton, SkeletonGroup, toast } from "@scaffoldapp/ui-web";
import { CalendarSync, Copy } from "lucide-react";
import { useState } from "react";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

/** Settings → Calendar: a secret feed URL calendar apps subscribe to (events you host or RSVP'd going, maybe or waitlisted). */
export function CalendarFeedScreen() {
  const feed = useCalendarFeed();
  const rotate = useRotateCalendarFeed();
  const remove = useDeleteCalendarFeed();
  const [url, setUrl] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<"replace" | "off" | null>(null);

  const create = async () => {
    const r = await rotate.mutateAsync();
    setUrl(r.url);
  };
  const copy = async () => {
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      toast.success("Link copied");
    } catch {
      toast.error("Couldn't copy. Select the link and copy it yourself.");
    }
  };

  return (
    <div className="flex flex-col gap-section">
      <PageHeader title="Calendar" />
      <Section
        title="Calendar feed"
        info="Subscribe to this link in Google Calendar, Apple Calendar or Outlook to see events you host or RSVP'd to. Anyone with the link can read those events, so keep it to yourself."
      >
        {feed.isPending ? (
          <SkeletonGroup label="Loading calendar feed" className="flex flex-col gap-3">
            <Skeleton className="h-14" />
            <Skeleton className="h-10 w-40" />
          </SkeletonGroup>
        ) : feed.isError ? (
          <Card>
            <ErrorState compact title="Couldn't load your calendar feed" info={feed.error.message} onRetry={() => void feed.refetch()} retrying={feed.isFetching} />
          </Card>
        ) : (
          <div className="flex max-w-form flex-col gap-4">
            {url ? (
              <div className="flex flex-col gap-3">
                <Banner tone="info">Copy this link now. It's shown once; replace it to get a new one.</Banner>
                <Input readOnly value={url} aria-label="Calendar feed link" onFocus={(e) => e.currentTarget.select()} trailing={<IconButton label="Copy link" icon={<Copy />} size="sm" onClick={() => void copy()} />} />
              </div>
            ) : (
              <Card padding="none" className="p-1">
                <ListRow
                  title={feed.data.active ? `Active since ${formatDate(feed.data.created_at ?? "")}` : "No feed link yet"}
                  leading={<CalendarSync className="size-5 text-fg-muted" aria-hidden />}
                />
              </Card>
            )}
            <div className="flex flex-wrap gap-2">
              {feed.data.active ? (
                <>
                  <Button variant="secondary" size="auto" onClick={() => setConfirm("replace")}>
                    Replace link
                  </Button>
                  <Button variant="ghost" size="auto" onClick={() => setConfirm("off")}>
                    Turn off
                  </Button>
                </>
              ) : (
                <Button size="auto" loading={rotate.isPending} onClick={() => void create().catch((e: unknown) => toast.error(errorMessage(e, "Couldn't create the link. Try again.")))}>
                  Create feed link
                </Button>
              )}
            </div>
          </div>
        )}
      </Section>
      <ConfirmDialog
        open={confirm === "replace"}
        onOpenChange={(o) => setConfirm(o ? "replace" : null)}
        title="Replace your calendar link?"
        confirmLabel="Replace link"
        onConfirm={create}
        errorText={(e) => errorMessage(e, "Couldn't replace the link. Try again.")}
      >
        <p className="type-body text-fg">The old link stops working. Calendars subscribed to it stop updating.</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "off"}
        onOpenChange={(o) => setConfirm(o ? "off" : null)}
        title="Turn off your calendar feed?"
        tone="danger"
        confirmLabel="Turn off"
        onConfirm={async () => {
          await remove.mutateAsync();
          setUrl(null);
          toast.success("Calendar feed turned off");
        }}
        errorText={(e) => errorMessage(e, "Couldn't turn off the feed. Try again.")}
      >
        <p className="type-body text-fg">The link stops working. Calendars subscribed to it stop updating.</p>
      </ConfirmDialog>
    </div>
  );
}
