"use client";

import {
  CALLS_ACCESS_HELP,
  CALLS_ACCESS_LABELS,
  CALLS_RECORDS_ACCESS_LABELS,
  errorMessage,
  useCallsJoinInfo,
  useUpdateCallsMeeting,
  type CallsAccess,
  type CallsRecordsAccess,
  type CallsUpdateMeetingRequest,
} from "@scaffoldapp/shared";
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
    <Dialog open={open} onOpenChange={onOpenChange} title="Meeting settings" size="lg">
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
          <section className="flex flex-col gap-4" aria-label="What people can do">
            <h3 className="type-title-sm text-fg">What people can do</h3>
            <Switch checked={m.allow_screen_share} disabled={update.isPending} onCheckedChange={(on) => save({ allow_screen_share: on }, on ? "People can present" : "Only hosts can present")} label="Present their screen" />
            <Switch checked={m.allow_chat} disabled={update.isPending} onCheckedChange={(on) => save({ allow_chat: on }, on ? "Chat is on" : "Only hosts can chat")} label="Send chat messages" />
            <Switch checked={m.allow_reactions} disabled={update.isPending} onCheckedChange={(on) => save({ allow_reactions: on }, on ? "Reactions are on" : "Reactions are off")} label="Send reactions" />
            <Switch
              checked={m.allow_unmute}
              disabled={update.isPending}
              onCheckedChange={(on) => save({ allow_unmute: on }, on ? "People can unmute" : "People unmute when you ask them")}
              label="Unmute themselves"
              info="Off: people stay muted until a host asks them to unmute (Ask to unmute in People). Raised hands still work."
            />
            <Switch checked={m.mute_on_entry} disabled={update.isPending} onCheckedChange={(on) => save({ mute_on_entry: on }, on ? "People join muted" : "People join as they choose")} label="Mute people when they join" />
            <Switch checked={m.moderate_questions} disabled={update.isPending} onCheckedChange={(on) => save({ moderate_questions: on }, on ? "Questions wait for approval" : "Questions show at once")} label="Approve questions first" />
          </section>
          <section className="flex flex-col gap-4" aria-label="Records">
            <h3 className="type-title-sm text-fg">Records</h3>
            <Switch
              checked={m.save_chat}
              disabled={update.isPending}
              onCheckedChange={(on) => save({ save_chat: on }, on ? "Chat is saved with the meeting" : "Chat isn't saved")}
              label="Save chat"
              info="Messages sent from now on are kept with this call. Everyone sees a notice in the chat."
            />
            <Field label="Who can open recordings, transcripts and saved chat" info="Hosts always can.">
              <Select
                value={m.records_access}
                disabled={update.isPending}
                onValueChange={(v) => save({ records_access: v as CallsRecordsAccess }, `Records: ${CALLS_RECORDS_ACCESS_LABELS[v as CallsRecordsAccess].toLowerCase()}`)}
                options={(["participants", "invited", "org"] as CallsRecordsAccess[]).filter((a) => a !== "org" || m.org_id).map((a) => ({ value: a, label: CALLS_RECORDS_ACCESS_LABELS[a] }))}
              />
            </Field>
          </section>
          <MeetingLinkField meeting={m} />
        </div>
      )}
    </Dialog>
  );
}
