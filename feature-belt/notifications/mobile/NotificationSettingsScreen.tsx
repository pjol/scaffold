import { errorMessage, groupLabel, groupPreferences, useNotificationPreferences, useUpdateNotificationPreference, type NotificationPreference } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState, ListGroup, Screen, SkeletonRow, Switch, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, Stack } from "expo-router";
import { Bell } from "lucide-react-native";
import { View } from "react-native";
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
        groups.map((g) => (
          <View key={g.group} style={{ gap: s[3] }}>
            {titled ? <TitleSm>{groupLabel(g.group)}</TitleSm> : null}
            {g.items.map((pref) => (
              <KindGroup key={pref.kind} pref={pref} />
            ))}
          </View>
        ))
      )}
    </Screen>
  );
}
