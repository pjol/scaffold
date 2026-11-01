/**
 * A line diff for unsaved edits (the editor's "Changes" view): Myers' algorithm, grouped
 * into hunks shaped like the API's (`ReposDiffHunk`), so the same diff view renders both.
 */
import type { ReposDiffFile, ReposDiffHunk, ReposDiffLine } from "./types";

type Op = { type: "context" | "add" | "del"; text: string };

const MAX_LINES = 20_000;

function splitLines(s: string): string[] {
  if (s === "") return [];
  const lines = s.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** The shortest edit script from a to b (equal, insert and delete lines). */
function myers(a: string[], b: string[]): Op[] {
  const n = a.length;
  const m = b.length;
  const max = n + m;
  const v = new Map<number, number>([[1, 0]]);
  const trace: Map<number, number>[] = [];
  outer: for (let d = 0; d <= max; d++) {
    trace.push(new Map(v));
    for (let k = -d; k <= d; k += 2) {
      let x = k === -d || (k !== d && (v.get(k - 1) ?? 0) < (v.get(k + 1) ?? 0)) ? (v.get(k + 1) ?? 0) : (v.get(k - 1) ?? 0) + 1;
      let y = x - k;
      while (x < n && y < m && a[x] === b[y]) {
        x++;
        y++;
      }
      v.set(k, x);
      if (x >= n && y >= m) break outer;
    }
  }
  // Walk the trace back to build the script.
  const ops: Op[] = [];
  let x = n;
  let y = m;
  for (let d = trace.length - 1; d >= 0 && (x > 0 || y > 0); d--) {
    const vd = trace[d]!;
    const k = x - y;
    const prevK = k === -d || (k !== d && (vd.get(k - 1) ?? 0) < (vd.get(k + 1) ?? 0)) ? k + 1 : k - 1;
    const prevX = vd.get(prevK) ?? 0;
    const prevY = prevX - prevK;
    while (x > prevX && y > prevY) {
      ops.push({ type: "context", text: a[x - 1]! });
      x--;
      y--;
    }
    if (d === 0) break;
    if (x === prevX) ops.push({ type: "add", text: b[y - 1]! });
    else ops.push({ type: "del", text: a[x - 1]! });
    x = prevX;
    y = prevY;
  }
  while (x > 0 && y > 0) {
    ops.push({ type: "context", text: a[x - 1]! });
    x--;
    y--;
  }
  return ops.reverse();
}

/** Hunks (3 lines of context) between two texts. */
export function reposLineDiff(before: string, after: string, context = 3): ReposDiffHunk[] {
  const a = splitLines(before);
  const b = splitLines(after);
  const ops: Op[] =
    a.length + b.length > MAX_LINES
      ? [...a.map((text) => ({ type: "del" as const, text })), ...b.map((text) => ({ type: "add" as const, text }))]
      : myers(a, b);
  // Number every line on both sides.
  const lines: ReposDiffLine[] = [];
  let o = 1;
  let nw = 1;
  for (const op of ops) {
    if (op.type === "context") lines.push({ type: "context", old: o++, new: nw++, text: op.text });
    else if (op.type === "del") lines.push({ type: "del", old: o++, new: 0, text: op.text });
    else lines.push({ type: "add", old: 0, new: nw++, text: op.text });
  }
  const hunks: ReposDiffHunk[] = [];
  let i = 0;
  while (i < lines.length) {
    while (i < lines.length && lines[i]!.type === "context") i++;
    if (i >= lines.length) break;
    const start = Math.max(0, i - context);
    let end = i;
    let lastChange = i;
    while (end < lines.length && end - lastChange <= context * 2) {
      if (lines[end]!.type !== "context") lastChange = end;
      end++;
    }
    end = Math.min(lines.length, lastChange + context + 1);
    const slice = lines.slice(start, end);
    const first = slice[0]!;
    const oldStart = first.old || (slice.find((l) => l.old)?.old ?? 0);
    const newStart = first.new || (slice.find((l) => l.new)?.new ?? 0);
    hunks.push({
      header: "",
      old_start: oldStart,
      old_lines: slice.filter((l) => l.type !== "add").length,
      new_start: newStart,
      new_lines: slice.filter((l) => l.type !== "del").length,
      lines: slice,
    });
    i = end;
  }
  return hunks;
}

/** A diff file for an unsaved edit, for the shared diff view. */
export function reposEditDiffFile(path: string, before: string | null, after: string | null, oldPath?: string): ReposDiffFile {
  const hunks = reposLineDiff(before ?? "", after ?? "");
  let additions = 0;
  let deletions = 0;
  for (const h of hunks) for (const l of h.lines) l.type === "add" ? additions++ : l.type === "del" ? deletions++ : null;
  const status = before === null ? "added" : after === null ? "deleted" : oldPath && oldPath !== path ? "renamed" : "modified";
  return { path, old_path: oldPath ?? path, status, old_mode: "", new_mode: "", additions, deletions, binary: false, truncated: false, hunks };
}
