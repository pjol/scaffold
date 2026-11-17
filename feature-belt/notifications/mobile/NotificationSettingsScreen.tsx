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
import { Button, Caption, EmptyState, ErrorState, ListGroup, ListRow, Screen, Sheet, SkeletonRow, Switch, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Bell, Check } from "lucide-react-native";
import { useState } from "react";
import { ScrollView, View } from "react-native";
import { PushBanner } from "./PushBanner";

function KindGroup({ pref }: { pref: NotificationPreference }) {
  const update = useUpdateNotificationPreference();
  const save = (next: Pick<NotificationPreference, "push" | "email">) =>
    update.mutate({ kind: pref.kind, ...next }, { onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) });
  return (
    <ListGroup title={pref.label}>
      <Switch label="Push" value={pref.push} onValueChange={(push) => save({ push, email: pref.email })} accessibilityLabel={`Push for ${pref.label}`} />
      <Switch label="Email" value={pref.email} onValueChange={(email) => save({ push: pref.push, email })} accessibilityLabel={`Email for ${pref.label}`} />
    </ListGroup>
  );
}

const hourFmt = new Intl.DateTimeFormat(undefined, { hour: "numeric" });
const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const CADENCE: { value: NotificationDigestCadence; label: string }[] = [
  { value: "off", label: "Never" },
  { value: "daily", label: "Every day" },
  { value: "weekly", label: "Every week" },
];

/** Email digest: cadence, weekday and local hour, each picked in a sheet. */
function DigestGroup() {
  const theme = useTheme();
  const c = theme.colors;
  const digest = useNotificationDigest();
  const update = useUpdateNotificationDigest();
  const [picking, setPicking] = useState<"cadence" | "weekday" | "hour" | null>(null);
  const d = digest.data;
  if (!d) return digest.isError ? <ErrorState title="Couldn't load the digest setting" info={errorMessage(digest.error)} onRetry={() => void digest.refetch()} /> : null;
  const save = (next: Partial<NotificationDigest>) =>
    update.mutate(
      { ...d, ...next, time_zone: notificationDeviceZone() },
      { onSuccess: (r) => toast.success(r.cadence === "off" ? "Digest off." : "Digest saved."), onError: (e) => toast.error(errorMessage(e, "Couldn't save. Try again.")) },
    );
  const options: { value: string; label: string }[] =
    picking === "cadence"
      ? CADENCE
      : picking === "weekday"
        ? WEEKDAYS.map((label, i) => ({ value: String(i), label }))
        : Array.from({ length: 24 }, (_, h) => ({ value: String(h), label: hourFmt.format(new Date(2026, 0, 1, h)) }));
  const current = picking === "cadence" ? d.cadence : picking === "weekday" ? String(d.weekday) : String(d.hour);
  return (
    <>
      <ListGroup title="Email digest" info="One email with your unread notifications. It waits while notifications are paused or in your quiet hours.">
        <ListRow title="Send" trailing={<Caption tone="muted">{CADENCE.find((x) => x.value === d.cadence)?.label ?? ""}</Caption>} onPress={() => setPicking("cadence")} />
        {d.cadence === "weekly" ? <ListRow title="On" trailing={<Caption tone="muted">{WEEKDAYS[d.weekday] ?? ""}</Caption>} onPress={() => setPicking("weekday")} /> : null}
        {d.cadence !== "off" ? <ListRow title="At" trailing={<Caption tone="muted">{hourFmt.format(new Date(2026, 0, 1, d.hour))}</Caption>} onPress={() => setPicking("hour")} /> : null}
      </ListGroup>
      <Sheet open={!!picking} onClose={() => setPicking(null)} title={picking === "cadence" ? "Send" : picking === "weekday" ? "On" : "At"} snapPoints={["55%"]}>
        <ScrollView>
          <ListGroup>
            {options.map((o) => (
              <ListRow
                key={o.value}
                title={o.label}
                chevron={false}
                trailing={o.value === current ? <Check color={c.primaryStrong} size={20} /> : null}
                onPress={() => {
                  if (picking === "cadence") save({ cadence: o.value as NotificationDigestCadence });
                  else if (picking === "weekday") save({ weekday: Number(o.value) });
                  else save({ hour: Number(o.value) });
                  setPicking(null);
                }}
              />
            ))}
          </ListGroup>
        </ScrollView>
      </Sheet>
    </>
  );
}

/** Muted from the inbox, with Unmute. */
function MutedGroup() {
  const mutes = useNotificationMutes();
  const unmute = useUnmuteNotificationResource();
  const items = mutes.data?.items ?? [];
  if (items.length === 0) return null;
  return (
    <ListGroup title="Muted" info="Nothing new arrives about these. Mute more from a notification's actions in your inbox.">
      {items.map((m) => (
        <ListRow
          key={m.resource}
          title={m.label || m.resource}
          chevron={false}
          trailing={<Caption tone="muted">Unmute</Caption>}
          onPress={() => unmute.mutate(m.resource, { onSuccess: () => toast.success("Unmuted."), onError: (e) => toast.error(errorMessage(e)) })}
        />
      ))}
    </ListGroup>
  );
}

/** Settings → Notifications: push and email per kind (the inbox is always on), and push on this phone. */
export function NotificationSettingsScreen() {
  const theme = useTheme();
  const s = theme.space;
  const prefs = useNotificationPreferences();
  const groups = groupPreferences(prefs.data?.items ?? []);
  const titled = groups.length > 1;

  return (
    <Screen edges={["bottom"]} gap={s[6]}>
      <Stack.Screen options={{ title: "Notifications" }} />
      <PushBanner />
      {prefs.isPending ? (
        <View>
          <SkeletonRow />
          <SkeletonRow />
        </View>
      ) : prefs.isError ? (
        <ErrorState title="Couldn't load notification settings" info={errorMessage(prefs.error)} onRetry={() => void prefs.refetch()} retrying={prefs.isRefetching} />
      ) : groups.length === 0 ? (
        <EmptyState
          icon={Bell}
          title="Nothing to set yet"
          info="Everything lands in your inbox."
          action={
            <Button variant="secondary" onPress={() => router.push("/notifications")}>
              Open inbox
            </Button>
          }
        />
      ) : (
        <>
          <DigestGroup />
          {groups.map((g) => (
            <View key={g.group} style={{ gap: s[3] }}>
              {titled ? <TitleSm>{groupLabel(g.group)}</TitleSm> : null}
              {g.items.map((pref) => (
                <KindGroup key={pref.kind} pref={pref} />
              ))}
            </View>
          ))}
          <MutedGroup />
        </>
      )}
    </Screen>
  );
}
