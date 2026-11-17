import {
  PRESENCE_CLOCK_OPTIONS,
  PRESENCE_WEEKDAYS,
  errorMessage,
  presenceClockLabel,
  presenceDefaultSchedule,
  presenceDeviceZone,
  useMyPresence,
  useSetPresenceSchedule,
  type PresenceQuietHours,
  type PresenceSchedule,
} from "@scaffoldapp/shared";
import { Button, ErrorState, InfoTip, ListGroup, ListRow, Screen, Sheet, Skeleton, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Check } from "lucide-react-native";
import { useEffect, useState } from "react";
import { ScrollView, View } from "react-native";

const night: PresenceQuietHours = { start: "22:00", end: "08:00" };
/** Mon–Sun order for display (the schedule stores Sunday first). */
const order = [1, 2, 3, 4, 5, 6, 0] as const;

function clock(t: string): string {
  return t === "24:00" ? "Midnight" : presenceClockLabel(t);
}

/** presence/schedule: quiet hours per weekday in your zone; pushes and emails wait until they end. */
export function ScheduleScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const mine = useMyPresence();
  const save = useSetPresenceSchedule();
  const [draft, setDraft] = useState<PresenceSchedule | null>(null);
  const [picking, setPicking] = useState<{ day: number; edge: "start" | "end" } | null>(null);
  const zone = presenceDeviceZone();

  useEffect(() => {
    if (mine.data && !draft) setDraft(mine.data.schedule ?? presenceDefaultSchedule(zone));
  }, [mine.data, draft, zone]);

  if (mine.isPending || (!draft && !mine.isError)) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Notification schedule" }} />
        <Skeleton height={48} />
        <Skeleton height={320} />
      </Screen>
    );
  }
  if (mine.isError || !draft) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "Notification schedule" }} />
        <ErrorState title="Couldn't load your schedule" info={mine.error?.message} onRetry={() => void mine.refetch()} retrying={mine.isFetching} />
      </Screen>
    );
  }

  const setDay = (i: number, w: PresenceQuietHours | null) => setDraft({ ...draft, days: draft.days.map((x, j) => (j === i ? w : x)) });
  const none = draft.days.every((d) => !d);
  const submit = (body: PresenceSchedule | null, done: string) =>
    save.mutate(body, {
      onSuccess: () => {
        toast.success(done);
        router.back();
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't save your schedule.")),
    });
  const picked = picking ? draft.days[picking.day] : null;
  const options = picking?.edge === "start" ? PRESENCE_CLOCK_OPTIONS.filter((t) => t !== "24:00") : PRESENCE_CLOCK_OPTIONS;

  return (
    <Screen
      edges={["bottom"]}
      footer={
        <View style={{ flexDirection: "row", gap: s[2] }}>
          {mine.data?.schedule ? (
            <Button variant="secondary" style={{ flex: 1 }} loading={save.isPending && save.variables === null} onPress={() => submit(null, "Schedule removed.")}>
              Remove
            </Button>
          ) : null}
          <Button style={{ flex: 1 }} disabled={draft.enabled && none} loading={save.isPending && save.variables !== null} onPress={() => submit(draft, "Schedule saved.")}>
            Save
          </Button>
        </View>
      }
    >
      <Stack.Screen options={{ title: "Notification schedule" }} />
      <Switch
        label="Pause notifications on a schedule"
        info="During quiet hours you get no push notifications or emails, and people see Do Not Disturb. Everything still arrives in the app."
        value={draft.enabled}
        onValueChange={(enabled) => setDraft({ ...draft, enabled })}
      />
      {order.map((i) => {
        const w = draft.days[i] ?? null;
        const day = PRESENCE_WEEKDAYS[i];
        return (
          <ListGroup key={day}>
            <Switch label={day} value={!!w} disabled={!draft.enabled} onValueChange={(on) => setDay(i, on ? night : null)} />
            {w ? (
              <>
                <ListRow title="From" disabled={!draft.enabled} trailing={<Text role="label" tone="muted">{clock(w.start)}</Text>} onPress={() => setPicking({ day: i, edge: "start" })} />
                <ListRow title="Until" disabled={!draft.enabled} trailing={<Text role="label" tone="muted">{clock(w.end)}</Text>} onPress={() => setPicking({ day: i, edge: "end" })} />
              </>
            ) : null}
          </ListGroup>
        );
      })}
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Text role="label" tone="muted" style={{ flexShrink: 1 }}>
          Times in {draft.time_zone || zone}
        </Text>
        <InfoTip content="An end earlier than the start runs into the next morning. Use this device's zone to move the schedule when you travel." />
      </View>
      {draft.time_zone !== zone ? (
        <Button variant="secondary" onPress={() => setDraft({ ...draft, time_zone: zone })}>
          {`Use ${zone}`}
        </Button>
      ) : null}

      <Sheet open={!!picking} onClose={() => setPicking(null)} title={picking ? `${PRESENCE_WEEKDAYS[picking.day]} · ${picking.edge === "start" ? "From" : "Until"}` : ""} snapPoints={["60%"]}>
        <ScrollView>
          <ListGroup>
            {options.map((t) => {
              const current = picked && picking ? picked[picking.edge] : "";
              return (
                <ListRow
                  key={t}
                  title={clock(t)}
                  chevron={false}
                  trailing={current === t ? <Check color={c.primaryStrong} size={20} /> : null}
                  onPress={() => {
                    if (picking && picked) setDay(picking.day, { ...picked, [picking.edge]: t });
                    setPicking(null);
                  }}
                />
              );
            })}
          </ListGroup>
        </ScrollView>
      </Sheet>
    </Screen>
  );
}
