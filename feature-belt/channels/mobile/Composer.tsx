import {
  CHANNELS_SCHEDULE_OPTIONS,
  channelsContentType,
  channelsEndpoints,
  channelsGroupToken,
  channelsUserToken,
  channelsWhen,
  decodeChannelsMentions,
  encodeChannelsMentions,
  errorMessage,
  findChannelsSlashCommand,
  matchChannelsSlashCommands,
  parseChannelsSlash,
  searchChannelsEmoji,
  useApi,
  useChannelsCreatePoll,
  useChannelsDraft,
  useChannelsDraftSaver,
  useChannelsEdit,
  useChannelsGroups,
  useChannelsPeople,
  useChannelsSchedule,
  useChannelsSend,
  useChannelsTypingSignal,
  useChannelsWorkspace,
  useMe,
  useQueryClient,
  useUpload,
  type ChannelsCachedMessage,
  type ChannelsDetail,
  type ChannelsPickedMention,
} from "@scaffoldapp/shared";
import { Avatar, Button, Checkbox, Field, FileIcon, IconButton, Input, ListGroup, ListRow, Sheet, Spinner, Switch, Text, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import * as DocumentPicker from "expo-document-picker";
import { Image } from "expo-image";
import { useRouter, type Href } from "expo-router";
import { AtSign, BarChart3, CalendarClock, ImagePlus, Megaphone, Paperclip, Pencil, Plus, SendHorizontal, Slash, Trash2, Users, X } from "lucide-react-native";
import { useEffect, useMemo, useRef, useState } from "react";
import { Pressable, ScrollView, View, type TextInput } from "react-native";
import { EmojiGlyph, useWorkspaceEmoji } from "./emoji";

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
  image: boolean;
}

interface Suggestion {
  key: string;
  label: string;
  /** What replaces the typed token. */
  insert: string;
  picked?: ChannelsPickedMention;
  leading: "avatar" | "emoji" | "custom" | "group" | "everyone" | "command";
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
  /** /collapse and /expand. */
  onCollapse?: (collapsed: boolean) => void;
  placeholder: string;
}

/** The token being typed before the cursor: "@jor", ":ta" or a "/command" at the start. */
function activeToken(text: string, cursor: number): { kind: "@" | ":" | "/"; query: string; start: number } | null {
  const before = text.slice(0, cursor);
  const cmd = /^\/([a-z0-9-]{0,24})$/i.exec(before);
  if (cmd) return { kind: "/", query: cmd[1] ?? "", start: 0 };
  const m = /(^|\s)([@:])([\w.+-]{0,40})$/.exec(before);
  if (!m) return null;
  const kind = m[2] as "@" | ":";
  if (kind === ":" && (m[3] ?? "").length < 2) return null;
  return { kind, query: m[3] ?? "", start: before.length - (m[3] ?? "").length - 1 };
}

/**
 * Text, photos and files, @ : and / autocomplete, slash commands, send later, polls and edits;
 * the draft is kept on the server. Sits above the keyboard (Screen footer). Mentions show as
 * "@Name" while typing and are sent as tokens.
 */
export function Composer({ channel, rootId = null, allowAlsoSend = false, editing, onCancelEdit, onSent, onCollapse, placeholder }: ComposerProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const router = useRouter();
  const api = useApi();
  const qc = useQueryClient();
  const me = useMe();
  const config = useChannelsWorkspace(channel.org_id).data?.config;
  const maxFiles = config?.max_attachments ?? 10;
  const maxBytes = config?.attachment_max_bytes ?? 50 * 1024 * 1024;
  const sender = useChannelsSend(channel.id);
  const edit = useChannelsEdit();
  const upload = useUpload();
  const schedule = useChannelsSchedule(channel.id, channel.org_id);
  const createPoll = useChannelsCreatePoll(channel.id);
  const serverDraft = useChannelsDraft(channel.member ? channel.id : null, rootId);
  const saver = useChannelsDraftSaver(channel.id, rootId, channel.org_id);
  const notifyTyping = useChannelsTypingSignal(channel.id, rootId);
  const { list: customEmoji } = useWorkspaceEmoji();
  const [text, setText] = useState("");
  const [cursor, setCursor] = useState(0);
  const [picked, setPicked] = useState<ChannelsPickedMention[]>([]);
  const [files, setFiles] = useState<Staged[]>([]);
  const [alsoSend, setAlsoSend] = useState(false);
  const [sheet, setSheet] = useState<"attach" | "later" | "poll" | null>(null);
  const [when, setWhen] = useState("");
  const [poll, setPoll] = useState({ question: "", options: ["", ""], multiple: false, anonymous: false });
  const afterSheet = useRef<(() => void) | null>(null);
  const loaded = useRef(false);
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

  // The server's draft (another device, or a previous visit) fills an empty composer once.
  useEffect(() => {
    const d = serverDraft.data;
    if (!d || loaded.current || editing) return;
    loaded.current = true;
    const decoded = decodeChannelsMentions(d.body, d.mentions);
    setText((t) => t || decoded.text);
    setPicked((p) => (p.length ? p : decoded.picked));
  }, [serverDraft.data, editing]);

  const updateText = (v: string) => {
    setText(v);
    if (!editing && channel.member && !v.startsWith("/")) saver.save(encodeChannelsMentions(v, picked));
  };

  const token = activeToken(text, cursor);
  const people = useChannelsPeople(channel.org_id, token?.kind === "@" ? token.query : "", channel.id, token?.kind === "@");
  const groups = useChannelsGroups(channel.org_id, token?.kind === "@" ? token.query : "", token?.kind === "@" && !!config?.groups_enabled);

  const suggestions = useMemo<Suggestion[]>(() => {
    if (!token) return [];
    if (token.kind === "/") {
      return matchChannelsSlashCommands(token.query, channel)
        .slice(0, 6)
        .map((cmd) => ({ key: `c-${cmd.name}`, label: `/${cmd.name}`, insert: `/${cmd.name} `, leading: "command" as const, hint: cmd.hint }));
    }
    if (token.kind === ":") {
      const q = token.query.toLowerCase();
      const custom = customEmoji
        .filter((e) => e.name.includes(q))
        .slice(0, 3)
        .map((e) => ({ key: `ce-${e.id}`, label: `:${e.name}:`, insert: `:${e.name}: `, leading: "custom" as const }));
      return [...custom, ...searchChannelsEmoji(token.query, 8 - custom.length).map((e) => ({ key: `e-${e.name}`, label: `:${e.name}:`, insert: `${e.emoji} `, leading: "emoji" as const, emoji: e.emoji }))];
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
  }, [token, people.data, groups.data, channel, customEmoji]);

  const choose = (sg: Suggestion) => {
    if (!token) return;
    const next = text.slice(0, token.start) + sg.insert + text.slice(cursor);
    updateText(next);
    setCursor(token.start + sg.insert.length);
    if (sg.picked) {
      const p = sg.picked;
      setPicked((list) => (list.some((x) => x.token === p.token) ? list : [...list, p]));
    }
  };

  const stage = (f: { uri: string; name: string; mimeType?: string | null; size?: number | null; width?: number; height?: number }) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const contentType = channelsContentType(f.name, f.mimeType);
    if (f.size && f.size > maxBytes) {
      toast.error(`${f.name} is too large. Choose a smaller file.`);
      return;
    }
    setFiles((list) => [
      ...list,
      { id, uri: f.uri, contentType, name: f.name, ...(f.size ? { size: f.size } : {}), ...(f.width && f.height ? { width: f.width, height: f.height } : {}), status: "uploading", image: contentType.startsWith("image/") },
    ]);
    upload.mutateAsync({ file: { uri: f.uri, mimeType: contentType, name: f.name, size: f.size ?? null }, purpose: "channel_attachment", contentType, maxBytes }).then(
      (res) => setFiles((list) => list.map((x) => (x.id === id ? { ...x, status: "done", key: res.key, size: res.size } : x))),
      (err: unknown) => {
        toast.error(errorMessage(err));
        setFiles((list) => list.map((x) => (x.id === id ? { ...x, status: "error" } : x)));
      },
    );
  };

  const addPhotos = async () => {
    const room = maxFiles - files.length;
    if (room <= 0) return;
    for (const p of await pickPhotos("library", room, 0.8)) {
      stage({ uri: p.uri, name: p.name || `photo-${Date.now()}.jpg`, mimeType: p.mimeType || "image/jpeg", size: p.size ?? null, ...(p.width && p.height ? { width: p.width, height: p.height } : {}) });
    }
  };

  const addFiles = async () => {
    const room = maxFiles - files.length;
    if (room <= 0) return;
    const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
    if (res.canceled) return;
    for (const a of res.assets.slice(0, room)) stage({ uri: a.uri, name: a.name, mimeType: a.mimeType ?? null, size: a.size ?? null });
  };

  const uploading = files.some((f) => f.status === "uploading");
  const ready = files.filter((f) => f.status === "done" && f.key);
  const canSend = editing
    ? text.trim().length > 0 || editing.attachments.length > 0
    : !uploading && !files.some((f) => f.status === "error") && (text.trim().length > 0 || ready.length > 0);
  const attachments = () =>
    ready.map((f) => ({
      key: f.key ?? "",
      file_name: f.name,
      content_type: f.contentType,
      ...(f.size ? { size_bytes: f.size } : {}),
      ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
    }));

  const reset = () => {
    setText("");
    setPicked([]);
    setFiles([]);
    setAlsoSend(false);
    saver.cancel();
  };

  const runCommand = (): boolean => {
    const parsed = parseChannelsSlash(text);
    if (!parsed || files.length > 0) return false;
    const cmd = findChannelsSlashCommand(parsed.name);
    if (!cmd) {
      toast.error(`/${parsed.name} isn't a command. Type / to see them.`);
      return true;
    }
    const args = encodeChannelsMentions(parsed.args, picked);
    reset();
    void Promise.resolve(
      cmd.run({
        args,
        channel,
        threadRootId: rootId,
        meId: me.data?.id ?? "",
        http: api.http,
        api: channelsEndpoints(api.http),
        qc,
        navigate: (path) => router.push(path as Href),
        notify: (message, tone) => (tone === "error" ? toast.error(message) : toast.success(message)),
        send: (body) => sender.send({ body, thread_root_id: rootId }),
        setText: (t) => updateText(t),
        openPoll: (q) => {
          setPoll({ question: q, options: ["", ""], multiple: false, anonymous: false });
          setSheet("poll");
        },
        setCollapsed: (v) => {
          onCollapse?.(v);
          toast.success(v ? "Previews and media collapsed here." : "Previews and media shown.");
        },
      }),
    ).catch((e: unknown) => toast.error(errorMessage(e)));
    return true;
  };

  const submit = () => {
    if (!editing && text.trim().startsWith("/") && runCommand()) return;
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
          ...(f.image ? { preview_url: f.uri } : {}),
          ...(f.size ? { size_bytes: f.size } : {}),
          ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
        })),
      })
      .then((msg) => {
        if (!msg) toast.error("Message not sent. Tap Retry on it.");
      });
    reset();
    onSent();
  };

  const sendLater = (w: string) =>
    schedule.mutate(
      { body: encodeChannelsMentions(text.trim(), picked), thread_root_id: rootId, also_send_to_channel: allowAlsoSend && alsoSend, attachments: attachments(), when: w },
      {
        onSuccess: (r) => {
          reset();
          setSheet(null);
          toast.success(`Scheduled for ${channelsWhen(r.send_at)}.`);
        },
        onError: (e) => toast.error(errorMessage(e)),
      },
    );

  const closeSheet = (then?: () => void) => {
    afterSheet.current = then ?? null;
    setSheet(null);
  };
  const filledOptions = poll.options.map((o) => o.trim()).filter(Boolean);
  const canPost = channel.permissions.post || (!!rootId && channel.permissions.reply);

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
              ) : sg.leading === "custom" ? (
                <EmojiGlyph value={sg.label} size={22} />
              ) : sg.leading === "command" ? (
                <Slash color={c.textMuted} size={18} />
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
              {f.image ? (
                <Image source={{ uri: f.uri }} style={{ width: s[16], height: s[16], borderRadius: theme.radius.md, opacity: f.status === "done" ? 1 : 0.6 }} contentFit="cover" />
              ) : (
                <View style={{ width: 160, height: s[16], flexDirection: "row", alignItems: "center", gap: s[2], paddingHorizontal: s[3], borderRadius: theme.radius.md, borderWidth: 1, borderColor: c.border, opacity: f.status === "done" ? 1 : 0.6 }}>
                  <FileIcon name={f.name} mime={f.contentType} />
                  <Text role="caption" numberOfLines={2} style={{ flex: 1 }}>
                    {f.name}
                  </Text>
                </View>
              )}
              {f.status === "uploading" ? (
                <View style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center" }}>
                  <Spinner label={`Uploading ${f.name}`} />
                </View>
              ) : f.status === "error" ? (
                <View style={{ position: "absolute", top: 0, left: 0, right: 0, bottom: 0, alignItems: "center", justifyContent: "center", borderRadius: theme.radius.md, backgroundColor: c.dangerSoft }}>
                  <Text role="caption" tone="danger">
                    Failed
                  </Text>
                </View>
              ) : null}
              <View style={{ position: "absolute", top: -s[2], right: -s[2] }}>
                <IconButton label={`Remove ${f.name}`} icon={X} variant="secondary" onPress={() => setFiles((list) => list.filter((x) => x.id !== f.id))} />
              </View>
            </View>
          ))}
        </ScrollView>
      ) : null}
      {allowAlsoSend && !editing ? <Checkbox label="Also send to channel" checked={alsoSend} onCheckedChange={setAlsoSend} /> : null}
      <View style={{ flexDirection: "row", alignItems: "flex-end", gap: s[1] }}>
        {!editing ? <IconButton label="Add to message" icon={Plus} variant="ghost" onPress={() => setSheet("attach")} /> : null}
        <Input
          ref={inputRef}
          value={text}
          onChangeText={(v) => {
            updateText(v);
            if (!editing && v && !v.startsWith("/")) notifyTyping();
          }}
          onSelectionChange={(e) => setCursor(e.nativeEvent.selection.end)}
          placeholder={editing ? "Edit message" : placeholder}
          accessibilityLabel={editing ? "Edit message" : placeholder}
          multiline
          containerStyle={{ flex: 1 }}
          style={{ maxHeight: theme.type.body.lineHeight * 6 + s[4] }}
        />
        {!editing && canSend && !text.trim().startsWith("/") && (channel.layout !== "forum" || rootId) ? (
          <IconButton label="Send later" icon={CalendarClock} variant="ghost" onPress={() => (setWhen(""), setSheet("later"))} />
        ) : null}
        <IconButton label={editing ? "Save" : "Send"} icon={SendHorizontal} variant="primary" onPress={submit} disabled={!canSend && !text.trim().startsWith("/")} loading={uploading} />
      </View>
      <Sheet
        open={sheet === "attach"}
        onClose={() => closeSheet()}
        onDismissed={() => {
          const run = afterSheet.current;
          afterSheet.current = null;
          run?.();
        }}
        title="Add to message"
      >
        <ListGroup>
          <ListRow title="Photos" leading={<ImagePlus color={c.textMuted} size={20} />} disabled={files.length >= maxFiles} onPress={() => closeSheet(() => void addPhotos())} />
          <ListRow title="Files" leading={<Paperclip color={c.textMuted} size={20} />} disabled={files.length >= maxFiles} onPress={() => closeSheet(() => void addFiles())} />
          <ListRow
            title="Mention someone"
            leading={<AtSign color={c.textMuted} size={20} />}
            onPress={() =>
              closeSheet(() => {
                const sep = text.length === 0 || /\s$/.test(text) ? "" : " ";
                const next = `${text}${sep}@`;
                setText(next);
                setCursor(next.length);
                inputRef.current?.focus();
              })
            }
          />
          {canPost ? (
            <ListRow
              title="Poll"
              leading={<BarChart3 color={c.textMuted} size={20} />}
              onPress={() =>
                closeSheet(() => {
                  setPoll({ question: "", options: ["", ""], multiple: false, anonymous: false });
                  setSheet("poll");
                })
              }
            />
          ) : null}
          <ListRow
            title="Commands"
            leading={<Slash color={c.textMuted} size={20} />}
            onPress={() =>
              closeSheet(() => {
                setText("/");
                setCursor(1);
                inputRef.current?.focus();
              })
            }
          />
        </ListGroup>
      </Sheet>
      <Sheet open={sheet === "later"} onClose={() => setSheet(null)} title="Send later">
        <View style={{ gap: s[4] }}>
          <ListGroup>
            {CHANNELS_SCHEDULE_OPTIONS.map((o) => (
              <ListRow key={o.when} title={o.label} leading={<CalendarClock color={c.textMuted} size={20} />} onPress={() => sendLater(o.when)} />
            ))}
          </ListGroup>
          <Field label="Or when" info='Try "in 2 hours", "friday at 3pm" or "2026-12-01 10am".'>
            <Input value={when} onChangeText={setWhen} placeholder="tomorrow at 4pm" autoCapitalize="none" />
          </Field>
          <Button disabled={!when.trim()} loading={schedule.isPending} onPress={() => sendLater(when.trim())}>
            Schedule
          </Button>
        </View>
      </Sheet>
      <Sheet open={sheet === "poll"} onClose={() => setSheet(null)} title="New poll" snapPoints={["85%"]}>
        <View style={{ gap: s[4] }}>
          <Field label="Question">
            <Input value={poll.question} onChangeText={(v) => setPoll((p) => ({ ...p, question: v }))} maxLength={300} />
          </Field>
          {poll.options.map((o, i) => (
            <View key={i} style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Input
                value={o}
                onChangeText={(v) => setPoll((p) => ({ ...p, options: p.options.map((x, j) => (j === i ? v : x)) }))}
                placeholder={`Option ${i + 1}`}
                accessibilityLabel={`Option ${i + 1}`}
                maxLength={200}
                containerStyle={{ flex: 1 }}
              />
              {poll.options.length > 2 ? <IconButton label={`Remove option ${i + 1}`} icon={Trash2} variant="ghost" onPress={() => setPoll((p) => ({ ...p, options: p.options.filter((_, j) => j !== i) }))} /> : null}
            </View>
          ))}
          {poll.options.length < 10 ? (
            <Button variant="ghost" leftIcon={Plus} onPress={() => setPoll((p) => ({ ...p, options: [...p.options, ""] }))}>
              Add option
            </Button>
          ) : null}
          <Switch label="Allow several choices" value={poll.multiple} onValueChange={(v) => setPoll((p) => ({ ...p, multiple: v }))} />
          <Switch label="Anonymous votes" info="Counts show; who voted doesn't." value={poll.anonymous} onValueChange={(v) => setPoll((p) => ({ ...p, anonymous: v }))} />
          <Button
            disabled={!poll.question.trim() || filledOptions.length < 2 || new Set(filledOptions).size !== filledOptions.length}
            loading={createPoll.isPending}
            onPress={() =>
              createPoll.mutate(
                { question: poll.question.trim(), options: filledOptions, multiple: poll.multiple, anonymous: poll.anonymous, thread_root_id: rootId },
                { onSuccess: () => (setSheet(null), onSent()), onError: (e) => toast.error(errorMessage(e)) },
              )
            }
          >
            Post poll
          </Button>
        </View>
      </Sheet>
    </View>
  );
}
