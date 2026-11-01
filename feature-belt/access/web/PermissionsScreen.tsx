"use client";

import {
  errorMessage,
  orgRoleAtLeast,
  orgsNounA,
  orgsNounTitle,
  topic,
  useAccessMyPermissions,
  useAccessPermissions,
  useAccessResetPermission,
  useAccessSetPermission,
  useActiveOrg,
  useOrgPickerVisible,
  useTopic,
  type AccessDirectoryEntry,
  type AccessOrgRole,
  type AccessPermissionPolicy,
  type AccessPolicySubject,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, Checkbox, Dialog, EmptyState, ErrorState, IconButton, InfoTip, SkeletonGroup, SkeletonRow, toast } from "@scaffoldapp/ui-web";
import { Building2, Check, Minus, Pencil, RotateCcw, ShieldCheck, X } from "lucide-react";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { Screen, Section } from "@/components/shell/PageHeader";
import { OrgSwitcher } from "@/features/orgs";
import { ORG_ROLE_LABEL } from "./labels";
import { PeoplePicker, SubjectAvatar } from "./PeoplePicker";

const ROLES: AccessOrgRole[] = ["owner", "admin", "member"];

function group(key: string): string {
  const f = key.split(".")[0] ?? key;
  return f.charAt(0).toUpperCase() + f.slice(1);
}

/**
 * Settings → Permissions. Org owners and admins see the matrix (permissions × roles, plus
 * teams and people per permission) and change it; everyone else sees what they hold.
 */
export function PermissionsScreen() {
  const { org, orgs, isPending } = useActiveOrg();
  const picker = useOrgPickerVisible();
  useTopic(org ? topic("access", org.id) : null);
  const admin = orgRoleAtLeast(org?.role, "admin");

  if (!isPending && !org) {
    return (
      <Screen title="Permissions">
        <Card padding="compact">
          <EmptyState
            compact
            icon={<Building2 />}
            title={`Join or create ${orgsNounA()} first`}
            action={
              <Button asChild size="auto">
                <Link href="/settings/orgs">{orgsNounTitle("many")}</Link>
              </Button>
            }
          />
        </Card>
      </Screen>
    );
  }
  return (
    <Screen title="Permissions" info={`What members of ${orgsNounA()} may do beyond what is shared with them. Owners and admins decide which roles, teams and people hold each one.`}>
      {orgs.length > 1 && picker ? <OrgSwitcher className="self-start" /> : null}
      {org ? admin ? <Matrix orgId={org.id} /> : <Mine orgId={org.id} orgName={org.name} /> : null}
    </Screen>
  );
}

function Loading() {
  return (
    <SkeletonGroup label="Loading permissions" className="flex flex-col">
      <SkeletonRow />
      <SkeletonRow />
      <SkeletonRow />
    </SkeletonGroup>
  );
}

function Mine({ orgId, orgName }: { orgId: string; orgName: string }) {
  const mine = useAccessMyPermissions(orgId);
  const entries = mine.data?.items ?? [];
  return (
    <Section title={`Yours in ${orgName}`} info="Only owners and admins change these. Ask one if you need something you don't have.">
      <Card padding="none" className="p-1">
        {mine.isPending ? (
          <Loading />
        ) : mine.isError ? (
          <ErrorState compact title="Couldn't load your permissions" info={mine.error.message} onRetry={() => void mine.refetch()} retrying={mine.isFetching} />
        ) : entries.length === 0 ? (
          <EmptyState compact icon={<ShieldCheck />} title="No permissions to show" />
        ) : (
          <ul className="flex flex-col">
            {entries.map((p) => (
              <li key={p.key} className="flex min-h-row items-center gap-3 px-4 py-2">
                <span className="flex min-w-0 flex-1 items-center gap-2 type-body text-fg">
                  <span className="truncate">{p.title}</span>
                  {p.info ? <InfoTip content={p.info} /> : null}
                </span>
                {p.allowed ? (
                  <Badge tone="success" icon={<Check />}>
                    Yes
                  </Badge>
                ) : (
                  <Badge icon={<Minus />}>No</Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </Card>
    </Section>
  );
}

function Matrix({ orgId }: { orgId: string }) {
  const list = useAccessPermissions(orgId);
  const set = useAccessSetPermission(orgId);
  const reset = useAccessResetPermission(orgId);
  const [editing, setEditing] = useState<AccessPermissionPolicy | null>(null);
  const items = list.data ?? [];

  const toggle = (p: AccessPermissionPolicy, role: AccessOrgRole, on: boolean) => {
    const roles = on ? ROLES.filter((r) => r === role || p.roles.includes(r)) : p.roles.filter((r) => r !== role);
    set.mutate(
      { key: p.key, roles, team_ids: p.teams.map((t) => t.id), user_ids: p.users.map((u) => u.id) },
      { onError: (e) => toast.error(errorMessage(e, "Couldn't change the permission. Try again.")) },
    );
  };
  const doReset = (p: AccessPermissionPolicy) =>
    reset.mutate(p.key, { onSuccess: () => toast.success(`“${p.title}” is back to its defaults`), onError: (e) => toast.error(errorMessage(e)) });

  if (list.isPending) return <Card padding="none" className="p-1"><Loading /></Card>;
  if (list.isError) {
    return (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load permissions" info={list.error.message} onRetry={() => void list.refetch()} retrying={list.isFetching} />
      </Card>
    );
  }
  if (items.length === 0) {
    return (
      <Card padding="compact">
        <EmptyState compact icon={<ShieldCheck />} title="No permissions to set" />
      </Card>
    );
  }

  return (
    <>
      {/* Desktop: the matrix. */}
      <Card padding="none" className="hidden overflow-x-auto md:block">
        <table className="w-full border-collapse text-left">
          <thead>
            <tr className="border-b border-border">
              <th scope="col" className="px-4 py-3 type-label font-semibold text-fg">Permission</th>
              {ROLES.map((r) => (
                <th key={r} scope="col" className="w-24 px-2 py-3 text-center type-label font-semibold text-fg">{ORG_ROLE_LABEL[r]}</th>
              ))}
              <th scope="col" className="min-w-64 px-4 py-3 type-label font-semibold text-fg">Teams and people</th>
              <th scope="col" className="w-24 px-2 py-3"><span className="sr-only">Reset</span></th>
            </tr>
          </thead>
          <tbody>
            {items.map((p, i) => (
              <tr key={p.key} className="border-b border-border last:border-b-0 align-middle">
                <th scope="row" className="px-4 py-3 font-normal">
                  <span className="flex flex-col gap-1">
                    {i === 0 || group(items[i - 1]?.key ?? "") !== group(p.key) ? <span className="type-caption text-fg-muted">{group(p.key)}</span> : null}
                    <span className="flex items-center gap-2 type-body text-fg">
                      {p.title}
                      {p.info ? <InfoTip content={p.info} /> : null}
                    </span>
                  </span>
                </th>
                {ROLES.map((r) => (
                  <td key={r} className="px-2 py-3">
                    <span className="flex justify-center">
                      <Checkbox checked={p.roles.includes(r)} onCheckedChange={(on) => toggle(p, r, on)} aria-label={`${ORG_ROLE_LABEL[r]}: ${p.title}`} />
                    </span>
                  </td>
                ))}
                <td className="px-4 py-3">
                  <Overrides p={p} onEdit={() => setEditing(p)} />
                </td>
                <td className="px-2 py-3 text-right">
                  {p.customized ? (
                    <Button variant="ghost" size="sm" leftIcon={<RotateCcw />} onClick={() => doReset(p)} aria-label={`Reset ${p.title} to its defaults`}>
                      Reset
                    </Button>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {/* Phones: one card per permission. */}
      <div className="flex flex-col gap-4 md:hidden">
        {items.map((p) => (
          <Card key={p.key} padding="compact" className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 type-body font-medium text-fg">
                {p.title} {p.info ? <InfoTip content={p.info} /> : null}
              </span>
              {p.customized ? (
                <IconButton label={`Reset ${p.title} to its defaults`} icon={<RotateCcw />} variant="ghost" size="sm" onClick={() => doReset(p)} />
              ) : null}
            </div>
            <div className="grid grid-cols-3 gap-2">
              {ROLES.map((r) => (
                <Checkbox key={r} checked={p.roles.includes(r)} onCheckedChange={(on) => toggle(p, r, on)} label={ORG_ROLE_LABEL[r]} />
              ))}
            </div>
            <Overrides p={p} onEdit={() => setEditing(p)} />
          </Card>
        ))}
      </div>

      <OverridesDialog orgId={orgId} policy={editing} onClose={() => setEditing(null)} />
    </>
  );
}

function Overrides({ p, onEdit }: { p: AccessPermissionPolicy; onEdit: () => void }) {
  const all: AccessPolicySubject[] = [...p.teams, ...p.users];
  const shown = all.slice(0, 3);
  return (
    <span className="flex flex-wrap items-center gap-2">
      {all.length === 0 ? <span className="type-label text-fg-muted">None</span> : null}
      {shown.map((s) => (
        <span key={s.id} className="flex h-control-sm items-center gap-1.5 rounded-pill bg-sunken pr-3 pl-1 type-label text-fg">
          <SubjectAvatar type={s.type} name={s.name} src={s.avatar_url} size="xs" />
          <span className="max-w-32 truncate">{s.type === "team" ? `@${s.handle}` : s.name}</span>
        </span>
      ))}
      {all.length > shown.length ? <span className="type-label text-fg-muted">{`+${all.length - shown.length}`}</span> : null}
      <IconButton label={`Edit teams and people for ${p.title}`} icon={<Pencil />} variant="ghost" size="sm" onClick={onEdit} />
    </span>
  );
}

function OverridesDialog({ orgId, policy, onClose }: { orgId: string; policy: AccessPermissionPolicy | null; onClose: () => void }) {
  const set = useAccessSetPermission(orgId);
  const [chosen, setChosen] = useState<AccessPolicySubject[]>([]);
  useEffect(() => {
    if (policy) setChosen([...policy.teams, ...policy.users]);
  }, [policy]);
  const hidden = useMemo(() => new Set(chosen.map((c) => c.id)), [chosen]);
  const add = (e: AccessDirectoryEntry) => setChosen((c) => [...c, { type: e.type, id: e.id, name: e.name, handle: e.handle, avatar_url: e.avatar_url }]);
  const save = () => {
    if (!policy) return;
    set.mutate(
      {
        key: policy.key,
        roles: policy.roles,
        team_ids: chosen.filter((c) => c.type === "team").map((c) => c.id),
        user_ids: chosen.filter((c) => c.type === "user").map((c) => c.id),
      },
      {
        onSuccess: () => {
          toast.success("Saved");
          onClose();
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")),
      },
    );
  };
  return (
    <Dialog
      open={policy !== null}
      onOpenChange={(o) => !o && !set.isPending && onClose()}
      title={policy ? `Who else holds “${policy.title}”` : ""}
      info="Teams and people here hold it in addition to the roles you ticked."
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={set.isPending} onClick={onClose}>
            Cancel
          </Button>
          <Button size="auto" loading={set.isPending} onClick={save}>
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <PeoplePicker orgId={orgId} exclude={hidden} onPick={add} autoFocus />
        {chosen.length === 0 ? (
          <p className="type-label text-fg-muted">Only the roles you ticked hold it.</p>
        ) : (
          <ul className="flex flex-col">
            {chosen.map((c) => (
              <li key={c.id} className="flex min-h-row items-center gap-3 py-1">
                <SubjectAvatar type={c.type} name={c.name} src={c.avatar_url} />
                <span className="min-w-0 flex-1 truncate type-label text-fg">{c.type === "team" ? `${c.name} (@${c.handle})` : c.name}</span>
                <IconButton label={`Remove ${c.name}`} icon={<X />} variant="ghost" size="sm" onClick={() => setChosen((all) => all.filter((x) => x.id !== c.id))} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </Dialog>
  );
}
