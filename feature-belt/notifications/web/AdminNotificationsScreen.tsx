"use client";

import { errorMessage, useAdminUser, useAdminUsers, useNotificationStats, useSendTestNotification, type AdminUser, type NotificationStats } from "@scaffoldapp/shared";
import { AnimatedNumber, Badge, Banner, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, InfoTip, Input, ListRow, Skeleton, SkeletonGroup, Stagger, StaggerItem, TextArea, toast } from "@scaffoldapp/ui-web";
import { BarChart3, Send, UserRound, X } from "lucide-react";
import { useState } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { Screen, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";

const MODE_INFO: Record<NotificationStats["push_mode"], string> = {
  expo: "Pushes go through the Expo push service.",
  sink: "Development: each push is written as a JSON file to PUSH_SINK_DIR instead of reaching a phone.",
  log: "Development: pushes are only logged. Set PUSH_SINK_DIR to keep them as files, or EXPO_ACCESS_TOKEN to send them.",
};

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

function Stats({ stats }: { stats: NotificationStats }) {
  const devices = stats.devices.ios + stats.devices.android + stats.devices.web;
  return (
    <Stagger as="ul" className="grid grid-cols-2 gap-4 lg:grid-cols-4" aria-label="Delivery">
      <StaggerItem as="li">
        <Stat label="Sent in 7 days" value={stats.notifications_7d} info={`Notifications created in the last 7 days. ${stats.notifications} in all.`} />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Unread" value={stats.unread} info="Inbox rows nobody has opened yet, across every account." />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Devices" value={devices} info={`${stats.devices.ios} iOS · ${stats.devices.android} Android · ${stats.devices.web} web, on ${stats.users_with_devices} accounts.`} />
      </StaggerItem>
      <StaggerItem as="li">
        <Stat label="Awaiting receipts" value={stats.pending_receipts} info="Pushes Expo accepted whose delivery receipt hasn't been checked yet (checked 15 minutes after sending)." />
      </StaggerItem>
    </Stagger>
  );
}

function UserPicker({ value, onChange }: { value: AdminUser | null; onChange: (u: AdminUser | null) => void }) {
  const [q, setQ] = useState("");
  const users = useAdminUsers({ q: q || undefined, limit: 5 }, { enabled: !value && q.length > 0 });
  if (value) {
    const name = value.display_name || value.email;
    return (
      <Card padding="none" className="flex items-center gap-3 p-2 pl-3">
        <AppAvatar name={name} src={value.avatar_url} size="sm" />
        <span className="flex min-w-0 flex-1 items-baseline gap-2">
          <span className="truncate type-body font-semibold text-fg">{name}</span>
          {value.display_name && <span className="hidden truncate type-label text-fg-muted sm:inline">{value.email}</span>}
        </span>
        <Button variant="ghost" size="sm" leftIcon={<X />} onClick={() => onChange(null)}>
          Change
        </Button>
      </Card>
    );
  }
  const rows = users.data?.items ?? [];
  return (
    <div className="flex flex-col gap-2">
      <SearchInput value={q} onSearch={setQ} placeholder="Name, email or ID" label="Find a user" />
      {q && (
        <Card padding="none" className="p-1">
          {users.isPending ? (
            <SkeletonGroup label="Searching" className="flex flex-col gap-2 p-3">
              <Skeleton shape="text" className="h-5 w-1/2" />
              <Skeleton shape="text" className="h-5 w-2/3" />
            </SkeletonGroup>
          ) : users.isError ? (
            <ErrorState compact title="Couldn't search users" info={users.error.message} onRetry={() => void users.refetch()} retrying={users.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={<UserRound />} title="No matching users" />
          ) : (
            <ul className="flex flex-col">
              {rows
                .filter((u) => u.status !== "deleted")
                .map((u) => (
                  <li key={u.id}>
                    <ListRow
                      title={u.display_name || u.email}
                      leading={<AppAvatar name={u.display_name || u.email} src={u.avatar_url} size="sm" />}
                      trailing={<span className="hidden truncate type-caption text-fg-muted sm:inline">{u.email}</span>}
                      onClick={() => onChange(u)}
                    />
                  </li>
                ))}
            </ul>
          )}
        </Card>
      )}
    </div>
  );
}

function TestSend() {
  const [userParam, setUserParam] = useUrlState("user", "");
  const preset = useAdminUser(userParam || null);
  const [picked, setPicked] = useState<AdminUser | null>(null);
  const user = picked ?? (userParam ? (preset.data ?? null) : null);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [confirming, setConfirming] = useState(false);
  const send = useSendTestNotification();
  const name = user ? user.display_name || user.email : "";

  const choose = (u: AdminUser | null) => {
    setPicked(u);
    setUserParam(u ? u.id : null);
  };

  return (
    <Section title="Send a test notification" info="Goes to the person's inbox and every phone they're signed in on, whatever their settings. No email. Recorded in the audit log.">
      <form
        className="flex max-w-form flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (user) setConfirming(true);
        }}
      >
        <Field label="User">{userParam && preset.isPending && !picked ? <Skeleton className="h-control" /> : <UserPicker value={user} onChange={choose} />}</Field>
        <Field label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Test notification" maxLength={200} />
        </Field>
        <Field label="Message">
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} placeholder="Sent from the admin console." maxLength={500} minRows={2} />
        </Field>
        <Button type="submit" size="auto" leftIcon={<Send />} loading={send.isPending} disabled={!user} className="self-start">
          Send test
        </Button>
      </form>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Send a test notification to ${name}?`}
        confirmLabel="Send"
        errorText={(err) => errorMessage(err, "Couldn't send. Try again.")}
        onConfirm={async () => {
          if (!user) return;
          const r = await send.mutateAsync({ user_id: user.id, ...(title.trim() ? { title: title.trim() } : {}), ...(body.trim() ? { body: body.trim() } : {}) });
          toast.success(r.devices === 0 ? "Sent to the inbox. No phones are registered." : `Sent to the inbox and ${r.devices} ${r.devices === 1 ? "phone" : "phones"}.`);
        }}
      >
        <p>It appears in their inbox and on every phone they're signed in on.</p>
      </ConfirmDialog>
    </Section>
  );
}

/** Admin → Notifications: delivery counts, the busiest kinds, and a test send to one person. */
export function AdminNotificationsScreen() {
  const stats = useNotificationStats();
  return (
    <Screen
      title="Notifications"
      action={
        stats.data ? (
          <span className="flex items-center gap-1">
            <Badge tone={stats.data.push_mode === "expo" ? "success" : "warning"} dot>
              {stats.data.push_mode === "expo" ? "Push live" : `Push: ${stats.data.push_mode}`}
            </Badge>
            <InfoTip content={MODE_INFO[stats.data.push_mode]} />
          </span>
        ) : undefined
      }
    >
      {stats.isPending ? (
        <SkeletonGroup label="Loading counts" className="grid grid-cols-2 gap-4 lg:grid-cols-4">
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
        <>
          {stats.data.push_mode !== "expo" && <Banner tone="warning">Pushes don't reach phones in this environment.</Banner>}
          <Stats stats={stats.data} />
          <Section title="Kinds in the last 7 days">
            <Card padding="none">
              {stats.data.kinds_7d.length === 0 ? (
                <EmptyState compact icon={<BarChart3 />} title="Nothing sent in 7 days" />
              ) : (
                <Table label="Kinds in the last 7 days">
                  <THead>
                    <TH>Kind</TH>
                    <TH align="right">Sent</TH>
                  </THead>
                  <tbody>
                    {stats.data.kinds_7d.map((k) => (
                      <TR key={k.kind}>
                        <TD className="font-mono">{k.kind}</TD>
                        <TD align="right">{k.count}</TD>
                      </TR>
                    ))}
                  </tbody>
                </Table>
              )}
            </Card>
          </Section>
        </>
      )}
      <TestSend />
    </Screen>
  );
}
