"use client";

import { ACCESS_PERMISSION_MANAGE_TEAMS, errorMessage, hasErrorCode, orgsNounA, orgsNounTitle, topic, useAccessCreateTeam, useAccessPermission, useAccessTeams, useActiveOrg, useOrgPickerVisible, useTopic } from "@scaffoldapp/shared";
import { Badge, Button, Card, Dialog, EmptyState, ErrorState, Field, Input, ListRow, SkeletonGroup, SkeletonRow, Tab, Tabs, TabsList, TextArea, toast } from "@scaffoldapp/ui-web";
import { Building2, Plus, Search, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { OrgSwitcher } from "@/features/orgs";
import { useUrlState } from "@/lib/url-state";
import { TEAM_ROLE_LABEL, TEAM_ROLE_TONE, memberCount } from "./labels";
import { SubjectAvatar } from "./PeoplePicker";

const HANDLE = /^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])$/;

/** Settings → Teams: the active org's teams (all or yours), search, and New team for people who manage teams. */
export function TeamsScreen() {
  const { org, orgs, isPending } = useActiveOrg();
  const picker = useOrgPickerVisible();
  const [tab, setTab] = useUrlState<"all" | "mine">("tab", "all");
  const [q, setQ] = useState("");
  const [creating, setCreating] = useState(false);
  const canCreate = useAccessPermission(org?.id, ACCESS_PERMISSION_MANAGE_TEAMS);
  const teams = useAccessTeams(org?.id, { q, mine: tab === "mine" });
  useTopic(org ? topic("access", org.id) : null);
  const rows = teams.data?.items ?? [];

  if (!isPending && !org) {
    return (
      <Screen title="Teams">
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
    <Screen
      title="Teams"
      info={`Teams are groups in ${orgsNounA()} you can share with and mention (@handle). Leads add and remove their members.`}
      action={
        canCreate ? (
          <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
            New team
          </Button>
        ) : undefined
      }
    >
      <section className="flex flex-col gap-4">
        {orgs.length > 1 && picker ? <OrgSwitcher className="self-start" /> : null}
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <Tabs value={tab} onValueChange={(v) => setTab(v as "all" | "mine")} variant="pill">
            <TabsList aria-label="Teams">
              <Tab value="all">All</Tab>
              <Tab value="mine">Yours</Tab>
            </TabsList>
          </Tabs>
          <Input
            aria-label="Search teams"
            placeholder="Search teams"
            leadingIcon={<Search aria-hidden />}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            wrapperClassName="sm:ml-auto sm:w-64"
          />
        </div>
        <Card padding="none" className="p-1">
          {teams.isPending || isPending ? (
            <SkeletonGroup label="Loading teams" className="flex flex-col">
              <SkeletonRow />
              <SkeletonRow />
              <SkeletonRow />
            </SkeletonGroup>
          ) : teams.isError ? (
            <ErrorState compact title="Couldn't load teams" info={teams.error.message} onRetry={() => void teams.refetch()} retrying={teams.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState
              compact
              icon={<Users />}
              title={q ? `No teams match “${q}”` : tab === "mine" ? "You're not on a team yet" : "No teams yet"}
              {...(canCreate && !q && tab === "all"
                ? {
                    action: (
                      <Button size="auto" leftIcon={<Plus />} onClick={() => setCreating(true)}>
                        New team
                      </Button>
                    ),
                  }
                : {})}
            />
          ) : (
            <ul className="flex flex-col">
              {rows.map((t) => (
                <li key={t.id}>
                  <ListRow
                    asChild
                    title={t.name}
                    leading={<SubjectAvatar type="team" name={t.name} src={t.avatar_url} size="md" />}
                    trailing={
                      <span className="flex items-center gap-3">
                        <span className="hidden type-caption text-fg-muted sm:inline">@{t.handle}</span>
                        <span className="hidden type-caption text-fg-muted sm:inline">{memberCount(t.member_count)}</span>
                        {t.my_role ? <Badge tone={TEAM_ROLE_TONE[t.my_role]}>{TEAM_ROLE_LABEL[t.my_role]}</Badge> : null}
                      </span>
                    }
                  >
                    <Link href={`/settings/teams/${t.id}`} />
                  </ListRow>
                </li>
              ))}
            </ul>
          )}
          <LoadMore hasNextPage={teams.hasNextPage} isFetchingNextPage={teams.isFetchingNextPage} fetchNextPage={teams.fetchNextPage} />
        </Card>
      </section>
      {org ? <CreateTeamDialog orgId={org.id} open={creating} onOpenChange={setCreating} /> : null}
    </Screen>
  );
}

/** New team: name, optional handle and description. Opens the team; you join as its lead. */
export function CreateTeamDialog({ orgId, open, onOpenChange }: { orgId: string; open: boolean; onOpenChange: (open: boolean) => void }) {
  const router = useRouter();
  const create = useAccessCreateTeam(orgId);
  const [name, setName] = useState("");
  const [handle, setHandle] = useState("");
  const [description, setDescription] = useState("");
  const [errors, setErrors] = useState<{ name?: string; handle?: string }>({});

  useEffect(() => {
    if (!open) return;
    setName("");
    setHandle("");
    setDescription("");
    setErrors({});
  }, [open]);

  const submit = async () => {
    const n = name.trim();
    const h = handle.trim().replace(/^@/, "").toLowerCase();
    const next: typeof errors = {};
    if (!n) next.name = "Enter a name.";
    if (h && (!HANDLE.test(h) || h.includes("--"))) next.handle = "Use 2 to 32 lowercase letters, numbers or single hyphens.";
    setErrors(next);
    if (next.name || next.handle) return;
    try {
      const team = await create.mutateAsync({ name: n, ...(h ? { handle: h } : {}), ...(description.trim() ? { description: description.trim() } : {}) });
      toast.success(`@${team.handle} created`);
      onOpenChange(false);
      router.push(`/settings/teams/${team.id}`);
    } catch (e) {
      if (hasErrorCode(e, "conflict")) setErrors({ handle: errorMessage(e) });
      else toast.error(errorMessage(e, "Couldn't create the team. Try again."));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => !create.isPending && onOpenChange(o)}
      title="New team"
      footer={
        <>
          <Button variant="ghost" size="auto" disabled={create.isPending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button size="auto" loading={create.isPending} onClick={() => void submit()}>
            Create
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <Field label="Name" error={errors.name} required>
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} autoFocus placeholder="Design" />
        </Field>
        <Field label="Handle" info="People mention the team with @handle. Left empty, one is made from the name." error={errors.handle}>
          <Input value={handle} onChange={(e) => setHandle(e.target.value.toLowerCase())} maxLength={33} placeholder="design" autoCapitalize="none" spellCheck={false} />
        </Field>
        <Field label="Description">
          <TextArea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={280} minRows={2} />
        </Field>
      </form>
    </Dialog>
  );
}
