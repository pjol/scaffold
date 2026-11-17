/**
 * Building block operations from the editor's intent (split, merge, indent, move, paste).
 * Pure functions over the session snapshot; the editor applies the result as one step.
 */
import {
  docsFolds,
  docsHoldsChildren,
  docsIsTextBlock,
  docsKeyBetween,
  docsKeysBetween,
  generateId,
  type DocsBlock,
  type DocsBlockProps,
  type DocsBlockType,
  type DocsDraftBlock,
  type DocsOp,
  type DocsSessionSnapshot,
} from "@scaffoldapp/shared";

export function siblingsOf(snap: DocsSessionSnapshot, parentId: string | null): readonly DocsBlock[] {
  return snap.children.get(parentId ?? "") ?? [];
}

export function nextSibling(snap: DocsSessionSnapshot, b: DocsBlock): DocsBlock | undefined {
  const sibs = siblingsOf(snap, b.parent_id);
  return sibs[sibs.findIndex((x) => x.id === b.id) + 1];
}

export function prevSibling(snap: DocsSessionSnapshot, b: DocsBlock): DocsBlock | undefined {
  const sibs = siblingsOf(snap, b.parent_id);
  const i = sibs.findIndex((x) => x.id === b.id);
  return i > 0 ? sibs[i - 1] : undefined;
}

/** A position right after `b` among its siblings. */
export function positionAfter(snap: DocsSessionSnapshot, b: DocsBlock): string {
  return docsKeyBetween(b.position, nextSibling(snap, b)?.position ?? null);
}

export function positionBefore(snap: DocsSessionSnapshot, b: DocsBlock): string {
  return docsKeyBetween(prevSibling(snap, b)?.position ?? null, b.position);
}

/** Position at the end of a parent's children. */
export function positionAtEnd(snap: DocsSessionSnapshot, parentId: string | null): string {
  const sibs = siblingsOf(snap, parentId);
  return docsKeyBetween(sibs[sibs.length - 1]?.position ?? null, null);
}

/** Visible blocks in document order (children of closed toggles and toggle headings left out), from `root` (null: the page). */
export function visibleOrder(snap: DocsSessionSnapshot, isOpen: (id: string) => boolean, root: string | null = null): DocsBlock[] {
  const out: DocsBlock[] = [];
  const walk = (parent: string, depth: number) => {
    if (depth > 64) return;
    for (const b of snap.children.get(parent) ?? []) {
      out.push(b);
      if (!docsFolds(b.type, b.props) || isOpen(b.id)) walk(b.id, depth + 1);
    }
  };
  walk(root ?? "", 0);
  return out;
}

export function newBlock(type: DocsBlockType, parentId: string | null, position: string, props: DocsBlockProps = {}): Extract<DocsOp, { op: "create" }> {
  const base: DocsBlockProps = docsIsTextBlock(type) ? { text: [], ...props } : props;
  if (type === "to_do" && base.checked === undefined) base.checked = false;
  if (type === "callout" && !base.icon) base.icon = "💡";
  if (type === "code" && base.code === undefined) {
    base.code = "";
    base.language = base.language ?? "plain";
  }
  if (type === "equation" && base.expression === undefined) base.expression = "";
  if (type === "button" && !base.label) base.label = "Button";
  return { op: "create", id: generateId(), type, parent_id: parentId, position, props: base };
}

/** Ops that turn a block into another type; children move up when the new type can't hold them. */
export function turnInto(snap: DocsSessionSnapshot, b: DocsBlock, type: DocsBlockType, extra: DocsBlockProps = {}): DocsOp[] {
  const ops: DocsOp[] = [];
  const props: Partial<DocsBlockProps> = { ...extra };
  const plain = (b.props.text ?? []).map((s) => s.text).join("");
  if (docsIsTextBlock(type) && !docsIsTextBlock(b.type)) {
    props.text = b.type === "code" ? [{ text: b.props.code ?? "" }] : b.type === "equation" ? [{ text: b.props.expression ?? "" }] : [];
    if (!props.text[0]?.text) props.text = [];
  }
  if (type === "to_do" && props.checked === undefined) props.checked = false;
  if (type === "callout" && !b.props.icon && !props.icon) props.icon = "💡";
  if (type.startsWith("heading") && props.toggleable === undefined) props.toggleable = false;
  if (type === "code" && b.type !== "code") {
    props.code = extra.code || (b.type === "equation" ? (b.props.expression ?? "") : plain);
    props.language = props.language ?? "plain";
  }
  if (type === "equation" && b.type !== "equation") props.expression = extra.expression || (b.type === "code" ? (b.props.code ?? "") : plain);
  ops.push({ op: "update", id: b.id, type, props });
  if (!docsHoldsChildren(type, { ...b.props, ...props } as DocsBlockProps)) {
    const kids = siblingsOf(snap, b.id);
    if (kids.length) {
      const keys = docsKeysBetween(b.position, nextSibling(snap, b)?.position ?? null, kids.length);
      kids.forEach((k, i) => ops.push({ op: "move", id: k.id, parent_id: b.parent_id, position: keys[i]! }));
    }
  }
  return ops;
}

/** Indent: the block becomes the last child of its previous sibling. */
export function indentOps(snap: DocsSessionSnapshot, b: DocsBlock): DocsOp[] | null {
  const prev = prevSibling(snap, b);
  if (!prev || !docsHoldsChildren(prev.type, prev.props) || prev.type === "column_list" || b.type === "column") return null;
  return [{ op: "move", id: b.id, parent_id: prev.id, position: positionAtEnd(snap, prev.id) }];
}

/** Outdent: the block moves right after its parent. */
export function outdentOps(snap: DocsSessionSnapshot, b: DocsBlock): DocsOp[] | null {
  if (!b.parent_id) return null;
  const parent = snap.byId.get(b.parent_id);
  // Blocks in a column, a synced block or a column list stay there (move them out by dragging).
  if (!parent || parent.type === "column" || parent.type === "column_list" || parent.type === "synced_block" || b.type === "column") return null;
  return [{ op: "move", id: b.id, parent_id: parent.parent_id, position: positionAfter(snap, parent) }];
}

/** Move a block up or down among its siblings. */
export function shiftOps(snap: DocsSessionSnapshot, b: DocsBlock, dir: -1 | 1): DocsOp[] | null {
  const sibs = siblingsOf(snap, b.parent_id);
  const i = sibs.findIndex((x) => x.id === b.id);
  const j = i + dir;
  if (j < 0 || j >= sibs.length) return null;
  const pos = dir < 0 ? docsKeyBetween(sibs[j - 1]?.position ?? null, sibs[j]!.position) : docsKeyBetween(sibs[j]!.position, sibs[j + 1]?.position ?? null);
  return [{ op: "move", id: b.id, parent_id: b.parent_id, position: pos }];
}

/** Create ops for draft blocks (with nesting), placed between two positions under a parent. */
export function draftOps(drafts: readonly DocsDraftBlock[], parentId: string | null, after: string | null, before: string | null): { ops: DocsOp[]; ids: string[] } {
  const ops: DocsOp[] = [];
  const ids: string[] = [];
  const keys = docsKeysBetween(after, before, drafts.length);
  drafts.forEach((d, i) => {
    const op = newBlock(d.type, parentId, keys[i]!, d.props);
    ops.push(op);
    ids.push(op.id);
    if (d.children.length && docsHoldsChildren(d.type, d.props)) ops.push(...draftOps(d.children, op.id, null, null).ops);
  });
  return { ops, ids };
}

/** Blocks (with their subtrees) as drafts, for copy and duplicate. */
export function toDrafts(snap: DocsSessionSnapshot, ids: readonly string[]): DocsDraftBlock[] {
  const conv = (b: DocsBlock): DocsDraftBlock => ({ type: b.type, props: b.props, children: siblingsOf(snap, b.id).map(conv) });
  const set = new Set(ids);
  // Skip blocks whose ancestor is also selected (they come with it).
  return ids
    .map((id) => snap.byId.get(id))
    .filter((b): b is DocsBlock => !!b && !hasSelectedAncestor(snap, b, set))
    .map(conv);
}

export function hasSelectedAncestor(snap: DocsSessionSnapshot, b: DocsBlock, set: ReadonlySet<string>): boolean {
  let p = b.parent_id;
  let depth = 0;
  while (p && depth++ < 64) {
    if (set.has(p)) return true;
    p = snap.byId.get(p)?.parent_id ?? null;
  }
  return false;
}

/** Whether `ancestor` contains `b` (or is it). */
export function isWithin(snap: DocsSessionSnapshot, b: DocsBlock, ancestorId: string): boolean {
  let cur: DocsBlock | undefined = b;
  let depth = 0;
  while (cur && depth++ < 64) {
    if (cur.id === ancestorId) return true;
    cur = cur.parent_id ? snap.byId.get(cur.parent_id) : undefined;
  }
  return false;
}

/** Ops moving blocks (in document order) to sit after `target` (or as its first children with `inside`). */
export function moveBlocksOps(snap: DocsSessionSnapshot, ids: readonly string[], target: DocsBlock | null, place: "before" | "after" | "inside"): DocsOp[] {
  const set = new Set(ids);
  const moving = snap.blocks.filter((b) => set.has(b.id) && !hasSelectedAncestor(snap, b, set));
  if (target && moving.some((m) => isWithin(snap, target, m.id))) return [];
  let parentId: string | null;
  let lo: string | null;
  let hi: string | null;
  if (!target) {
    parentId = null;
    const sibs = siblingsOf(snap, null).filter((b) => !set.has(b.id));
    lo = sibs[sibs.length - 1]?.position ?? null;
    hi = null;
  } else if (place === "inside") {
    parentId = target.id;
    lo = null;
    hi = siblingsOf(snap, target.id).filter((b) => !set.has(b.id))[0]?.position ?? null;
  } else {
    parentId = target.parent_id;
    const sibs = siblingsOf(snap, target.parent_id).filter((b) => !set.has(b.id) || b.id === target.id);
    const i = sibs.findIndex((b) => b.id === target.id);
    if (place === "after") {
      lo = target.position;
      hi = sibs.slice(i + 1).find((b) => !set.has(b.id))?.position ?? null;
    } else {
      hi = target.position;
      lo = sibs.slice(0, i).reverse().find((b) => !set.has(b.id))?.position ?? null;
    }
  }
  const keys = docsKeysBetween(lo, hi, moving.length);
  return moving.map((b, i) => ({ op: "move" as const, id: b.id, parent_id: parentId, position: keys[i]! }));
}

// ---- columns ---------------------------------------------------------------------------

/**
 * Ops that put `ids` beside `target` as columns: a new column next to target's column when target is already in one
 * (up to 5), else a new column list where target was, holding target and the moved blocks.
 */
export function columnDropOps(snap: DocsSessionSnapshot, ids: readonly string[], target: DocsBlock, side: "left" | "right"): DocsOp[] {
  const set = new Set(ids);
  const moving = snap.blocks.filter((b) => set.has(b.id) && !hasSelectedAncestor(snap, b, set));
  if (!moving.length || moving.some((m) => isWithin(snap, target, m.id) || m.type === "column" || m.type === "column_list")) return [];
  const parent = target.parent_id ? snap.byId.get(target.parent_id) : undefined;
  const ops: DocsOp[] = [];
  if (parent?.type === "column") {
    const list = snap.byId.get(parent.parent_id ?? "");
    const cols = list ? siblingsOf(snap, list.id) : [];
    if (!list || cols.length >= 5) return [];
    const i = cols.findIndex((c) => c.id === parent.id);
    const lo = side === "left" ? (cols[i - 1]?.position ?? null) : parent.position;
    const hi = side === "left" ? parent.position : (cols[i + 1]?.position ?? null);
    const col = newBlock("column", list.id, docsKeyBetween(lo, hi));
    ops.push(col, ...cols.map((c) => ({ op: "update" as const, id: c.id, props: { width: 0 } })));
    const keys = docsKeysBetween(null, null, moving.length);
    moving.forEach((m, k) => ops.push({ op: "move", id: m.id, parent_id: col.id, position: keys[k]! }));
    return ops;
  }
  const list = newBlock("column_list", target.parent_id, positionBefore(snap, target));
  const a = newBlock("column", list.id, "a0");
  const b = newBlock("column", list.id, "a1");
  const [targetCol, movedCol] = side === "left" ? [b, a] : [a, b];
  ops.push(list, a, b, { op: "move", id: target.id, parent_id: targetCol.id, position: "a0" });
  const keys = docsKeysBetween(null, null, moving.length);
  moving.forEach((m, k) => ops.push({ op: "move", id: m.id, parent_id: movedCol.id, position: keys[k]! }));
  return ops;
}

/** A column list with n columns, the first holding `first` (if given), the others an empty paragraph each. */
export function columnsOps(snap: DocsSessionSnapshot, at: DocsBlock, n: number, replace: boolean): { ops: DocsOp[]; focus: string } {
  const list = newBlock("column_list", at.parent_id, replace ? at.position : positionAfter(snap, at));
  const ops: DocsOp[] = replace ? [{ op: "delete", id: at.id }, list] : [list];
  const keys = docsKeysBetween(null, null, n);
  let focus = "";
  for (let i = 0; i < n; i++) {
    const col = newBlock("column", list.id, keys[i]!);
    const p = newBlock("paragraph", col.id, "a0");
    if (!focus) focus = p.id;
    ops.push(col, p);
  }
  return { ops, focus };
}

/**
 * Tidies columns after blocks leave them: an empty column goes; a list left with one column becomes that column's
 * blocks in its place; a list with none goes.
 */
export function columnCleanupOps(snap: DocsSessionSnapshot): DocsOp[] {
  const ops: DocsOp[] = [];
  for (const list of snap.blocks) {
    if (list.type !== "column_list") continue;
    const cols = siblingsOf(snap, list.id).filter((c) => c.type === "column");
    const full = cols.filter((c) => siblingsOf(snap, c.id).length > 0);
    if (full.length === cols.length && cols.length >= 2) continue;
    for (const c of cols) if (!full.includes(c)) ops.push({ op: "delete", id: c.id });
    if (full.length >= 2) {
      for (const c of full) ops.push({ op: "update", id: c.id, props: { width: 0 } });
      continue;
    }
    if (full.length === 1) {
      const kids = siblingsOf(snap, full[0]!.id);
      const keys = docsKeysBetween(prevSibling(snap, list)?.position ?? null, list.position, kids.length);
      kids.forEach((k, i) => ops.push({ op: "move", id: k.id, parent_id: list.parent_id, position: keys[i]! }));
    }
    ops.push({ op: "delete", id: list.id });
  }
  return ops;
}
