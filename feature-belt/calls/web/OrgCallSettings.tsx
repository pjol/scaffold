"use client";

import { errorMessage, orgsNoun, useCallsOrgSettings, useUpdateCallsOrgSettings } from "@scaffoldapp/shared";
import { Banner, Dialog, ErrorState, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";

/** What a workspace allows in its calls: captions, saved transcripts, recording (owners and admins change it). */
export function OrgCallSettingsDialog({ orgId, open, onOpenChange }: { orgId: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const q = useCallsOrgSettings(open ? orgId : null);
  const update = useUpdateCallsOrgSettings(orgId);
  const save = (body: Parameters<typeof update.mutate>[0], done: string) =>
    update.mutate(body, { onSuccess: () => toast.success(done), onError: (e) => toast.error(errorMessage(e, "Couldn't change the setting.")) });
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={`Calls in this ${orgsNoun()}`}>
      {q.isPending ? (
        <SkeletonGroup label="Loading settings" className="flex flex-col gap-4">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-10 w-full" />
        </SkeletonGroup>
      ) : q.isError ? (
        <ErrorState compact title="Couldn't load the settings" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      ) : (
        <div className="flex flex-col gap-5">
          {!q.data.can_manage && <Banner tone="info">Only owners and admins change these.</Banner>}
          <Switch
            checked={q.data.captions}
            disabled={!q.data.can_manage || update.isPending}
            onCheckedChange={(on) => save({ captions: on }, on ? "Captions are on" : "Captions are off")}
            label="Live captions"
            info="Each speaker's own browser turns their speech into text. Chrome and Edge send that audio to Google or Microsoft, so each person also chooses whether to caption themselves."
          />
          <Switch
            checked={q.data.transcripts}
            disabled={!q.data.can_manage || update.isPending}
            onCheckedChange={(on) => save({ transcripts: on }, on ? "Hosts can save transcripts" : "Transcripts are off")}
            label="Save transcripts"
            info="Hosts can keep a call's captions as a transcript. Everyone in the call sees a notice, and only people who caption themselves are in it."
          />
          <Switch
            checked={q.data.recording && q.data.recording_available}
            disabled={!q.data.can_manage || !q.data.recording_available || update.isPending}
            onCheckedChange={(on) => save({ recording: on }, on ? "Hosts can record" : "Recording is off")}
            label="Recording"
            info={q.data.recording_available ? "Hosts can record calls. Everyone sees a notice before and while a call is recorded." : "This server isn't set up to record calls."}
          />
        </div>
      )}
    </Dialog>
  );
}
