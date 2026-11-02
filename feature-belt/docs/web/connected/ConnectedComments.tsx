"use client";

import { docsPlain, docsTimeAgo, errorMessage, useDocsConnectedAddComment, useDocsConnectedComments, type DocsConnectedComment } from "@scaffoldapp/shared";
import { Avatar, Button, EmptyState, ErrorState, IconButton, Skeleton, TextArea, toast } from "@scaffoldapp/ui-web";
import { MessageSquare, X } from "lucide-react";
import { useState } from "react";

function Comment({ c }: { c: DocsConnectedComment }) {
  const name = c.author?.name ?? "Someone";
  return (
    <div className="flex gap-2">
      <Avatar size="xs" name={name} {...(c.author?.avatar_url ? { src: c.author.avatar_url } : {})} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex flex-wrap items-baseline gap-x-2 type-label">
          <span className="font-semibold text-fg">{name}</span>
          <span className="type-caption text-fg-muted">{docsTimeAgo(c.created_at)}</span>
        </p>
        {c.quote ? <p className="border-l-2 border-border-strong pl-2 type-caption text-fg-muted">{c.quote}</p> : null}
        <p className="whitespace-pre-wrap break-words type-body text-fg">{docsPlain(c.body)}</p>
      </div>
    </div>
  );
}

function Composer({ placeholder, busy, onSend, autoFocus }: { placeholder: string; busy: boolean; onSend: (text: string) => Promise<unknown>; autoFocus?: boolean }) {
  const [text, setText] = useState("");
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(e) => {
        e.preventDefault();
        const t = text.trim();
        if (!t) return;
        void onSend(t).then(() => setText(""));
      }}
    >
      <TextArea value={text} onChange={(e) => setText(e.target.value)} placeholder={placeholder} aria-label={placeholder} rows={2} maxLength={2000} autoFocus={autoFocus} />
      <div className="flex justify-end">
        <Button type="submit" size="sm" loading={busy} disabled={!text.trim()}>
          Send
        </Button>
      </div>
    </form>
  );
}

/** Comments on a connected page, read and written on the platform (as the person's account there). */
export function ConnectedComments({
  connectionId,
  pageRef,
  platform,
  canComment,
  pendingQuote,
  onClearQuote,
  onClose,
  header,
}: {
  connectionId: string;
  pageRef: string;
  platform: string;
  canComment: boolean;
  pendingQuote: string | null;
  onClearQuote: () => void;
  onClose: () => void;
  header: boolean;
}) {
  const list = useDocsConnectedComments(connectionId, pageRef);
  const add = useDocsConnectedAddComment(connectionId, pageRef);
  const [replying, setReplying] = useState<string | null>(null);
  const send = (text: string, thread?: string) =>
    add.mutateAsync({ body: [{ text: pendingQuote && !thread ? `> ${pendingQuote}\n\n${text}` : text }], ...(thread ? { thread_id: thread } : {}) }).then(
      () => {
        if (!thread) onClearQuote();
        setReplying(null);
      },
      (e) => toast.error(errorMessage(e, `Couldn't add the comment in ${platform}.`)),
    );
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header ? (
        <div className="flex min-h-12 items-center gap-2 border-b border-border px-4">
          <h2 className="flex-1 type-title-sm text-fg">Comments</h2>
          <IconButton label="Close comments" icon={<X />} size="sm" variant="ghost" onClick={onClose} />
        </div>
      ) : null}
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
        {canComment ? (
          <div className="flex flex-col gap-2">
            {pendingQuote ? (
              <div className="flex items-start gap-2 rounded-sm bg-sunken px-2 py-1">
                <p className="flex-1 border-l-2 border-border-strong pl-2 type-caption text-fg-muted">{pendingQuote}</p>
                <IconButton label="Remove the quote" icon={<X />} size="sm" variant="ghost" onClick={onClearQuote} />
              </div>
            ) : null}
            <Composer placeholder={`Comment in ${platform}`} busy={add.isPending && !replying} onSend={(t) => send(t)} autoFocus={!!pendingQuote} />
          </div>
        ) : null}
        {list.isPending ? (
          <div className="flex flex-col gap-3">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : list.isError ? (
          <ErrorState title={`Couldn't load comments from ${platform}.`} onRetry={() => void list.refetch()} />
        ) : list.data.length === 0 ? (
          <EmptyState icon={<MessageSquare />} title="No comments yet" />
        ) : (
          <ul className="flex flex-col gap-4">
            {list.data.map((t) => (
              <li key={t.id} className="flex flex-col gap-3 rounded-md border border-border p-3">
                <Comment c={t} />
                {t.replies.map((r) => (
                  <div key={r.id} className="pl-6">
                    <Comment c={r} />
                  </div>
                ))}
                {canComment ? (
                  replying === t.thread_id ? (
                    <div className="pl-6">
                      <Composer placeholder="Reply" busy={add.isPending} onSend={(text) => send(text, t.thread_id)} autoFocus />
                    </div>
                  ) : (
                    <Button variant="ghost" size="sm" className="self-start" onClick={() => setReplying(t.thread_id)}>
                      Reply
                    </Button>
                  )
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
