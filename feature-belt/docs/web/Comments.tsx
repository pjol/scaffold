"use client";

import {
  docsPlain,
  docsTimeAgo,
  errorMessage,
  useAccessDirectory,
  useAuthSession,
  useDocsComments,
  useDocsCreateComment,
  useDocsDeleteComment,
  useDocsEditComment,
  useDocsResolveComment,
  useMe,
  type DocsComment,
  type DocsRefs,
  type DocsSpan,
} from "@scaffoldapp/shared";
import { Avatar, Button, EmptyState, ErrorState, IconButton, Menu, MenuItem, SegmentedControl, SkeletonRow, TextArea, cn, toast } from "@scaffoldapp/ui-web";
import { CheckCircle2, MessageSquare, MoreHorizontal, Pencil, RotateCcw, Trash2, Flag, X } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { ReportDialog } from "@/features/moderation";
import { mentionLabel } from "./editor/dom";

export interface CommentTarget {
  blockId: string;
  start?: number;
  end?: number;
  quote?: string;
}

/** Comment text → spans, with the people picked from "@" as mentions. */
function toSpans(text: string, people: { id: string; name: string }[]): DocsSpan[] {
  const out: DocsSpan[] = [];
  let rest = text;
  while (rest) {
    let best: { at: number; p: { id: string; name: string } } | null = null;
    for (const p of people) {
      const at = rest.indexOf(`@${p.name}`);
      if (at >= 0 && (!best || at < best.at)) best = { at, p };
    }
    if (!best) {
      out.push({ text: rest });
      break;
    }
    if (best.at > 0) out.push({ text: rest.slice(0, best.at) });
    out.push({ text: `@${best.p.name}`, mention: { type: "user", id: best.p.id } });
    rest = rest.slice(best.at + best.p.name.length + 1);
  }
  return out;
}

function spansToText(spans: DocsSpan[]): { text: string; people: { id: string; name: string }[] } {
  const people: { id: string; name: string }[] = [];
  let text = "";
  for (const s of spans) {
    if (s.mention?.type === "user") people.push({ id: s.mention.id, name: s.text.replace(/^@/, "") });
    text += s.text;
  }
  return { text, people };
}

/** A textarea with "@" to mention people in the org. */
function Composer({
  orgId,
  placeholder,
  initial,
  submitLabel,
  autoFocus,
  busy,
  onSubmit,
  onCancel,
}: {
  orgId: string;
  placeholder: string;
  initial?: DocsSpan[];
  submitLabel: string;
  autoFocus?: boolean;
  busy?: boolean;
  onSubmit: (body: DocsSpan[]) => Promise<unknown> | void;
  onCancel?: () => void;
}) {
  const start = useMemo(() => spansToText(initial ?? []), [initial]);
  const [text, setText] = useState(start.text);
  const [people, setPeople] = useState(start.people);
  const [query, setQuery] = useState<{ at: number; q: string } | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const dir = useAccessDirectory(orgId, query?.q ?? "", { types: "user", limit: 5, enabled: !!query });
  const pick = (p: { id: string; name: string }) => {
    if (!query) return;
    const before = text.slice(0, query.at);
    const after = text.slice(query.at + 1 + query.q.length);
    const next = `${before}@${p.name} ${after}`;
    setText(next);
    setPeople((list) => (list.some((x) => x.id === p.id) ? list : [...list, p]));
    setQuery(null);
    requestAnimationFrame(() => {
      const pos = before.length + p.name.length + 2;
      ref.current?.focus();
      ref.current?.setSelectionRange(pos, pos);
    });
  };
  const submit = async () => {
    if (!text.trim() || busy) return;
    await onSubmit(toSpans(text.trim(), people));
    setText("");
    setPeople([]);
  };
  return (
    <div className="relative flex flex-col gap-2">
      <TextArea
        ref={ref}
        value={text}
        rows={2}
        maxLength={4000}
        placeholder={placeholder}
        aria-label={placeholder}
        autoFocus={autoFocus}
        onChange={(e) => {
          const v = e.target.value;
          setText(v);
          const caret = e.target.selectionStart;
          const m = /(^|\s)@([^\s@]{0,30})$/.exec(v.slice(0, caret));
          setQuery(m ? { at: caret - m[2]!.length - 1, q: m[2]! } : null);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            void submit();
          } else if (e.key === "Escape" && query) {
            e.preventDefault();
            setQuery(null);
          } else if (e.key === "Enter" && query && dir.data?.[0]) {
            e.preventDefault();
            pick({ id: dir.data[0].id, name: dir.data[0].name });
          }
        }}
      />
      {query && (dir.data?.length ?? 0) > 0 ? (
        <div role="listbox" aria-label="Mention someone" className="flex flex-col rounded-md border border-border bg-surface-raised p-1 shadow-md">
          {dir.data!.map((p) => (
            <button key={p.id} type="button" role="option" aria-selected={false} className="flex min-h-control items-center gap-2 rounded-sm px-2 text-left type-label hover:bg-sunken" onMouseDown={(e) => e.preventDefault()} onClick={() => pick({ id: p.id, name: p.name })}>
              <Avatar size="xs" name={p.name} {...(p.avatar_url ? { src: p.avatar_url } : {})} />
              {p.name}
            </button>
          ))}
        </div>
      ) : null}
      <div className="flex justify-end gap-2">
        {onCancel ? (
          <Button variant="ghost" size="sm" onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
        <Button size="sm" loading={busy} disabled={!text.trim()} onClick={() => void submit()}>
          {submitLabel}
        </Button>
      </div>
    </div>
  );
}

function Body({ spans, refs }: { spans: DocsSpan[]; refs: DocsRefs }) {
  return (
    <p className="whitespace-pre-wrap break-words type-body text-fg [overflow-wrap:anywhere]">
      {spans.map((s, i) =>
        s.mention ? (
          <span key={i} className="rounded-xs bg-primary-soft px-1 text-primary-strong">
            {mentionLabel(s, refs)}
          </span>
        ) : (
          <span key={i}>{s.text}</span>
        ),
      )}
    </p>
  );
}

function CommentItem({ c, pageId, orgId, canManage, refs }: { c: DocsComment; pageId: string; orgId: string; canManage: boolean; refs: DocsRefs }) {
  const me = useMe();
  const [editing, setEditing] = useState(false);
  const [report, setReport] = useState(false);
  const edit = useDocsEditComment(pageId);
  const del = useDocsDeleteComment(pageId);
  const mine = !!c.author && c.author.id === me.data?.id;
  if (c.deleted) return <p className="type-label italic text-fg-subtle">Comment deleted</p>;
  return (
    <div className="flex gap-2">
      <Avatar size="sm" name={c.author?.name ?? "Deleted user"} {...(c.author?.avatar_url ? { src: c.author.avatar_url } : {})} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-center gap-2">
          <span className="truncate type-label font-semibold text-fg">{c.author?.name ?? "Deleted user"}</span>
          <time dateTime={c.created_at} title={new Date(c.created_at).toLocaleString()} className="shrink-0 type-caption text-fg-muted">
            {docsTimeAgo(c.created_at)}
            {c.edited_at ? " · edited" : ""}
          </time>
          <span className="ml-auto">
            <Menu trigger={<IconButton label="Comment actions" icon={<MoreHorizontal />} size="sm" variant="ghost" />}>
              {mine ? (
                <MenuItem icon={<Pencil />} onSelect={() => setEditing(true)}>
                  Edit
                </MenuItem>
              ) : null}
              {!mine ? (
                <MenuItem icon={<Flag />} onSelect={() => setReport(true)}>
                  Report
                </MenuItem>
              ) : null}
              {mine || canManage ? (
                <MenuItem icon={<Trash2 />} destructive onSelect={() => del.mutate(c.id, { onError: (e) => toast.error(errorMessage(e, "Couldn't delete the comment.")) })}>
                  Delete
                </MenuItem>
              ) : null}
            </Menu>
          </span>
        </div>
        {editing ? (
          <Composer
            orgId={orgId}
            placeholder="Edit comment"
            initial={c.body}
            submitLabel="Save"
            autoFocus
            busy={edit.isPending}
            onCancel={() => setEditing(false)}
            onSubmit={(body) =>
              edit.mutateAsync({ id: c.id, body }).then(
                () => setEditing(false),
                (e) => toast.error(errorMessage(e, "Couldn't save the comment.")),
              )
            }
          />
        ) : (
          <Body spans={c.body} refs={refs} />
        )}
      </div>
      <ReportDialog open={report} onOpenChange={setReport} targetKind="docs.comment" targetId={c.id} noun="comment" author={c.author ? { id: c.author.id, name: c.author.name } : null} />
    </div>
  );
}

function Thread({ t, pageId, orgId, canComment, canManage, active, refs, onFocus }: { t: DocsComment; pageId: string; orgId: string; canComment: boolean; canManage: boolean; active: boolean; refs: DocsRefs; onFocus: () => void }) {
  const reply = useDocsCreateComment(pageId);
  const resolve = useDocsResolveComment(pageId);
  const [replying, setReplying] = useState(false);
  return (
    <article
      id={`thread-${t.id}`}
      className={cn("flex flex-col gap-3 rounded-md border bg-surface p-3", active ? "border-warning" : "border-border")}
      onClick={onFocus}
    >
      {t.quote ? <p className="line-clamp-2 rounded-sm bg-warning-soft px-2 py-1 type-label text-fg">{t.quote}</p> : null}
      <CommentItem c={t} pageId={pageId} orgId={orgId} canManage={canManage} refs={refs} />
      {t.replies.map((r) => (
        <CommentItem key={r.id} c={r} pageId={pageId} orgId={orgId} canManage={canManage} refs={refs} />
      ))}
      <div className="flex flex-wrap items-center gap-2">
        {canComment && !t.resolved_at && !replying ? (
          <Button variant="ghost" size="sm" onClick={() => setReplying(true)}>
            Reply
          </Button>
        ) : null}
        {canComment ? (
          <Button
            variant="ghost"
            size="sm"
            leftIcon={t.resolved_at ? <RotateCcw /> : <CheckCircle2 />}
            loading={resolve.isPending}
            onClick={() => resolve.mutate({ id: t.id, resolved: !t.resolved_at }, { onError: (e) => toast.error(errorMessage(e, "Couldn't update the thread.")) })}
          >
            {t.resolved_at ? "Reopen" : "Resolve"}
          </Button>
        ) : null}
        {t.resolved_at && t.resolved_by ? <span className="type-caption text-fg-muted">Resolved by {t.resolved_by.name}</span> : null}
      </div>
      {replying ? (
        <Composer
          orgId={orgId}
          placeholder="Reply"
          submitLabel="Reply"
          autoFocus
          busy={reply.isPending}
          onCancel={() => setReplying(false)}
          onSubmit={(body) =>
            reply.mutateAsync({ thread_id: t.id, body }).then(
              () => setReplying(false),
              (e) => toast.error(errorMessage(e, "Couldn't send the reply.")),
            )
          }
        />
      ) : null}
    </article>
  );
}

/** Threads on the page and its blocks: new, reply, resolve, reopen, edit, delete, report. */
export function CommentsPanel({
  pageId,
  orgId,
  canComment,
  canManage,
  refs,
  blockFilter,
  activeThread,
  pending,
  onClearFilter,
  onClearPending,
  onActivate,
  onClose,
  header = true,
}: {
  pageId: string;
  orgId: string;
  canComment: boolean;
  canManage: boolean;
  refs: DocsRefs;
  blockFilter: string | null;
  activeThread: string | null;
  pending: CommentTarget | null;
  onClearFilter: () => void;
  onClearPending: () => void;
  onActivate: (threadId: string, blockId: string | null) => void;
  onClose: () => void;
  /** Off inside a sheet, which has its own title and close button. */
  header?: boolean;
}) {
  const [status, setStatus] = useState<"open" | "resolved">("open");
  const threads = useDocsComments(pageId, status);
  const create = useDocsCreateComment(pageId);
  const auth = useAuthSession();
  const list = (threads.data ?? []).filter((t) => !blockFilter || t.block_id === blockFilter);
  return (
    <div className="flex h-full min-h-0 flex-col">
      {header ? (
        <div className="flex items-center gap-2 border-b border-border px-4 py-3">
          <h2 className="flex-1 type-title-sm text-fg">Comments</h2>
          <IconButton label="Close comments" icon={<X />} size="sm" variant="ghost" onClick={onClose} />
        </div>
      ) : null}
      <div className={cn("flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto", header ? "p-4" : "")}>
        <SegmentedControl<"open" | "resolved">
          value={status}
          onValueChange={setStatus}
          aria-label="Show"
          options={[
            { value: "open", label: "Open" },
            { value: "resolved", label: "Resolved" },
          ]}
        />
        {blockFilter ? (
          <div className="flex items-center gap-2 type-label text-fg-muted">
            Comments on one block.
            <Button variant="ghost" size="sm" onClick={onClearFilter}>
              Show all
            </Button>
          </div>
        ) : null}
        {canComment && auth.isSignedIn ? (
          <div className="flex flex-col gap-2 rounded-md border border-border p-3">
            {pending?.quote ? <p className="line-clamp-2 rounded-sm bg-warning-soft px-2 py-1 type-label text-fg">{pending.quote}</p> : null}
            {pending && !pending.quote ? <p className="type-label text-fg-muted">On the selected block</p> : null}
            <Composer
              key={pending ? `${pending.blockId}-${pending.start ?? ""}` : "page"}
              orgId={orgId}
              placeholder={pending ? "Comment on the selection" : "Add a comment"}
              submitLabel="Comment"
              autoFocus={!!pending}
              busy={create.isPending}
              {...(pending ? { onCancel: onClearPending } : {})}
              onSubmit={(body) =>
                create
                  .mutateAsync({ body, ...(pending ? { block_id: pending.blockId } : {}), ...(pending?.start !== undefined && pending.end !== undefined ? { anchor_start: pending.start, anchor_end: pending.end, quote: pending.quote } : {}) })
                  .then(
                    () => {
                      onClearPending();
                      setStatus("open");
                    },
                    (e) => toast.error(errorMessage(e, "Couldn't post the comment.")),
                  )
              }
            />
          </div>
        ) : null}
        {threads.isPending ? (
          <div className="flex flex-col gap-3">
            <SkeletonRow />
            <SkeletonRow />
          </div>
        ) : threads.isError ? (
          <ErrorState title="Couldn't load comments." onRetry={() => void threads.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState icon={<MessageSquare />} title={status === "open" ? "No open comments" : "No resolved comments"} />
        ) : (
          list.map((t) => (
            <Thread key={t.id} t={t} pageId={pageId} orgId={orgId} canComment={canComment} canManage={canManage} active={t.id === activeThread} refs={refs} onFocus={() => onActivate(t.id, t.block_id)} />
          ))
        )}
      </div>
    </div>
  );
}

export function commentPlain(c: DocsComment): string {
  return docsPlain(c.body);
}
