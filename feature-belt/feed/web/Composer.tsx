"use client";

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
  useMe,
  useUpdateFeedPost,
  useUpload,
  type FeedPost,
} from "@scaffoldapp/shared";
import { Button, cn, IconButton, Input, Spinner, toast } from "@scaffoldapp/ui-web";
import { ImagePlus, Link2, Paperclip, TriangleAlert, X } from "lucide-react";
import Image from "next/image";
import { useEffect, useRef, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { MentionTextArea, type MentionPerson } from "./MentionTextArea";
import { FileRow, VideoBadge } from "./parts";

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

function imageSize(file: File): Promise<{ width: number; height: number } | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const img = new window.Image();
    img.onload = () => {
      resolve({ width: img.naturalWidth, height: img.naturalHeight });
      URL.revokeObjectURL(url);
    };
    img.onerror = () => {
      resolve(null);
      URL.revokeObjectURL(url);
    };
    img.src = url;
  });
}

function fromPost(post: FeedPost): Attachment[] {
  return post.media.map((m) => ({ id: m.id, kind: m.kind, status: "done", name: m.name ?? "", preview: m.kind === "image" ? m.url : null, key: m.key, width: m.width, height: m.height }));
}

export interface ComposerProps {
  /** Edit this post instead of creating one. */
  post?: FeedPost;
  onDone?: () => void;
  autoFocus?: boolean;
  className?: string;
}

/**
 * New post (or edit): text with @mention suggestions, photos and videos (feed_media),
 * documents (feed_file), an optional link. Uploads start as soon as files are chosen.
 */
export function Composer({ post, onDone, autoFocus, className }: ComposerProps) {
  const me = useMe();
  const create = useCreateFeedPost();
  const update = useUpdateFeedPost();
  const upload = useUpload();
  const [body, setBody] = useState(post?.body ?? "");
  const [people, setPeople] = useState<MentionPerson[]>(() => (post ? mentionedPeople(post.mentions) : []));
  const [link, setLink] = useState(post?.link_url ?? "");
  const [showLink, setShowLink] = useState(!!post?.link_url);
  const [items, setItems] = useState<Attachment[]>(() => (post ? fromPost(post) : []));
  const mediaInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const previews = useRef<string[]>([]);

  useEffect(() => () => previews.current.forEach((u) => URL.revokeObjectURL(u)), []);

  const patch = (id: string, next: Partial<Attachment>) => setItems((list) => list.map((a) => (a.id === id ? { ...a, ...next } : a)));

  const addFiles = async (files: FileList | null, purpose: typeof FEED_MEDIA_PURPOSE | typeof FEED_FILE_PURPOSE) => {
    if (!files || files.length === 0) return;
    const room = FEED_LIMITS.attachments - items.length;
    const chosen = Array.from(files).slice(0, Math.max(0, room));
    if (files.length > room) toast.error(`A post can have up to ${FEED_LIMITS.attachments} attachments.`);
    for (const file of chosen) {
      const isFile = purpose === FEED_FILE_PURPOSE;
      const allowed: readonly string[] = isFile ? FEED_FILE_TYPES : FEED_MEDIA_TYPES;
      if (!allowed.includes(file.type)) {
        toast.error(isFile ? `${file.name} isn't a supported document. Use PDF, Word, Excel, PowerPoint, text, CSV or zip.` : `${file.name} isn't a supported photo or video.`);
        continue;
      }
      const max = isFile ? FEED_LIMITS.fileBytes : FEED_LIMITS.mediaBytes;
      if (file.size > max) {
        toast.error(`${file.name} is over ${Math.round(max / (1024 * 1024))} MB. Choose a smaller file.`);
        continue;
      }
      const kind = isFile ? "file" : file.type.startsWith("video/") ? "video" : "image";
      const preview = kind === "image" ? URL.createObjectURL(file) : null;
      if (preview) previews.current.push(preview);
      const id = localId();
      setItems((list) => [...list, { id, kind, status: "uploading", name: file.name, preview }]);
      try {
        const size = kind === "image" ? await imageSize(file) : null;
        const res = await upload.mutateAsync({ file, purpose, maxBytes: max });
        patch(id, { status: "done", key: res.key, width: size?.width ?? null, height: size?.height ?? null });
      } catch (e) {
        patch(id, { status: "error" });
        toast.error(errorMessage(e, `Couldn't upload ${file.name}. Try again.`));
      }
    }
  };

  const uploading = items.some((a) => a.status === "uploading");
  const ready = items.filter((a) => a.status === "done" && a.key);
  const over = body.length > FEED_LIMITS.postChars;
  const empty = !body.trim() && ready.length === 0 && !link.trim();
  const busy = create.isPending || update.isPending;

  const reset = () => {
    setBody("");
    setPeople([]);
    setLink("");
    setShowLink(false);
    setItems([]);
  };

  const submit = async () => {
    if (empty || over || uploading || busy) return;
    const media = ready.map((a) => ({ key: a.key ?? "", width: a.width ?? null, height: a.height ?? null, ...(a.kind === "file" ? { name: a.name } : {}) }));
    const mentions = buildMentions(body, people);
    try {
      if (post) {
        await update.mutateAsync({ id: post.id, body, link_url: link.trim(), media, mentions });
        toast.success("Post updated");
      } else {
        await create.mutateAsync({ body, link_url: link.trim() || null, media, mentions });
        reset();
        toast.success("Posted");
      }
      onDone?.();
    } catch (e) {
      toast.error(errorMessage(e, post ? "Couldn't save your post. Try again." : "Couldn't post. Try again."));
    }
  };

  const visual = items.filter((a) => a.kind !== "file");
  const files = items.filter((a) => a.kind === "file");
  const name = me.data?.display_name || me.data?.email || "";
  const remaining = FEED_LIMITS.postChars - body.length;

  return (
    <form
      className={cn("flex flex-col gap-3", className)}
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <div className="flex items-start gap-3">
        {!post ? <AppAvatar name={name} src={me.data?.avatar_url ?? null} size="md" className="hidden sm:flex" /> : null}
        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <MentionTextArea
            value={body}
            onChange={setBody}
            people={people}
            onPeopleChange={setPeople}
            placeholder={post ? undefined : "Share something with everyone"}
            aria-label={post ? "Post text" : "New post"}
            minRows={post ? 4 : 2}
            autoFocus={autoFocus}
            onSubmit={() => void submit()}
          />

          {visual.length > 0 ? (
            <ul className="grid grid-cols-3 gap-2 sm:grid-cols-4" aria-label="Photos and videos">
              {visual.map((a) => (
                <li key={a.id} className="relative aspect-square overflow-hidden rounded-md bg-sunken">
                  {a.preview ? <Image src={a.preview} alt={a.name || "Photo"} fill sizes="160px" className="object-cover" unoptimized /> : null}
                  {a.kind === "video" ? <VideoBadge /> : null}
                  {a.status === "uploading" ? (
                    <span className="absolute inset-0 flex items-center justify-center bg-overlay">
                      <Spinner size={20} label={`Uploading ${a.name}`} className="text-fg-inverse" />
                    </span>
                  ) : a.status === "error" ? (
                    <span className="absolute inset-0 flex items-center justify-center bg-danger-soft text-danger-strong" title="Upload failed">
                      <TriangleAlert className="size-5" aria-label="Upload failed" />
                    </span>
                  ) : null}
                  <IconButton label={`Remove ${a.name || "attachment"}`} icon={<X />} size="sm" variant="inverse" className="absolute right-1 top-1" onClick={() => setItems((l) => l.filter((x) => x.id !== a.id))} />
                </li>
              ))}
            </ul>
          ) : null}

          {files.length > 0 ? (
            <ul className="flex flex-col gap-2" aria-label="Documents">
              {files.map((a) => (
                <li key={a.id}>
                  <FileRow
                    media={{ name: a.name, key: a.name }}
                    trailing={
                      <>
                        {a.status === "uploading" ? <Spinner size={16} label={`Uploading ${a.name}`} /> : null}
                        {a.status === "error" ? <TriangleAlert className="size-4 text-danger-strong" aria-label="Upload failed" /> : null}
                        <IconButton label={`Remove ${a.name}`} icon={<X />} size="sm" variant="ghost" onClick={() => setItems((l) => l.filter((x) => x.id !== a.id))} />
                      </>
                    }
                  />
                </li>
              ))}
            </ul>
          ) : null}

          {showLink ? (
            <div className="flex items-center gap-2">
              <Input type="url" inputMode="url" value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://" aria-label="Link" className="flex-1" maxLength={2048} />
              <IconButton
                label="Remove link"
                icon={<X />}
                variant="ghost"
                onClick={() => {
                  setLink("");
                  setShowLink(false);
                }}
              />
            </div>
          ) : null}

          <div className="flex flex-wrap items-center gap-2">
            <IconButton label="Add photos or videos" tooltip icon={<ImagePlus />} variant="ghost" disabled={items.length >= FEED_LIMITS.attachments} onClick={() => mediaInput.current?.click()} />
            <IconButton label="Attach a document" tooltip icon={<Paperclip />} variant="ghost" disabled={items.length >= FEED_LIMITS.attachments} onClick={() => fileInput.current?.click()} />
            <IconButton label="Add a link" tooltip icon={<Link2 />} variant="ghost" disabled={showLink} onClick={() => setShowLink(true)} />
            <span className="flex-1" />
            {remaining < 500 ? (
              <span className={cn("type-caption tabular-nums", over ? "text-danger-strong" : "text-fg-muted")} aria-live="polite">
                {remaining}
              </span>
            ) : null}
            {onDone && post ? (
              <Button type="button" variant="ghost" onClick={onDone}>
                Cancel
              </Button>
            ) : null}
            <Button type="submit" loading={busy} disabled={empty || over || uploading}>
              {post ? "Save" : "Post"}
            </Button>
          </div>

        </div>
      </div>

      <input
        ref={mediaInput}
        type="file"
        hidden
        multiple
        accept={FEED_MEDIA_TYPES.join(",")}
        onChange={(e) => {
          void addFiles(e.target.files, FEED_MEDIA_PURPOSE);
          e.target.value = "";
        }}
      />
      <input
        ref={fileInput}
        type="file"
        hidden
        multiple
        accept={FEED_FILE_TYPES.join(",")}
        onChange={(e) => {
          void addFiles(e.target.files, FEED_FILE_PURPOSE);
          e.target.value = "";
        }}
      />
    </form>
  );
}
