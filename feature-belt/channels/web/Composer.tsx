"use client";

import {
  CHANNELS_FILE_ACCEPT,
  channelsContentType,
  channelsDisplayName,
  channelsFileSize,
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
  useDebouncedValue,
  useUpload,
  type ChannelsCachedMessage,
  type ChannelsConfig,
  type ChannelsDetail,
  type ChannelsPickedMention,
} from "@scaffoldapp/shared";
import { Badge, Checkbox, IconButton, Spinner, TextArea, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { AtSign, Bold, Code, FileText, Italic, Link2, List, ListOrdered, Paperclip, Pencil, Quote, SendHorizontal, Smile, SquareCode, Strikethrough, Users, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ClipboardEvent, type KeyboardEvent, type ReactNode } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { UserPresenceDot } from "@/features/presence";
import { EmojiPickerDialog } from "./EmojiPicker";

interface Staged {
  id: string;
  file: File;
  previewUrl: string | null;
  contentType: string;
  status: "uploading" | "done" | "error";
  key?: string;
  width?: number;
  height?: number;
}

interface Draft {
  text: string;
  picked: ChannelsPickedMention[];
}

/** Unsent text per channel and thread, kept while the app is open. */
const drafts = new Map<string, Draft>();

function imageSize(url: string): Promise<{ width: number; height: number } | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight });
    img.onerror = () => resolve(null);
    img.src = url;
  });
}

type Suggest = { kind: "mention" | "emoji"; query: string; start: number };

interface SuggestionItem {
  key: string;
  label: string;
  /** Inserted text (without the trigger); the mention it records, if any. */
  insert: string;
  picked?: ChannelsPickedMention;
  leading: ReactNode;
  badge?: string;
}

/** The token being typed at the caret: "@sa" or ":fi". */
function suggestAt(text: string, caret: number): Suggest | null {
  const before = text.slice(0, caret);
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
  autoFocus?: boolean;
}

/**
 * The message box: formatting toolbar, @ and : autocomplete, files (uploaded as you pick
 * them), edit mode. Enter sends, Shift+Enter adds a line, ↑ edits your last message.
 */
export function Composer({ channel, config, threadRootId = null, editing, onCancelEdit, onEditLast, onSent, autoFocus }: ComposerProps) {
  const draftKey = `${channel.id}:${threadRootId ?? ""}`;
  const sender = useChannelsSend(channel.id);
  const edit = useChannelsEdit();
  const upload = useUpload();
  const notifyTyping = useChannelsTypingSignal(channel.id, threadRootId);
  const [text, setText] = useState(() => drafts.get(draftKey)?.text ?? "");
  const [picked, setPicked] = useState<ChannelsPickedMention[]>(() => drafts.get(draftKey)?.picked ?? []);
  const [files, setFiles] = useState<Staged[]>([]);
  const [alsoSend, setAlsoSend] = useState(false);
  const [suggest, setSuggest] = useState<Suggest | null>(null);
  const [highlight, setHighlight] = useState(0);
  const [emojiOpen, setEmojiOpen] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const restore = useRef<Draft | null>(null);

  // A different channel or thread: its own draft.
  useEffect(() => {
    const d = drafts.get(draftKey);
    setText(d?.text ?? "");
    setPicked(d?.picked ?? []);
    setFiles([]);
    setSuggest(null);
  }, [draftKey]);

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

  // ── autocomplete ──
  const mentionQuery = useDebouncedValue(suggest?.kind === "mention" ? suggest.query : "", 120);
  const people = useChannelsPeople(channel.org_id, mentionQuery, channel.id, suggest?.kind === "mention");
  const groups = useChannelsGroups(channel.org_id, mentionQuery, suggest?.kind === "mention" && config.groups_enabled);
  const items: SuggestionItem[] = useMemo(() => {
    if (!suggest) return [];
    if (suggest.kind === "emoji") {
      return searchChannelsEmoji(suggest.query, 8).map((e) => ({ key: e.name, label: `:${e.name}:`, insert: e.emoji, leading: <span className="type-body-lg">{e.emoji}</span> }));
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
  }, [suggest, people.data, groups.data, channel.kind, channel.permissions.mention_all]);
  const open = !!suggest && items.length > 0;

  const syncSuggest = (value: string) => {
    const el = inputRef.current;
    const s = el ? suggestAt(value, el.selectionStart ?? value.length) : null;
    setSuggest(s);
    setHighlight(0);
  };

  const replaceRange = (start: number, end: number, insert: string) => {
    const next = text.slice(0, start) + insert + text.slice(end);
    setText(next);
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
    setText(next);
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
    setText(next);
    requestAnimationFrame(() => {
      el.focus();
      const end = lineStart + lines.join("\n").length;
      el.setSelectionRange(end, end);
    });
  };
  const tools: { label: string; keys?: string; icon: ReactNode; run: () => void }[] = [
    { label: "Bold", keys: "⌘B", icon: <Bold />, run: () => wrap("**") },
    { label: "Italic", keys: "⌘I", icon: <Italic />, run: () => wrap("_") },
    { label: "Strikethrough", icon: <Strikethrough />, run: () => wrap("~~") },
    { label: "Code", icon: <Code />, run: () => wrap("`", "`", "code") },
    { label: "Code block", icon: <SquareCode />, run: () => wrap("```\n", "\n```", "code") },
    { label: "Quote", icon: <Quote />, run: () => prefixLines(() => "> ") },
    { label: "Bulleted list", icon: <List />, run: () => prefixLines(() => "- ") },
    { label: "Numbered list", icon: <ListOrdered />, run: () => prefixLines((i) => `${i + 1}. `) },
    { label: "Link", icon: <Link2 />, run: () => wrap("[", "](https://)", "link text") },
  ];

  // ── files ──
  const addFiles = (list: FileList | File[]) => {
    const pickedFiles = Array.from(list);
    const room = config.max_attachments - files.length;
    if (pickedFiles.length > room) toast.error(`Attach up to ${config.max_attachments} files per message.`);
    for (const file of pickedFiles.slice(0, Math.max(0, room))) {
      const contentType = channelsContentType(file.name, file.type);
      if (!contentType) {
        toast.error(`${file.name} isn't a supported file type.`);
        continue;
      }
      if (file.size > config.attachment_max_bytes) {
        toast.error(`${file.name} is over ${channelsFileSize(config.attachment_max_bytes)}. Choose a smaller file.`);
        continue;
      }
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const previewUrl = contentType.startsWith("image/") || contentType.startsWith("video/") ? URL.createObjectURL(file) : null;
      setFiles((s) => [...s, { id, file, previewUrl, contentType, status: "uploading" }]);
      const size = previewUrl && contentType.startsWith("image/") ? imageSize(previewUrl) : Promise.resolve(null);
      void Promise.all([upload.mutateAsync({ file, purpose: "channel_attachment", contentType, maxBytes: config.attachment_max_bytes }), size]).then(
        ([res, dims]) => setFiles((s) => s.map((f) => (f.id === id ? { ...f, status: "done", key: res.key, ...(dims ?? {}) } : f))),
        (err: unknown) => {
          toast.error(errorMessage(err));
          setFiles((s) => s.map((f) => (f.id === id ? { ...f, status: "error" } : f)));
        },
      );
    }
  };
  const removeFile = (id: string) =>
    setFiles((s) => {
      const f = s.find((x) => x.id === id);
      if (f?.previewUrl) URL.revokeObjectURL(f.previewUrl);
      return s.filter((x) => x.id !== id);
    });

  const uploading = files.some((f) => f.status === "uploading");
  const ready = files.filter((f) => f.status === "done" && f.key);
  const canSend = !uploading && (text.trim().length > 0 || ready.length > 0) && !files.some((f) => f.status === "error");

  const submit = () => {
    const body = encodeChannelsMentions(text.trim(), picked);
    if (editing) {
      if (!body && editing.attachments.length === 0) return;
      if (body !== editing.body) edit.mutate({ message: editing, body }, { onError: (err) => toast.error(errorMessage(err)) });
      onCancelEdit();
      return;
    }
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
    setText("");
    setPicked([]);
    setFiles([]);
    setAlsoSend(false);
    drafts.delete(draftKey);
    onSent?.();
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
    if ((e.metaKey || e.ctrlKey) && (e.key === "b" || e.key === "i")) {
      e.preventDefault();
      wrap(e.key === "b" ? "**" : "_");
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

  const placeholder = editing ? "Edit message" : threadRootId ? "Reply" : `Message ${channelsDisplayName(channel)}`;
  const direct = channel.kind === "dm" || channel.kind === "group_dm";

  return (
    <div className="px-gutter pb-3 lg:px-5">
      <div className="relative flex flex-col rounded-lg border border-border-strong bg-surface focus-within:border-ring">
        {open && (
          <ul role="listbox" aria-label={suggest?.kind === "emoji" ? "Emoji" : "People"} className="absolute inset-x-0 bottom-full z-20 mb-2 flex max-h-72 flex-col overflow-y-auto rounded-lg border border-border bg-surface-raised p-1 shadow-lg">
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
                  <span className="min-w-0 flex-1 truncate type-label text-fg">{item.label}</span>
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
                  <span className="flex h-16 w-40 items-center gap-2 rounded-md border border-border px-3">
                    <FileText aria-hidden className="size-5 shrink-0 text-fg-muted" />
                    <span className="min-w-0 truncate type-caption text-fg">{f.file.name}</span>
                  </span>
                )}
                {f.status === "uploading" && (
                  <span className="absolute inset-0 flex items-center justify-center rounded-md bg-overlay">
                    <Spinner size={20} label="Uploading" />
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
            setText(e.target.value);
            syncSuggest(e.target.value);
            if (!editing && e.target.value) notifyTyping();
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
                accept={CHANNELS_FILE_ACCEPT}
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
              const next = replaceRange(at, at, pre);
              setSuggest({ kind: "mention", query: "", start: at + pre.length - 1 });
              void next;
            }}
          />
          {threadRootId && channel.layout !== "forum" && !editing && (
            <span className="ml-2 min-w-0">
              <Checkbox checked={alsoSend} onCheckedChange={setAlsoSend} label={direct ? "Also send to the conversation" : `Also send to ${channelsDisplayName(channel)}`} />
            </span>
          )}
          <span className="flex-1" />
          <IconButton
            label={editing ? "Save" : "Send"}
            icon={<SendHorizontal />}
            variant="primary"
            size="sm"
            onClick={submit}
            disabled={editing ? !text.trim() && editing.attachments.length === 0 : !canSend}
            loading={uploading}
          />
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
    </div>
  );
}
