"use client";

import {
  errorMessage,
  isPinned,
  useAdminFeedPosts,
  useAdminPinFeedPost,
  useAdminRemoveFeedPost,
  useCreateAnnouncement,
  type FeedPost,
  type FeedPostKind,
} from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, Dialog, EmptyState, ErrorState, Field, IconButton, Input, Menu, MenuItem, Select, TextArea, toast } from "@scaffoldapp/ui-web";
import { Megaphone, MoreHorizontal, Newspaper, Pin, PinOff, SearchX, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState, type FormEvent } from "react";
import { SearchInput } from "@/components/admin/SearchInput";
import { Table, TableSkeleton, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { LoadMore } from "@/components/shell/LoadMore";
import { PageHeader } from "@/components/shell/PageHeader";
import { formatDate, formatDateTime } from "@/lib/format";
import { useUrlState } from "@/lib/url-state";

type KindFilter = "any" | FeedPostKind;
type StatusFilter = "any" | "visible" | "removed";

const KIND_OPTIONS = [
  { value: "any", label: "All kinds" },
  { value: "post", label: "Posts" },
  { value: "announcement", label: "Announcements" },
];
const STATUS_OPTIONS = [
  { value: "any", label: "Any status" },
  { value: "visible", label: "Visible" },
  { value: "removed", label: "Removed" },
];

const pad = (n: number) => String(n).padStart(2, "0");
/** `datetime-local` value for a Date, in the browser's zone. */
function localInput(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function AnnouncementDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const create = useCreateAnnouncement();
  const [body, setBody] = useState("");
  const [link, setLink] = useState("");
  const [pin, setPin] = useState(() => localInput(new Date(Date.now() + 7 * 86_400_000)));
  const [error, setError] = useState<string | null>(null);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!body.trim()) {
      setError("Write the announcement.");
      return;
    }
    const until = pin ? new Date(pin) : null;
    if (until && until.getTime() <= Date.now()) {
      setError("Pick a pin end time in the future.");
      return;
    }
    setError(null);
    try {
      await create.mutateAsync({ body, link_url: link.trim() || null, pinned_until: until ? until.toISOString() : null });
      toast.success("Announcement posted");
      setBody("");
      setLink("");
      onOpenChange(false);
    } catch (err) {
      toast.error(errorMessage(err, "Couldn't post the announcement. Try again."));
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="New announcement"
      info="Everyone gets a notification. While pinned, it shows above the feed (3 at most)."
      size="lg"
      footer={
        <>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" form="announcement-form" loading={create.isPending}>
            Post announcement
          </Button>
        </>
      }
    >
      <form id="announcement-form" className="flex flex-col gap-4" onSubmit={(e) => void submit(e)} noValidate>
        <Field label="Announcement" error={error ?? undefined} required>
          <TextArea value={body} onChange={(e) => setBody(e.target.value)} minRows={4} maxLength={5000} />
        </Field>
        <Field label="Link" info="Optional. Shown under the text.">
          <Input type="url" inputMode="url" value={link} onChange={(e) => setLink(e.target.value)} placeholder="https://" />
        </Field>
        <Field label="Pinned until" info="Leave empty to post without pinning. Up to 90 days ahead.">
          <Input type="datetime-local" value={pin} onChange={(e) => setPin(e.target.value)} min={localInput(new Date())} />
        </Field>
      </form>
    </Dialog>
  );
}

function RowActions({ post }: { post: FeedPost }) {
  const remove = useAdminRemoveFeedPost();
  const pin = useAdminPinFeedPost();
  const [confirm, setConfirm] = useState(false);
  const [reason, setReason] = useState("");
  const pinned = isPinned(post);
  const removed = !!post.removed_at;
  if (removed) return null;
  const name = post.author.display_name || "this person";

  return (
    <>
      <Menu align="end" trigger={<IconButton label="Post actions" icon={<MoreHorizontal />} size="sm" variant="ghost" />}>
        {post.kind === "announcement" ? (
          <MenuItem
            icon={pinned ? <PinOff /> : <Pin />}
            onSelect={() =>
              pin.mutate(
                { id: post.id, pinned_until: pinned ? null : new Date(Date.now() + 7 * 86_400_000).toISOString() },
                {
                  onSuccess: () => toast.success(pinned ? "Unpinned" : "Pinned for 7 days"),
                  onError: (e) => toast.error(errorMessage(e, "Couldn't change the pin. Try again.")),
                },
              )
            }
          >
            {pinned ? "Unpin" : "Pin for 7 days"}
          </MenuItem>
        ) : null}
        <MenuItem icon={<Trash2 />} destructive onSelect={() => setConfirm(true)}>
          Remove
        </MenuItem>
      </Menu>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={`Remove this post by ${name}?`}
        tone="danger"
        confirmLabel="Remove"
        onConfirm={async () => {
          await remove.mutateAsync({ id: post.id, reason: reason.trim() });
          toast.success("Post removed");
          setReason("");
        }}
        errorText={(e) => errorMessage(e, "Couldn't remove the post. Try again.")}
      >
        <div className="flex flex-col gap-4">
          <p>It disappears for everyone. The record stays in the audit log.</p>
          <Field label="Reason" info="Saved in the audit log.">
            <TextArea value={reason} onChange={(e) => setReason(e.target.value)} minRows={2} maxLength={500} />
          </Field>
        </div>
      </ConfirmDialog>
    </>
  );
}

/** Admin → Feed: every post (removed included), search, kind and status filters, removal, pins and new announcements. */
export function AdminFeedScreen() {
  const [q, setQ] = useUrlState("q", "");
  const [kind, setKind] = useUrlState<KindFilter>("kind", "any");
  const [status, setStatus] = useUrlState<StatusFilter>("status", "any");
  const [composing, setComposing] = useState(false);
  const posts = useAdminFeedPosts({ q: q || undefined, kind: kind === "any" ? undefined : kind, status: status === "any" ? undefined : status, limit: 50 });
  const rows = posts.data?.items ?? [];
  const filtered = !!q || kind !== "any" || status !== "any";

  return (
    <div className="flex flex-col gap-section">
      <PageHeader
        title="Feed"
        action={
          <Button leftIcon={<Megaphone />} onClick={() => setComposing(true)}>
            New announcement
          </Button>
        }
      />
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2 md:flex-row">
          <SearchInput value={q} onSearch={setQ} placeholder="Words or post ID" label="Search posts" className="md:max-w-sm md:flex-1" />
          <Select aria-label="Kind" options={KIND_OPTIONS} value={kind} onValueChange={(v) => setKind(v as KindFilter)} className="md:w-56" />
          <Select aria-label="Status" options={STATUS_OPTIONS} value={status} onValueChange={(v) => setStatus(v as StatusFilter)} className="md:w-40" />
        </div>
        <Card padding="none">
          {posts.isPending ? (
            <TableSkeleton rows={8} cols={4} />
          ) : posts.isError ? (
            <ErrorState compact title="Couldn't load posts" info={posts.error.message} onRetry={() => void posts.refetch()} retrying={posts.isFetching} />
          ) : rows.length === 0 ? (
            <EmptyState compact icon={filtered ? <SearchX /> : <Newspaper />} title={filtered ? "No matching posts" : "No posts yet"} />
          ) : (
            <>
              <Table label="Posts">
                <THead>
                  <TH>Author</TH>
                  <TH>Post</TH>
                  <TH className="hidden md:table-cell">Status</TH>
                  <TH className="hidden lg:table-cell" align="right">
                    Posted
                  </TH>
                  <TH>
                    <span className="sr-only">Actions</span>
                  </TH>
                </THead>
                <tbody>
                  {rows.map((p) => {
                    const name = p.author.display_name || "Deleted user";
                    const pinned = isPinned(p);
                    const text = p.body || (p.media.length ? `${p.media.length} attachment${p.media.length === 1 ? "" : "s"}` : p.link_url ?? "");
                    return (
                      <TR key={p.id}>
                        <TD>
                          <Link href={`/admin/users/${p.author.id}`} className="flex min-w-0 items-center gap-3 underline-offset-2 hover:underline">
                            <AppAvatar name={name} src={p.author.avatar_url} size="sm" />
                            <span className="truncate font-semibold">{name}</span>
                          </Link>
                        </TD>
                        <TD className="max-w-md">
                          <span className="flex min-w-0 flex-col gap-1">
                            <span className="line-clamp-2 text-fg">{text}</span>
                            {p.kind === "announcement" ? (
                              <span className="flex flex-wrap gap-1">
                                <Badge tone="primary" size="sm">
                                  Announcement
                                </Badge>
                                {pinned && p.pinned_until ? (
                                  <Badge tone="info" size="sm">
                                    Pinned until {formatDateTime(p.pinned_until)}
                                  </Badge>
                                ) : null}
                              </span>
                            ) : null}
                          </span>
                        </TD>
                        <TD className="hidden md:table-cell">
                          {p.removed_at ? (
                            <Badge tone="danger" size="sm">
                              Removed
                            </Badge>
                          ) : (
                            <Badge tone="success" size="sm">
                              Visible
                            </Badge>
                          )}
                        </TD>
                        <TD className="hidden whitespace-nowrap text-fg-muted lg:table-cell" align="right">
                          {formatDate(p.created_at)}
                        </TD>
                        <TD align="right">
                          <RowActions post={p} />
                        </TD>
                      </TR>
                    );
                  })}
                </tbody>
              </Table>
              <LoadMore hasNextPage={posts.hasNextPage} isFetchingNextPage={posts.isFetchingNextPage} fetchNextPage={posts.fetchNextPage} />
            </>
          )}
        </Card>
      </div>
      <AnnouncementDialog open={composing} onOpenChange={setComposing} />
    </div>
  );
}
