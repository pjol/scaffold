import {
  channelsGroupToken,
  channelsUserToken,
  decodeChannelsMentions,
  encodeChannelsMentions,
  errorMessage,
  searchChannelsEmoji,
  useChannelsEdit,
  useChannelsGroups,
  useChannelsPeople,
  useChannelsSend,
  useChannelsTypingSignal,
  useChannelsWorkspace,
  useUpload,
  type ChannelsCachedMessage,
  type ChannelsDetail,
  type ChannelsPickedMention,
} from "@scaffoldapp/shared";
import { Avatar, Checkbox, IconButton, Input, Spinner, Text, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { AtSign, ImagePlus, Megaphone, Pencil, SendHorizontal, Users, X } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { Pressable, ScrollView, View, type TextInput } from "react-native";

interface Staged {
  id: string;
  uri: string;
  contentType: string;
  name: string;
  size?: number;
  width?: number;
  height?: number;
  status: "uploading" | "done" | "error";
  key?: string;
}

interface Suggestion {
  key: string;
  label: string;
  /** What replaces the typed token. */
  insert: string;
  picked?: ChannelsPickedMention;
  leading: "avatar" | "emoji" | "group" | "everyone";
  avatar?: string | null;
  emoji?: string;
  hint?: string;
}

export interface ComposerProps {
  channel: ChannelsDetail;
  /** Replies go to this thread. */
  rootId?: string | null;
  /** Offer "Also send to channel" (threads outside forums). */
  allowAlsoSend?: boolean;
  editing: ChannelsCachedMessage | null;
  onCancelEdit: () => void;
  /** After a send, so the list can scroll to the newest message. */
  onSent: () => void;
  placeholder: string;
}

/** The token being typed before the cursor: "@jor" or ":ta". */
function activeToken(text: string, cursor: number): { kind: "@" | ":"; query: string; start: number } | null {
  const before = text.slice(0, cursor);
  const m = /(^|\s)([@:])([\w.+-]{0,40})$/.exec(before);
  if (!m) return null;
  const kind = m[2] as "@" | ":";
  if (kind === ":" && (m[3] ?? "").length < 2) return null;
  return { kind, query: m[3] ?? "", start: before.length - (m[3] ?? "").length - 1 };
}

/**
 * Text, photos, @ and : autocomplete, edits. Sits above the keyboard (Screen footer).
 * Mentions show as "@Name" while typing and are sent as tokens.
 */
export function Composer({ channel, rootId = null, allowAlsoSend = false, editing, onCancelEdit, onSent, placeholder }: ComposerProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const config = useChannelsWorkspace(channel.org_id).data?.config;
  const maxFiles = config?.max_attachments ?? 10;
  const maxBytes = config?.attachment_max_bytes ?? 50 * 1024 * 1024;
  const sender = useChannelsSend(channel.id);
  const edit = useChannelsEdit();
  const upload = useUpload();
  const notifyTyping = useChannelsTypingSignal(channel.id, rootId);
  const [text, setText] = useState("");
  const [cursor, setCursor] = useState(0);
  const [picked, setPicked] = useState<ChannelsPickedMention[]>([]);
  const [files, setFiles] = useState<Staged[]>([]);
  const [alsoSend, setAlsoSend] = useState(false);
  const inputRef = useRef<TextInput>(null);

  useEffect(() => {
    if (editing) {
      const d = decodeChannelsMentions(editing.body, editing.mentions);
      setText(d.text);
      setPicked(d.picked);
      inputRef.current?.focus();
    } else {
      setText("");
      setPicked([]);
    }
  }, [editing]);

  const token = activeToken(text, cursor);
  const people = useChannelsPeople(channel.org_id, token?.kind === "@" ? token.query : "", channel.id, token?.kind === "@");
  const groups = useChannelsGroups(channel.org_id, token?.kind === "@" ? token.query : "", token?.kind === "@" && !!config?.groups_enabled);

  const suggestions = useMemo<Suggestion[]>(() => {
    if (!token) return [];
    if (token.kind === ":") {
      return searchChannelsEmoji(token.query, 8).map((e) => ({ key: `e-${e.name}`, label: `:${e.name}:`, insert: `${e.emoji} `, leading: "emoji", emoji: e.emoji }));
    }
    const q = token.query.toLowerCase();
    const out: Suggestion[] = (people.data?.items ?? []).slice(0, 6).map((u) => ({
      key: `u-${u.id}`,
      label: u.display_name,
      insert: `@${u.display_name} `,
      picked: { label: u.display_name, token: channelsUserToken(u.id) },
      leading: "avatar",
      avatar: u.avatar_url,
      ...(u.in_channel ? {} : { hint: "Not in this channel" }),
    }));
    for (const g of (groups.data?.items ?? []).slice(0, 3)) {
      out.push({ key: `g-${g.id}`, label: `@${g.handle}`, insert: `@${g.handle} `, picked: { label: g.handle, token: channelsGroupToken(g.id, g.handle) }, leading: "group", hint: g.name });
    }
    if (channel.permissions.mention_all) {
      for (const k of ["here", "channel"] as const) {
        if (k.startsWith(q)) out.push({ key: `s-${k}`, label: `@${k}`, insert: `@${k} `, leading: "everyone", hint: k === "here" ? "People online" : "Everyone in this channel" });
      }
    }
    return out;
  }, [token, people.data, groups.data, channel.permissions.mention_all]);

  const choose = (sg: Suggestion) => {
    if (!token) return;
    const next = text.slice(0, token.start) + sg.insert + text.slice(cursor);
    setText(next);
    setCursor(token.start + sg.insert.length);
    if (sg.picked) {
      const p = sg.picked;
      setPicked((list) => (list.some((x) => x.token === p.token) ? list : [...list, p]));
    }
  };

  const addPhotos = async () => {
    const room = maxFiles - files.length;
    if (room <= 0) return;
    const chosen = await pickPhotos("library", room, 0.8);
    for (const p of chosen) {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const contentType = p.mimeType || "image/jpeg";
      const name = p.name || `photo-${id}.jpg`;
      if (p.size && p.size > maxBytes) {
        toast.error(`${name} is too large. Choose a smaller photo.`);
        continue;
      }
      setFiles((list) => [...list, { id, uri: p.uri, contentType, name, ...(p.size ? { size: p.size } : {}), ...(p.width && p.height ? { width: p.width, height: p.height } : {}), status: "uploading" }]);
      upload.mutateAsync({ file: { uri: p.uri, mimeType: contentType, name, size: p.size ?? null }, purpose: "channel_attachment", contentType, maxBytes }).then(
        (res) => setFiles((list) => list.map((f) => (f.id === id ? { ...f, status: "done", key: res.key, size: res.size } : f))),
        (err: unknown) => {
          toast.error(errorMessage(err));
          setFiles((list) => list.map((f) => (f.id === id ? { ...f, status: "error" } : f)));
        },
      );
    }
  };

  const uploading = files.some((f) => f.status === "uploading");
  const ready = files.filter((f) => f.status === "done" && f.key);
  const canSend = editing
    ? text.trim().length > 0 || editing.attachments.length > 0
    : !uploading && !files.some((f) => f.status === "error") && (text.trim().length > 0 || ready.length > 0);

  const submit = () => {
    if (!canSend) return;
    const body = encodeChannelsMentions(text.trim(), picked);
    if (editing) {
      if (body !== editing.body) edit.mutate({ message: editing, body }, { onError: (err) => toast.error(errorMessage(err)) });
      onCancelEdit();
      return;
    }
    void sender
      .send({
        body,
        thread_root_id: rootId,
        also_send_to_channel: allowAlsoSend && alsoSend,
        attachments: ready.map((f) => ({
          key: f.key ?? "",
          file_name: f.name,
          content_type: f.contentType,
          preview_url: f.uri,
          ...(f.size ? { size_bytes: f.size } : {}),
          ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
        })),
      })
      .then((msg) => {
        if (!msg) toast.error("Message not sent. Tap Retry on it.");
      });
    setText("");
    setPicked([]);
    setFiles([]);
    setAlsoSend(false);
    onSent();
  };

  return (
    <View style={{ gap: s[2] }}>
      {suggestions.length > 0 ? (
        <View style={{ borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, backgroundColor: c.surfaceRaised, overflow: "hidden" }} accessibilityLabel="Suggestions">
          {suggestions.map((sg) => (
            <Pressable
              key={sg.key}
              accessibilityRole="button"
              accessibilityLabel={sg.hint ? `${sg.label}, ${sg.hint}` : sg.label}
              onPress={() => choose(sg)}
              style={({ pressed }) => ({ flexDirection: "row", alignItems: "center", gap: s[3], minHeight: theme.tap.base, paddingHorizontal: s[3], backgroundColor: pressed ? c.sunken : "transparent" })}
            >
              {sg.leading === "avatar" ? (
                <Avatar name={sg.label} src={sg.avatar ?? null} size="xs" />
              ) : sg.leading === "emoji" ? (
                <Text role="bodyLg">{sg.emoji}</Text>
              ) : sg.leading === "group" ? (
                <Users color={c.textMuted} size={20} />
              ) : (
                <Megaphone color={c.textMuted} size={20} />
              )}
              <Text role="body" numberOfLines={1} style={{ flexShrink: 1 }}>
                {sg.label}
              </Text>
              {sg.hint ? (
                <Text role="caption" tone="muted" numberOfLines={1} style={{ flexShrink: 1, marginLeft: "auto" }}>
                  {sg.hint}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      ) : null}
      {editing ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingLeft: s[3], borderRadius: theme.radius.md, backgroundColor: c.sunken }}>
          <Pencil color={c.textMuted} size={16} />
          <Text role="label" weight={600} style={{ flex: 1 }}>
            Edit message
          </Text>
          <IconButton label="Cancel edit" icon={X} variant="ghost" onPress={onCancelEdit} />
        </View>
      ) : null}
      {files.length > 0 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2], paddingTop: s[2], paddingRight: s[2] }}>
          {files.map((f) => (
            <View key={f.id}>
              <Image source={{ uri: f.uri }} style={{ width: s[16], height: s[16], borderRadius: theme.radius.md, opacity: f.status === "done" ? 1 : 0.6 }} contentFit="cover" />
              {f.status === "uploading" ? (
                <View style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }}>
                  <Spinner label="Uploading" />
                </View>
              ) : null}
              <View style={{ position: "absolute", top: -s[2], right: -s[2] }}>
                <IconButton label="Remove photo" icon={X} variant="secondary" onPress={() => setFiles((list) => list.filter((x) => x.id !== f.id))} />
              </View>
            </View>
          ))}
        </ScrollView>
      ) : null}
      {allowAlsoSend && !editing ? <Checkbox label="Also send to channel" checked={alsoSend} onCheckedChange={setAlsoSend} /> : null}
      <View style={{ flexDirection: "row", alignItems: "flex-end", gap: s[1] }}>
        {!editing ? <IconButton label="Add photos" icon={ImagePlus} variant="ghost" onPress={() => void addPhotos()} disabled={files.length >= maxFiles} /> : null}
        {!editing ? (
          <IconButton
            label="Mention someone"
            icon={AtSign}
            variant="ghost"
            onPress={() => {
              const sep = text.length === 0 || /\s$/.test(text) ? "" : " ";
              const next = `${text}${sep}@`;
              setText(next);
              setCursor(next.length);
              inputRef.current?.focus();
            }}
          />
        ) : null}
        <Input
          ref={inputRef}
          value={text}
          onChangeText={(v) => {
            setText(v);
            if (!editing && v) notifyTyping();
          }}
          onSelectionChange={(e) => setCursor(e.nativeEvent.selection.end)}
          placeholder={editing ? "Edit message" : placeholder}
          accessibilityLabel={editing ? "Edit message" : placeholder}
          multiline
          containerStyle={{ flex: 1 }}
          style={{ maxHeight: theme.type.body.lineHeight * 6 + s[4] }}
        />
        <IconButton label={editing ? "Save" : "Send"} icon={SendHorizontal} variant="primary" onPress={submit} disabled={!canSend} loading={uploading} />
      </View>
    </View>
  );
}
