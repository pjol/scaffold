"use client";

import { errorMessage, useChannelsBookmarks, useCreateChannelsBookmark, useDeleteChannelsBookmark, useUpdateChannelsBookmark, type ChannelsBookmark, type ChannelsDetail } from "@scaffoldapp/shared";
import { Button, Dialog, Field, IconButton, Input, Menu, MenuItem, Skeleton, Tooltip, cn, toast } from "@scaffoldapp/ui-web";
import { ExternalLink, FileText, Link2, Lock, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";

function BookmarkChip({ b, onEdit, onDelete }: { b: ChannelsBookmark; onEdit: () => void; onDelete: () => void }) {
  const cls = "flex h-control-sm max-w-56 items-center gap-1.5 rounded-md px-2 type-label text-fg transition-colors hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring";
  const icon = !b.accessible ? <Lock aria-hidden className="size-3.5 shrink-0 text-fg-subtle" /> : b.kind === "internal" ? <FileText aria-hidden className="size-3.5 shrink-0 text-fg-muted" /> : <Link2 aria-hidden className="size-3.5 shrink-0 text-fg-muted" />;
  const label = <span className="truncate">{b.title}</span>;
  return (
    <li className="group/bm flex shrink-0 items-center">
      {!b.accessible ? (
        <Tooltip content="You can't open this item.">
          <span tabIndex={0} className={cn(cls, "text-fg-subtle")}>
            {icon}
            {label}
          </span>
        </Tooltip>
      ) : b.kind === "internal" ? (
        <Link href={b.card?.link ?? b.url} className={cls}>
          {icon}
          {label}
        </Link>
      ) : (
        <a href={b.url} target="_blank" rel="noopener noreferrer nofollow" className={cls}>
          {icon}
          {label}
          <ExternalLink aria-hidden className="size-3 shrink-0 text-fg-subtle" />
        </a>
      )}
      {b.can_edit && (
        <Menu trigger={<IconButton size="sm" variant="ghost" label={`Bookmark actions: ${b.title}`} icon={<MoreHorizontal />} className="opacity-0 group-hover/bm:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100" />}>
          <MenuItem icon={<Pencil />} onSelect={onEdit}>
            Edit bookmark
          </MenuItem>
          <MenuItem icon={<Trash2 />} destructive onSelect={onDelete}>
            Remove bookmark
          </MenuItem>
        </Menu>
      )}
    </li>
  );
}

/** Links pinned under a channel's header: outside links and pages in the app (checked against your access). */
export function BookmarksBar({ channel }: { channel: ChannelsDetail }) {
  const list = useChannelsBookmarks(channel.bookmark_count > 0 || channel.member ? channel.id : null);
  const remove = useDeleteChannelsBookmark(channel.id);
  const [editing, setEditing] = useState<ChannelsBookmark | "new" | null>(null);
  const items = list.data?.items ?? [];
  const canAdd = channel.member && !channel.archived;
  if (!canAdd && items.length === 0) return null;
  return (
    <div className="flex min-h-control items-center gap-1 overflow-x-auto border-b border-border px-2 lg:px-4" aria-label="Bookmarks">
      {list.isPending && channel.bookmark_count > 0 ? (
        <Skeleton className="h-5 w-40" />
      ) : (
        <ul className="flex items-center gap-1">
          {items.map((b) => (
            <BookmarkChip key={b.id} b={b} onEdit={() => setEditing(b)} onDelete={() => remove.mutate(b.id, { onSuccess: () => toast.success("Bookmark removed."), onError: (e) => toast.error(errorMessage(e)) })} />
          ))}
        </ul>
      )}
      {canAdd && (
        <Button size="sm" variant="ghost" leftIcon={<Plus />} onClick={() => setEditing("new")} className="shrink-0">
          {items.length === 0 ? "Add a bookmark" : <span className="sr-only">Add a bookmark</span>}
        </Button>
      )}
      <BookmarkDialog channelId={channel.id} bookmark={editing} onOpenChange={(o) => !o && setEditing(null)} />
    </div>
  );
}

function BookmarkDialog({ channelId, bookmark, onOpenChange }: { channelId: string; bookmark: ChannelsBookmark | "new" | null; onOpenChange: (o: boolean) => void }) {
  const create = useCreateChannelsBookmark(channelId);
  const update = useUpdateChannelsBookmark(channelId);
  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  useEffect(() => {
    if (bookmark === "new") {
      setUrl("");
      setTitle("");
    } else if (bookmark) {
      setUrl(bookmark.url);
      setTitle(bookmark.title);
    }
  }, [bookmark]);
  const isNew = bookmark === "new";
  const busy = create.isPending || update.isPending;
  const save = () => {
    const done = { onSuccess: () => onOpenChange(false), onError: (e: unknown) => toast.error(errorMessage(e)) };
    if (isNew) create.mutate({ url: url.trim(), ...(title.trim() ? { title: title.trim() } : {}) }, done);
    else if (bookmark) update.mutate({ id: bookmark.id, url: url.trim(), title: title.trim() || bookmark.title }, done);
  };
  return (
    <Dialog
      open={bookmark !== null}
      onOpenChange={onOpenChange}
      title={isNew ? "Add a bookmark" : "Edit bookmark"}
      info="A link to a site, or to a page, file or record in this app. People who can't open an app link see it as locked."
      footer={
        <Button disabled={!url.trim()} loading={busy} onClick={save}>
          {isNew ? "Add bookmark" : "Save"}
        </Button>
      }
    >
      <div className="flex flex-col gap-4">
        <Field label="Link">
          <Input autoFocus value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://" inputMode="url" />
        </Field>
        <Field label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} />
        </Field>
      </div>
    </Dialog>
  );
}
