"use client";

import { attachmentSummary, errorMessage, systemEventText, useAdminRemoveChatMessage, useChatAdminConversation, type ChatMessage, type ChatWindowParams, chatFullTime } from "@scaffoldapp/shared";
import { Badge, Button, Card, ConfirmDialog, EmptyState, ErrorState, Field, Skeleton, SkeletonGroup, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { ArrowLeft, MessagesSquare, Paperclip } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { Table, TD, TH, THead, TR } from "@/components/admin/Table";
import { AppAvatar } from "@/components/shell/AppAvatar";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { formatDate } from "@/lib/format";

const DELETED_LABEL = { staff: "Removed by staff", sender: "Deleted by sender", account_deleted: "Account deleted" } as const;

/** Admin → Chat → a conversation: members (current and past) and messages, hidden ones readable. Audited per load. */
export function AdminConversationScreen({ id }: { id: string }) {
  const around = useSearchParams()?.get("around") ?? null;
  const [win, setWin] = useState<ChatWindowParams>(around ? { around } : {});
  useEffect(() => setWin(around ? { around } : {}), [around]);
  const conv = useChatAdminConversation(id, win);
  const remove = useAdminRemoveChatMessage();
  const [removing, setRemoving] = useState<ChatMessage | null>(null);
  const [reason, setReason] = useState("");

  const back = (
    <Button asChild variant="ghost" size="sm" leftIcon={<ArrowLeft />}>
      <Link href="/admin/chat">Chat</Link>
    </Button>
  );

  if (conv.isPending) {
    return (
      <SkeletonGroup className="flex flex-col gap-section">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40" />
        <Skeleton className="h-80" />
      </SkeletonGroup>
    );
  }
  if (conv.isError) {
    return conv.error.status === 404 ? (
      <EmptyState icon={<MessagesSquare />} title="Conversation not found" action={back} />
    ) : (
      <ErrorState title="Couldn't load this conversation" info={conv.error.message} onRetry={() => void conv.refetch()} retrying={conv.isFetching} />
    );
  }
  const c = conv.data;
  const items = c.messages.items;
  const target = win.around;

  return (
    <div className="flex flex-col gap-section">
      <div className="flex flex-col gap-2">
        <div>{back}</div>
        <PageHeader title={c.title || "Conversation"} info="Opening this view is recorded in the audit log." action={<Badge>{c.kind === "group" ? "Group" : "Direct"}</Badge>} />
      </div>

      <Section title="Members">
        <Card padding="none">
          <Table label="Members">
            <THead>
              <TH>Person</TH>
              <TH>Role</TH>
              <TH className="hidden sm:table-cell">Joined</TH>
              <TH>Left</TH>
            </THead>
            <tbody>
              {c.members.map((m) => (
                <TR key={m.user.id}>
                  <TD>
                    <span className="flex min-w-0 items-center gap-3">
                      <AppAvatar name={m.user.display_name} src={m.user.avatar_url} size="sm" />
                      <Link href={`/admin/users/${m.user.id}`} className="truncate font-semibold text-fg underline-offset-2 hover:underline">
                        {m.user.display_name}
                      </Link>
                    </span>
                  </TD>
                  <TD className="capitalize">{m.role}</TD>
                  <TD className="hidden whitespace-nowrap text-fg-muted sm:table-cell">{formatDate(m.joined_at)}</TD>
                  <TD className="whitespace-nowrap text-fg-muted">{m.left_at ? formatDate(m.left_at) : ""}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        </Card>
      </Section>

      <Section title="Messages">
        <Card padding="none">
          {c.messages.has_older && items[0] && (
            <div className="flex justify-center border-b border-border p-2">
              <Button size="sm" variant="ghost" onClick={() => setWin({ before: items[0]?.id ?? "" })}>
                Older messages
              </Button>
            </div>
          )}
          {items.length === 0 ? (
            <EmptyState compact icon={<MessagesSquare />} title="No messages" />
          ) : (
            <ol className={cn("flex flex-col", conv.isFetching && "opacity-70")} aria-label="Messages">
              {items.map((m) => (
                <li key={m.id} className={cn("flex gap-3 border-b border-border px-4 py-3 last:border-b-0", m.id === target && "bg-primary-soft")}>
                  {m.kind === "system" ? (
                    <p className="flex-1 text-center type-caption text-fg-muted">{systemEventText(m)}</p>
                  ) : (
                    <>
                      <AppAvatar name={m.sender.display_name} src={m.sender.avatar_url} size="sm" />
                      <div className="flex min-w-0 flex-1 flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <Link href={`/admin/users/${m.sender.id}`} className="type-label font-semibold text-fg underline-offset-2 hover:underline">
                            {m.sender.display_name}
                          </Link>
                          <time dateTime={m.created_at} className="type-caption text-fg-muted">
                            {chatFullTime(m.created_at)}
                          </time>
                          {m.edited_at && <Badge>Edited</Badge>}
                          {m.deleted_by && <Badge tone={m.deleted_by === "staff" ? "warning" : "neutral"}>{DELETED_LABEL[m.deleted_by]}</Badge>}
                        </div>
                        {m.body && <p className={cn("whitespace-pre-wrap break-words type-body text-fg", m.deleted && "text-fg-muted")}>{m.body}</p>}
                        {m.attachments.length > 0 && (
                          <ul className="flex flex-wrap gap-2">
                            {m.attachments.map((a) => (
                              <li key={a.id}>
                                <a href={a.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 type-label text-primary-strong underline-offset-2 hover:underline">
                                  <Paperclip aria-hidden className="size-4" />
                                  {a.file_name || attachmentSummary([a])}
                                </a>
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                      {!m.deleted && (
                        <Button
                          size="sm"
                          variant="danger-ghost"
                          onClick={() => {
                            setReason("");
                            setRemoving(m);
                          }}
                        >
                          Remove
                        </Button>
                      )}
                    </>
                  )}
                </li>
              ))}
            </ol>
          )}
          {c.messages.has_newer && items.length > 0 && (
            <div className="flex justify-center border-t border-border p-2">
              <Button size="sm" variant="ghost" onClick={() => setWin({ after: items[items.length - 1]?.id ?? "" })}>
                Newer messages
              </Button>
            </div>
          )}
        </Card>
      </Section>

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.sender.display_name ?? "this person"}'s message?`}
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
