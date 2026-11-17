"use client";

import {
  CALENDAR_SHARE_LEVEL_LABEL,
  errorMessage,
  orgsNoun,
  useAccessAddGrants,
  useAccessRemoveGrant,
  useAccessResource,
  useAccessSetGeneral,
  useAccessUpdateGrant,
  useActiveOrgId,
  type AccessDirectoryEntry,
  type AccessLevel,
  type CalendarCalendar,
} from "@scaffoldapp/shared";
import { Button, Dialog, ErrorState, IconButton, Select, Skeleton, toast } from "@scaffoldapp/ui-web";
import { X } from "lucide-react";
import { useMemo, useState } from "react";
import { PeoplePicker, SubjectAvatar } from "@/features/access";

const LEVELS: AccessLevel[] = ["view", "comment", "edit", "manage"];

/** Room and resource levels in the words people book with. */
export const CALENDAR_ROOM_LEVEL_LABEL: Record<AccessLevel, string> = { view: "See when it's free", comment: "See when it's free", edit: "Book it", manage: "Book and manage it" };

/**
 * Who can see and change a calendar, in calendar words: free/busy only, all details, make
 * changes, make changes and manage sharing. The grants are access's (one permission model);
 * this dialog only names the levels the way calendars do. Colleagues always see the free/busy
 * of a primary calendar for scheduling.
 */
export function CalendarShareDialog({ calendar, open, onOpenChange }: { calendar: CalendarCalendar; open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <LevelShareDialog
      kind="calendar.calendar"
      id={calendar.id}
      title={calendar.name}
      orgId={calendar.org_id}
      labels={CALENDAR_SHARE_LEVEL_LABEL}
      initial="comment"
      info="People you add see this calendar at the level you pick and can add it to their list. AI agents acting for them get at most that level, and never share it."
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}

/** Who may see and book a room or resource (access levels in booking words). */
export function RoomShareDialog({ room, open, onOpenChange }: { room: { id: string; name: string; org_id: string }; open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <LevelShareDialog
      kind="calendar.room"
      id={room.id}
      title={room.name}
      orgId={room.org_id}
      labels={CALENDAR_ROOM_LEVEL_LABEL}
      initial="edit"
      info="Everyone in the workspace books new rooms. Restrict it to teams or people here; admins always manage rooms."
      open={open}
      onOpenChange={onOpenChange}
    />
  );
}

function LevelShareDialog({
  kind,
  id,
  title,
  orgId: ownOrg,
  labels,
  initial,
  info,
  open,
  onOpenChange,
}: {
  kind: string;
  id: string;
  title: string;
  orgId: string | null;
  labels: Record<AccessLevel, string>;
  initial: AccessLevel;
  info: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const levelOptions = (levels: AccessLevel[]) => levels.map((l) => ({ value: l, label: labels[l] }));
  const sharing = useAccessResource(kind, open ? id : null);
  const add = useAccessAddGrants(kind, id);
  const update = useAccessUpdateGrant(kind, id);
  const remove = useAccessRemoveGrant(kind, id);
  const general = useAccessSetGeneral(kind, id);
  const activeOrg = useActiveOrgId();
  const [level, setLevel] = useState<AccessLevel>(initial);
  const data = sharing.data;
  const orgId = data?.org_id ?? ownOrg ?? activeOrg;
  const grantable = data?.levels.length ? data.levels : LEVELS;
  const existing = useMemo(() => new Set((data?.grants ?? []).map((g) => g.subject.id)), [data]);
  const fail = (e: unknown) => toast.error(errorMessage(e, "Couldn't change sharing. Try again."));

  const share = (e: AccessDirectoryEntry) =>
    add.mutate(
      { subjects: [{ type: e.type, id: e.id }], level },
      { onSuccess: () => toast.success(`Shared with ${e.name}`), onError: fail },
    );

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Share “${title}”`}
      info={info}
      size="md"
      footer={
        <Button size="auto" onClick={() => onOpenChange(false)}>
          Done
        </Button>
      }
    >
      {sharing.isPending ? (
        <div className="flex flex-col gap-3" aria-busy>
          <Skeleton className="h-10" />
          <Skeleton className="h-12" />
          <Skeleton className="h-12" />
        </div>
      ) : sharing.isError ? (
        <ErrorState title="Couldn't load sharing" info={sharing.error.message} onRetry={() => void sharing.refetch()} retrying={sharing.isFetching} />
      ) : data && !data.can_manage ? (
        <p className="type-body text-fg-muted">You can {labels[data.my_level].toLowerCase()}. Only people who manage sharing change who has access.</p>
      ) : data ? (
        <div className="flex flex-col gap-6">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1">
              {orgId ? (
                <PeoplePicker orgId={orgId} types={data.org_id ? "user,team" : "user"} exclude={existing} onPick={share} label="Add people or teams" />
              ) : (
                <p className="type-caption text-fg-muted">Join a {orgsNoun()} to share this calendar.</p>
              )}
            </div>
            <Select aria-label="Level for people you add" className="sm:w-64" value={level} onValueChange={(v) => setLevel(v as AccessLevel)} options={levelOptions(grantable)} />
          </div>

          <section aria-label="People with access" className="flex flex-col gap-1">
            <h3 className="type-label font-semibold text-fg-muted">People with access</h3>
            <ul className="flex flex-col">
              {data.owner ? (
                <li className="flex min-h-control items-center gap-3 py-1">
                  <SubjectAvatar type="user" name={data.owner.name} src={data.owner.avatar_url} />
                  <span className="min-w-0 flex-1 truncate type-body text-fg">{data.owner.name}</span>
                  <span className="type-caption text-fg-muted">Owner</span>
                </li>
              ) : null}
              {data.grants
                .filter((g) => g.subject.type !== "org")
                .map((g) => (
                  <li key={g.id} className="flex min-h-control items-center gap-3 py-1">
                    <SubjectAvatar type={g.subject.type} name={g.subject.name} src={g.subject.avatar_url} />
                    <span className="min-w-0 flex-1 truncate type-body text-fg">
                      {g.subject.name}
                      {g.subject.type === "team" && g.subject.member_count != null ? <span className="text-fg-muted"> · {g.subject.member_count} people</span> : null}
                    </span>
                    {g.inherited_from ? (
                      <span className="type-caption text-fg-muted">{labels[g.level]}</span>
                    ) : (
                      <>
                        <Select
                          aria-label={`${g.subject.name}'s level`}
                          className="w-56"
                          value={g.level}
                          onValueChange={(v) => update.mutate({ grantId: g.id, level: v as AccessLevel }, { onError: fail })}
                          options={levelOptions(grantable)}
                        />
                        <IconButton
                          variant="ghost"
                          size="sm"
                          label={`Remove ${g.subject.name}`}
                          icon={<X />}
                          onClick={() => remove.mutate(g.id, { onSuccess: () => toast.success(`${g.subject.name} removed`), onError: fail })}
                        />
                      </>
                    )}
                  </li>
                ))}
            </ul>
            {data.grants.filter((g) => g.subject.type !== "org").length === 0 ? <p className="type-caption text-fg-muted">Nobody else yet.</p> : null}
          </section>

          {data.org_id ? (
            <section aria-label={`Everyone in the ${orgsNoun()}`} className="flex flex-col gap-2">
              <h3 className="type-label font-semibold text-fg-muted">Everyone in the {orgsNoun()}</h3>
              <Select
                aria-label={`What everyone in the ${orgsNoun()} can do`}
                value={data.general.access === "org" && data.general.own_level ? data.general.own_level : "restricted"}
                onValueChange={(v) =>
                  general.mutate(v === "restricted" ? { access: "restricted" } : { access: "org", level: v as Exclude<AccessLevel, "manage"> }, {
                    onSuccess: () => toast.success("Sharing updated"),
                    onError: fail,
                  })
                }
                options={[
                  { value: "restricted", label: "Only people added above" },
                  ...levelOptions(grantable.filter((l) => l !== "manage")),
                ]}
              />
            </section>
          ) : (
            <p className="type-caption text-fg-muted">This isn&apos;t in a {orgsNoun()}, so it&apos;s shared with people one by one.</p>
          )}
        </div>
      ) : null}
    </Dialog>
  );
}
