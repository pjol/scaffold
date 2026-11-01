"use client";

import { errorMessage, useAdminUsers, useAgentsAdminSetPaused, useAgentsAdminStats, useAgentsAdminUser, useMe } from "@scaffoldapp/shared";
import {
  AnimatedNumber,
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  InfoTip,
  Input,
  ListRow,
  Skeleton,
  SkeletonGroup,
  Stagger,
  StaggerItem,
  toast,
} from "@scaffoldapp/ui-web";
import { Bot, SearchX, UserRound } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Screen, Section } from "@/components/shell/PageHeader";
import { formatDateTime, formatRelative } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";
import { agentIcon } from "./labels";

function Stat({ label, value, info }: { label: string; value: number; info: string }) {
  return (
    <Card className="flex h-full flex-col gap-2">
      <span className="flex items-center gap-2 type-label text-fg-muted">
        {label}
        <InfoTip content={info} />
      </span>
      <AnimatedNumber value={value} className="type-headline text-fg" />
    </Card>
  );
}

function PersonScope({ userId }: { userId: string }) {
  const me = useMe();
  const q = useAgentsAdminUser(userId);
  const setPaused = useAgentsAdminSetPaused();
  const [confirm, setConfirm] = useState(false);
  const [reason, setReason] = useState("");
  const isAdmin = me.data?.admin_role === "admin";
  if (q.isPending) {
    return (
      <SkeletonGroup label="Loading" className="flex flex-col gap-3">
        <Skeleton className="h-24 w-full rounded-lg" />
      </SkeletonGroup>
    );
  }
  if (q.isError) {
    return (
      <Card padding="compact">
        <ErrorState compact title="Couldn't load this person's agent access" info={q.error.message} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      </Card>
    );
  }
  const u = q.data;
  const next = !u.staff_paused;
  return (
    <Card className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <span className="flex min-w-0 flex-1 items-center gap-3">
          <AppAvatar name={u.display_name || u.email} src={null} size="md" />
          <Link href={`/admin/users/${u.user_id}`} className="truncate type-body font-semibold text-fg underline-offset-2 hover:underline">
            {u.display_name || u.email}
          </Link>
        </span>
        {u.staff_paused ? <Badge tone="danger">Off by staff</Badge> : u.paused ? <Badge tone="warning">Off by them</Badge> : <Badge tone="success">On</Badge>}
      </div>
      {u.staff_paused && u.staff_paused_at && <Banner tone="danger">Staff turned agent access off {formatDateTime(u.staff_paused_at)}. Only staff can turn it back on.</Banner>}
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {[
          ["Default", u.default_level],
          ["Feature settings", String(u.feature_overrides)],
          ["Item settings", String(u.resource_overrides)],
          ["Waiting suggestions", String(u.pending)],
        ].map(([label, value]) => (
          <div key={label} className="flex flex-col gap-1">
            <dt className="type-caption text-fg-muted">{label}</dt>
            <dd className="type-body text-fg first-letter:uppercase">{value}</dd>
          </div>
        ))}
      </dl>
      {u.agents.length > 0 && (
        <ul className="flex flex-col">
          {u.agents.map((a) => {
            const Icon = agentIcon(a.via);
            return (
              <li key={`${a.via}|${a.client_key}`}>
                <ListRow
                  title={a.client}
                  leading={<Icon className="size-5 text-fg-muted" aria-hidden />}
                  trailing={<span className="type-caption">Active {formatRelative(a.last_seen_at)}</span>}
                />
              </li>
            );
          })}
        </ul>
      )}
      {isAdmin && (
        <div className="flex justify-end">
          <Button variant={next ? "danger" : "secondary"} size="auto" onClick={() => setConfirm(true)}>
            {next ? "Turn off their agent access" : "Turn their agent access back on"}
          </Button>
        </div>
      )}
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={next ? `Turn off agent access for ${u.display_name || u.email}?` : `Turn agent access back on for ${u.display_name || u.email}?`}
        confirmLabel={next ? "Turn off" : "Turn on"}
        tone={next ? "danger" : "primary"}
        onConfirm={() =>
          setPaused.mutateAsync({ userId: u.user_id, paused: next, ...(reason.trim() ? { reason: reason.trim() } : {}) }).then(
            () => {
              setReason("");
              toast.success(next ? "Agent access turned off" : "Agent access turned back on");
            },
            (e: unknown) => {
              toast.error(errorMessage(e, "Couldn't change it. Try again."));
              throw e;
            },
          )
        }
      >
        <div className="flex flex-col gap-4">
          <p>
            {next
              ? "Every agent acting for them stops at once, and they can't turn it back on themselves. Their own settings are kept. This goes in the audit log."
              : "Their own settings apply again. This goes in the audit log."}
          </p>
          <Field label="Reason" info="Kept in the audit log.">
            <Input value={reason} onChange={(e) => setReason(e.target.value)} maxLength={500} />
          </Field>
        </div>
      </ConfirmDialog>
    </Card>
  );
}

/** Admin → AI agents: usage counts, and one person's agent access with an audited off switch. */
export function AdminAgentsScreen() {
  const stats = useAgentsAdminStats();
  const [q, setQ] = useUrlState("q", "");
  const [userId, setUserId] = useUrlState("user", "");
  const users = useAdminUsers(q ? { q, limit: 8 } : { limit: 8 }, { enabled: !!q });
  const found = users.data?.items ?? [];
  return (
    <Screen title="AI agents">
      {stats.isPending ? (
        <SkeletonGroup label="Loading counts" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
        </SkeletonGroup>
      ) : stats.isError ? (
        <Card padding="compact">
          <ErrorState compact title="Couldn't load the counts" info={stats.error.message} onRetry={() => void stats.refetch()} retrying={stats.isFetching} />
        </Card>
      ) : (
        <Stagger as="ul" className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" aria-label="AI agent counts">
          <StaggerItem as="li">
            <Stat label="Active agents" value={stats.data.active_agents_7d} info="Agent and person pairs that called a tool in the last 7 days." />
          </StaggerItem>
          <StaggerItem as="li">
            <Stat label="Waiting suggestions" value={stats.data.pending_now} info="Suggested changes nobody has accepted or rejected yet." />
          </StaggerItem>
          <StaggerItem as="li">
            <Stat
              label="Accepted"
              value={stats.data.suggestions_7d.accepted}
              info={`Of suggestions made in the last 7 days: ${stats.data.suggestions_7d.rejected} rejected, ${stats.data.suggestions_7d.failed} failed, ${stats.data.suggestions_7d.expired} expired.`}
            />
          </StaggerItem>
          <StaggerItem as="li">
            <Stat
              label="Access off"
              value={stats.data.paused_users + stats.data.staff_paused_users}
              info={`${stats.data.paused_users} turned it off themselves; staff turned it off for ${stats.data.staff_paused_users}.`}
            />
          </StaggerItem>
        </Stagger>
      )}
      <Section title="Find a person" info="Look someone up to see their agent access, or turn it off if an agent misbehaves on their account (admins).">
        <SearchInput value={q} onSearch={setQ} placeholder="Name or email" label="Search people" className="sm:max-w-sm" />
        {q && (
          <Card padding="none" className="p-1">
            {users.isPending ? (
              <SkeletonGroup label="Searching" className="flex flex-col">
                <Skeleton className="h-12" />
              </SkeletonGroup>
            ) : users.isError ? (
              <ErrorState compact title="Couldn't search" info={users.error.message} onRetry={() => void users.refetch()} retrying={users.isFetching} />
            ) : found.length === 0 ? (
              <EmptyState compact icon={<SearchX />} title="No matching people" />
            ) : (
              <ul className="flex flex-col">
                {found.map((u) => (
                  <li key={u.id}>
                    <ListRow
                      title={u.display_name || u.email}
                      leading={<UserRound className="size-5 text-fg-muted" aria-hidden />}
                      current={u.id === userId}
                      onClick={() => setUserId(u.id)}
                    />
                  </li>
                ))}
              </ul>
            )}
          </Card>
        )}
        {userId ? <PersonScope userId={userId} /> : !q ? <EmptyState compact icon={<Bot />} title="Search for a person" /> : null}
      </Section>
    </Screen>
  );
}
