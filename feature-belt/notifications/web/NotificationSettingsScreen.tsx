"use client";

import { errorMessage, groupLabel, groupPreferences, useNotificationPreferences, useUpdateNotificationPreference, type NotificationPreference } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, InfoTip, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";
import { Bell, Inbox } from "lucide-react";
import Link from "next/link";
import { Screen, Section } from "@/components/shell/PageHeader";

const PUSH_INFO = "Pushes go to the phones where you're signed in to the app.";

function PreferenceRow({ pref }: { pref: NotificationPreference }) {
  const update = useUpdateNotificationPreference();
  const save = (next: Pick<NotificationPreference, "push" | "email">) =>
    update.mutate({ kind: pref.kind, ...next }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });
  return (
    <li className="flex min-h-row items-center gap-4 px-4 py-2">
      <span className="min-w-0 flex-1 type-body text-fg">{pref.label}</span>
      <span className="flex w-14 justify-center">
        <Switch aria-label={`Push for ${pref.label}`} checked={pref.push} onCheckedChange={(push) => save({ push, email: pref.email })} />
      </span>
      <span className="flex w-14 justify-center">
        <Switch aria-label={`Email for ${pref.label}`} checked={pref.email} onCheckedChange={(email) => save({ push: pref.push, email })} />
      </span>
    </li>
  );
}

function ColumnHeads() {
  return (
    <div className="flex h-row items-center gap-4 border-b border-border px-4">
      <span className="flex-1" />
      <span className="flex w-14 items-center justify-center gap-1 type-caption font-semibold text-fg-muted">
        Push
        <InfoTip content={PUSH_INFO} />
      </span>
      <span className="flex w-14 justify-center type-caption font-semibold text-fg-muted">Email</span>
    </div>
  );
}

/** Settings → Notifications: push and email per kind, grouped by feature. The inbox is always on. */
export function NotificationSettingsScreen() {
  const prefs = useNotificationPreferences();
  const groups = groupPreferences(prefs.data?.items ?? []);

  return (
    <Screen title="Notifications" info="Everything also lands in your inbox. Choose where else each kind reaches you.">
      {prefs.isPending ? (
        <SkeletonGroup label="Loading notification settings" className="flex flex-col gap-3">
          <Skeleton className="h-6 w-32" />
          <Skeleton className="h-40" />
        </SkeletonGroup>
      ) : prefs.isError ? (
        <Card padding="compact">
          <ErrorState compact title="Couldn't load notification settings" info={prefs.error.message} onRetry={() => void prefs.refetch()} retrying={prefs.isFetching} />
        </Card>
      ) : groups.length === 0 ? (
        <Card padding="compact">
          <EmptyState
            compact
            icon={<Bell />}
            title="Nothing to set yet"
            action={
              <Button asChild variant="secondary" size="auto" leftIcon={<Inbox />}>
                <Link href="/notifications">Open inbox</Link>
              </Button>
            }
          />
        </Card>
      ) : (
        groups.map((g) => (
          <Section key={g.group} title={groupLabel(g.group)}>
            <Card padding="none" className="max-w-form">
              <ColumnHeads />
              <ul className="flex flex-col divide-y divide-border">
                {g.items.map((pref) => (
                  <PreferenceRow key={pref.kind} pref={pref} />
                ))}
              </ul>
            </Card>
          </Section>
        ))
      )}
    </Screen>
  );
}
