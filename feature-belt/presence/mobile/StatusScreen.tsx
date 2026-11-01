import {
  PRESENCE_CLEAR_AFTER_OPTIONS,
  PRESENCE_DND_OPTIONS,
  PRESENCE_STATUS_EMOJI,
  PRESENCE_STATUS_PRESETS,
  activeCustomStatus,
  errorMessage,
  isPresenceDND,
  isPresenceEmoji,
  presenceDndUntil,
  presenceStatusRequest,
  presenceUntilLabel,
  useClearPresenceStatus,
  useMe,
  useMyPresence,
  useSetPresenceAway,
  useSetPresenceDND,
  useSetPresenceStatus,
  type PresenceClearAfter,
} from "@scaffoldapp/shared";
import { Button, ErrorState, Field, Input, ListGroup, ListRow, Screen, Skeleton, Switch, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router } from "expo-router";
import { Check } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Pressable, ScrollView, View } from "react-native";
import { PresenceAvatar } from "./PresenceDot";

/** presence/status: custom status (emoji, text, clear after, presets), pause notifications, appear away. */
export function StatusScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const me = useMe();
  const mine = useMyPresence();
  const set = useSetPresenceStatus();
  const clear = useClearPresenceStatus();
  const dnd = useSetPresenceDND();
  const away = useSetPresenceAway();
  const current = activeCustomStatus(mine.data);
  const [emoji, setEmoji] = useState("");
  const [text, setText] = useState("");
  const [clearAfter, setClearAfter] = useState<PresenceClearAfter>("today");
  const loaded = !!mine.data;

  useEffect(() => {
    if (!loaded) return;
    setEmoji(current?.emoji ?? "");
    setText(current?.text ?? "");
    setClearAfter(current?.clear_after ?? "today");
    // Start from the saved status once it has loaded.
  }, [loaded]);

  if (mine.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Status" }} />
        <Skeleton height={48} />
        <Skeleton height={200} />
      </Screen>
    );
  }
  if (mine.isError) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "Status" }} />
        <ErrorState title="Couldn't load your status" info={mine.error.message} onRetry={() => void mine.refetch()} retrying={mine.isFetching} />
      </Screen>
    );
  }

  const data = mine.data;
  const empty = !emoji && !text.trim();
  const paused = isPresenceDND(data);
  const save = () =>
    set.mutate(presenceStatusRequest(emoji, text.trim(), clearAfter), {
      onSuccess: () => {
        toast.success("Status updated.");
        router.back();
      },
      onError: (e) => toast.error(errorMessage(e, "Couldn't save your status.")),
    });

  return (
    <Screen
      edges={["bottom"]}
      keyboard
      footer={
        <View style={{ flexDirection: "row", gap: s[2] }}>
          {current ? (
            <Button
              variant="secondary"
              loading={clear.isPending}
              style={{ flex: 1 }}
              onPress={() => clear.mutate(undefined, { onSuccess: () => toast.success("Status cleared."), onError: (e) => toast.error(errorMessage(e)) })}
            >
              Clear
            </Button>
          ) : null}
          <Button disabled={empty} loading={set.isPending} style={{ flex: 1 }} onPress={save}>
            Save
          </Button>
        </View>
      }
    >
      <Stack.Screen options={{ title: "Status" }} />
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[3] }}>
        <PresenceAvatar userId={data.user_id} name={me.data?.display_name || "You"} src={me.data?.avatar_url} size="lg" showOffline />
        <Text role="label" tone="muted" style={{ flex: 1 }}>
          {paused ? `Notifications paused ${presenceUntilLabel(data.dnd_until)}` : data.away ? "Appearing away" : data.status === "online" ? "Online" : "Away"}
        </Text>
      </View>

      <View style={{ gap: s[3] }}>
        <Field label="Status">
          <Input value={text} onChangeText={setText} maxLength={100} placeholder="What's your status?" returnKeyType="done" />
        </Field>
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2] }} accessibilityLabel="Emoji">
          {PRESENCE_STATUS_EMOJI.map((e) => (
            <Pressable
              key={e}
              accessibilityRole="button"
              accessibilityState={{ selected: emoji === e }}
              accessibilityLabel={e}
              onPress={() => setEmoji(emoji === e ? "" : e)}
              style={{
                width: theme.tap.base,
                height: theme.tap.base,
                borderRadius: theme.radius.md,
                alignItems: "center",
                justifyContent: "center",
                borderWidth: 1,
                borderColor: emoji === e ? c.primary : c.border,
                backgroundColor: emoji === e ? c.primarySoft : c.surface,
              }}
            >
              <Text role="titleSm">{e}</Text>
            </Pressable>
          ))}
        </ScrollView>
        <Field label="Any emoji">
          <Input
            placeholder="Type or paste an emoji"
            onChangeText={(v) => {
              if (isPresenceEmoji(v.trim())) setEmoji(v.trim());
            }}
          />
        </Field>
      </View>

      {empty ? (
        <ListGroup title="Suggestions">
          {PRESENCE_STATUS_PRESETS.map((p) => (
            <ListRow
              key={p.text}
              title={p.text}
              leading={<Text role="bodyLg">{p.emoji}</Text>}
              chevron={false}
              onPress={() => {
                setEmoji(p.emoji);
                setText(p.text);
                setClearAfter(p.clear_after);
              }}
            />
          ))}
        </ListGroup>
      ) : (
        <ListGroup title="Clear after">
          {PRESENCE_CLEAR_AFTER_OPTIONS.map((o) => (
            <ListRow key={o.value} title={o.label} chevron={false} trailing={clearAfter === o.value ? <Check color={c.primaryStrong} size={20} /> : null} onPress={() => setClearAfter(o.value)} />
          ))}
        </ListGroup>
      )}

      <ListGroup title="Pause notifications" info="While paused you get no push notifications. Messages and badges still arrive in the app.">
        {paused ? <ListRow title="Resume notifications" chevron={false} onPress={() => dnd.mutate(null, { onError: (e) => toast.error(errorMessage(e)) })} /> : null}
        {PRESENCE_DND_OPTIONS.map((o) => (
          <ListRow
            key={o.label}
            title={o.label}
            chevron={false}
            onPress={() =>
              dnd.mutate(presenceDndUntil(o), {
                onSuccess: (m) => toast.success(`Notifications paused ${presenceUntilLabel(m.dnd_until)}.`),
                onError: (e) => toast.error(errorMessage(e)),
              })
            }
          />
        ))}
      </ListGroup>

      <Switch label="Appear away" value={data.away} onValueChange={(v) => away.mutate(v, { onError: (e) => toast.error(errorMessage(e)) })} />
    </Screen>
  );
}
