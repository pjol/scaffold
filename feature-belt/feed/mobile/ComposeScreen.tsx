import {
  buildMentions,
  errorMessage,
  FEED_FILE_PURPOSE,
  FEED_FILE_TYPES,
  FEED_LIMITS,
  FEED_MEDIA_PURPOSE,
  FEED_MEDIA_TYPES,
  mentionedPeople,
  useCreateFeedPost,
  useFeedPost,
  useUpdateFeedPost,
  useUpload,
  type FeedPost,
  type NativeFileRef,
} from "@scaffoldapp/shared";
import { Button, ErrorState, IconButton, Input, Screen, Skeleton, Spinner, Text, haptic, toast, useTheme } from "@scaffoldapp/ui-native";
import * as DocumentPicker from "expo-document-picker";
import { Image } from "expo-image";
import * as ImagePicker from "expo-image-picker";
import { router } from "expo-router";
import { ImagePlus, Link2, Paperclip, Play, Send, TriangleAlert, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { StyleSheet, View } from "react-native";
import { MentionInput, type MentionPerson } from "./MentionInput";
import { FileRow } from "./parts";

interface Attachment {
  id: string;
  kind: "image" | "video" | "file";
  status: "uploading" | "done" | "error";
  name: string;
  preview: string | null;
  key?: string;
  width?: number | null;
  height?: number | null;
}

let seq = 0;
const localId = () => `a${++seq}`;

function Editor({ post }: { post?: FeedPost }) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const create = useCreateFeedPost();
  const update = useUpdateFeedPost();
  const upload = useUpload();
  const [body, setBody] = useState(post?.body ?? "");
  const [people, setPeople] = useState<MentionPerson[]>(() => (post ? mentionedPeople(post.mentions) : []));
  const [link, setLink] = useState(post?.link_url ?? "");
  const [showLink, setShowLink] = useState(!!post?.link_url);
  const [items, setItems] = useState<Attachment[]>(() =>
    post ? post.media.map((m) => ({ id: m.id, kind: m.kind, status: "done", name: m.name ?? "", preview: m.kind === "image" ? m.url : null, key: m.key, width: m.width, height: m.height })) : [],
  );

  const patch = (id: string, next: Partial<Attachment>) => setItems((list) => list.map((a) => (a.id === id ? { ...a, ...next } : a)));
  const room = FEED_LIMITS.attachments - items.length;

  const add = async (file: NativeFileRef, kind: Attachment["kind"], dims?: { width?: number | null; height?: number | null }) => {
    const isFile = kind === "file";
    const type = file.mimeType ?? file.type ?? "";
    const allowed: readonly string[] = isFile ? FEED_FILE_TYPES : FEED_MEDIA_TYPES;
    const name = file.name ?? (isFile ? "Document" : kind === "video" ? "Video" : "Photo");
    if (type && !allowed.includes(type)) {
      toast.error(isFile ? `${name} isn't a supported document.` : `${name} isn't a supported photo or video.`);
      return;
    }
    const max = isFile ? FEED_LIMITS.fileBytes : FEED_LIMITS.mediaBytes;
    if (file.size && file.size > max) {
      toast.error(`${name} is over ${Math.round(max / (1024 * 1024))} MB. Choose a smaller file.`);
      return;
    }
    const id = localId();
    setItems((list) => [...list, { id, kind, status: "uploading", name, preview: kind === "image" ? file.uri : null }]);
    try {
      const res = await upload.mutateAsync({ file, purpose: isFile ? FEED_FILE_PURPOSE : FEED_MEDIA_PURPOSE, maxBytes: max });
      patch(id, { status: "done", key: res.key, width: dims?.width ?? null, height: dims?.height ?? null });
    } catch (e) {
      patch(id, { status: "error" });
      toast.error(errorMessage(e, `Couldn't upload ${name}. Try again.`));
    }
  };

  // The system photo picker needs no library permission.
  const pickMedia = async () => {
    if (room <= 0) return;
    const res = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ["images", "videos"], allowsMultipleSelection: true, selectionLimit: room, quality: 0.85, exif: false });
    if (res.canceled) return;
    for (const a of res.assets.slice(0, room)) {
      void add({ uri: a.uri, mimeType: a.mimeType ?? null, name: a.fileName ?? null, size: a.fileSize ?? null }, a.type === "video" ? "video" : "image", { width: a.width, height: a.height });
    }
  };

  const pickDocs = async () => {
    if (room <= 0) return;
    const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true, type: [...FEED_FILE_TYPES] });
    if (res.canceled) return;
    for (const a of res.assets.slice(0, room)) void add({ uri: a.uri, mimeType: a.mimeType ?? null, name: a.name, size: a.size ?? null }, "file");
  };

  const uploading = items.some((a) => a.status === "uploading");
  const ready = items.filter((a) => a.status === "done" && a.key);
  const over = body.length > FEED_LIMITS.postChars;
  const empty = !body.trim() && ready.length === 0 && !link.trim();
  const busy = create.isPending || update.isPending;

  const submit = async () => {
    if (empty || over || uploading || busy) return;
    const media = ready.map((a) => ({ key: a.key ?? "", width: a.width ?? null, height: a.height ?? null, ...(a.kind === "file" ? { name: a.name } : {}) }));
    const mentions = buildMentions(body, people);
    try {
      if (post) await update.mutateAsync({ id: post.id, body, link_url: link.trim(), media, mentions });
      else await create.mutateAsync({ body, link_url: link.trim() || null, media, mentions });
      haptic.success();
      toast.success(post ? "Post updated" : "Posted");
      router.back();
    } catch (e) {
      toast.error(errorMessage(e, post ? "Couldn't save your post. Try again." : "Couldn't post. Try again."));
    }
  };

  const visual = items.filter((a) => a.kind !== "file");
  const files = items.filter((a) => a.kind === "file");
  const remaining = FEED_LIMITS.postChars - body.length;

  return (
    <Screen
      keyboard
      edges={["bottom"]}
      gap={s[4]}
      footer={
        <Button fullWidth leftIcon={Send} loading={busy} disabled={empty || over || uploading} onPress={() => void submit()}>
          {post ? "Save" : "Post"}
        </Button>
      }
    >
      <MentionInput value={body} onChange={setBody} people={people} onPeopleChange={setPeople} placeholder={post ? undefined : "Share something with everyone"} accessibilityLabel="Post text" autoFocus={!post} minHeight={theme.tap.xl * 2} />
      {remaining < 500 ? (
        <Text role="caption" tone={over ? "danger" : "muted"} style={{ alignSelf: "flex-end" }}>
          {remaining}
        </Text>
      ) : null}

      {visual.length > 0 ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {visual.map((a) => (
            <View key={a.id} style={{ width: theme.tap.xl + s[6], height: theme.tap.xl + s[6], borderRadius: theme.radius.md, overflow: "hidden", backgroundColor: a.kind === "video" ? c.text : c.sunken }}>
              {a.preview ? <Image source={{ uri: a.preview }} style={{ flex: 1 }} contentFit="cover" accessibilityLabel={a.name} /> : null}
              {a.kind === "video" ? (
                <View style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center" }]}>
                  <Play size={24} color={c.textInverse} fill={c.textInverse} />
                </View>
              ) : null}
              {a.status !== "done" ? (
                <View style={[StyleSheet.absoluteFill, { alignItems: "center", justifyContent: "center", backgroundColor: c.overlay }]}>
                  {a.status === "uploading" ? <Spinner size={20} color={c.textInverse} /> : <TriangleAlert size={20} color={c.textInverse} accessibilityLabel="Upload failed" />}
                </View>
              ) : null}
              <View style={{ position: "absolute", top: s[1], right: s[1] }}>
                <IconButton label={`Remove ${a.name}`} icon={X} variant="secondary" onPress={() => setItems((l) => l.filter((x) => x.id !== a.id))} />
              </View>
            </View>
          ))}
        </View>
      ) : null}

      {files.length > 0 ? (
        <View style={{ gap: s[2] }}>
          {files.map((a) => (
            <FileRow
              key={a.id}
              name={a.name}
              trailing={
                <>
                  {a.status === "uploading" ? <Spinner size={16} /> : null}
                  {a.status === "error" ? <TriangleAlert size={16} color={c.dangerStrong} accessibilityLabel="Upload failed" /> : null}
                  <IconButton label={`Remove ${a.name}`} icon={X} variant="ghost" onPress={() => setItems((l) => l.filter((x) => x.id !== a.id))} />
                </>
              }
            />
          ))}
        </View>
      ) : null}

      {showLink ? (
        <Input
          value={link}
          onChangeText={setLink}
          placeholder="https://"
          accessibilityLabel="Link"
          keyboardType="url"
          autoCapitalize="none"
          autoCorrect={false}
          leadingIcon={Link2}
          trailing={
            <IconButton
              label="Remove link"
              icon={X}
              variant="ghost"
              onPress={() => {
                setLink("");
                setShowLink(false);
              }}
            />
          }
        />
      ) : null}

      <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
        <Button variant="secondary" size="md" leftIcon={ImagePlus} disabled={room <= 0} onPress={() => void pickMedia()}>
          Photos
        </Button>
        <Button variant="secondary" size="md" leftIcon={Paperclip} disabled={room <= 0} onPress={() => void pickDocs()}>
          File
        </Button>
        {!showLink ? (
          <Button variant="secondary" size="md" leftIcon={Link2} onPress={() => setShowLink(true)}>
            Link
          </Button>
        ) : null}
      </View>
    </Screen>
  );
}

/** New post, or edit one (`id`). Presented as a modal by its route. */
export function ComposeScreen({ id }: { id?: string }) {
  const theme = useTheme();
  const post = useFeedPost(id ?? null);
  const [seed, setSeed] = useState<FeedPost | null>(null);
  useEffect(() => {
    if (post.data && !seed) setSeed(post.data);
  }, [post.data, seed]);

  if (!id) return <Editor />;
  if (seed) return <Editor post={seed} />;
  if (post.isError) {
    return (
      <Screen edges={["bottom"]}>
        <ErrorState title="Couldn't load the post" info={errorMessage(post.error)} onRetry={() => void post.refetch()} retrying={post.isRefetching} />
      </Screen>
    );
  }
  return (
    <Screen edges={["bottom"]}>
      <Skeleton height={theme.tap.xl * 2} />
    </Screen>
  );
}
