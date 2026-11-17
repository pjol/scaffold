"use client";

import { docsTitle, useDocsPage, useDocsPageSession, useDocsUploadFile, type DocsBlock } from "@scaffoldapp/shared";
import { Button, Skeleton, toast } from "@scaffoldapp/ui-web";
import { ArrowUpRight, Copy, Lock, RefreshCw } from "lucide-react";
import { useEffect, useRef } from "react";
import { BlockChildren } from "./BlockTree";
import { useEditor, type EditorSelection } from "./context";
import { Editor } from "./Editor";
import { newBlock, positionAtEnd, toDrafts } from "./ops";

/** What "Copy and sync" puts on the clipboard; pasting it on a page makes a synced copy (the server checks access). */
export const SYNCED_PASTE_RE = /^\[Synced block\]\(#synced-([0-9a-f-]{36})\)$/;

/** A synced block: the original holds blocks (edited here); a copy shows its original's blocks, edited live. */
export function SyncedBlock({ block }: { block: DocsBlock }) {
  return block.props.source_id ? <SyncedCopy block={block} /> : <SyncedOriginal block={block} />;
}

export function copyAndSync(sourceId: string) {
  void navigator.clipboard?.writeText(`[Synced block](#synced-${sourceId})`).then(
    () => toast.success("Copied. Paste it on any page to show the same blocks there."),
    () => toast.error("Couldn't copy. Try again."),
  );
}

function SyncedOriginal({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const kids = ctx.snap.children.get(block.id) ?? [];
  const editable = ctx.editable && !ctx.isReadOnly(block.id);
  return (
    <div className="group/synced relative rounded-md p-1 ring-1 ring-accent/50 hover:ring-2">
      <div className="absolute -top-3 right-2 z-10 flex items-center gap-1 rounded-sm bg-surface-raised px-1 opacity-100 shadow-xs sm:opacity-0 sm:group-hover/synced:opacity-100 sm:group-focus-within/synced:opacity-100">
        <span className="flex items-center gap-1 px-1 type-caption text-accent-strong">
          <RefreshCw aria-hidden className="size-3" />
          Synced block
        </span>
        <Button size="sm" variant="ghost" leftIcon={<Copy />} onClick={() => copyAndSync(block.id)}>
          Copy and sync
        </Button>
      </div>
      <BlockChildren parentId={block.id} depth={1} />
      {kids.length === 0 && editable ? (
        <button
          type="button"
          className="flex min-h-10 w-full items-center rounded-sm px-1 text-left type-body text-fg-subtle hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
          onClick={() => {
            const p = newBlock("paragraph", block.id, positionAtEnd(ctx.snap, block.id));
            ctx.apply([p], { block: p.id, start: 0, end: 0 });
            ctx.focusBlock(p.id, 0);
          }}
        >
          Write what you want to show on other pages
        </button>
      ) : null}
    </div>
  );
}

/** A copy: opens the original's page with the viewer's own access; no access shows nothing of it. */
function SyncedCopy({ block }: { block: DocsBlock }) {
  const outer = useEditor();
  const sourcePage = block.props.source_page_id ?? null;
  const sourceId = block.props.source_id!;
  const detail = useDocsPage(sourcePage, { retry: false });
  const { session, snapshot } = useDocsPageSession<EditorSelection>(detail.data ? sourcePage : null, detail.data, (m) => toast.error(m));
  const upload = useDocsUploadFile(sourcePage ?? "");
  const rowRef = useRef<HTMLDivElement>(null);

  // "Unsync" from the block menu: the copy becomes ordinary blocks on this page.
  useEffect(() => {
    const el = rowRef.current?.closest<HTMLElement>("[data-block-id]");
    if (!el) return undefined;
    const onUnsync = () => {
      if (!session) return;
      const snap = session.getSnapshot();
      const kids = snap.children.get(sourceId) ?? [];
      outer.insertAfter(block, toDrafts(snap, kids.map((k) => k.id)));
      outer.apply([{ op: "delete", id: block.id }]);
    };
    el.addEventListener("docs:unsync", onUnsync);
    return () => el.removeEventListener("docs:unsync", onUnsync);
  }, [session, outer, block, sourceId]);

  if (!sourcePage) return null;
  if (detail.isPending) return <Skeleton className="h-16 w-full rounded-md" />;
  if (detail.isError) {
    return (
      <div ref={rowRef} className="flex items-center gap-2 rounded-md border border-dashed border-border px-3 py-3 type-label text-fg-muted">
        <Lock aria-hidden className="size-4 shrink-0" />
        {detail.error.status === 404 || detail.error.status === 403 ? "You don't have access to the original of this synced block." : "Couldn't load this synced block."}
      </div>
    );
  }
  const d = detail.data;
  const gone = session && snapshot.blocks.length > 0 ? !snapshot.byId.has(sourceId) : !d.blocks.some((b) => b.id === sourceId);
  if (gone) {
    return (
      <div ref={rowRef} className="rounded-md border border-dashed border-border px-3 py-3 type-label text-fg-muted">
        The original of this synced block was deleted.
      </div>
    );
  }
  const canEdit = outer.editable && (d.page.my_level === "edit" || d.page.my_level === "manage") && !d.page.locked && !d.page.deleted_at;
  return (
    <div ref={rowRef} className="group/synced relative rounded-md p-1 ring-1 ring-accent/50 hover:ring-2">
      <div className="absolute -top-3 right-2 z-10 flex items-center gap-1 rounded-sm bg-surface-raised px-1 opacity-100 shadow-xs sm:opacity-0 sm:group-hover/synced:opacity-100 sm:group-focus-within/synced:opacity-100">
        <button type="button" className="flex items-center gap-1 rounded-sm px-1 py-0.5 type-caption text-accent-strong hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring" onClick={() => outer.openPage(d.page.id)}>
          <RefreshCw aria-hidden className="size-3" />
          Synced from {docsTitle(d.page.title)}
          <ArrowUpRight aria-hidden className="size-3" />
        </button>
      </div>
      {session ? (
        <Editor
          pageId={d.page.id}
          orgId={d.page.org_id}
          root={sourceId}
          session={session}
          snap={snapshot}
          refs={d.refs}
          editable={canEdit}
          canComment={false}
          viewers={[]}
          commentCounts={{}}
          threads={[]}
          activeThread={null}
          onFocusBlock={() => {}}
          onOpenComments={() => {}}
          onComment={() => {}}
          onOpenPage={outer.nested.onOpenPage}
          onCreateSubpage={() => Promise.resolve(null)}
          onCreateDatabase={() => Promise.resolve(null)}
          onUpload={(file) => upload.mutateAsync({ file, name: file.name })}
          onMoveToPage={outer.nested.onMoveToPage}
        />
      ) : (
        <Skeleton className="h-16 w-full rounded-md" />
      )}
    </div>
  );
}
