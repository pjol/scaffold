"use client";

import { CALLS_ACCESS_HELP, CALLS_ACCESS_LABELS, errorMessage, useCallsJoinInfo, useUpdateCallsMeeting, type CallsAccess, type CallsUpdateMeetingRequest } from "@scaffoldapp/shared";
import { Dialog, ErrorState, Field, Select, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";
import { MeetingLinkField } from "../MeetingDialogs";
import { useCallSession } from "./session";

/** Hosts: who can join, the lobby, locking the room, and the link. Changes apply at once. */
export function HostSettingsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const s = useCallSession();
  const info = useCallsJoinInfo(open ? s.code : null);
  const update = useUpdateCallsMeeting(s.meetingId);
  const m = info.data?.meeting;
  const save = (body: CallsUpdateMeetingRequest, done: string) =>
    update.mutate(body, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't change the setting. Try again.")) });

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Meeting settings">
      {info.isPending ? (
        <SkeletonGroup label="Loading settings" className="flex flex-col gap-4">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-2/3" />
        </SkeletonGroup>
      ) : info.isError || !m ? (
        <ErrorState compact title="Couldn't load the settings" {...(info.error ? { info: info.error.message } : {})} onRetry={() => void info.refetch()} retrying={info.isFetching} />
      ) : (
        <div className="flex flex-col gap-6">
          <Field label="Who can join" info={CALLS_ACCESS_HELP[m.access]}>
            <Select
              value={m.access}
              disabled={update.isPending}
              onValueChange={(v) => save({ access: v as CallsAccess }, `Now: ${CALLS_ACCESS_LABELS[v as CallsAccess].toLowerCase()}`)}
              options={(["open", "invited", "org"] as CallsAccess[]).filter((a) => a !== "org" || m.org_id).map((a) => ({ value: a, label: CALLS_ACCESS_LABELS[a] }))}
            />
          </Field>
          <div className="flex flex-col gap-4">
            {m.access !== "org" && (
              <Switch
                checked={m.lobby_enabled}
                disabled={update.isPending}
                onCheckedChange={(on) => save({ lobby_enabled: on }, on ? "People ask to join" : m.access === "open" ? "Anyone with the link joins" : "Only invited people join")}
                label="Ask to join"
                info={m.access === "open" ? "Off: anyone with the link joins, guests too. Turning it off lets everyone waiting in." : "On: people who weren't invited can ask; a host lets them in."}
              />
            )}
            <Switch
              checked={m.locked}
              disabled={update.isPending}
              onCheckedChange={(on) => save({ locked: on }, on ? "Meeting locked" : "Meeting unlocked")}
              label="Lock meeting"
              info="Nobody new can join or ask to join. People already here stay and can reconnect."
            />
          </div>
          <MeetingLinkField meeting={m} />
        </div>
      )}
    </Dialog>
  );
}
