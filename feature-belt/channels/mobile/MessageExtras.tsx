import {
  channelsAuthorName,
  channelsDisplayName,
  channelsShortTime,
  errorMessage,
  useChannelsCard,
  useChannelsClosePoll,
  useChannelsMessage,
  useChannelsVote,
  type ChannelsCachedMessage,
  type ChannelsLinkPreview,
} from "@scaffoldapp/shared";
import { Button, Checkbox, Skeleton, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { useRouter, type Href } from "expo-router";
import { BarChart3, FileText, Forward, X } from "lucide-react-native";
import { Linking, Pressable, View } from "react-native";
import { WEB_URL } from "@/lib/env";
import { ChannelsMarkdown } from "./Markdown";

/** App paths channels can open here; anything else opens on the web app. */
export function useOpenAppPath(): (path: string) => void {
  const router = useRouter();
  return (path: string) => {
    if (path.startsWith("/channels/")) router.push(path as Href);
    else if (WEB_URL) void Linking.openURL(`${WEB_URL}${path}`);
  };
}

function InternalCard({ url }: { url: string }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const card = useChannelsCard(url);
  const open = useOpenAppPath();
  if (card.isPending) return <Skeleton width="100%" height={56} />;
  if (!card.data) return null;
  const d = card.data;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`${d.title}. ${d.context}`}
      onPress={() => open(d.link)}
      style={({ pressed }) => ({ flexDirection: "row", gap: s[3], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface })}
    >
      <FileText color={c.textMuted} size={18} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <Text role="label" weight={600} numberOfLines={1}>
          {d.title}
        </Text>
        {d.context ? (
          <Text role="caption" tone="muted" numberOfLines={1}>
            {d.context}
          </Text>
        ) : null}
        {d.snippet ? (
          <Text role="label" tone="muted" numberOfLines={2}>
            {d.snippet}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function ExternalCard({ link }: { link: ChannelsLinkPreview }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const ratio = link.image_width && link.image_height ? link.image_width / link.image_height : null;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`${link.title || link.url}, ${link.site_name}`}
      onPress={() => void Linking.openURL(link.url)}
      style={({ pressed }) => ({ borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: pressed ? c.sunken : c.surface, overflow: "hidden" })}
    >
      {link.image_url && ratio && ratio > 1.3 ? <Image source={{ uri: link.image_url }} contentFit="cover" style={{ width: "100%", aspectRatio: Math.min(ratio, 2.2) }} /> : null}
      <View style={{ padding: s[3], gap: s[1], flexDirection: "row" }}>
        <View style={{ flex: 1, minWidth: 0, gap: s[1] }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            {link.icon_url ? <Image source={{ uri: link.icon_url }} style={{ width: 16, height: 16, borderRadius: 2 }} /> : null}
            <Text role="caption" tone="muted" numberOfLines={1}>
              {link.site_name}
            </Text>
          </View>
          {link.title ? (
            <Text role="label" weight={600} tone="primary" numberOfLines={2}>
              {link.title}
            </Text>
          ) : null}
          {link.description ? (
            <Text role="label" tone="muted" numberOfLines={3}>
              {link.description}
            </Text>
          ) : null}
        </View>
        {link.image_url && !(ratio && ratio > 1.3) ? <Image source={{ uri: link.image_url }} contentFit="cover" style={{ width: 56, height: 56, borderRadius: theme.radius.sm }} /> : null}
      </View>
    </Pressable>
  );
}

/** Link previews under a message; the author can remove them. */
export function LinkPreviews({ message, own, onRemove }: { message: ChannelsCachedMessage; own: boolean; onRemove: () => void }) {
  const s = useTheme().space;
  if (message.links_hidden || message.links.length === 0) return null;
  const external = message.links.filter((l) => l.kind === "external");
  return (
    <View style={{ gap: s[2], maxWidth: 360 }}>
      {message.links
        .filter((l) => l.kind === "internal")
        .map((l) => (
          <InternalCard key={l.url} url={l.url} />
        ))}
      {external.map((l) => (
        <ExternalCard key={l.url} link={l} />
      ))}
      {own && external.length > 0 ? (
        <View style={{ alignSelf: "flex-start" }}>
          <Button size="md" variant="ghost" leftIcon={X} onPress={onRemove}>
            Remove previews
          </Button>
        </View>
      ) : null}
    </View>
  );
}

/** A forwarded message: the original as you may see it. */
export function ForwardCard({ messageId, meId }: { messageId: string | null; meId: string | undefined }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const original = useChannelsMessage(messageId);
  const open = useOpenAppPath();
  const box = { gap: s[1], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: c.surface, maxWidth: 360 } as const;
  if (!messageId || original.isError) {
    return (
      <View style={[box, { flexDirection: "row", alignItems: "center", gap: s[2] }]}>
        <Forward color={c.textMuted} size={14} />
        <Text role="caption" tone="muted">
          Forwarded message unavailable
        </Text>
      </View>
    );
  }
  if (original.isPending) return <Skeleton width="100%" height={64} />;
  const { message: m, channel } = original.data;
  return (
    <Pressable accessibilityRole="link" accessibilityLabel="Forwarded message. Open it" onPress={() => open(m.thread_root_id ? `/channels/thread/${m.thread_root_id}` : `/channels/${m.channel_id}?message=${m.id}`)} style={box}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Forward color={c.textMuted} size={14} />
        <Text role="caption" tone="muted" numberOfLines={1} style={{ flex: 1 }}>
          {channelsAuthorName(m, meId)} · {channelsDisplayName(channel)} · {channelsShortTime(m.created_at)}
        </Text>
      </View>
      {m.deleted ? (
        <Text role="body" tone="muted" style={{ fontStyle: "italic" }}>
          Message deleted
        </Text>
      ) : (
        <ChannelsMarkdown body={m.body || " "} mentions={m.mentions} meId={meId} numberOfLines={6} />
      )}
    </Pressable>
  );
}

/** A poll with live counts; tap to vote (one or several), the author can close it. */
export function PollCard({ message, meId, canVote, canClose, names }: { message: ChannelsCachedMessage; meId: string | undefined; canVote: boolean; canClose: boolean; names: Map<string, string> }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const vote = useChannelsVote();
  const close = useChannelsClosePoll();
  const poll = message.poll;
  if (!poll) return null;
  const mine = new Set(poll.my_votes ?? []);
  const total = poll.options.reduce((n, o) => n + o.count, 0);
  const open = !poll.closed && canVote && !message.status;
  const choose = (id: string) => {
    const next = poll.multiple ? (mine.has(id) ? [...mine].filter((x) => x !== id) : [...mine, id]) : mine.has(id) ? [] : [id];
    vote.mutate({ message, optionIds: next }, { onError: (e) => toast.error(errorMessage(e)) });
  };
  return (
    <View style={{ gap: s[3], padding: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: c.surface, maxWidth: 360 }} accessibilityLabel={`Poll: ${message.body}`}>
      <View style={{ flexDirection: "row", gap: s[2] }}>
        <BarChart3 color={c.primaryStrong} size={18} />
        <View style={{ flex: 1 }}>
          <ChannelsMarkdown body={message.body} mentions={message.mentions} meId={meId} />
        </View>
      </View>
      {poll.options.map((o) => {
        const pct = total ? Math.round((o.count / total) * 100) : 0;
        const chosen = mine.has(o.id);
        const voters = !poll.anonymous && o.voter_ids.length ? o.voter_ids.map((id) => (id === meId ? "You" : (names.get(id) ?? "Someone"))).join(", ") : "";
        return (
          <View key={o.id} style={{ gap: s[1] }}>
            {open && poll.multiple ? (
              <Checkbox label={`${o.text} · ${o.count}`} checked={chosen} onCheckedChange={() => choose(o.id)} />
            ) : (
              <Pressable
                accessibilityRole={open ? "radio" : "text"}
                accessibilityState={{ checked: chosen, disabled: !open }}
                disabled={!open}
                onPress={() => choose(o.id)}
                style={{ flexDirection: "row", alignItems: "center", gap: s[2], minHeight: theme.tap.sm }}
              >
                {open ? (
                  <View style={{ width: 18, height: 18, borderRadius: 9, borderWidth: 1.5, borderColor: chosen ? c.primary : c.borderStrong, alignItems: "center", justifyContent: "center" }}>
                    {chosen ? <View style={{ width: 9, height: 9, borderRadius: 5, backgroundColor: c.primary }} /> : null}
                  </View>
                ) : null}
                <Text role="label" weight={chosen ? 600 : 400} style={{ flex: 1 }}>
                  {o.text}
                </Text>
                <Text role="caption" tone="muted" tabular>
                  {o.count} · {pct}%
                </Text>
              </Pressable>
            )}
            <View style={{ height: 6, borderRadius: 3, backgroundColor: c.sunken, overflow: "hidden" }}>
              <View style={{ width: `${pct}%`, height: "100%", backgroundColor: chosen ? c.primary : c.borderStrong }} />
            </View>
            {voters ? (
              <Text role="caption" tone="muted" numberOfLines={1}>
                {voters}
              </Text>
            ) : null}
          </View>
        );
      })}
      <View style={{ flexDirection: "row", alignItems: "center" }}>
        <Text role="caption" tone="muted" style={{ flex: 1 }}>
          {poll.voters === 1 ? "1 vote" : `${poll.voters} votes`}
          {poll.multiple ? " · Several choices" : ""}
          {poll.anonymous ? " · Anonymous" : ""}
          {poll.closed ? " · Closed" : ""}
        </Text>
        {canClose && !poll.closed ? (
          <Button size="md" variant="ghost" loading={close.isPending} onPress={() => close.mutate(message, { onError: (e) => toast.error(errorMessage(e)) })}>
            Close poll
          </Button>
        ) : null}
      </View>
    </View>
  );
}

