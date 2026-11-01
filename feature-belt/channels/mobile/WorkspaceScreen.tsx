import {
  channelsNameFrom,
  channelsSections,
  errorMessage,
  hasErrorCode,
  orgsNoun,
  orgsNounA,
  orgsNounTitle,
  useActiveOrg,
  useChannelsWorkspace,
  useCreateChannel,
  type ChannelsSidebarChannel,
} from "@scaffoldapp/shared";
import { AvatarGroup, Badge, Button, EmptyState, ErrorState, Field, IconButton, Input, SegmentedControl, Sheet, SkeletonRow, Text, Title, haptic, toast, useLayout, useTheme } from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { Bookmark, Building2, Compass, Hash, LayoutList, Lock, MessageSquareText, Plus, Search, SquarePen } from "lucide-react-native";
import { useRef, useState } from "react";
import { Pressable, RefreshControl, SectionList, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { PresenceAvatar } from "@/features/presence";

type Row =
  | { kind: "shortcut"; id: "threads" | "later"; label: string; count: number }
  | { kind: "channel"; channel: ChannelsSidebarChannel }
  | { kind: "action"; id: "browse" | "new-dm"; label: string };

function ChannelIcon({ c, color }: { c: ChannelsSidebarChannel; color: string }) {
  if (c.layout === "forum") return <LayoutList color={color} size={20} />;
  if (c.kind === "private") return <Lock color={color} size={20} />;
  return <Hash color={color} size={20} />;
}

function ChannelRow({ c }: { c: ChannelsSidebarChannel }) {
  const theme = useTheme();
  const col = theme.colors;
  const s = theme.space;
  const direct = c.kind === "dm" || c.kind === "group_dm";
  const bold = c.unread > 0 && !c.muted;
  const tone = c.muted ? "subtle" : bold ? "default" : "muted";
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${direct ? c.name : `${c.kind === "private" ? "Private channel" : "Channel"} ${c.name}`}${c.mentions ? `, ${c.mentions} mentions` : bold ? ", unread" : ""}${c.muted ? ", muted" : ""}`}
      onPress={() => router.push(`/channels/${c.id}`)}
      style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, paddingVertical: s[1], backgroundColor: pressed ? col.sunken : "transparent" })}
    >
      <View style={{ width: s[8], alignItems: "center" }}>
        {c.kind === "dm" && c.people[0] ? (
          <PresenceAvatar userId={c.people[0].id} name={c.people[0].display_name} src={c.people[0].avatar_url} size="xs" />
        ) : c.kind === "group_dm" ? (
          <AvatarGroup people={c.people.map((p) => ({ name: p.display_name, src: p.avatar_url }))} size="xs" max={2} />
        ) : c.kind === "dm" ? (
          <PresenceAvatar userId={null} name="You" size="xs" />
        ) : (
          <ChannelIcon c={c} color={bold ? col.text : col.textMuted} />
        )}
      </View>
      <Text role="body" weight={bold ? 700 : 400} tone={tone} numberOfLines={1} style={{ flex: 1 }}>
        {c.name}
      </Text>
      {c.mentions > 0 ? (
        <Badge tone="danger" variant="solid" size="sm">
          {c.mentions > 99 ? "99+" : String(c.mentions)}
        </Badge>
      ) : null}
    </Pressable>
  );
}

function NewChannelSheet({ orgId, open, onClose, canPublic, canPrivate }: { orgId: string; open: boolean; onClose: () => void; canPublic: boolean; canPrivate: boolean }) {
  const theme = useTheme();
  const create = useCreateChannel(orgId);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"public" | "private">(canPublic ? "public" : "private");
  const [layout, setLayout] = useState<"chat" | "forum">("chat");
  const [error, setError] = useState<string | null>(null);
  const submit = async () => {
    const n = channelsNameFrom(name);
    if (!n) return setError("Enter a name.");
    setError(null);
    try {
      const c = await create.mutateAsync({ name: n, kind, layout });
      haptic.success();
      setName("");
      onClose();
      router.push(`/channels/${c.id}`);
    } catch (e) {
      if (hasErrorCode(e, "conflict") || hasErrorCode(e, "invalid_request")) setError(errorMessage(e));
      else toast.error(errorMessage(e, "Couldn't create the channel. Try again."));
    }
  };
  return (
    <Sheet
      open={open}
      onClose={() => !create.isPending && onClose()}
      title="New channel"
      snapPoints={[440]}
      footer={
        <Button fullWidth loading={create.isPending} onPress={() => void submit()}>
          Create
        </Button>
      }
    >
      <View style={{ gap: theme.space[4] }}>
        <Field label="Name" error={error} info="Lowercase letters, numbers and dashes. People find it as #name.">
          <Input value={name} onChangeText={(v) => setName(channelsNameFrom(v))} maxLength={80} autoCapitalize="none" autoCorrect={false} placeholder="e.g. design-reviews" autoFocus returnKeyType="done" onSubmitEditing={() => void submit()} />
        </Field>
        {canPublic && canPrivate ? (
          <Field label="Who can join" info={`Anyone in the ${orgsNoun()} can find and join a public channel. A private one is by invitation.`}>
            <SegmentedControl<"public" | "private">
              value={kind}
              onValueChange={setKind}
              options={[
                { value: "public", label: "Public" },
                { value: "private", label: "Private" },
              ]}
            />
          </Field>
        ) : null}
        <Field label="Layout" info="Chat is a stream of messages. Forum is a list of posts with titles, each with its own replies.">
          <SegmentedControl<"chat" | "forum">
            value={layout}
            onValueChange={setLayout}
            options={[
              { value: "chat", label: "Chat" },
              { value: "forum", label: "Forum" },
            ]}
          />
        </Field>
      </View>
    </Sheet>
  );
}

/** Channels tab: the active org's workspace — Threads, Later, starred, channels and direct messages. */
export function WorkspaceScreen() {
  const theme = useTheme();
  const layout = useLayout();
  const insets = useSafeAreaInsets();
  const col = theme.colors;
  const s = theme.space;
  const { org, isPending: orgPending } = useActiveOrg();
  const ws = useChannelsWorkspace(org?.id);
  const [creating, setCreating] = useState(false);
  const listRef = useRef<SectionList<Row>>(null);
  useScrollToTop(listRef);
  const pad = { paddingLeft: insets.left + layout.gutter, paddingRight: insets.right + layout.gutter };
  const perms = ws.data?.permissions;
  const canCreate = !!perms && (perms.create_public || perms.create_private);

  const header = (
    <View style={{ flexDirection: "row", alignItems: "center", gap: s[1], paddingTop: insets.top + s[4], paddingBottom: s[3], ...pad }}>
      <Title numberOfLines={1} style={{ flex: 1 }}>
        {org?.name ?? "Channels"}
      </Title>
      {org ? (
        <>
          <IconButton label="Search messages" icon={Search} variant="ghost" onPress={() => router.push("/channels/search")} />
          <IconButton label="New message" icon={SquarePen} variant="soft" onPress={() => router.push("/channels/new-dm")} />
          <ResourceMenu
            feature="channels"
            title={org.name}
            orgId={org.id}
            actions={[
              { id: "browse", label: "Browse channels", icon: Compass, onSelect: () => router.push("/channels/browse") },
              ...(canCreate ? [{ id: "new", label: "New channel", icon: Plus, onSelect: () => setCreating(true) }] : []),
              { id: "threads", label: "Threads", icon: MessageSquareText, onSelect: () => router.push("/channels/threads") },
              { id: "later", label: "Later", icon: Bookmark, onSelect: () => router.push("/channels/later") },
            ]}
          />
        </>
      ) : null}
    </View>
  );

  let body;
  if (orgPending || (org && ws.isPending)) {
    body = (
      <View style={{ gap: s[2], ...pad }}>
        {Array.from({ length: 8 }, (_, i) => (
          <SkeletonRow key={i} />
        ))}
      </View>
    );
  } else if (!org) {
    body = (
      <EmptyState
        icon={Building2}
        title={`Join ${orgsNounA()} first`}
        info={`Channels belong to ${orgsNounA()}. Create one or accept an invite in Settings.`}
        action={
          <Button variant="secondary" onPress={() => router.push("/settings/orgs")}>
            {orgsNounTitle("many")}
          </Button>
        }
      />
    );
  } else if (ws.isError) {
    body = <ErrorState title="Couldn't load channels" info={ws.error.message} onRetry={() => void ws.refetch()} retrying={ws.isFetching} />;
  } else {
    const data = ws.data;
    const sec = channelsSections(data?.channels ?? []);
    const sections: { key: string; title?: string; data: Row[] }[] = [
      {
        key: "shortcuts",
        data: [
          { kind: "shortcut", id: "threads", label: "Threads", count: data?.threads_unread ?? 0 },
          { kind: "shortcut", id: "later", label: "Later", count: data?.later_count ?? 0 },
        ],
      },
      ...(sec.starred.length ? [{ key: "starred", title: "Starred", data: sec.starred.map((channel): Row => ({ kind: "channel", channel })) }] : []),
      { key: "channels", title: "Channels", data: [...sec.channels.map((channel): Row => ({ kind: "channel", channel })), { kind: "action", id: "browse", label: "Browse channels" }] },
      { key: "direct", title: "Direct messages", data: [...sec.direct.map((channel): Row => ({ kind: "channel", channel })), { kind: "action", id: "new-dm", label: "New message" }] },
    ];
    body = (
      <SectionList
        ref={listRef}
        sections={sections}
        keyExtractor={(r) => (r.kind === "channel" ? r.channel.id : `${r.kind}-${r.id}`)}
        stickySectionHeadersEnabled={false}
        renderSectionHeader={({ section }) =>
          section.title ? (
            <View style={{ paddingTop: s[6], paddingBottom: s[1] }}>
              <Text role="label" weight={600} tone="muted" accessibilityRole="header">
                {section.title}
              </Text>
            </View>
          ) : null
        }
        renderItem={({ item }) => {
          if (item.kind === "channel") return <ChannelRow c={item.channel} />;
          const Icon = item.kind === "shortcut" ? (item.id === "threads" ? MessageSquareText : Bookmark) : item.id === "browse" ? Compass : Plus;
          const onPress = () => router.push(item.kind === "shortcut" ? `/channels/${item.id}` : item.id === "browse" ? "/channels/browse" : "/channels/new-dm");
          const count = item.kind === "shortcut" ? item.count : 0;
          return (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={count ? `${item.label}, ${count}` : item.label}
              onPress={onPress}
              style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, backgroundColor: pressed ? col.sunken : "transparent" })}
            >
              <View style={{ width: s[8], alignItems: "center" }}>
                <Icon color={col.textMuted} size={20} />
              </View>
              <Text role="body" weight={count ? 700 : 400} tone={count || item.kind === "shortcut" ? "default" : "muted"} style={{ flex: 1 }}>
                {item.label}
              </Text>
              {count ? (
                <Badge tone="primary" variant="solid" size="sm">
                  {count > 99 ? "99+" : String(count)}
                </Badge>
              ) : null}
            </Pressable>
          );
        }}
        ListFooterComponent={
          canCreate ? (
            <View style={{ paddingTop: s[6] }}>
              <Button variant="secondary" leftIcon={Plus} onPress={() => setCreating(true)}>
                New channel
              </Button>
            </View>
          ) : null
        }
        contentContainerStyle={{ ...pad, paddingBottom: s[8] }}
        refreshControl={<RefreshControl refreshing={ws.isRefetching} onRefresh={() => void ws.refetch()} tintColor={col.textMuted} />}
      />
    );
  }

  return (
    <View style={{ flex: 1, backgroundColor: col.canvas }}>
      {header}
      <View style={{ flex: 1 }}>{body}</View>
      {org && perms ? <NewChannelSheet orgId={org.id} open={creating} onClose={() => setCreating(false)} canPublic={perms.create_public} canPrivate={perms.create_private} /> : null}
    </View>
  );
}
