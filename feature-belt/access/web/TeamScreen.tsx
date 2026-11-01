"use client";

import {
  errorMessage,
  hasErrorCode,
  topic,
  useAccessAddTeamMembers,
  useAccessDeleteTeam,
  useAccessRemoveTeamMember,
  useAccessTeam,
  useAccessTeamMembers,
  useAccessUpdateTeam,
  useAccessUpdateTeamMember,
  useMe,
  useTopic,
  useUpload,
  ACCESS_TEAM_AVATAR_PURPOSE,
  type AccessDirectoryEntry,
  type AccessTeam,
  type AccessTeamMember,
  type AccessTeamRole,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Select,
  Skeleton,
  SkeletonGroup,
  SkeletonRow,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { Camera, ChevronLeft, LogOut, Pencil, Trash2, UserMinus, UserPlus, Users, X, Crown, UserRound } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { Section } from "@/components/shell/PageHeader";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import type { ResourceMenuAction } from "@/features/registry";
import { TEAM_ROLE_LABEL, TEAM_ROLE_TONE, memberCount } from "./labels";
import { PeoplePicker, SubjectAvatar } from "./PeoplePicker";

const PHOTO_TYPES = ["image/jpeg", "image/png", "image/webp"];
const PHOTO_MAX = 5 * 1024 * 1024;
const HANDLE = /^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])$/;

/** Settings → Teams → one team: description, members and leads; team details for people who manage teams. */
export function TeamScreen({ teamId }: { teamId: string }) {
  const router = useRouter();
  const me = useMe();
  const team = useAccessTeam(teamId);
  const leave = useAccessRemoveTeamMember(teamId);
  const del = useAccessDeleteTeam();
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"leave" | "delete" | null>(null);
  useTopic(team.data ? topic("access", team.data.org_id) : null);

  if (team.isPending) {
    return (
      <SkeletonGroup label="Loading team" className="flex flex-col gap-section">
        <div className="flex items-center gap-4">
          <Skeleton shape="circle" className="size-14" />
          <Skeleton className="h-8 w-56" />
        </div>
        <Skeleton className="h-24" />
        <Skeleton className="h-48" />
      </SkeletonGroup>
    );
  }
  if (team.isError) {
    return (
      <Card padding="compact">
        {team.error.status === 404 ? (
          <EmptyState
            compact
            icon={<Users />}
            title="Team not found"
            action={
              <Button asChild size="auto" variant="secondary" leftIcon={<ChevronLeft />}>
                <Link href="/settings/teams">Teams</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState compact title="Couldn't load this team" info={team.error.message} onRetry={() => void team.refetch()} retrying={team.isFetching} />
        )}
      </Card>
    );
  }

  const t = team.data;
  const actions: ResourceMenuAction[] = [
    ...(t.can_manage ? [{ id: "edit", label: "Edit details", icon: Pencil, onSelect: () => setEditing(true) }] : []),
    ...(t.my_role ? [{ id: "leave", label: "Leave team", icon: LogOut, destructive: true, onSelect: () => setConfirm("leave") }] : []),
    ...(t.can_manage ? [{ id: "delete", label: "Delete team", icon: Trash2, destructive: true, onSelect: () => setConfirm("delete") }] : []),
  ];

  return (
    <div className="flex flex-col gap-section">
      <div className="flex min-h-control-lg items-center gap-4">
        <SubjectAvatar type="team" name={t.name} src={t.avatar_url} size="lg" />
        <h1 className="min-w-0 flex-1 break-words type-title text-fg">
          {t.name} <span className="type-body text-fg-muted">@{t.handle}</span>
        </h1>
        {t.my_role ? <Badge tone={TEAM_ROLE_TONE[t.my_role]}>{TEAM_ROLE_LABEL[t.my_role]}</Badge> : null}
        <ResourceMenu feature="access" title={t.name} actions={actions} label={`Actions for @${t.handle}`} />
      </div>

      <About team={t} />
      <Members team={t} meId={me.data?.id} />

      {t.can_manage ? <EditTeamDialog team={t} open={editing} onOpenChange={setEditing} /> : null}
      <ConfirmDialog
        open={confirm === "leave"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Leave @${t.handle}?`}
        tone="danger"
        confirmLabel="Leave"
        errorText={(e) => errorMessage(e, "Couldn't leave the team. Try again.")}
        onConfirm={async () => {
          if (!me.data) return;
          await leave.mutateAsync(me.data.id);
          toast.success(`You left @${t.handle}`);
        }}
      >
        <p>You lose what was shared with the team. A lead can add you again.</p>
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === "delete"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={`Delete @${t.handle}?`}
        tone="danger"
        confirmLabel="Delete team"
        requireText={t.handle}
        errorText={(e) => errorMessage(e, "Couldn't delete the team. Try again.")}
        onConfirm={async () => {
          await del.mutateAsync({ teamId: t.id, orgId: t.org_id });
          toast.success(`@${t.handle} deleted`);
          router.push("/settings/teams");
        }}
      >
        <p>{`Its ${memberCount(t.member_count)} lose everything shared with the team, and permissions given to it end. It can't be undone.`}</p>
      </ConfirmDialog>
    </div>
  );
}

function About({ team: t }: { team: AccessTeam }) {
  const update = useAccessUpdateTeam(t.id);
  const [text, setText] = useState(t.description);
  useEffect(() => setText(t.description), [t.description]);
  if (!t.can_manage_members) {
    return t.description ? <p className="max-w-prose type-body text-fg">{t.description}</p> : null;
  }
  const dirty = text.trim() !== t.description;
  return (
    <Section title="About">
      <form
        className="flex max-w-form flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          update.mutate({ description: text.trim() }, { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) });
        }}
      >
        <TextArea aria-label="Description" value={text} onChange={(e) => setText(e.target.value)} maxLength={280} minRows={2} placeholder="What the team does" />
        <Button type="submit" size="auto" className="self-start" disabled={!dirty} loading={update.isPending}>
          Save
        </Button>
      </form>
    </Section>
  );
}

function Members({ team: t, meId }: { team: AccessTeam; meId: string | undefined }) {
  const members = useAccessTeamMembers(t.id);
  const update = useAccessUpdateTeamMember(t.id);
  const remove = useAccessRemoveTeamMember(t.id);
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<AccessTeamMember | null>(null);
  const rows = members.data?.items ?? [];

  const setRole = (m: AccessTeamMember, role: AccessTeamRole) =>
    update.mutate(
      { userId: m.user_id, role },
      { onSuccess: () => toast.success(role === "lead" ? `${m.display_name} is a lead` : `${m.display_name} is a member`), onError: (e) => toast.error(errorMessage(e)) },
    );

  return (
    <Section
      title={`Members · ${t.member_count}`}
      info="Leads add and remove members. People who manage teams can change any team."
      action={
        t.can_manage_members ? (
          <Button variant="secondary" size="auto" leftIcon={<UserPlus />} onClick={() => setAdding(true)}>
            Add people
          </Button>
        ) : undefined
      }
    >
      <Card padding="none" className="p-1">
        {members.isPending ? (
          <SkeletonGroup label="Loading members" className="flex flex-col">
            <SkeletonRow />
            <SkeletonRow />
          </SkeletonGroup>
        ) : members.isError ? (
          <ErrorState compact title="Couldn't load members" info={members.error.message} onRetry={() => void members.refetch()} retrying={members.isFetching} />
        ) : rows.length === 0 ? (
          <EmptyState
            compact
            icon={<Users />}
            title="No members yet"
            {...(t.can_manage_members
              ? {
                  action: (
                    <Button size="auto" leftIcon={<UserPlus />} onClick={() => setAdding(true)}>
                      Add people
                    </Button>
                  ),
                }
              : {})}
          />
        ) : (
          <ul className="flex flex-col">
            {rows.map((m) => {
              const self = m.user_id === meId;
              const actions: ResourceMenuAction[] =
                t.can_manage_members && !self
                  ? [
                      m.role === "lead"
                        ? { id: "member", label: "Make member", icon: UserRound, onSelect: () => setRole(m, "member") }
                        : { id: "lead", label: "Make lead", icon: Crown, onSelect: () => setRole(m, "lead") },
                      { id: "remove", label: "Remove from team", icon: UserMinus, destructive: true, onSelect: () => setRemoving(m) },
                    ]
                  : [];
              return (
                <li key={m.user_id} className="flex min-h-tap-lg items-center gap-3 rounded-md px-4 py-2">
                  <AppAvatar name={m.display_name || "Member"} src={m.avatar_url} size="md" />
                  <span className="flex min-w-0 flex-1 items-baseline gap-2">
                    <span className="truncate type-body font-medium text-fg">{self ? `${m.display_name} (you)` : m.display_name}</span>
                    {m.email ? <span className="hidden truncate type-caption text-fg-muted md:inline">{m.email}</span> : null}
                  </span>
                  <Badge tone={TEAM_ROLE_TONE[m.role]}>{TEAM_ROLE_LABEL[m.role]}</Badge>
                  {actions.length > 0 ? (
                    <ResourceMenu feature="access" title={m.display_name} actions={actions} label={`Actions for ${m.display_name}`} />
                  ) : (
                    <span className="block size-control-sm shrink-0" aria-hidden />
                  )}
                </li>
              );
            })}
          </ul>
        )}
        <LoadMore hasNextPage={members.hasNextPage} isFetchingNextPage={members.isFetchingNextPage} fetchNextPage={members.fetchNextPage} />
      </Card>
      <AddMembersDialog team={t} existing={rows} open={adding} onOpenChange={setAdding} />
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.display_name ?? ""} from @${t.handle}?`}
        tone="danger"
        confirmLabel="Remove"
        errorText={(e) => errorMessage(e, "Couldn't remove them. Try again.")}
        onConfirm={async () => {
          if (!removing) return;
          await remove.mutateAsync(removing.user_id);
          toast.success(`${removing.display_name} removed`);
        }}
      >
        <p>They lose what was shared with the team right away.</p>
      </ConfirmDialog>
    </Section>
  );
}

function AddMembersDialog({ team: t, existing, open, onOpenChange }: { team: AccessTeam; existing: AccessTeamMember[]; open: boolean; onOpenChange: (open: boolean) => void }) {
  const add = useAccessAddTeamMembers(t.id);
  const [picked, setPicked] = useState<AccessDirectoryEntry[]>([]);
  const [role, setRole] = useState<AccessTeamRole>("member");
  useEffect(() => {
    if (open) {
      setPicked([]);
      setRole("member");
    }
  }, [open]);
  const hidden = useMemo(() => new Set([...existing.map((m) => m.user_id), ...picked.map((p) => p.id)]), [existing, picked]);
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !add.isPending && onOpenChange(o)}
      title={`Add people to @${t.handle}`}
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={add.isPending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            size="auto"
            disabled={picked.length === 0}
            loading={add.isPending}
            onClick={() =>
              add.mutate(
                { userIds: picked.map((p) => p.id), role },
                {
                  onSuccess: (r) => {
                    toast.success(r.added === 1 ? "1 person added" : `${r.added} people added`);
                    onOpenChange(false);
                  },
                  onError: (e) => toast.error(errorMessage(e, "Couldn't add them. Try again.")),
                },
              )
            }
          >
            Add
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <PeoplePicker orgId={t.org_id} types="user" exclude={hidden} label="Add people" onPick={(e) => setPicked((p) => [...p, e])} autoFocus />
        {picked.length > 0 ? (
          <ul className="flex flex-wrap gap-2" aria-label="Chosen">
            {picked.map((p) => (
              <li key={p.id} className="flex h-control-sm items-center gap-1.5 rounded-pill bg-sunken pl-1 type-label text-fg">
                <SubjectAvatar type="user" name={p.name} src={p.avatar_url} size="xs" />
                <span className="max-w-40 truncate">{p.name}</span>
                <IconButton label={`Remove ${p.name}`} icon={<X />} variant="ghost" size="sm" onClick={() => setPicked((all) => all.filter((x) => x.id !== p.id))} />
              </li>
            ))}
          </ul>
        ) : null}
        <Field label="Role" info="Leads can add and remove members.">
          <Select value={role} onValueChange={(v) => setRole(v as AccessTeamRole)} options={[{ value: "member", label: "Member" }, { value: "lead", label: "Lead" }]} />
        </Field>
      </div>
    </Dialog>
  );
}

function EditTeamDialog({ team: t, open, onOpenChange }: { team: AccessTeam; open: boolean; onOpenChange: (open: boolean) => void }) {
  const update = useAccessUpdateTeam(t.id);
  const upload = useUpload();
  const input = useRef<HTMLInputElement>(null);
  const [name, setName] = useState(t.name);
  const [handle, setHandle] = useState(t.handle);
  const [errors, setErrors] = useState<{ name?: string; handle?: string }>({});
  useEffect(() => {
    if (!open) return;
    setName(t.name);
    setHandle(t.handle);
    setErrors({});
  }, [open, t.name, t.handle]);

  const onFile = async (file: File | undefined) => {
    if (!file) return;
    if (!PHOTO_TYPES.includes(file.type)) return void toast.error("Choose a JPEG, PNG or WebP image.");
    if (file.size > PHOTO_MAX) return void toast.error("That picture is over 5 MB. Choose a smaller one.");
    try {
      const r = await upload.mutateAsync({ file, purpose: ACCESS_TEAM_AVATAR_PURPOSE, maxBytes: PHOTO_MAX });
      await update.mutateAsync({ avatar_key: r.key });
      toast.success("Picture updated");
    } catch (e) {
      toast.error(errorMessage(e, "Couldn't update the picture. Try again."));
    }
  };

  const save = async () => {
    const n = name.trim();
    const h = handle.trim().replace(/^@/, "").toLowerCase();
    const next: typeof errors = {};
    if (!n) next.name = "Enter a name.";
    if (!HANDLE.test(h) || h.includes("--")) next.handle = "Use 2 to 32 lowercase letters, numbers or single hyphens.";
    setErrors(next);
    if (next.name || next.handle) return;
    try {
      await update.mutateAsync({ ...(n !== t.name ? { name: n } : {}), ...(h !== t.handle ? { handle: h } : {}) });
      toast.success("Saved");
      onOpenChange(false);
    } catch (e) {
      if (hasErrorCode(e, "conflict")) setErrors({ handle: errorMessage(e) });
      else toast.error(errorMessage(e, "Couldn't save. Try again."));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !update.isPending && onOpenChange(o)}
      title="Edit team"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={update.isPending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={update.isPending && !upload.isPending} onClick={() => void save()}>
            Save
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <div className="flex flex-wrap items-center gap-4">
          <AppAvatar name={t.name} src={t.avatar_url} size="lg" />
          <Button variant="secondary" size="auto" leftIcon={<Camera />} loading={upload.isPending} onClick={() => input.current?.click()}>
            {t.avatar_url ? "Change picture" : "Add picture"}
          </Button>
          {t.avatar_url ? (
            <Button variant="ghost" size="auto" onClick={() => update.mutate({ avatar_key: "" }, { onError: (e) => toast.error(errorMessage(e)) })}>
              Remove
            </Button>
          ) : null}
          <input
            ref={input}
            type="file"
            accept={PHOTO_TYPES.join(",")}
            hidden
            onChange={(e) => {
              void onFile(e.target.files?.[0]);
              e.target.value = "";
            }}
          />
        </div>
        <Field label="Name" error={errors.name} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} />
        </Field>
        <Field label="Handle" info="Changing it breaks mentions people already wrote with the old handle." error={errors.handle} required>
          <Input value={handle} onChange={(e) => setHandle(e.target.value.toLowerCase())} maxLength={33} autoCapitalize="none" spellCheck={false} />
        </Field>
      </form>
    </Dialog>
  );
}
