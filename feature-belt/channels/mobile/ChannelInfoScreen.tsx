import {
  CHANNELS_NOTIFY_OPTIONS,
  channelsDisplayName,
  channelsEffectiveNotify,
  channelsPlainText,
  channelsShortTime,
  errorMessage,
  isChannelsDirect,
  presenceLabel,
  useAddChannelMembers,
  useChannel,
  useChannelPrefs,
  useChannelsMembers,
  useChannelsPins,
  useMe,
  usePresence,
  useRemoveChannelMember,
  useUpdateChannel,
  type ChannelsPerson,
} from "@scaffoldapp/shared";
import { Button, ConfirmDialog, EmptyState, ErrorState, Input, ListGroup, ListRow, Screen, Sheet, Skeleton, Switch, Text, Title, toast, useTheme } from "@scaffoldapp/ui-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { Bell, MessagesSquare, Pin, UserPlus, UserRoundMinus } from "lucide-react-native";
import { useState } from "react";
import { Pressable, View } from "react-native";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { PresenceAvatar } from "@/features/presence";
import { useChannelMenuActions } from "./ChannelMenu";
import { PeoplePicker } from "./PeoplePicker";

/** channels/<id>/info: about, notifications, members with presence, pinned messages, leave. */
export function ChannelInfoScreen() {
  const { id = "" } = useLocalSearchParams<{ id: string }>();
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const me = useMe();
  const channel = useChannel(id);
  const c = channel.data;
  const members = useChannelsMembers(id);
  const pins = useChannelsPins(id);
  const prefs = useChannelPrefs(id, c?.org_id);
  const add = useAddChannelMembers(id);
  const removeMember = useRemoveChannelMember(id);
  const menu = useChannelMenuActions(c, { details: false });
  const [notifyOpen, setNotifyOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [picked, setPicked] = useState<ChannelsPerson[]>([]);
  const [removing, setRemoving] = useState<{ id: string; name: string } | null>(null);
  const [editingTopic, setEditingTopic] = useState(false);
  const [topic, setTopic] = useState("");
  const update = useUpdateChannel(id);
  const memberList = members.data?.items ?? [];
  const presence = usePresence(memberList.map((m) => m.user.id));

  if (channel.isPending) {
    return (
      <Screen edges={["bottom"]}>
        <Stack.Screen options={{ title: "Details" }} />
        <Skeleton height={32} width="60%" />
        <Skeleton height={120} />
        <Skeleton height={200} />
      </Screen>
    );
  }
  if (channel.isError || !c) {
    return (
      <Screen centered edges={["bottom"]}>
        <Stack.Screen options={{ title: "Details" }} />
        {channel.error?.status === 404 ? (
          <EmptyState icon={MessagesSquare} title="Channel not found" />
        ) : (
          <ErrorState title="Couldn't load details" info={channel.error?.message} onRetry={() => void channel.refetch()} retrying={channel.isFetching} />
        )}
      </Screen>
    );
  }

  const direct = isChannelsDirect(c);
  const notifyLabel = CHANNELS_NOTIFY_OPTIONS.find((o) => o.value === c.notify)?.label ?? "Default";
  const effective = channelsEffectiveNotify(c);
  const setPrefs = (body: Parameters<typeof prefs.mutate>[0]) => prefs.mutate(body, { onError: (e) => toast.error(errorMessage(e)) });

  return (
    <Screen edges={["bottom"]}>
      <Stack.Screen
        options={{
          title: "Details",
          headerRight: () => (
            <View>
              <ResourceMenu feature="channels" kind="channels.channel" id={c.id} title={channelsDisplayName(c)} orgId={c.org_id} actions={menu.actions} label="Channel options" />
              {menu.element}
            </View>
          ),
        }}
      />
      <View style={{ gap: s[2] }}>
        <Title>{channelsDisplayName(c)}</Title>
        {c.archived ? (
          <Text role="label" tone="warning">
            Archived
          </Text>
        ) : null}
      </View>

      {!direct ? (
        <ListGroup title="About">
          <ListRow title={c.topic || "Add a topic"} {...(c.permissions.post ? { onPress: () => { setTopic(c.topic); setEditingTopic(true); } } : {})} />
          {c.description ? <ListRow title={c.description} /> : null}
          {c.created_by ? <ListRow title={`Created by ${c.created_by.display_name}`} trailing={<Text tone="muted">{channelsShortTime(c.created_at)}</Text>} /> : null}
        </ListGroup>
      ) : null}

      {c.member ? (
        <ListGroup title="Notifications">
          <ListRow title="Notify me about" leading={<Bell color={col.textMuted} size={20} />} trailing={<Text tone="muted">{c.notify === "default" ? `Default (${effective === "all" ? "all" : "mentions"})` : notifyLabel}</Text>} onPress={() => setNotifyOpen(true)} />
          <Switch label="Mute" info="Muted channels don't show as unread or notify you. Mentions still show a badge." value={c.muted} onValueChange={(v) => setPrefs({ muted: v })} />
          <Switch label="Star" value={c.starred} onValueChange={(v) => setPrefs({ starred: v })} />
        </ListGroup>
      ) : null}

      <View style={{ gap: s[3] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Text role="titleSm" style={{ flex: 1 }} accessibilityRole="header">
            {`Members (${c.member_count})`}
          </Text>
          {!direct && c.permissions.post ? (
            <Button size="md" variant="secondary" leftIcon={UserPlus} onPress={() => setAdding(true)}>
              Add
            </Button>
          ) : null}
        </View>
        {members.isPending ? (
          <Skeleton height={160} />
        ) : members.isError ? (
          <ErrorState compact title="Couldn't load members" info={members.error.message} onRetry={() => void members.refetch()} retrying={members.isFetching} />
        ) : (
          <View>
            {memberList.map((m) => {
              const removable = c.permissions.remove_members && m.user.id !== me.data?.id && !direct;
              return (
                <View key={m.user.id} style={{ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.lg }}>
                  <PresenceAvatar userId={m.user.id} name={m.user.display_name} src={m.user.avatar_url} size="sm" showOffline />
                  <View style={{ flex: 1, minWidth: 0, flexDirection: "row", alignItems: "baseline", gap: s[2], flexWrap: "wrap" }}>
                    <Text role="body" numberOfLines={1}>
                      {m.user.id === me.data?.id ? `${m.user.display_name} (you)` : m.user.display_name}
                    </Text>
                    <Text role="caption" tone="muted">
                      {m.role === "admin" ? "Channel admin · " : ""}
                      {presenceLabel(presence[m.user.id])}
                    </Text>
                  </View>
                  {removable ? (
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel={`Remove ${m.user.display_name}`}
                      onPress={() => setRemoving({ id: m.user.id, name: m.user.display_name })}
                      hitSlop={s[2]}
                      style={{ width: theme.tap.base, height: theme.tap.base, alignItems: "center", justifyContent: "center" }}
                    >
                      <UserRoundMinus color={col.textMuted} size={20} />
                    </Pressable>
                  ) : null}
                </View>
              );
            })}
            {members.hasNextPage ? (
              <Button variant="ghost" loading={members.isFetchingNextPage} onPress={() => void members.fetchNextPage()}>
                Show more
              </Button>
            ) : null}
          </View>
        )}
      </View>

      <ListGroup title={`Pinned (${c.pinned_count})`}>
        {pins.isPending ? (
          <ListRow title="Loading pins" />
        ) : pins.isError ? (
          <ListRow title="Couldn't load pins. Tap to retry." onPress={() => void pins.refetch()} />
        ) : pins.data.items.length === 0 ? (
          <ListRow title="Nothing pinned yet" info="Long-press a message and choose Pin to channel." />
        ) : (
          pins.data.items.map((m) => (
            <ListRow
              key={m.id}
              title={`${m.sender?.display_name ?? m.bot_name ?? "Someone"}: ${m.title ?? channelsPlainText(m.body, m.mentions)}`}
              leading={<Pin color={col.textMuted} size={20} />}
              onPress={() => router.push(m.thread_root_id ? `/channels/thread/${m.thread_root_id}?message=${m.id}` : `/channels/${c.id}?message=${m.id}`)}
            />
          ))
        )}
      </ListGroup>

      <Sheet open={notifyOpen} onClose={() => setNotifyOpen(false)} title="Notify me about">
        <ListGroup>
          {CHANNELS_NOTIFY_OPTIONS.map((o) => (
            <ListRow
              key={o.value}
              title={o.value === "default" ? `Default (${effective === "all" ? "all messages" : "mentions"})` : o.label}
              chevron={false}
              trailing={c.notify === o.value ? <Text tone="primary">Current</Text> : null}
              onPress={() => {
                setPrefs({ notify: o.value });
                setNotifyOpen(false);
              }}
            />
          ))}
        </ListGroup>
      </Sheet>

      <Sheet
        open={adding}
        onClose={() => setAdding(false)}
        title={`Add people to ${channelsDisplayName(c)}`}
        snapPoints={["85%"]}
        footer={
          <Button
            fullWidth
            disabled={picked.length === 0}
            loading={add.isPending}
            onPress={() =>
              add.mutate(
                picked.map((p) => p.id),
                {
                  onSuccess: () => {
                    toast.success(picked.length === 1 ? `${picked[0]?.display_name ?? "1 person"} added.` : `${picked.length} people added.`);
                    setPicked([]);
                    setAdding(false);
                    void members.refetch();
                  },
                  onError: (e) => toast.error(errorMessage(e)),
                },
              )
            }
          >
            {picked.length > 1 ? `Add ${picked.length} people` : "Add"}
          </Button>
        }
      >
        <PeoplePicker orgId={c.org_id} channelId={c.id} selected={picked} onChange={setPicked} max={100} meId={me.data?.id} />
      </Sheet>

      <Sheet
        open={editingTopic}
        onClose={() => !update.isPending && setEditingTopic(false)}
        title="Topic"
        info="Shows at the top of the channel. Anyone in the channel can change it."
        snapPoints={[320]}
        footer={
          <Button
            fullWidth
            loading={update.isPending}
            onPress={() =>
              update.mutate(
                { topic: topic.trim() },
                { onSuccess: () => setEditingTopic(false), onError: (e) => toast.error(errorMessage(e, "Couldn't save the topic.")) },
              )
            }
          >
            Save
          </Button>
        }
      >
        <Input value={topic} onChangeText={setTopic} maxLength={250} autoFocus placeholder="What this channel is for" accessibilityLabel="Topic" returnKeyType="done" />
      </Sheet>

      <ConfirmDialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={`Remove ${removing?.name ?? "this person"} from ${channelsDisplayName(c)}?`}
        confirmLabel="Remove"
        tone="danger"
        onConfirm={() => (removing ? removeMember.mutateAsync(removing.id) : undefined)}
      >
        {c.kind === "private" ? "They lose access to its messages until someone adds them back." : "They can join again from Browse channels."}
      </ConfirmDialog>
    </Screen>
  );
}
