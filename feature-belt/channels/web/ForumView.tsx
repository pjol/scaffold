"use client";

import { channelsPlainText, channelsReplyCount, channelsShortTime, useChannelsSend, useChannelsTopics, type ChannelsDetail, type ChannelsMessage } from "@scaffoldapp/shared";
import { Badge, Button, Dialog, EmptyState, ErrorState, Field, Input, SkeletonRow, TextArea, ToggleChip, cn, toast } from "@scaffoldapp/ui-web";
import { MessageSquarePlus, MessagesSquare, SquarePen } from "lucide-react";
import { useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { ResourceMenu } from "@/components/resource/ResourceMenu";
import { AppAvatar } from "@/components/shell/AppAvatar";

function NewPostDialog({ channel, open, onOpenChange, onPosted }: { channel: ChannelsDetail; open: boolean; onOpenChange: (o: boolean) => void; onPosted: (m: ChannelsMessage) => void }) {
  const sender = useChannelsSend(channel.id);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!title.trim() || !body.trim()) return;
    setBusy(true);
    const msg = await sender.send({ body, title, tags });
    setBusy(false);
    if (!msg) {
      toast.error("Couldn't post. Check your connection and try again.");
      return;
    }
    setTitle("");
    setBody("");
    setTags([]);
    onOpenChange(false);
    onPosted(msg);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New post"
      size="lg"
      footer={
        <Button loading={busy} disabled={!title.trim() || !body.trim()} onClick={() => void submit()}>
          Post
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Title">
          <Input autoFocus value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label="Post" info="Markdown works: **bold**, _italic_, `code`, lists and links.">
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} autoGrow minRows={4} maxRows={12} />
        </Field>
        {channel.forum_tags.length > 0 && (
          <fieldset className="flex flex-col gap-2">
            <legend className="type-label font-semibold text-fg">Tags</legend>
            <div className="flex flex-wrap gap-2">
              {channel.forum_tags.map((t) => (
                <ToggleChip key={t} pressed={tags.includes(t)} onPressedChange={(on) => setTags((s) => (on ? [...s, t] : s.filter((x) => x !== t)))}>
                  {t}
                </ToggleChip>
              ))}
            </div>
          </fieldset>
        )}
      </div>
    </Dialog>
  );
}

function TopicRow({ topic: m, active, onOpen }: { topic: ChannelsMessage; active: boolean; onOpen: () => void }) {
  const activity = m.last_reply_at && m.last_reply_at > m.created_at ? m.last_reply_at : m.created_at;
  const author = m.sender?.display_name ?? m.bot_name ?? "Someone";
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-current={active ? "true" : undefined}
      className={cn(
        "flex w-full gap-3 rounded-lg border border-border bg-surface p-4 text-left transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
        active && "border-primary bg-primary-soft hover:bg-primary-soft",
      )}
    >
      <AppAvatar name={author} src={m.sender?.avatar_url ?? null} size="md" />
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="flex items-start gap-2">
          <span className={cn("min-w-0 flex-1 type-body font-semibold text-fg", m.deleted && "italic text-fg-muted")}>{m.deleted ? "Post deleted" : m.title}</span>
          <span className="shrink-0 type-caption text-fg-muted">{channelsShortTime(activity)}</span>
        </span>
        {!m.deleted && m.body && <span className="line-clamp-2 type-label text-fg-muted">{channelsPlainText(m.body, m.mentions)}</span>}
        <span className="flex flex-wrap items-center gap-2">
          {m.tags.map((t) => (
            <Badge key={t} tone="primary">
              {t}
            </Badge>
          ))}
          <span className="type-caption text-fg-muted">{author}</span>
          <span className="flex items-center gap-1 type-caption text-fg-muted">
            <MessagesSquare aria-hidden className="size-3.5" />
            {m.reply_count > 0 ? channelsReplyCount(m.reply_count) : "No replies"}
          </span>
        </span>
      </span>
    </button>
  );
}

/** A forum channel: posts by latest activity, filtered by tag; each opens its thread. */
export function ForumView({ channel, activeThread, onOpenThread }: { channel: ChannelsDetail; activeThread: string | null; onOpenThread: (rootId: string) => void }) {
  const [tag, setTag] = useState("");
  const [creating, setCreating] = useState(false);
  const topics = useChannelsTopics(channel.id, tag);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-gutter py-3 lg:px-5">
        <div className="flex min-w-0 flex-1 flex-wrap gap-2" role="group" aria-label="Filter by tag">
          <ToggleChip pressed={tag === ""} onPressedChange={() => setTag("")}>
            All
          </ToggleChip>
          {channel.forum_tags.map((t) => (
            <ToggleChip key={t} pressed={tag === t} onPressedChange={(on) => setTag(on ? t : "")}>
              {t}
            </ToggleChip>
          ))}
        </div>
        {channel.permissions.post && (
          <Button size="sm" leftIcon={<SquarePen />} onClick={() => setCreating(true)}>
            New post
          </Button>
        )}
        <ResourceMenu feature="channels" kind="channels.channel" id={channel.id} orgId={channel.org_id} title={`#${channel.name}`} label="Forum actions" />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {topics.isPending ? (
          <div className="flex flex-col gap-2 p-gutter lg:p-5" aria-hidden>
            {Array.from({ length: 4 }, (_, i) => (
              <SkeletonRow key={i} />
            ))}
          </div>
        ) : topics.isError ? (
          <ErrorState compact title="Couldn't load posts" info={topics.error.message} onRetry={() => void topics.refetch()} retrying={topics.isFetching} />
        ) : topics.data.items.length === 0 ? (
          <EmptyState
            compact
            icon={<MessagesSquare />}
            title={tag ? `No posts tagged ${tag}` : "No posts yet"}
            action={
              channel.permissions.post ? (
                <Button variant="secondary" size="auto" leftIcon={<MessageSquarePlus />} onClick={() => setCreating(true)}>
                  Write the first post
                </Button>
              ) : undefined
            }
          />
        ) : (
          <>
            <ul className="flex flex-col gap-2 p-gutter lg:p-5" aria-label="Posts">
              {topics.data.items.map((m) => (
                <li key={m.id}>
                  <TopicRow topic={m} active={activeThread === m.id} onOpen={() => onOpenThread(m.id)} />
                </li>
              ))}
            </ul>
            <LoadMore hasNextPage={topics.hasNextPage} isFetchingNextPage={topics.isFetchingNextPage} fetchNextPage={topics.fetchNextPage} />
          </>
        )}
      </div>
      <NewPostDialog
        channel={channel}
        open={creating}
        onOpenChange={setCreating}
        onPosted={(m) => {
          toast.success("Posted.");
          onOpenThread(m.id);
        }}
      />
    </div>
  );
}
