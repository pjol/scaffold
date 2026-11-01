"use client";

import {
  CALLS_ACCESS_HELP,
  CALLS_ACCESS_LABELS,
  callsZoneLabel,
  errorMessage,
  fromCallsLocalInput,
  orgsNounTitle,
  toCallsLocalInput,
  useActiveOrg,
  useCallsTimeZone,
  useCreateCallsMeeting,
  useStartCallsCall,
  useUpdateCallsMeeting,
  type CallsAccess,
  type CallsMeeting,
  type CallsUser,
} from "@scaffoldapp/shared";
import { Banner, Button, Dialog, Field, IconButton, InfoTip, Input, Select, Switch, toast } from "@scaffoldapp/ui-web";
import { Copy, Phone } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { CallsPeoplePicker } from "./PeoplePicker";

/** Copy a meeting link and say so. */
export async function copyMeetingLink(url: string) {
  try {
    await navigator.clipboard.writeText(url);
    toast.success("Link copied");
  } catch {
    toast.error("Couldn't copy. Select the link and copy it.");
  }
}

/** The link to a meeting with a Copy button. */
export function MeetingLinkField({ meeting }: { meeting: Pick<CallsMeeting, "join_url" | "code"> }) {
  return (
    <Field label="Meeting link" info={`People can also join with the code ${meeting.code}.`}>
      <Input
        readOnly
        value={meeting.join_url}
        onFocus={(e) => e.currentTarget.select()}
        trailing={<IconButton label="Copy link" icon={<Copy />} variant="ghost" size="sm" onClick={() => void copyMeetingLink(meeting.join_url)} />}
      />
    </Field>
  );
}

/** Shows a new meeting's link to share. */
export function MeetingLinkDialog({ meeting, onClose }: { meeting: CallsMeeting | null; onClose: () => void }) {
  const router = useRouter();
  return (
    <Dialog
      open={!!meeting}
      onOpenChange={(o) => !o && onClose()}
      title="Your meeting is ready"
      info="Share the link with the people you want in the meeting. It keeps working until you delete it."
      footer={
        meeting ? (
          <>
            <Button variant="ghost" onClick={onClose}>
              Done
            </Button>
            <Button onClick={() => router.push(`/meet/${meeting.code}`)}>Join now</Button>
          </>
        ) : null
      }
    >
      {meeting ? <MeetingLinkField meeting={meeting} /> : null}
    </Dialog>
  );
}

const ACCESS_OPTIONS = (hasOrg: boolean) =>
  (["open", "invited", "org"] as CallsAccess[]).filter((a) => a !== "org" || hasOrg).map((a) => ({ value: a, label: CALLS_ACCESS_LABELS[a] }));

/** Schedule a meeting, or edit one you host (`meeting`). */
export function ScheduleMeetingDialog({
  open,
  onOpenChange,
  meeting,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  meeting?: CallsMeeting | null;
  onSaved?: (m: CallsMeeting) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={meeting ? "Edit meeting" : "Schedule a meeting"} size="lg">
      {open ? <ScheduleForm meeting={meeting ?? null} onDone={(m) => (onOpenChange(false), onSaved?.(m))} onCancel={() => onOpenChange(false)} /> : null}
    </Dialog>
  );
}

function defaultStart(): Date {
  const d = new Date(Date.now() + 60 * 60 * 1000);
  d.setMinutes(d.getMinutes() < 30 ? 30 : 60, 0, 0);
  return d;
}

function ScheduleForm({ meeting, onDone, onCancel }: { meeting: CallsMeeting | null; onDone: (m: CallsMeeting) => void; onCancel: () => void }) {
  const { org, orgs } = useActiveOrg();
  const create = useCreateCallsMeeting();
  const update = useUpdateCallsMeeting(meeting?.id ?? "");
  // Times are entered in Calls' zone (setCallsTimeZone), named when it isn't the device's.
  const zone = useCallsTimeZone();
  const zoneLabel = callsZoneLabel(zone);
  const start0 = meeting?.starts_at ? new Date(meeting.starts_at) : defaultStart();
  const end0 = meeting?.ends_at ? new Date(meeting.ends_at) : new Date(start0.getTime() + 30 * 60 * 1000);
  const [title, setTitle] = useState(meeting?.title ?? "");
  const [start, setStart] = useState(toCallsLocalInput(start0.toISOString(), zone));
  const [end, setEnd] = useState(toCallsLocalInput(end0.toISOString(), zone));
  const [orgId, setOrgId] = useState<string>(meeting ? (meeting.org_id ?? "") : (org?.id ?? ""));
  const [access, setAccess] = useState<CallsAccess>(meeting?.access ?? (org ? "org" : "invited"));
  const [lobby, setLobby] = useState(meeting?.lobby_enabled ?? true);
  const [people, setPeople] = useState<CallsUser[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const pending = create.isPending || update.isPending;

  const submit = () => {
    setProblem(null);
    const startsAt = fromCallsLocalInput(start, zone);
    const endsAt = fromCallsLocalInput(end, zone);
    if (!startsAt) return setProblem("Choose when the meeting starts.");
    if (endsAt && endsAt <= startsAt) return setProblem("The meeting must end after it starts.");
    const fail = (e: unknown) => setProblem(errorMessage(e, "Couldn't save the meeting. Try again."));
    if (meeting) {
      update.mutate(
        { title, access, lobby_enabled: lobby, starts_at: startsAt, ...(endsAt ? { ends_at: endsAt } : {}) },
        { onSuccess: (m) => (toast.success("Meeting updated"), onDone(m)), onError: fail },
      );
      return;
    }
    create.mutate(
      { title, org_id: orgId || null, access, lobby_enabled: lobby, starts_at: startsAt, ends_at: endsAt, invitee_ids: people.map((p) => p.id) },
      { onSuccess: (m) => (toast.success("Meeting scheduled"), onDone(m)), onError: fail },
    );
  };

  return (
    <form
      className="flex flex-col gap-6"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="flex flex-col gap-4">
        <Field label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Weekly sync" maxLength={200} autoFocus />
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={zoneLabel ? `Starts (${zoneLabel})` : "Starts"} required>
            <Input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} required />
          </Field>
          <Field label={zoneLabel ? `Ends (${zoneLabel})` : "Ends"}>
            <Input type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} />
          </Field>
        </div>
        {!meeting && orgs.length > 0 ? (
              <Field label={orgsNounTitle()} info={`${orgsNounTitle()} members can join without asking.`}>
                <Select
                  value={orgId || "none"}
                  onValueChange={(v) => {
                    const next = v === "none" ? "" : v;
                    setOrgId(next);
                    if (!next && access === "org") setAccess("invited");
                  }}
                  options={[{ value: "none", label: "None" }, ...orgs.map((o) => ({ value: o.id, label: o.name }))]}
                />
              </Field>
        ) : null}
        <Field label="Who can join" info={CALLS_ACCESS_HELP[access]}>
          <Select value={access} onValueChange={(v) => setAccess(v as CallsAccess)} options={ACCESS_OPTIONS(!!(meeting ? meeting.org_id : orgId))} />
        </Field>
        {access !== "org" && (
          <Switch
            checked={lobby}
            onCheckedChange={setLobby}
            label="Ask to join"
            info={access === "open" ? "On: people outside the meeting, guests too, wait until a host lets them in. Off: anyone with the link joins." : "On: people who weren't invited can ask; a host lets them in."}
          />
        )}
        {!meeting && (
          <Field label="Invite people" info="They get a notification with the link.">
            <CallsPeoplePicker selected={people} onChange={setPeople} orgId={orgId || null} max={50} disabled={pending} />
          </Field>
        )}
      </div>
      {problem && <Banner tone="danger">{problem}</Banner>}
      <div className="flex justify-end gap-3">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" loading={pending}>
          {meeting ? "Save" : "Schedule"}
        </Button>
      </div>
    </form>
  );
}

/**
 * Start call: ring the people picked (their devices ring; people who don't answer get a
 * missed-call notification), or with nobody picked, start a call alone and share its link.
 */
export function StartCallDialog({ open, onOpenChange, initial = [] }: { open: boolean; onOpenChange: (open: boolean) => void; initial?: CallsUser[] }) {
  const router = useRouter();
  const { org } = useActiveOrg();
  const start = useStartCallsCall();
  const create = useCreateCallsMeeting();
  const [people, setPeople] = useState<CallsUser[]>(initial);
  const [problem, setProblem] = useState<string | null>(null);
  const pending = start.isPending || create.isPending;

  const go = (code: string) => {
    onOpenChange(false);
    setPeople([]);
    router.push(`/meet/${code}?autojoin=1`);
  };
  const call = () => {
    setProblem(null);
    const onError = (e: unknown) => setProblem(errorMessage(e, "Couldn't start the call. Try again."));
    if (people.length === 0) create.mutate({ access: "open", org_id: org?.id ?? null }, { onSuccess: (m) => go(m.code), onError });
    else start.mutate({ user_ids: people.map((p) => p.id), org_id: org?.id ?? null }, { onSuccess: (m) => go(m.code), onError });
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        <span className="flex items-center gap-2">
          Start call
          <InfoTip content="The people you pick are rung for 45 seconds; people who don't answer get a missed-call notification. Pick nobody to start alone and share the link." />
        </span>
      }
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button leftIcon={<Phone />} onClick={call} loading={pending}>
            {people.length === 0 ? "Start call" : people.length === 1 ? `Call ${people[0]?.display_name ?? ""}` : `Call ${people.length} people`}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <CallsPeoplePicker selected={people} onChange={setPeople} disabled={pending} />
        {problem && <Banner tone="danger">{problem}</Banner>}
      </div>
    </Dialog>
  );
}
