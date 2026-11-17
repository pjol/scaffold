"use client";

import { type DocsBlock, type DocsOp } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";
import { useRef } from "react";
import { BlockChildren } from "./BlockTree";
import { useEditor } from "./context";
import { newBlock, positionAtEnd } from "./ops";

/** Equal shares when a column has no width; widths always sum to 1 once someone resizes. */
export function columnWidths(cols: readonly DocsBlock[]): number[] {
  const set = cols.map((c) => c.props.width ?? 0);
  if (set.every((w) => w > 0)) {
    const sum = set.reduce((a, b) => a + b, 0);
    return set.map((w) => w / sum);
  }
  return cols.map(() => 1 / Math.max(1, cols.length));
}

/**
 * 2–5 columns side by side (stacked below 640 px). Drag the gap between two columns to share their width; drag blocks
 * into a column or beside a block to make columns (the editor's drop targets).
 */
export function ColumnList({ block }: { block: DocsBlock }) {
  const ctx = useEditor();
  const cols = ctx.snap.children.get(block.id) ?? [];
  const widths = columnWidths(cols);
  const box = useRef<HTMLDivElement>(null);
  const editable = ctx.editable && !ctx.isReadOnly(block.id);

  const startResize = (i: number, e: React.PointerEvent) => {
    e.preventDefault();
    const el = box.current;
    if (!el) return;
    const total = el.getBoundingClientRect().width;
    const x0 = e.clientX;
    const base = columnWidths(cols);
    const pair = base[i]! + base[i + 1]!;
    const move = (ev: PointerEvent) => {
      const delta = (ev.clientX - x0) / total;
      const left = Math.min(pair - 0.08, Math.max(0.08, base[i]! + delta));
      const next = base.map((w, j) => (j === i ? left : j === i + 1 ? pair - left : w));
      const ops: DocsOp[] = cols.map((c, j) => ({ op: "update", id: c.id, props: { width: Math.round(next[j]! * 1000) / 1000 } }));
      ctx.apply(ops, undefined, "merge", `columns:${block.id}`);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const keyResize = (i: number, e: React.KeyboardEvent) => {
    if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
    e.preventDefault();
    const base = columnWidths(cols);
    const pair = base[i]! + base[i + 1]!;
    const left = Math.min(pair - 0.08, Math.max(0.08, base[i]! + (e.key === "ArrowRight" ? 0.05 : -0.05)));
    const next = base.map((w, j) => (j === i ? left : j === i + 1 ? pair - left : w));
    ctx.apply(cols.map((c, j) => ({ op: "update" as const, id: c.id, props: { width: Math.round(next[j]! * 1000) / 1000 } })), undefined, "merge", `columns:${block.id}`);
  };

  return (
    <div ref={box} className="flex flex-col gap-2 sm:flex-row sm:gap-0">
      {cols.map((col, i) => (
        <div key={col.id} className="relative flex min-w-0 sm:contents">
          <div data-column-id={col.id} className={cn("min-w-0 flex-1 sm:px-2", i === 0 && "sm:pl-0", i === cols.length - 1 && "sm:pr-0")} style={{ flexGrow: widths[i]! * 100, flexBasis: 0 }}>
            <BlockChildren parentId={col.id} depth={1} />
            {(ctx.snap.children.get(col.id)?.length ?? 0) === 0 && editable ? (
              <button
                type="button"
                className="flex min-h-12 w-full items-center rounded-sm px-1 text-left type-body text-fg-subtle hover:bg-sunken focus-visible:outline-2 focus-visible:outline-ring"
                onClick={() => {
                  const p = newBlock("paragraph", col.id, positionAtEnd(ctx.snap, col.id));
                  ctx.apply([p], { block: p.id, start: 0, end: 0 });
                  ctx.focusBlock(p.id, 0);
                }}
              >
                Empty column
              </button>
            ) : null}
          </div>
          {editable && i < cols.length - 1 ? (
            <div
              role="separator"
              tabIndex={0}
              aria-orientation="vertical"
              aria-label={`Resize columns ${i + 1} and ${i + 2}`}
              aria-valuenow={Math.round(widths[i]! * 100)}
              className="group/gap relative hidden w-1 shrink-0 cursor-col-resize touch-none self-stretch focus-visible:outline-2 focus-visible:outline-ring sm:block"
              onPointerDown={(e) => startResize(i, e)}
              onKeyDown={(e) => keyResize(i, e)}
            >
              <span aria-hidden className="absolute inset-y-0 left-1/2 w-0.5 -translate-x-1/2 rounded-pill bg-primary opacity-0 transition-opacity group-hover/gap:opacity-60 group-focus-visible/gap:opacity-60 motion-reduce:transition-none" />
            </div>
          ) : null}
        </div>
      ))}
    </div>
  );
}
