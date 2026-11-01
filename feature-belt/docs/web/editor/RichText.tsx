"use client";

import { docsPlain, docsSpansEqual, type DocsRefs, type DocsSpan } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { forwardRef, useImperativeHandle, useLayoutEffect, useRef, type ClipboardEvent, type KeyboardEvent } from "react";
import { getCaret, readSpans, renderSpans, setCaret, textLength, type Decoration } from "./dom";

export interface RichTextHandle {
  readonly el: HTMLDivElement | null;
  focus(at?: number | "start" | "end", end?: number): void;
  caret(): { start: number; end: number } | null;
}

export interface RichTextProps {
  spans: readonly DocsSpan[];
  refs?: DocsRefs | undefined;
  editable: boolean;
  placeholder?: string | undefined;
  /** "focus": the placeholder shows only while focused and empty. */
  placeholderWhen?: "focus" | "always";
  className?: string;
  decorations?: readonly Decoration[] | undefined;
  ariaLabel?: string;
  onChange?: (spans: DocsSpan[], caret: { start: number; end: number } | null) => void;
  onKeyDown?: (e: KeyboardEvent<HTMLDivElement>) => void;
  /** Enter from soft keyboards that don't send keydown (shift = line break). */
  onEnter?: (shift: boolean) => void;
  onPaste?: (e: ClipboardEvent<HTMLDivElement>) => void;
  onFocus?: () => void;
  onBlur?: () => void;
  onSelectionChange?: (caret: { start: number; end: number } | null) => void;
  onOpenPage?: (pageId: string) => void;
  onDecorationClick?: (threadId: string) => void;
}

function labelsKey(spans: readonly DocsSpan[], refs?: DocsRefs): string {
  let k = "";
  for (const s of spans) {
    if (!s.mention) continue;
    if (s.mention.type === "user") k += `u${refs?.users[s.mention.id]?.name ?? ""}|`;
    else if (s.mention.type === "page") {
      const p = refs?.pages[s.mention.id];
      k += `p${p ? (p.title ?? "∅") + (p.icon ?? "") : ""}|`;
    }
  }
  return k;
}

/**
 * One contenteditable rich-text field over the span model. Uncontrolled while typing (the
 * DOM is read back on input); re-rendered only when the spans change from elsewhere
 * (undo, a collaborator, formatting), keeping the caret where it was.
 */
export const RichText = forwardRef<RichTextHandle, RichTextProps>(function RichText(
  { spans, refs, editable, placeholder, placeholderWhen = "focus", className, decorations, ariaLabel, onChange, onKeyDown, onEnter, onPaste, onFocus, onBlur, onSelectionChange, onOpenPage, onDecorationClick },
  ref,
) {
  const elRef = useRef<HTMLDivElement>(null);
  const rendered = useRef<{ spans: readonly DocsSpan[]; labels: string; deco: string } | null>(null);
  const composing = useRef(false);

  useImperativeHandle(
    ref,
    () => ({
      get el() {
        return elRef.current;
      },
      focus(at = "end", end) {
        const el = elRef.current;
        if (!el) return;
        el.focus({ preventScroll: false });
        const len = textLength(el);
        const s = at === "start" ? 0 : at === "end" ? len : Math.min(at, len);
        setCaret(el, s, end === undefined ? s : Math.min(end, len));
      },
      caret() {
        return elRef.current ? getCaret(elRef.current) : null;
      },
    }),
    [],
  );

  const labels = labelsKey(spans, refs);
  const deco = decorations?.map((d) => `${d.id}:${d.start}-${d.end}:${d.active ? 1 : 0}`).join(",") ?? "";
  useLayoutEffect(() => {
    const el = elRef.current;
    if (!el) return;
    const prev = rendered.current;
    if (prev && prev.labels === labels && prev.deco === deco && docsSpansEqual(prev.spans, spans)) return;
    const focused = document.activeElement === el;
    const caret = focused ? getCaret(el) : null;
    const before = docsPlain(prev?.spans ?? []);
    renderSpans(el, spans, refs, decorations);
    rendered.current = { spans, labels, deco };
    if (focused && caret) {
      // Keep the caret on the same text when someone else's edit landed before it.
      const after = docsPlain(spans);
      let p = 0;
      while (p < before.length && p < after.length && before[p] === after[p]) p++;
      const delta = after.length - before.length;
      const shift = (x: number) => (x > p ? Math.max(p, x + delta) : x);
      setCaret(el, shift(caret.start), shift(caret.end));
    }
  });

  const handleInput = () => {
    const el = elRef.current;
    if (!el || composing.current) return;
    const next = readSpans(el);
    if (next.length === 0 && el.childNodes.length > 0) el.replaceChildren();
    rendered.current = { spans: next, labels: labelsKey(next, refs), deco };
    onChange?.(next, getCaret(el));
  };

  return (
    <div
      ref={elRef}
      role="textbox"
      aria-multiline="true"
      aria-label={ariaLabel}
      aria-readonly={!editable}
      contentEditable={editable}
      suppressContentEditableWarning
      spellCheck={editable}
      data-placeholder={placeholder}
      tabIndex={editable ? 0 : -1}
      className={cn(
        "min-w-0 flex-1 cursor-text whitespace-pre-wrap break-words outline-none [overflow-wrap:anywhere]",
        placeholder &&
          (placeholderWhen === "always"
            ? "empty:before:pointer-events-none empty:before:text-fg-subtle empty:before:content-[attr(data-placeholder)]"
            : "focus:empty:before:pointer-events-none focus:empty:before:text-fg-subtle focus:empty:before:content-[attr(data-placeholder)]"),
        !editable && "cursor-auto",
        className,
      )}
      onInput={handleInput}
      onCompositionStart={() => {
        composing.current = true;
      }}
      onCompositionEnd={() => {
        composing.current = false;
        handleInput();
      }}
      onKeyDown={(e) => {
        if (composing.current || e.nativeEvent.isComposing) return;
        onKeyDown?.(e);
      }}
      onBeforeInput={(e) => {
        const t = (e.nativeEvent as InputEvent).inputType;
        if (t === "insertParagraph" || t === "insertLineBreak") {
          e.preventDefault();
          onEnter?.(t === "insertLineBreak");
        }
        if (t === "formatBold" || t === "formatItalic" || t === "formatUnderline") e.preventDefault();
      }}
      onPaste={(e) => {
        if (!editable) return;
        onPaste?.(e);
      }}
      onDrop={(e) => {
        // Blocks move with the drag handle; dropped HTML would bypass sanitising.
        if (e.dataTransfer.types.includes("text/html") || e.dataTransfer.files.length) e.preventDefault();
      }}
      onFocus={onFocus}
      onBlur={onBlur}
      onSelect={() => {
        const el = elRef.current;
        if (el) onSelectionChange?.(getCaret(el));
      }}
      onMouseDown={(e) => {
        const chip = (e.target as HTMLElement).closest<HTMLElement>("[data-page-id]");
        if (chip) e.preventDefault();
      }}
      onClick={(e) => {
        const target = e.target as HTMLElement;
        const chip = target.closest<HTMLElement>("[data-page-id]");
        if (chip?.dataset["pageId"]) {
          e.preventDefault();
          onOpenPage?.(chip.dataset["pageId"]);
          return;
        }
        const mark = target.closest<HTMLElement>("mark[data-thread]");
        if (mark?.dataset["thread"]) onDecorationClick?.(mark.dataset["thread"]);
        const a = target.closest("a");
        if (a && editable && !(e.metaKey || e.ctrlKey)) e.preventDefault();
      }}
    />
  );
});
