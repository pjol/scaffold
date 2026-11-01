"use client";

import { channelsAuthorName, channelsFullTime, channelsSystemText, errorMessage, useAdminRemoveChannelsMessage, useChannelsAdminChannel, type ChannelsMessage, type ChannelsMessagePage, type ChannelsWindowParams } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, Skeleton, SkeletonGroup, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, Hash, Paperclip } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PageHeader, Section } from "@/components/shell/PageHeader";

const DELETED_LABEL = { staff: "Removed by staff", sender: "Deleted by sender", moderator: "Deleted by an admin", account_deleted: "Account deleted" } as const;
const KIND_LABEL = { public: "Public channel", private: "Private channel", dm: "Direct message", group_dm: "Group message" } as const;

function Messages({ page, target, onRemove, onWindow, label }: { page: ChannelsMessagePage; target: string | null; onRemove: (m: ChannelsMessage) => void; onWindow: (w: ChannelsWindowParams) => void; label: string }) {
  const items = page.items;
  return (
    <Card padding="none">
      {page.has_older && items[0] && (
        <div className="flex justify-center border-b border-border p-2">
          <Button size="sm" variant="ghost" onClick={() => onWindow({ before: items[0]?.id ?? "" })}>
            Older messages
          </Button>
        </div>
      )}
      {items.length === 0 ? (
        <EmptyState compact icon={<Hash />} title="No messages" />
      ) : (
        <ol className="flex flex-col" aria-label={label}>
          {items.map((m) => (
            <li key={m.id} className={cn("flex gap-3 border-b border-border px-4 py-3 last:border-b-0", m.id === target && "bg-primary-soft")}>
              {m.kind === "system" ? (
                <p className="flex-1 text-center type-caption text-fg-muted">{channelsSystemText(m)}</p>
              ) : (
                <>
                  <AppAvatar name={channelsAuthorName(m)} src={m.sender?.avatar_url ?? null} size="sm" />
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex flex-wrap items-center gap-2">
                      {m.sender ? (
                        <Link href={`/admin/users/${m.sender.id}`} className="type-label font-semibold text-fg underline-offset-2 hover:underline">
                          {m.sender.display_name}
                        </Link>
                      ) : (
                        <span className="type-label font-semibold text-fg">{m.bot_name}</span>
                      )}
                      <time dateTime={m.created_at} className="type-caption text-fg-muted">
                        {channelsFullTime(m.created_at)}
                      </time>
                      {m.reply_count > 0 && <Badge>{m.reply_count} replies</Badge>}
                      {m.edited_at && <Badge>Edited</Badge>}
                      {m.deleted_by && <Badge tone={m.deleted_by === "staff" ? "warning" : "neutral"}>{DELETED_LABEL[m.deleted_by]}</Badge>}
                    </div>
                    {m.title && <p className="type-body font-semibold text-fg">{m.title}</p>}
                    {m.body && <p className={cn("whitespace-pre-wrap break-words type-body text-fg", m.deleted && "text-fg-muted")}>{m.body}</p>}
                    {m.attachments.length > 0 && (
                      <ul className="flex flex-wrap gap-2">
                        {m.attachments.map((a) => (
                          <li key={a.id}>
                            <a href={a.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 type-label text-primary-strong underline-offset-2 hover:underline">
                              <Paperclip aria-hidden className="size-4" />
                              {a.file_name || "File"}
                            </a>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                  {!m.deleted && (
                    <Button size="sm" variant="danger-ghost" onClick={() => onRemove(m)}>
                      Remove
                    </Button>
                  )}
                </>
              )}
            </li>
          ))}
        </ol>
      )}
      {page.has_newer && items.length > 0 && (
        <div className="flex justify-center border-t border-border p-2">
          <Button size="sm" variant="ghost" onClick={() => onWindow({ after: items[items.length - 1]?.id ?? "" })}>
            Newer messages
          </Button>
        </div>
      )}
    </Card>
  );
}

/** Admin → Channels → a channel: members and messages (and a thread), hidden ones readable. Audited per load. */
export function AdminChannelScreen({ id }: { id: string }) {
  const params = useSearchParams();
  const around = params?.get("around") ?? null;
  const thread = params?.get("thread") ?? null;
  const [win, setWin] = useState<ChannelsWindowParams>(around ? { around } : {});
  useEffect(() => setWin(around ? { around } : {}), [around]);
  const ch = useChannelsAdminChannel(id, { ...win, ...(thread ? { thread } : {}) });
  const remove = useAdminRemoveChannelsMessage();
  const [removing, setRemoving] = useState<ChannelsMessage | null>(null);
  const [reason, setReason] = useState("");
  const back = (
    <Button asChild variant="ghost" size="sm" leftIcon={<ArrowLeft />}>
      <Link href="/admin/channels">Channels</Link>
    </Button>
  );
  if (ch.isPending) {
    return (
      <SkeletonGroup className="flex flex-col gap-section">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-80" />
      </SkeletonGroup>
    );
  }
  if (ch.isError) {
    return ch.error.status === 404 ? (
      <EmptyState icon={<Hash />} title="Channel not found" action={back} />
    ) : (
      <ErrorState title="Couldn't load this channel" info={ch.error.message} onRetry={() => void ch.refetch()} retrying={ch.isFetching} />
    );
  }
  const c = ch.data;
  const startRemove = (m: ChannelsMessage) => {
    setReason("");
    setRemoving(m);
  };
  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-2">
        <div>{back}</div>
        <PageHeader
          title={c.kind === "public" || c.kind === "private" ? `#${c.name}` : c.name}
          info={`In ${c.org_name}. Opening this view is recorded in the audit log.`}
          action={
            <span className="flex gap-2">
              <Badge>{KIND_LABEL[c.kind]}</Badge>
              {c.archived && <Badge tone="warning">Archived</Badge>}
            </span>
          }
        />
      </div>
      <Section title={`Members (${c.members.length})`}>
        <ul className="flex flex-wrap gap-2">
          {c.members.map((u) => (
            <li key={u.id}>
              <Link href={`/admin/users/${u.id}`} className="flex items-center gap-2 rounded-pill border border-border px-3 py-1 type-label text-fg hover:bg-sunken">
                <AppAvatar name={u.display_name} src={u.avatar_url} size="xs" />
                {u.display_name}
              </Link>
            </li>
          ))}
        </ul>
      </Section>
      {c.thread && (
        <Section title="Thread">
          <Messages page={c.thread} target={around} onRemove={startRemove} onWindow={() => undefined} label="Thread replies" />
        </Section>
      )}
      <Section title="Messages">
        <Messages page={c.messages} target={thread ?? around} onRemove={startRemove} onWindow={setWin} label="Messages" />
      </Section>
      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing ? channelsAuthorName(removing) : "this"}'s message?`}
        confirmLabel="Remove"
        tone="danger"
        onConfirm={async () => {
          if (!removing) return;
          await remove.mutateAsync({ messageId: removing.id, reason: reason.trim() });
          toast.success("Message removed.");
        }}
        errorText={(e) => errorMessage(e)}
      >
        <div className="flex flex-col gap-4">
          <p className="type-body text-fg-muted">Members see "Message deleted". The text stays here for the record.</p>
          <Field label="Reason">
            <TextArea value={reason} maxLength={500} onChange={(e) => setReason(e.target.value)} autoGrow minRows={2} maxRows={5} />
          </Field>
        </div>
      </ConfirmDialog>
    </div>
  );
}
