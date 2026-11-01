import { CHAT_ATTACHMENT_MAX_BYTES, CHAT_MAX_ATTACHMENTS, errorMessage, useEditMessage, useSendMessage, useTypingSignal, useUpload, type ChatThreadMessage } from "@scaffoldapp/shared";
import { IconButton, Input, Spinner, Text, pickPhotos, toast, useTheme } from "@scaffoldapp/ui-native";
import { Image } from "expo-image";
import { ImagePlus, Pencil, Reply, SendHorizontal, X } from "lucide-react-native";
import { useEffect, useRef, useState } from "react";
import { ScrollView, View, type TextInput } from "react-native";

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

export interface ComposerProps {
  conversationId: string;
  replyTo: ChatThreadMessage | null;
  onCancelReply: () => void;
  editing: ChatThreadMessage | null;
  onCancelEdit: () => void;
  onSent: () => void;
}

/** Text, photos from the system picker, replies and edits. Sits above the keyboard (Screen footer). */
export function Composer({ conversationId, replyTo, onCancelReply, editing, onCancelEdit, onSent }: ComposerProps) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const sender = useSendMessage(conversationId);
  const edit = useEditMessage(conversationId);
  const upload = useUpload();
  const notifyTyping = useTypingSignal(conversationId);
  const [text, setText] = useState("");
  const [files, setFiles] = useState<Staged[]>([]);
  const inputRef = useRef<TextInput>(null);

  useEffect(() => {
    setText(editing ? editing.body : "");
    if (editing || replyTo) inputRef.current?.focus();
  }, [editing, replyTo]);

  const addPhotos = async () => {
    const room = CHAT_MAX_ATTACHMENTS - files.length;
    if (room <= 0) return;
    const picked = await pickPhotos("library", room, 0.8);
    for (const p of picked) {
      const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
      const contentType = p.mimeType || "image/jpeg";
      const name = p.name || `photo-${id}.jpg`;
      setFiles((list) => [...list, { id, uri: p.uri, contentType, name, ...(p.size ? { size: p.size } : {}), ...(p.width && p.height ? { width: p.width, height: p.height } : {}), status: "uploading" }]);
      upload.mutateAsync({ file: { uri: p.uri, mimeType: contentType, name, size: p.size ?? null }, purpose: "chat_attachment", contentType, maxBytes: CHAT_ATTACHMENT_MAX_BYTES }).then(
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
  const canSend = editing ? text.trim().length > 0 || editing.attachments.length > 0 : !uploading && !files.some((f) => f.status === "error") && (text.trim().length > 0 || ready.length > 0);

  const submit = () => {
    if (!canSend) return;
    if (editing) {
      const body = text.trim();
      if (body !== editing.body) edit.mutate({ id: editing.id, body }, { onError: (err) => toast.error(errorMessage(err)) });
      onCancelEdit();
      return;
    }
    sender.send({
      body: text,
      reply_to: replyTo ? { id: replyTo.id, sender: replyTo.sender, body: replyTo.body.slice(0, 200), has_attachments: replyTo.attachments.length > 0, deleted: false } : null,
      attachments: ready.map((f) => ({
        key: f.key ?? "",
        file_name: f.name,
        content_type: f.contentType,
        preview_url: f.uri,
        ...(f.size ? { size_bytes: f.size } : {}),
        ...(f.width && f.height ? { width: f.width, height: f.height } : {}),
      })),
    });
    setText("");
    setFiles([]);
    onCancelReply();
    onSent();
  };

  const banner = editing || replyTo;
  return (
    <View style={{ gap: s[2] }}>
      {banner ? (
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[3], paddingLeft: s[3], borderRadius: theme.radius.md, backgroundColor: c.sunken }}>
          {editing ? <Pencil color={c.textMuted} size={16} /> : <Reply color={c.textMuted} size={16} />}
          <View style={{ flex: 1, minWidth: 0, paddingVertical: s[2] }}>
            <Text role="label" weight={600} numberOfLines={1}>
              {editing ? "Edit message" : `Reply to ${replyTo?.sender.display_name ?? ""}`}
            </Text>
            {!editing && replyTo?.body ? (
              <Text role="label" tone="muted" numberOfLines={1}>
                {replyTo.body}
              </Text>
            ) : null}
          </View>
          <IconButton label={editing ? "Cancel edit" : "Cancel reply"} icon={X} variant="ghost" onPress={editing ? onCancelEdit : onCancelReply} />
        </View>
      ) : null}
      {files.length > 0 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: s[2], paddingTop: s[2], paddingRight: s[2] }}>
          {files.map((f) => (
            <View key={f.id}>
              <Image source={{ uri: f.uri }} style={{ width: 64, height: 64, borderRadius: theme.radius.md, opacity: f.status === "done" ? 1 : 0.6 }} contentFit="cover" />
              {f.status === "uploading" ? (
                <View style={{ position: "absolute", inset: 0, alignItems: "center", justifyContent: "center" }}>
                  <Spinner label="Uploading" />
                </View>
              ) : null}
              <View style={{ position: "absolute", top: -s[2], right: -s[2] }}>
                <IconButton label="Remove photo" icon={X} variant="secondary" size="md" onPress={() => setFiles((list) => list.filter((x) => x.id !== f.id))} />
              </View>
            </View>
          ))}
        </ScrollView>
      ) : null}
      <View style={{ flexDirection: "row", alignItems: "flex-end", gap: s[2] }}>
        {!editing ? <IconButton label="Add photos" icon={ImagePlus} variant="ghost" onPress={() => void addPhotos()} disabled={files.length >= CHAT_MAX_ATTACHMENTS} /> : null}
        <Input
          ref={inputRef}
          value={text}
          onChangeText={(v) => {
            setText(v);
            if (!editing && v) notifyTyping();
          }}
          placeholder={editing ? "Edit message" : "Message"}
          accessibilityLabel={editing ? "Edit message" : "Message"}
          multiline
          containerStyle={{ flex: 1 }}
          style={{ maxHeight: theme.type.body.lineHeight * 6 + s[4] }}
        />
        <IconButton label={editing ? "Save" : "Send"} icon={SendHorizontal} variant="primary" onPress={submit} disabled={!canSend} loading={uploading} />
      </View>
    </View>
  );
}
