"use client";

import { docsFolds, docsHoldsChildren, docsIsTextBlock, docsListNumbers, type DocsBlock, type DocsTextBlockType } from "@scaffoldapp/shared";
import { Avatar, cn } from "@scaffoldapp/ui-web";
import { Check, ChevronRight, GripVertical, MessageSquare, MoreVertical, Plus } from "lucide-react";
import { memo, useCallback, useState } from "react";
import { EmojiPicker } from "../EmojiPicker";
import { Floating } from "../Floating";
import { BlockBody } from "./blocks";
import { colorClass } from "./colors";
import { useEditor } from "./context";
import { RichText } from "./RichText";

const TEXT_CLASS: Record<DocsTextBlockType, string> = {
  paragraph: "type-body",
  heading_1: "type-headline",
  heading_2: "type-title",
  heading_3: "type-title-sm",
  heading_4: "type-body-lg font-semibold",
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
  heading_4: "Heading 4",
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
  heading_4: "pt-2",
  divider: "py-2",
  callout: "py-1",
  code: "py-1",
  table: "py-1",
  image: "py-2",
  video: "py-2",
  embed: "py-2",
  pdf: "py-2",
  equation: "py-1",
  column_list: "py-1",
  database: "py-3",
};

/** Blocks whose body draws their own children (no indent under the row). */
const OWN_CHILDREN = new Set<DocsBlock["type"]>(["column_list", "column", "synced_block"]);

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

function DropLine({ inset, vertical, side }: { inset?: boolean; vertical?: boolean; side?: "left" | "right" }) {
  if (vertical) return <div aria-hidden className={cn("pointer-events-none absolute top-0 bottom-0 z-10 w-0.5 rounded-pill bg-primary", side === "left" ? "-left-2" : "-right-2")} />;
  return <div aria-hidden className={cn("pointer-events-none absolute right-0 z-10 h-0.5 rounded-pill bg-primary", inset ? "left-8" : "left-0")} />;
}

export const BlockRow = memo(function BlockRow({ block: b, depth, number }: { block: DocsBlock; depth: number; number: number | undefined }) {
  const ctx = useEditor();
  const selected = ctx.selected.has(b.id);
  const dragging = ctx.dragging.has(b.id);
  const viewers = ctx.viewersByBlock.get(b.id);
  const comments = ctx.commentCounts[b.id] ?? 0;
  const drop = ctx.dropTarget?.id === b.id ? ctx.dropTarget.place : null;
  const folds = docsFolds(b.type, b.props);
  const open = folds ? ctx.isOpen(b.id) : true;
  const hasKids = (ctx.snap.children.get(b.id)?.length ?? 0) > 0;
  const nestable = docsHoldsChildren(b.type, b.props) && !OWN_CHILDREN.has(b.type);
  const editable = ctx.editable && !ctx.isReadOnly(b.id);
  const rowRef = useCallback((el: HTMLDivElement | null) => ctx.registerRow(b.id, el), [ctx, b.id]);
  const color = b.props.color;
  const bg = color?.endsWith("_background");

  return (
    <div ref={rowRef} data-block-id={b.id} className={cn("relative scroll-mt-24 transition-shadow motion-reduce:transition-none", ROW_SPACE[b.type], dragging && "opacity-40")}>
      {drop === "before" ? (
        <div className="relative -top-0.5">
          <DropLine />
        </div>
      ) : null}
      <div
        className={cn(
          "group/block relative flex items-start gap-1 rounded-sm transition-colors motion-reduce:transition-none",
          selected && "bg-primary-soft",
          viewers?.length && !selected && "ring-1 ring-accent/40",
        )}
        onMouseDown={(e) => ctx.onRowMouseDown(b, e)}
      >
        {drop === "left" || drop === "right" ? <DropLine vertical side={drop} /> : null}
        {editable ? <Gutter block={b} /> : null}
        <div className={cn("min-w-0 flex-1", color && b.type !== "callout" && colorClass(color), bg && b.type !== "callout" && "px-1")}>
          <BlockContent block={b} number={number} open={open} hasKids={hasKids} editable={editable} />
        </div>
        {ctx.editable && !editable ? (
          <span className="flex shrink-0 items-center self-start pt-1 opacity-100 transition-opacity sm:opacity-0 sm:group-hover/block:opacity-100 sm:group-focus-within/block:opacity-100 motion-reduce:transition-none">
            <button
              type="button"
              className="rounded-sm px-1.5 py-0.5 type-caption text-fg-muted hover:bg-sunken hover:text-fg focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-tap"
              onClick={(e) => e.currentTarget.dispatchEvent(new CustomEvent("docs:block-menu", { bubbles: true, detail: b.id }))}
            >
              Read-only
            </button>
          </span>
        ) : null}
        {editable && b.type !== "column_list" ? (
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
      {drop === "inside" ? (
        <div className="relative">
          <DropLine inset />
        </div>
      ) : null}
      {nestable && hasKids && open ? (
        <div className="pl-6">
          <BlockChildren parentId={b.id} depth={depth + 1} />
        </div>
      ) : null}
      {drop === "after" ? (
        <div className="relative">
          <DropLine />
        </div>
      ) : null}
    </div>
  );
});

function Gutter({ block: b }: { block: DocsBlock }) {
  const ctx = useEditor();
  const top = ROW_SPACE[b.type];
  return (
    <div
      className={cn(
        "absolute top-0 -left-14 z-10 hidden h-8 items-center gap-0.5 opacity-0 transition-opacity group-hover/block:opacity-100 group-focus-within/block:opacity-100 motion-reduce:transition-none sm:flex",
        top === "pt-6" && "top-6",
        top === "pt-5" && "top-5",
        top === "pt-3" && "top-3",
        top === "pt-2" && "top-2",
        b.type === "column_list" && "top-1/2 -translate-y-1/2",
      )}
    >
      {b.type !== "column_list" ? (
        <button
          type="button"
          aria-label="Add a block below"
          title="Add a block below · ⌥-click to add above"
          className="flex size-6 items-center justify-center rounded-sm text-fg-subtle hover:bg-sunken hover:text-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4"
          onClick={(e) => ctx.addBlockNear(b, e.altKey ? "before" : "after")}
          onMouseDown={(e) => e.preventDefault()}
        >
          <Plus aria-hidden />
        </button>
      ) : null}
      <button
        type="button"
        aria-label={ctx.canMove ? "Drag to move, or open block actions" : "Block actions"}
        title={ctx.canMove ? "Drag to move · Click for actions" : "Actions"}
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

function BlockContent({ block: b, number, open, hasKids, editable }: { block: DocsBlock; number: number | undefined; open: boolean; hasKids: boolean; editable: boolean }) {
  const ctx = useEditor();
  const [iconOpen, setIconOpen] = useState<HTMLElement | null>(null);
  if (!docsIsTextBlock(b.type)) return <BlockBody block={b} />;
  const type = b.type;
  const checked = type === "to_do" && !!b.props.checked;
  const toggleHeading = type.startsWith("heading") && !!b.props.toggleable;
  const text = (
    <RichText
      ref={(h) => ctx.register(b.id, h)}
      spans={b.props.text ?? []}
      refs={ctx.refs}
      editable={editable}
      placeholder={editable ? PLACEHOLDER[type] : undefined}
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
      onEquationClick={(start, end, rect) => ctx.editInlineEquation(b, start, end, rect)}
    />
  );
  const chevron = (
    <button
      type="button"
      aria-expanded={open}
      aria-label={open ? "Collapse" : "Expand"}
      className={cn(
        "flex w-6 shrink-0 items-center justify-center rounded-sm text-fg-muted hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring [&_svg]:size-4",
        type === "heading_1" ? "h-11" : type === "heading_2" ? "h-10" : "h-8",
      )}
      onMouseDown={(e) => e.preventDefault()}
      onClick={() => ctx.toggleOpen(b.id)}
    >
      <ChevronRight aria-hidden className={cn("transition-transform motion-reduce:transition-none", open && "rotate-90", !hasKids && "opacity-60")} />
    </button>
  );
  if (toggleHeading) {
    return (
      <div className="flex items-start">
        {chevron}
        {text}
      </div>
    );
  }
  switch (type) {
    case "bulleted_list":
      return (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 w-6 shrink-0 items-center justify-center">
            <span className="size-1.5 rounded-pill bg-current" />
          </span>
          {text}
        </div>
      );
    case "numbered_list":
      return (
        <div className="flex items-start">
          <span aria-hidden className="flex h-8 min-w-6 shrink-0 items-center justify-end pr-1.5 type-body tabular-nums">
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
              disabled={!editable}
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
          {chevron}
          {text}
        </div>
      );
    case "quote":
      return <div className="border-l-2 border-current pl-4">{text}</div>;
    case "callout":
      return (
        <div className={cn("flex items-start gap-2 rounded-md px-3 py-2", b.props.color ? colorClass(b.props.color) : "bg-sunken", b.props.color && !b.props.color.endsWith("_background") && "border border-border")}>
          <button
            type="button"
            aria-label="Change icon"
            disabled={!editable}
            className="flex size-8 shrink-0 items-center justify-center rounded-sm type-body-lg hover:bg-surface/60 focus-visible:outline-2 focus-visible:outline-ring"
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
