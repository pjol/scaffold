"use client";

import {
  CHAT_ATTACHMENT_MAX_BYTES,
  CHAT_FILE_ACCEPT,
  CHAT_MAX_ATTACHMENTS,
  chatContentType,
  errorMessage,
  formatFileSize,
  useEditMessage,
  useSendMessage,
  useTypingSignal,
  useUpload,
  type ChatSendInput,
  type ChatThreadMessage,
} from "@scaffoldapp/shared";
import { IconButton, Spinner, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { FileText, Paperclip, Pencil, Reply, SendHorizontal, X } from "lucide-react";
import { useEffect, useRef, useState, type ClipboardEvent, type KeyboardEvent, type ReactNode } from "react";

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

function imageSize(url: string): Promise<{ width: number; height: number } | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight });
    img.onerror = () => resolve(null);
    img.src = url;
  });
}

export interface ComposerProps {
  conversationId: string;
  replyTo: ChatThreadMessage | null;
  onCancelReply: () => void;
  editing: ChatThreadMessage | null;
  onCancelEdit: () => void;
  /** Called after a send so the thread can scroll to the bottom. */
  onSent: () => void;
}

/** Text, files (uploaded as you pick them), replies and edits. Enter sends; Shift+Enter adds a line. */
export function Composer({ conversationId, replyTo, onCancelReply, editing, onCancelEdit, onSent }: ComposerProps) {
  const sender = useSendMessage(conversationId);
  const edit = useEditMessage(conversationId);
  const upload = useUpload();
  const notifyTyping = useTypingSignal(conversationId);
  const [text, setText] = useState("");
  const [files, setFiles] = useState<Staged[]>([]);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setText(editing ? editing.body : "");
    if (editing || replyTo) inputRef.current?.focus();
  }, [editing, replyTo]);

  useEffect(() => {
    // Drafts belong to one conversation.
    setText("");
    setFiles([]);
  }, [conversationId]);

  const addFiles = (list: FileList | File[]) => {
    const picked = Array.from(list);
    const room = CHAT_MAX_ATTACHMENTS - files.length;
    if (picked.length > room) toast.error(`Attach up to ${CHAT_MAX_ATTACHMENTS} files per message.`);
    for (const file of picked.slice(0, Math.max(0, room))) {
      const contentType = chatContentType(file.name, file.type);
      if (!contentType) {
        toast.error(`${file.name} isn't a supported file type.`);
        continue;
      }
      if (file.size > CHAT_ATTACHMENT_MAX_BYTES) {
        toast.error(`${file.name} is over ${formatFileSize(CHAT_ATTACHMENT_MAX_BYTES)}. Choose a smaller file.`);
        continue;
      }
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const previewUrl = contentType.startsWith("image/") || contentType.startsWith("video/") ? URL.createObjectURL(file) : null;
      setFiles((s) => [...s, { id, file, previewUrl, contentType, status: "uploading" }]);
      const size = previewUrl && contentType.startsWith("image/") ? imageSize(previewUrl) : Promise.resolve(null);
      void Promise.all([upload.mutateAsync({ file, purpose: "chat_attachment", contentType, maxBytes: CHAT_ATTACHMENT_MAX_BYTES }), size]).then(
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
    if (editing) {
      const body = text.trim();
      if (!body && editing.attachments.length === 0) return;
      if (body !== editing.body) edit.mutate({ id: editing.id, body }, { onError: (err) => toast.error(errorMessage(err)) });
      onCancelEdit();
      return;
    }
    if (!canSend) return;
    const input: ChatSendInput = {
      body: text,
      reply_to: replyTo
        ? { id: replyTo.id, sender: replyTo.sender, body: replyTo.body.slice(0, 200), has_attachments: replyTo.attachments.length > 0, deleted: false }
        : null,
      attachments: ready.map((f) => ({
        key: f.key ?? "",
        file_name: f.file.name,
        size_bytes: f.file.size,
        content_type: f.contentType,
        ...(f.previewUrl ? { preview_url: f.previewUrl } : {}),
        ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
      })),
    };
    sender.send(input);
    setText("");
    setFiles([]);
    onCancelReply();
    onSent();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
    } else if (e.key === "Escape" && (editing || replyTo)) {
      e.preventDefault();
      if (editing) onCancelEdit();
      else onCancelReply();
    }
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (editing || e.clipboardData.files.length === 0) return;
    e.preventDefault();
    addFiles(e.clipboardData.files);
  };

  const banner = editing ? (
    <ComposerBanner icon={<Pencil />} title="Edit message" onClose={onCancelEdit} closeLabel="Cancel edit" />
  ) : replyTo ? (
    <ComposerBanner icon={<Reply />} title={`Reply to ${replyTo.sender.display_name}`} text={replyTo.body} onClose={onCancelReply} closeLabel="Cancel reply" />
  ) : null;

  return (
    <div className="flex flex-col gap-2 border-t border-border bg-surface px-gutter py-3 lg:px-6">
      {banner}
      {files.length > 0 && (
        <ul className="flex gap-2 overflow-x-auto pb-1" aria-label="Files to send">
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
      <div className="flex items-end gap-2">
        {!editing && (
          <>
            <input
              ref={fileRef}
              type="file"
              multiple
              accept={CHAT_FILE_ACCEPT}
              className="hidden"
              onChange={(e) => {
                if (e.target.files) addFiles(e.target.files);
                e.target.value = "";
              }}
            />
            <IconButton label="Attach files" icon={<Paperclip />} variant="ghost" onClick={() => fileRef.current?.click()} disabled={files.length >= CHAT_MAX_ATTACHMENTS} />
          </>
        )}
        <TextArea
          ref={inputRef}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            if (!editing && e.target.value) notifyTyping();
          }}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          autoGrow
          minRows={1}
          maxRows={6}
          placeholder={editing ? "Edit message" : "Message"}
          aria-label={editing ? "Edit message" : "Message"}
          className="min-w-0 flex-1 resize-none"
        />
        <IconButton
          label={editing ? "Save" : "Send"}
          icon={<SendHorizontal />}
          variant="primary"
          onClick={submit}
          disabled={editing ? !text.trim() && editing.attachments.length === 0 : !canSend}
          loading={uploading}
        />
      </div>
    </div>
  );
}

function ComposerBanner({ icon, title, text, onClose, closeLabel }: { icon: ReactNode; title: string; text?: string; onClose: () => void; closeLabel: string }) {
  return (
    <div className="flex items-center gap-3 rounded-md bg-sunken px-3 py-2">
      <span aria-hidden className="text-fg-muted [&_svg]:size-4">
        {icon}
      </span>
      <span className="flex min-w-0 flex-1 items-baseline gap-2">
        <span className="shrink-0 type-label font-semibold text-fg">{title}</span>
        {text && <span className={cn("min-w-0 truncate type-label text-fg-muted")}>{text}</span>}
      </span>
      <IconButton size="sm" variant="ghost" label={closeLabel} icon={<X />} onClick={onClose} />
    </div>
  );
}
