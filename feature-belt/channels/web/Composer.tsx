"use client";

import {
  CHANNELS_SCHEDULE_OPTIONS,
  channelsContentType,
  channelsDisplayName,
  channelsFileSize,
  channelsGroupToken,
  channelsUserToken,
  channelsWhen,
  channelsEndpoints,
  decodeChannelsMentions,
  encodeChannelsMentions,
  errorMessage,
  findChannelsSlashCommand,
  matchChannelsSlashCommands,
  parseChannelsSlash,
  searchChannelsEmoji,
  useApi,
  useChannelsDraft,
  useChannelsDraftSaver,
  useChannelsEdit,
  useChannelsGroups,
  useChannelsPeople,
  useChannelsSchedule,
  useChannelsSend,
  useChannelsTypingSignal,
  useDebouncedValue,
  useMe,
  useQueryClient,
  type ChannelsCachedMessage,
  type ChannelsConfig,
  type ChannelsDetail,
  type ChannelsPickedMention,
} from "@scaffoldapp/shared";
import { Badge, Checkbox, FileIcon, IconButton, Menu, MenuItem, MenuLabel, MenuSeparator, TextArea, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { AtSign, Bold, CalendarClock, ChevronDown, Code, Italic, Link2, List, ListOrdered, Paperclip, Pencil, Quote, SendHorizontal, Slash, Smile, SquareCode, Strikethrough, Upload, Users, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { UserPresenceDot } from "@/features/presence";
import { PickTimeDialog, PollDialog } from "./ActionDialogs";
import { EmojiGlyph, useWorkspaceEmoji } from "./emoji";
import { EmojiPickerDialog } from "./EmojiPicker";
import { uploadWithProgress } from "./upload";

interface Staged {
  id: string;
  file: File;
  previewUrl: string | null;
  contentType: string;
  status: "uploading" | "done" | "error";
  progress: number;
  key?: string;
  width?: number;
  height?: number;
  abort: AbortController;
}

interface Draft {
  text: string;
  picked: ChannelsPickedMention[];
}

/** Unsent text per channel and thread while the app is open (the server keeps a copy for other devices). */
const drafts = new Map<string, Draft>();

function imageSize(url: string): Promise<{ width: number; height: number } | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight });
    img.onerror = () => resolve(null);
    img.src = url;
  });
}

type Suggest = { kind: "mention" | "emoji" | "command"; query: string; start: number };

interface SuggestionItem {
  key: string;
  label: string;
  /** Inserted text (without the trigger); the mention it records, if any. */
  insert: string;
  picked?: ChannelsPickedMention;
  leading: ReactNode;
  badge?: string;
  hint?: string;
}

/** The token being typed at the caret: "@sa", ":fi" or a "/command" at the start. */
function suggestAt(text: string, caret: number): Suggest | null {
  const before = text.slice(0, caret);
  const cmd = /^\/([a-z0-9-]{0,24})$/i.exec(before);
  if (cmd) return { kind: "command", query: cmd[1] ?? "", start: 0 };
  const mention = /(^|[\s(])@([\p{L}\p{N}_.-]{0,30})$/u.exec(before);
  if (mention) return { kind: "mention", query: mention[2] ?? "", start: caret - (mention[2]?.length ?? 0) - 1 };
  const emoji = /(^|\s):([a-z0-9_+-]{2,30})$/.exec(before);
  if (emoji) return { kind: "emoji", query: emoji[2] ?? "", start: caret - (emoji[2]?.length ?? 0) - 1 };
  return null;
}

export interface ComposerProps {
  channel: ChannelsDetail;
  config: ChannelsConfig;
  /** Reply in this thread. */
  threadRootId?: string | null;
  /** Edit this message instead of sending a new one. */
  editing: ChannelsCachedMessage | null;
  onCancelEdit: () => void;
  /** ↑ in an empty composer: edit your last message. */
  onEditLast?: () => void;
  /** After a send, so the list can scroll to the bottom. */
  onSent?: () => void;
  /** /collapse and /expand. */
  onCollapse?: (collapsed: boolean) => void;
  autoFocus?: boolean;
}

/**
 * The message box: formatting toolbar, @ : and / autocomplete, files (picked, pasted or
 * dropped, uploaded with progress), slash commands, send later, edit mode and a draft kept on
 * the server. Enter sends, Shift+Enter adds a line, ↑ edits your last message.
 */
export function Composer({ channel, config, threadRootId = null, editing, onCancelEdit, onEditLast, onSent, onCollapse, autoFocus }: ComposerProps) {
  const router = useRouter();
  const api = useApi();
  const qc = useQueryClient();
  const me = useMe();
  const draftKey = `${channel.id}:${threadRootId ?? ""}`;
  const sender = useChannelsSend(channel.id);
  const edit = useChannelsEdit();
  const schedule = useChannelsSchedule(channel.id, channel.org_id);
  const serverDraft = useChannelsDraft(channel.member ? channel.id : null, threadRootId);
  const saver = useChannelsDraftSaver(channel.id, threadRootId, channel.org_id);
  const notifyTyping = useChannelsTypingSignal(channel.id, threadRootId);
  const { list: customEmoji } = useWorkspaceEmoji();
  const [text, setText] = useState(() => drafts.get(draftKey)?.text ?? "");
  const [picked, setPicked] = useState<ChannelsPickedMention[]>(() => drafts.get(draftKey)?.picked ?? []);
  const [files, setFiles] = useState<Staged[]>([]);
  const [alsoSend, setAlsoSend] = useState(false);
  const [suggest, setSuggest] = useState<Suggest | null>(null);
  const [highlight, setHighlight] = useState(0);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const [pickTime, setPickTime] = useState(false);
  const [poll, setPoll] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const restore = useRef<Draft | null>(null);
  const loadedServer = useRef<string | null>(null);

  // A different channel or thread: its own draft.
  useEffect(() => {
    const d = drafts.get(draftKey);
    setText(d?.text ?? "");
    setPicked(d?.picked ?? []);
    setFiles([]);
    setSuggest(null);
  }, [draftKey]);

  // The server's draft (another device) fills an empty composer once.
  useEffect(() => {
    const d = serverDraft.data;
    if (!d || loadedServer.current === draftKey || editing) return;
    loadedServer.current = draftKey;
    if (drafts.get(draftKey)?.text) return;
    const decoded = decodeChannelsMentions(d.body, d.mentions);
    setText(decoded.text);
    setPicked(decoded.picked);
  }, [serverDraft.data, draftKey, editing]);

  // Edit mode loads the message as editable text, and puts the draft back after.
  useEffect(() => {
    if (editing) {
      restore.current = { text, picked };
      const d = decodeChannelsMentions(editing.body, editing.mentions);
      setText(d.text);
      setPicked(d.picked);
      requestAnimationFrame(() => {
        const el = inputRef.current;
        if (el) {
          el.focus();
          el.setSelectionRange(el.value.length, el.value.length);
        }
      });
    } else if (restore.current) {
      setText(restore.current.text);
      setPicked(restore.current.picked);
      restore.current = null;
    }
    // Runs only when the message being edited changes.
  }, [editing?.id]);

  useEffect(() => {
    if (!editing) drafts.set(draftKey, { text, picked });
  }, [draftKey, text, picked, editing]);

  const updateText = (value: string) => {
    setText(value);
    if (!editing && channel.member && !value.startsWith("/")) saver.save(encodeChannelsMentions(value, picked));
  };

  // ── autocomplete ──
  const mentionQuery = useDebouncedValue(suggest?.kind === "mention" ? suggest.query : "", 120);
  const people = useChannelsPeople(channel.org_id, mentionQuery, channel.id, suggest?.kind === "mention");
  const groups = useChannelsGroups(channel.org_id, mentionQuery, suggest?.kind === "mention" && config.groups_enabled);
  const items: SuggestionItem[] = useMemo(() => {
    if (!suggest) return [];
    if (suggest.kind === "command") {
      return matchChannelsSlashCommands(suggest.query, channel)
        .slice(0, 10)
        .map((c) => ({ key: c.name, label: `/${c.name}`, insert: `/${c.name}`, hint: c.hint, leading: <Slash aria-hidden className="size-4 text-fg-muted" /> }));
    }
    if (suggest.kind === "emoji") {
      const q = suggest.query.toLowerCase();
      const custom = customEmoji
        .filter((e) => e.name.startsWith(q) || e.name.includes(q))
        .slice(0, 4)
        .map((e) => ({ key: `c-${e.id}`, label: `:${e.name}:`, insert: `:${e.name}:`, leading: <EmojiGlyph value={`:${e.name}:`} className="type-body-lg" /> }));
      const builtin = searchChannelsEmoji(suggest.query, 8 - custom.length).map((e) => ({ key: e.name, label: `:${e.name}:`, insert: e.emoji, leading: <span className="type-body-lg">{e.emoji}</span> }));
      return [...custom, ...builtin];
    }
    const q = suggest.query.toLowerCase();
    const out: SuggestionItem[] = [];
    for (const p of people.data?.items ?? []) {
      out.push({
        key: p.id,
        label: p.display_name,
        insert: `@${p.display_name}`,
        picked: { label: p.display_name, token: channelsUserToken(p.id) },
        leading: (
          <span className="relative flex">
            <AppAvatar name={p.display_name} src={p.avatar_url} size="xs" />
            <span className="absolute -bottom-0.5 -right-0.5 flex">
              <UserPresenceDot userId={p.id} ring />
            </span>
          </span>
        ),
        ...(channel.kind !== "dm" && channel.kind !== "group_dm" && !p.in_channel ? { badge: "Not in channel" } : {}),
      });
    }
    for (const g of groups.data?.items ?? []) {
      out.push({ key: `g-${g.id}`, label: `${g.handle} · ${g.name}`, insert: `@${g.handle}`, picked: { label: g.handle, token: channelsGroupToken(g.id, g.handle) }, leading: <Users aria-hidden className="size-5 text-fg-muted" /> });
    }
    if (channel.permissions.mention_all) {
      for (const k of ["here", "channel"] as const) {
        if (k.startsWith(q)) out.push({ key: k, label: k === "here" ? "@here · people online" : "@channel · everyone here", insert: `@${k}`, leading: <AtSign aria-hidden className="size-5 text-fg-muted" /> });
      }
    }
    return out.slice(0, 10);
  }, [suggest, people.data, groups.data, channel, customEmoji]);
  const open = !!suggest && items.length > 0;

  const syncSuggest = (value: string) => {
    const el = inputRef.current;
    const s = el ? suggestAt(value, el.selectionStart ?? value.length) : null;
    setSuggest(s);
    setHighlight(0);
  };

  const replaceRange = (start: number, end: number, insert: string) => {
    const next = text.slice(0, start) + insert + text.slice(end);
    updateText(next);
    requestAnimationFrame(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(start + insert.length, start + insert.length);
    });
    return next;
  };

  const pick = (item: SuggestionItem) => {
    if (!suggest) return;
    const caret = inputRef.current?.selectionStart ?? text.length;
    replaceRange(suggest.start, caret, `${item.insert} `);
    if (item.picked) {
      const p = item.picked;
      setPicked((list) => (list.some((x) => x.token === p.token) ? list : [...list, p]));
    }
    setSuggest(null);
  };

  // ── formatting ──
  const wrap = (before: string, after = before, placeholder = "text") => {
    const el = inputRef.current;
    if (!el) return;
    const s = el.selectionStart ?? 0;
    const e = el.selectionEnd ?? s;
    const inner = text.slice(s, e) || placeholder;
    const next = text.slice(0, s) + before + inner + after + text.slice(e);
    updateText(next);
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(s + before.length, s + before.length + inner.length);
    });
  };
  const prefixLines = (prefix: (i: number) => string) => {
    const el = inputRef.current;
    if (!el) return;
    const s = el.selectionStart ?? 0;
    const e = el.selectionEnd ?? s;
    const lineStart = text.lastIndexOf("\n", s - 1) + 1;
    const block = text.slice(lineStart, e) || "";
    const lines = (block || "").split("\n").map((l, i) => prefix(i) + l);
    const next = text.slice(0, lineStart) + lines.join("\n") + text.slice(e);
    updateText(next);
    requestAnimationFrame(() => {
      el.focus();
      const end = lineStart + lines.join("\n").length;
      el.setSelectionRange(end, end);
    });
  };
  const tools: { label: string; keys?: string; icon: ReactNode; run: () => void }[] = [
    { label: "Bold", keys: "⌘B", icon: <Bold />, run: () => wrap("**") },
    { label: "Italic", keys: "⌘I", icon: <Italic />, run: () => wrap("_") },
    { label: "Strikethrough", keys: "⌘⇧X", icon: <Strikethrough />, run: () => wrap("~~") },
    { label: "Code", icon: <Code />, run: () => wrap("`", "`", "code") },
    { label: "Code block", keys: "⌘⌥C", icon: <SquareCode />, run: () => wrap("```\n", "\n```", "code") },
    { label: "Quote", keys: "⌘⇧9", icon: <Quote />, run: () => prefixLines(() => "> ") },
    { label: "Bulleted list", keys: "⌘⇧8", icon: <List />, run: () => prefixLines(() => "- ") },
    { label: "Numbered list", keys: "⌘⇧7", icon: <ListOrdered />, run: () => prefixLines((i) => `${i + 1}. `) },
    { label: "Link", keys: "⌘⇧U", icon: <Link2 />, run: () => wrap("[", "](https://)", "link text") },
  ];

  // ── files ──
  const addFiles = (list: FileList | File[]) => {
    const pickedFiles = Array.from(list);
    const room = config.max_attachments - files.length;
    if (pickedFiles.length > room) toast.error(`Attach up to ${config.max_attachments} files per message.`);
    for (const file of pickedFiles.slice(0, Math.max(0, room))) {
      const contentType = channelsContentType(file.name, file.type);
      if (file.size > config.attachment_max_bytes) {
        toast.error(`${file.name} is over ${channelsFileSize(config.attachment_max_bytes)}. Choose a smaller file.`);
        continue;
      }
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const previewUrl = contentType.startsWith("image/") || contentType.startsWith("video/") ? URL.createObjectURL(file) : null;
      const abort = new AbortController();
      setFiles((s) => [...s, { id, file, previewUrl, contentType, status: "uploading", progress: 0, abort }]);
      const size = previewUrl && contentType.startsWith("image/") ? imageSize(previewUrl) : Promise.resolve(null);
      const onProgress = (p: number) => setFiles((s) => s.map((f) => (f.id === id ? { ...f, progress: p } : f)));
      void Promise.all([uploadWithProgress(api.http, { file, purpose: "channel_attachment", contentType, onProgress, signal: abort.signal }), size]).then(
        ([res, dims]) => setFiles((s) => s.map((f) => (f.id === id ? { ...f, status: "done", progress: 1, key: res.key, ...(dims ?? {}) } : f))),
        (err: unknown) => {
          if (abort.signal.aborted) return;
          toast.error(errorMessage(err));
          setFiles((s) => s.map((f) => (f.id === id ? { ...f, status: "error" } : f)));
        },
      );
    }
  };
  const removeFile = (id: string) =>
    setFiles((s) => {
      const f = s.find((x) => x.id === id);
      f?.abort.abort();
      if (f?.previewUrl) URL.revokeObjectURL(f.previewUrl);
      return s.filter((x) => x.id !== id);
    });

  const uploading = files.some((f) => f.status === "uploading");
  const ready = files.filter((f) => f.status === "done" && f.key);
  const canSend = !uploading && (text.trim().length > 0 || ready.length > 0) && !files.some((f) => f.status === "error");
  const attachmentsInput = () =>
    ready.map((f) => ({
      key: f.key ?? "",
      file_name: f.file.name,
      size_bytes: f.file.size,
      content_type: f.contentType,
      ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
    }));

  const reset = () => {
    setText("");
    setPicked([]);
    setFiles([]);
    setAlsoSend(false);
    drafts.delete(draftKey);
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
        threadRootId,
        meId: me.data?.id ?? "",
        http: api.http,
        api: channelsEndpoints(api.http),
        qc,
        navigate: (path) => router.push(path),
        notify: (message, tone) => (tone === "error" ? toast.error(message) : toast.success(message)),
        send: (body) => sender.send({ body, thread_root_id: threadRootId }),
        setText: (t) => updateText(t),
        openPoll: (q) => setPoll(q),
        setCollapsed: (c) => {
          onCollapse?.(c);
          toast.success(c ? "Previews and media collapsed here." : "Previews and media shown.");
        },
      }),
    ).catch((e: unknown) => toast.error(errorMessage(e)));
    return true;
  };

  const submit = () => {
    const body = encodeChannelsMentions(text.trim(), picked);
    if (editing) {
      if (!body && editing.attachments.length === 0) return;
      if (body !== editing.body) edit.mutate({ message: editing, body }, { onError: (err) => toast.error(errorMessage(err)) });
      onCancelEdit();
      return;
    }
    if (text.trim().startsWith("/") && runCommand()) return;
    if (!canSend) return;
    void sender
      .send({
        body,
        thread_root_id: threadRootId,
        also_send_to_channel: !!threadRootId && alsoSend,
        attachments: ready.map((f) => ({
          key: f.key ?? "",
          file_name: f.file.name,
          size_bytes: f.file.size,
          content_type: f.contentType,
          ...(f.previewUrl ? { preview_url: f.previewUrl } : {}),
          ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
        })),
      })
      .then((msg) => {
        if (!msg) toast.error("Message not sent. Retry it from the list.");
      });
    reset();
    onSent?.();
  };

  const sendLater = (when: { when?: string; send_at?: string }) => {
    const body = encodeChannelsMentions(text.trim(), picked);
    if (!canSend) return Promise.resolve();
    return schedule
      .mutateAsync({ body, thread_root_id: threadRootId, also_send_to_channel: !!threadRootId && alsoSend, attachments: attachmentsInput(), ...when })
      .then((s) => {
        reset();
        toast.success(`Scheduled for ${channelsWhen(s.send_at)}.`, { action: { label: "View", onClick: () => router.push("/channels/drafts?tab=scheduled") } });
      })
      .catch((e: unknown) => {
        toast.error(errorMessage(e));
        throw e;
      });
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (open) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setHighlight((h) => (h + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        const item = items[highlight];
        if (item) {
          e.preventDefault();
          pick(item);
          return;
        }
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSuggest(null);
        return;
      }
    }
    const mod = e.metaKey || e.ctrlKey;
    if (mod && !e.shiftKey && !e.altKey && (e.key === "b" || e.key === "i")) {
      e.preventDefault();
      wrap(e.key === "b" ? "**" : "_");
      return;
    }
    if (mod && e.shiftKey) {
      const key = e.key.toLowerCase();
      const map: Record<string, () => void> = {
        x: () => wrap("~~"),
        u: () => wrap("[", "](https://)", "link text"),
        "7": () => prefixLines((i) => `${i + 1}. `),
        "&": () => prefixLines((i) => `${i + 1}. `),
        "8": () => prefixLines(() => "- "),
        "*": () => prefixLines(() => "- "),
        "9": () => prefixLines(() => "> "),
        "(": () => prefixLines(() => "> "),
      };
      const run = map[key];
      if (run) {
        e.preventDefault();
        run();
        return;
      }
    }
    if (mod && e.altKey && (e.code === "KeyC" || e.key.toLowerCase() === "c")) {
      e.preventDefault();
      wrap("```\n", "\n```", "code");
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    } else if (e.key === "Escape" && editing) {
      e.preventDefault();
      onCancelEdit();
    } else if (e.key === "ArrowUp" && !text && !editing && onEditLast) {
      e.preventDefault();
      onEditLast();
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (editing || e.clipboardData.files.length === 0) return;
    e.preventDefault();
    addFiles(e.clipboardData.files);
  };
  const onDragOver = (e: DragEvent<HTMLDivElement>) => {
    if (editing || !e.dataTransfer.types.includes("Files")) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
    setDragging(true);
  };
  const onDrop = (e: DragEvent<HTMLDivElement>) => {
    setDragging(false);
    if (editing || e.dataTransfer.files.length === 0) return;
    e.preventDefault();
    addFiles(e.dataTransfer.files);
  };

  const placeholder = editing ? "Edit message" : threadRootId ? "Reply" : `Message ${channelsDisplayName(channel)}`;
  const direct = channel.kind === "dm" || channel.kind === "group_dm";

  return (
    <div className="px-gutter pb-3 lg:px-5" onDragOver={onDragOver} onDragLeave={() => setDragging(false)} onDrop={onDrop}>
      <div className={cn("relative flex flex-col rounded-lg border border-border-strong bg-surface focus-within:border-ring", dragging && "border-primary bg-primary-soft")}>
        {dragging && (
          <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center gap-2 rounded-lg type-label font-semibold text-primary-strong" aria-hidden>
            <Upload className="size-5" />
            Drop files to attach
          </div>
        )}
        {open && (
          <ul role="listbox" aria-label={suggest?.kind === "emoji" ? "Emoji" : suggest?.kind === "command" ? "Commands" : "People"} className="absolute inset-x-0 bottom-full z-20 mb-2 flex max-h-72 flex-col overflow-y-auto rounded-lg border border-border bg-surface-raised p-1 shadow-lg">
            {items.map((item, i) => (
              <li key={item.key} role="option" aria-selected={i === highlight}>
                <button
                  type="button"
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(item)}
                  onMouseEnter={() => setHighlight(i)}
                  className={cn("flex min-h-tap-sm w-full items-center gap-3 rounded-md px-3 py-1.5 text-left", i === highlight && "bg-sunken")}
                >
                  <span className="flex size-6 shrink-0 items-center justify-center">{item.leading}</span>
                  <span className="shrink-0 type-label text-fg">{item.label}</span>
                  {item.hint && <span className="min-w-0 flex-1 truncate type-caption text-fg-muted">{item.hint}</span>}
                  {!item.hint && <span className="flex-1" />}
                  {item.badge && <Badge size="sm">{item.badge}</Badge>}
                </button>
              </li>
            ))}
          </ul>
        )}
        {editing && (
          <div className="flex items-center gap-2 border-b border-border px-3 py-1.5">
            <Pencil aria-hidden className="size-4 text-fg-muted" />
            <span className="flex-1 type-label font-semibold text-fg">Editing message</span>
            <IconButton size="sm" variant="ghost" label="Cancel edit" icon={<X />} onClick={onCancelEdit} />
          </div>
        )}
        <div className="flex flex-wrap items-center gap-0.5 border-b border-border px-1.5 py-1" role="toolbar" aria-label="Formatting">
          {tools.map((t) => (
            <Tooltip key={t.label} content={t.keys ? `${t.label} ${t.keys}` : t.label}>
              <IconButton size="sm" variant="ghost" label={t.label} icon={t.icon} onMouseDown={(e) => e.preventDefault()} onClick={t.run} />
            </Tooltip>
          ))}
        </div>
        {files.length > 0 && (
          <ul className="flex gap-2 overflow-x-auto px-3 pt-3" aria-label="Files to send">
            {files.map((f) => (
              <li key={f.id} className="relative shrink-0">
                {f.previewUrl && f.contentType.startsWith("image/") ? (
                  <img src={f.previewUrl} alt={f.file.name} className="size-16 rounded-md object-cover" />
                ) : (
                  <span className="flex h-16 w-44 items-center gap-2 rounded-md border border-border px-3">
                    <FileIcon name={f.file.name} mime={f.contentType} className="size-5 shrink-0" />
                    <span className="flex min-w-0 flex-col">
                      <span className="truncate type-caption text-fg">{f.file.name}</span>
                      <span className="type-caption text-fg-muted">{channelsFileSize(f.file.size)}</span>
                    </span>
                  </span>
                )}
                {f.status === "uploading" && (
                  <span className="absolute inset-x-1 bottom-1 flex flex-col gap-0.5" role="progressbar" aria-label={`Uploading ${f.file.name}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(f.progress * 100)}>
                    <span className="h-1 overflow-hidden rounded-pill bg-overlay">
                      <span className="block h-full rounded-pill bg-primary transition-[width] motion-reduce:transition-none" style={{ width: `${Math.max(4, Math.round(f.progress * 100))}%` }} />
                    </span>
                  </span>
                )}
                {f.status === "error" && <span className="absolute inset-0 flex items-center justify-center rounded-md bg-danger-soft type-caption text-danger-strong">Failed</span>}
                <span className="absolute -right-2 -top-2">
                  <IconButton size="sm" variant="inverse" label={`Remove ${f.file.name}`} icon={<X />} onClick={() => removeFile(f.id)} />
                </span>
              </li>
            ))}
          </ul>
        )}
        <TextArea
          ref={inputRef}
          value={text}
          autoFocus={autoFocus}
          onChange={(e) => {
            updateText(e.target.value);
            syncSuggest(e.target.value);
            if (!editing && e.target.value && !e.target.value.startsWith("/")) notifyTyping();
          }}
          onSelect={() => syncSuggest(text)}
          onBlur={() => setTimeout(() => setSuggest(null), 120)}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          autoGrow
          minRows={1}
          maxRows={10}
          maxLength={config.max_chars}
          placeholder={placeholder}
          aria-label={placeholder}
          aria-autocomplete="list"
          aria-expanded={open}
          className="min-w-0 resize-none border-0 bg-transparent shadow-none focus-visible:outline-none focus-visible:ring-0"
        />
        <div className="flex items-center gap-1 px-1.5 pb-1.5">
          {!editing && (
            <>
              <input
                ref={fileRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => {
                  if (e.target.files) addFiles(e.target.files);
                  e.target.value = "";
                }}
              />
              <IconButton size="sm" variant="ghost" label="Attach files" icon={<Paperclip />} onClick={() => fileRef.current?.click()} disabled={files.length >= config.max_attachments} />
            </>
          )}
          <IconButton size="sm" variant="ghost" label="Emoji" icon={<Smile />} onClick={() => setEmojiOpen(true)} />
          <IconButton
            size="sm"
            variant="ghost"
            label="Mention someone"
            icon={<AtSign />}
            onClick={() => {
              const el = inputRef.current;
              const at = el?.selectionStart ?? text.length;
              const pre = at > 0 && !/\s/.test(text[at - 1] ?? "") ? " @" : "@";
              replaceRange(at, at, pre);
              setSuggest({ kind: "mention", query: "", start: at + pre.length - 1 });
            }}
          />
          {!editing && (
            <IconButton
              size="sm"
              variant="ghost"
              label="Commands"
              icon={<Slash />}
              onClick={() => {
                updateText("/");
                setSuggest({ kind: "command", query: "", start: 0 });
                requestAnimationFrame(() => inputRef.current?.focus());
              }}
            />
          )}
          {threadRootId && channel.layout !== "forum" && !editing && (
            <span className="ml-2 min-w-0">
              <Checkbox checked={alsoSend} onCheckedChange={setAlsoSend} label={direct ? "Also send to the conversation" : `Also send to ${channelsDisplayName(channel)}`} />
            </span>
          )}
          <span className="flex-1" />
          <span className="flex items-center">
            <IconButton
              label={editing ? "Save" : "Send"}
              icon={<SendHorizontal />}
              variant="primary"
              size="sm"
              onClick={submit}
              disabled={editing ? !text.trim() && editing.attachments.length === 0 : !canSend && !text.trim().startsWith("/")}
              loading={uploading}
              className={cn(!editing && channel.layout !== "forum" && "rounded-r-none")}
            />
            {!editing && (channel.layout !== "forum" || threadRootId) && (
              <Menu
                trigger={<IconButton label="Send later" icon={<ChevronDown />} variant="primary" size="sm" disabled={!canSend || text.trim().startsWith("/")} className="rounded-l-none border-l border-primary-strong" />}
                align="end"
                side="top"
              >
                <MenuLabel>Send later</MenuLabel>
                {CHANNELS_SCHEDULE_OPTIONS.map((o) => (
                  <MenuItem key={o.when} icon={<CalendarClock />} onSelect={() => void sendLater({ when: o.when }).catch(() => undefined)}>
                    {o.label}
                  </MenuItem>
                ))}
                <MenuSeparator />
                <MenuItem icon={<CalendarClock />} onSelect={() => setPickTime(true)}>
                  Custom time…
                </MenuItem>
              </Menu>
            )}
          </span>
        </div>
      </div>
      <EmojiPickerDialog
        open={emojiOpen}
        onOpenChange={setEmojiOpen}
        title="Insert emoji"
        onPick={(emoji) => {
          setEmojiOpen(false);
          const at = inputRef.current?.selectionStart ?? text.length;
          replaceRange(at, at, emoji);
        }}
      />
      <PickTimeDialog open={pickTime} onOpenChange={setPickTime} title="Send later" confirmLabel="Schedule" onPick={(at) => sendLater({ send_at: at.toISOString() })} />
      {poll !== null && <PollDialog channel={channel} threadRootId={threadRootId} initial={poll} open onOpenChange={(o) => !o && setPoll(null)} />}
    </div>
  );
}
