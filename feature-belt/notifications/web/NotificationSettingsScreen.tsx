"use client";

import {
  errorMessage,
  groupLabel,
  groupPreferences,
  notificationDeviceZone,
  useNotificationDigest,
  useNotificationMutes,
  useNotificationPreferences,
  useUnmuteNotificationResource,
  useUpdateNotificationDigest,
  useUpdateNotificationPreference,
  type NotificationDigest,
  type NotificationDigestCadence,
  type NotificationPreference,
} from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, Field, InfoTip, Select, Skeleton, SkeletonGroup, Switch, toast } from "@scaffoldapp/ui-web";
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

const hourFmt = new Intl.DateTimeFormat(undefined, { hour: "numeric" });
const HOURS = Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: hourFmt.format(new Date(2026, 0, 1, h)) }));
const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"].map((label, i) => ({ value: String(i), label }));

/** Email digest: off, daily or weekly at a local hour. */
function DigestSection() {
  const digest = useNotificationDigest();
  const update = useUpdateNotificationDigest();
  const d = digest.data;
  const save = (next: Partial<NotificationDigest>) => {
    if (!d) return;
    update.mutate(
      { ...d, ...next, time_zone: notificationDeviceZone() },
      { onSuccess: (r) => toast.success(r.cadence === "off" ? "Digest off." : "Digest saved."), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) },
    );
  };
  return (
    <Section title="Email digest" info="One email with your unread notifications. It waits while notifications are paused or in your quiet hours.">
      <Card className="max-w-form">
        {digest.isPending ? (
          <Skeleton className="h-10" />
        ) : digest.isError ? (
          <ErrorState compact title="Couldn't load the digest setting" info={digest.error.message} onRetry={() => void digest.refetch()} retrying={digest.isFetching} />
        ) : d ? (
          <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
            <Field label="Send" className="sm:w-40">
              <Select
                value={d.cadence}
                onValueChange={(v) => save({ cadence: v as NotificationDigestCadence })}
                options={[
                  { value: "off", label: "Never" },
                  { value: "daily", label: "Every day" },
                  { value: "weekly", label: "Every week" },
                ]}
              />
            </Field>
            {d.cadence === "weekly" ? (
              <Field label="On" className="sm:w-40">
                <Select value={String(d.weekday)} onValueChange={(v) => save({ weekday: Number(v) })} options={WEEKDAYS} />
              </Field>
            ) : null}
            {d.cadence !== "off" ? (
              <Field label="At" className="sm:w-32">
                <Select value={String(d.hour)} onValueChange={(v) => save({ hour: Number(v) })} options={HOURS} />
              </Field>
            ) : null}
          </div>
        ) : null}
      </Card>
    </Section>
  );
}

/** Things muted from the inbox, with Unmute. */
function MutedSection() {
  const mutes = useNotificationMutes();
  const unmute = useUnmuteNotificationResource();
  const items = mutes.data?.items ?? [];
  if (mutes.isPending || (mutes.isSuccess && items.length === 0)) return null;
  return (
    <Section title="Muted" info="Nothing new arrives about these. Mute more from a notification's menu in your inbox.">
      <Card padding="none" className="max-w-form">
        {mutes.isError ? (
          <ErrorState compact title="Couldn't load muted items" info={mutes.error.message} onRetry={() => void mutes.refetch()} retrying={mutes.isFetching} />
        ) : (
          <ul className="flex flex-col divide-y divide-border">
            {items.map((m) => (
              <li key={m.resource} className="flex min-h-row items-center gap-4 px-4 py-2">
                <span className="min-w-0 flex-1 truncate type-body text-fg">{m.label || m.resource}</span>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => unmute.mutate(m.resource, { onSuccess: () => toast.success("Unmuted."), onError: (e) => toast.error(errorMessage(e)) })}
                >
                  Unmute
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </Section>
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
        <>
          <DigestSection />
          {groups.map((g) => (
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
          ))}
          <MutedSection />
        </>
      )}
    </Screen>
  );
}
