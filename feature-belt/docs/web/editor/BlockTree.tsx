"use client";

import { docsListNumbers, type DocsBlock, type DocsTextBlockType } from "@scaffoldapp/shared";
import { Avatar, cn } from "@scaffoldapp/ui-web";
import { Check, ChevronRight, GripVertical, MessageSquare, MoreVertical, Plus } from "lucide-react";
import { memo, useCallback, useState } from "react";
import { EmojiPicker } from "../EmojiPicker";
import { Floating } from "../Floating";
import { BlockBody } from "./blocks";
import { useEditor } from "./context";
import { RichText } from "./RichText";

const TEXT_CLASS: Record<DocsTextBlockType, string> = {
  paragraph: "type-body",
  heading_1: "type-headline",
  heading_2: "type-title",
  heading_3: "type-title-sm",
  bulleted_list: "type-body",
  numbered_list: "type-body",
  to_do: "type-body",
  toggle: "type-body",
  quote: "type-body-lg",
  callout: "type-body",
};

const PLACEHOLDER: Record<DocsTextBlockType, string> = {
  paragraph: "Write, or press / for commands",
  heading_1: "Heading 1",
  heading_2: "Heading 2",
  heading_3: "Heading 3",
  bulleted_list: "List",
  numbered_list: "List",
  to_do: "To-do",
  toggle: "Toggle",
  quote: "Quote",
  callout: "Callout",
};

/** Space above a block: headings get room above (they belong to what follows). */
const ROW_SPACE: Partial<Record<DocsBlock["type"], string>> = {
  heading_1: "pt-6",
  heading_2: "pt-5",
  heading_3: "pt-3",
  divider: "py-2",
  callout: "py-1",
  code: "py-1",
  table: "py-1",
  image: "py-2",
  database: "py-3",
};

export function BlockChildren({ parentId, depth }: { parentId: string | null; depth: number }) {
  const { snap } = useEditor();
  const kids = snap.children.get(parentId ?? "") ?? [];
  const numbers = docsListNumbers(kids);
  return (
    <>
      {kids.map((b) => (
        <BlockRow key={b.id} block={b} depth={depth} number={numbers.get(b.id)} />
      ))}
    </>
  );
}

function DropLine({ inset }: { inset?: boolean }) {
  return <div aria-hidden className={cn("pointer-events-none absolute right-0 z-10 h-0.5 rounded-pill bg-primary", inset ? "left-8" : "left-0")} />;
}

const BlockRow = memo(function BlockRow({ block: b, depth, number }: { block: DocsBlock; depth: number; number: number | undefined }) {
  const ctx = useEditor();
  const selected = ctx.selected.has(b.id);
  const dragging = ctx.dragging.has(b.id);
  const viewers = ctx.viewersByBlock.get(b.id);
  const comments = ctx.commentCounts[b.id] ?? 0;
  const drop = ctx.dropTarget?.id === b.id ? ctx.dropTarget.place : null;
  const open = b.type === "toggle" ? ctx.isOpen(b.id) : true;
  const hasKids = (ctx.snap.children.get(b.id)?.length ?? 0) > 0;
  const nestable = ["paragraph", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"].includes(b.type);
  const rowRef = useCallback((el: HTMLDivElement | null) => ctx.registerRow(b.id, el), [ctx, b.id]);

  return (
    <div ref={rowRef} data-block-id={b.id} className={cn("relative", ROW_SPACE[b.type], dragging && "opacity-40")}>
      {drop === "before" ? <div className="relative -top-0.5"><DropLine /></div> : null}
      <div
        className={cn(
          "group/block relative flex items-start gap-1 rounded-sm transition-colors motion-reduce:transition-none",
          selected && "bg-primary-soft",
          viewers?.length && !selected && "ring-1 ring-accent/40",
        )}
        onMouseDown={(e) => ctx.onRowMouseDown(b, e)}
      >
        {ctx.editable ? <Gutter block={b} /> : null}
        <div className="min-w-0 flex-1">
          <BlockContent block={b} number={number} open={open} hasKids={hasKids} />
        </div>
        {ctx.editable ? (
          <span className="sm:hidden">
            <button
              type="button"
              aria-label="Block actions"
              className="hidden size-8 shrink-0 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring group-focus-within/block:flex pointer-coarse:size-11 [&_svg]:size-4"
              onMouseDown={(e) => e.preventDefault()}
              onClick={(e) => e.currentTarget.dispatchEvent(new CustomEvent("docs:block-menu", { bubbles: true, detail: b.id }))}
            >
              <MoreVertical aria-hidden />
            </button>
          </span>
        ) : null}
        {viewers?.length || comments ? (
          <div className="flex shrink-0 items-center gap-1 self-start pt-1">
            {viewers?.slice(0, 2).map((v) => (
              <span key={v.session_id} title={`${v.user.name} is editing`} className="flex items-center">
                <Avatar size="xs" name={v.user.name} {...(v.user.avatar_url ? { src: v.user.avatar_url } : {})} />
                <span className="sr-only">{v.user.name} is editing this block</span>
              </span>
            ))}
            {comments ? (
              <button
                type="button"
                className="flex min-h-6 items-center gap-1 rounded-sm px-1 type-caption text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap"
                aria-label={`${comments} ${comments === 1 ? "comment" : "comments"} on this block`}
                onClick={() => ctx.openComments(b.id)}
              >
                <MessageSquare aria-hidden className="size-4" />
                {comments}
              </button>
            ) : null}
          </div>
        ) : null}
      </div>
      {drop === "inside" ? <div className="relative"><DropLine inset /></div> : null}
      {nestable && hasKids && open ? (
        <div className="pl-6">
          <BlockChildren parentId={b.id} depth={depth + 1} />
        </div>
      ) : null}
      {drop === "after" ? <div className="relative"><DropLine /></div> : null}
    </div>
  );
});

function Gutter({ block: b }: { block: DocsBlock }) {
  const ctx = useEditor();
  return (
    <div
      className={cn(
        "absolute top-0 -left-14 hidden h-8 items-center gap-0.5 opacity-0 transition-opacity group-hover/block:opacity-100 group-focus-within/block:opacity-100 motion-reduce:transition-none sm:flex",
        ROW_SPACE[b.type] === "pt-6" && "top-6",
        ROW_SPACE[b.type] === "pt-5" && "top-5",
        ROW_SPACE[b.type] === "pt-3" && "top-3",
      )}
    >
      <button
        type="button"
        aria-label="Add a block below"
        title="Add a block below"
        className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4"
        onClick={() => ctx.onEnter(b, false)}
        onMouseDown={(e) => e.preventDefault()}
      >
        <Plus aria-hidden />
      </button>
      <button
        type="button"
        aria-label="Drag to move, or open block actions"
        title="Drag to move · Click for actions"
        data-block-handle={b.id}
        className="flex size-6 cursor-grab touch-none items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring active:cursor-grabbing [&_svg]:size-4"
        onPointerDown={(e) => ctx.startDrag(b, e)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            (e.currentTarget as HTMLElement).dispatchEvent(new CustomEvent("docs:block-menu", { bubbles: true, detail: b.id }));
          }
        }}
      >
        <GripVertical aria-hidden />
      </button>
    </div>
  );
}

function BlockContent({ block: b, number, open, hasKids }: { block: DocsBlock; number: number | undefined; open: boolean; hasKids: boolean }) {
  const ctx = useEditor();
  const [iconOpen, setIconOpen] = useState<HTMLElement | null>(null);
  if (!["paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list", "numbered_list", "to_do", "toggle", "quote", "callout"].includes(b.type)) {
    return <BlockBody block={b} />;
  }
  const type = b.type as DocsTextBlockType;
  const checked = type === "to_do" && !!b.props.checked;
  const text = (
    <RichText
      ref={(h) => ctx.register(b.id, h)}
      spans={b.props.text ?? []}
      refs={ctx.refs}
      editable={ctx.editable}
      placeholder={ctx.editable ? PLACEHOLDER[type] : undefined}
      placeholderWhen={type.startsWith("heading") ? "always" : "focus"}
      decorations={ctx.decorations.get(b.id)}
      ariaLabel={type === "paragraph" ? "Text" : PLACEHOLDER[type]}
      className={cn(TEXT_CLASS[type], "py-1", checked && "text-fg-muted line-through")}
      onChange={(spans, caret) => ctx.onTextChange(b, spans, caret)}
      onKeyDown={(e) => ctx.onTextKeyDown(b, e)}
      onEnter={(shift) => ctx.onEnter(b, shift)}
      onPaste={(e) => ctx.onPaste(b, e)}
      onFocus={() => ctx.onFocusBlock(b)}
      onSelectionChange={(c) => ctx.onSelectionChange(b, c)}
      onOpenPage={ctx.openPage}
      onDecorationClick={(id) => ctx.openComments(b.id, id)}
    />
  );
  switch (type) {
    case "bulleted_list":
      return (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 w-6 shrink-0 items-center justify-center">
            <span className="size-1.5 rounded-pill bg-fg" />
          </span>
          {text}
        </div>
      );
    case "numbered_list":
      return (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 min-w-6 shrink-0 items-center justify-end pr-1.5 type-body tabular-nums text-fg">
            {number ?? 1}.
          </span>
          {text}
        </div>
      );
    case "to_do":
      return (
        <div className="flex items-start">
          <span className="flex h-8 w-6 shrink-0 items-center justify-center">
            <button
              type="button"
              role="checkbox"
              aria-checked={checked}
              aria-label={checked ? "Mark as not done" : "Mark as done"}
              disabled={!ctx.editable}
              className={cn(
                "relative flex size-4 items-center justify-center rounded-xs border transition-colors before:absolute before:-inset-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring motion-reduce:transition-none [&_svg]:size-3",
                checked ? "border-primary bg-primary text-primary-fg" : "border-border-strong bg-surface hover:bg-sunken",
              )}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => ctx.apply([{ op: "update", id: b.id, props: { checked: !checked } }])}
            >
              {checked ? <Check aria-hidden strokeWidth={3} /> : null}
            </button>
          </span>
          {text}
        </div>
      );
    case "toggle":
      return (
        <div className="flex items-start">
          <button
            type="button"
            aria-expanded={open}
            aria-label={open ? "Collapse" : "Expand"}
            className="flex h-8 w-6 shrink-0 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => ctx.toggleOpen(b.id)}
          >
            <ChevronRight aria-hidden className={cn("transition-transform motion-reduce:transition-none", open && "rotate-90", !hasKids && "opacity-60")} />
          </button>
          {text}
        </div>
      );
    case "quote":
      return <div className="border-l-2 border-border-strong pl-4">{text}</div>;
    case "callout":
      return (
        <div className="flex items-start gap-2 rounded-md bg-sunken px-3 py-2">
          <button
            type="button"
            aria-label="Change icon"
            disabled={!ctx.editable}
            className="flex size-8 shrink-0 items-center justify-center rounded-sm type-body-lg hover:bg-surface focus-visible:outline-2 focus-visible:outline-ring"
            onClick={(e) => setIconOpen(e.currentTarget)}
          >
            {b.props.icon ?? "💡"}
          </button>
          {text}
          <Floating open={!!iconOpen} anchor={iconOpen} onClose={() => setIconOpen(null)} autoFocus label="Choose an icon">
            <EmojiPicker
              onPick={(emoji) => {
                setIconOpen(null);
                ctx.apply([{ op: "update", id: b.id, props: { icon: emoji } }]);
              }}
            />
          </Floating>
        </div>
      );
    default:
      return text;
  }
}
