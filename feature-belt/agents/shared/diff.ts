/**
 * Readable before/after diffs for suggestion previews, and the copy both clients use
 * for levels. Portable: no DOM or Node APIs.
 */
import type { AgentsLevel, AgentsScopeInfo } from "./types";

export const AGENTS_LEVEL_LABEL: Record<AgentsLevel, string> = { off: "Off", read: "Read", suggest: "Suggest", edit: "Edit" };

/** One sentence per level, for InfoTips. */
export const AGENTS_LEVEL_INFO: Record<AgentsLevel, string> = {
  off: "Agents can't see it.",
  read: "Agents can read it but change nothing.",
  suggest: "Agents can propose changes. Each one waits in your notifications until you accept it.",
  edit: "Agents can make changes as you. They never get more access than you have.",
};

/** The levels explained together (settings InfoTip). */
export const AGENTS_LEVELS_INFO =
  "Off: agents can't see it. Read: they read but change nothing. Suggest: each change waits in your notifications for you to accept. " +
  "Edit: they make changes as you. Agents never get more access than you have, and never change sharing, permissions or keys.";

/** "from folder Design", "from Calendar", "your default". */
export function agentsInheritedSource(info: Pick<AgentsScopeInfo, "inherited" | "feature_label">): string {
  const i = info.inherited;
  if (i.from === "parent") return i.title ? `from ${i.noun ?? "item"} ${i.title}` : `from a parent ${i.noun ?? "item"}`;
  if (i.from === "feature") return `from ${info.feature_label}`;
  return "your default";
}

/** Row hint for the ⋮ menu: the level that applies ("Read"); where it comes from shows on the Default choice. */
export function agentsScopeHint(info: AgentsScopeInfo): string {
  if (info.paused) return "Off";
  return AGENTS_LEVEL_LABEL[info.override ?? info.inherited.level];
}

/** Whether your own access caps a level (an agent at Edit on something you can only view stays read-only). */
export function agentsCappedBy(info: AgentsScopeInfo, level: AgentsLevel): string | null {
  const mine = info.my_level;
  if (!mine || level === "off" || level === "read") return null;
  if (mine === "view") return "You can only view this, so agents can only read it.";
  if (mine === "comment" && level === "edit") return "You can comment but not edit, so agents can't edit it either.";
  return null;
}

export type AgentsDiffOp = "same" | "add" | "del";

export interface AgentsDiffPart {
  op: AgentsDiffOp;
  text: string;
}

export interface AgentsDiffLine {
  op: AgentsDiffOp;
  /** The line's text; on a changed line that mostly matches its replacement, the changed words are "add"/"del" parts. */
  parts: AgentsDiffPart[];
}

const MAX_CELLS = 400_000; // LCS table size before falling back to replace-all

function lcs<T>(a: readonly T[], b: readonly T[], eq: (x: T, y: T) => boolean): { op: AgentsDiffOp; item: T }[] | null {
  const n = a.length;
  const m = b.length;
  if (n * m > MAX_CELLS) return null;
  const w = m + 1;
  const t = new Uint32Array((n + 1) * w);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      t[i * w + j] = eq(a[i] as T, b[j] as T) ? (t[(i + 1) * w + j + 1] ?? 0) + 1 : Math.max(t[(i + 1) * w + j] ?? 0, t[i * w + j + 1] ?? 0);
    }
  }
  const out: { op: AgentsDiffOp; item: T }[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (eq(a[i] as T, b[j] as T)) {
      out.push({ op: "same", item: a[i] as T });
      i++;
      j++;
    } else if ((t[(i + 1) * w + j] ?? 0) >= (t[i * w + j + 1] ?? 0)) {
      out.push({ op: "del", item: a[i] as T });
      i++;
    } else {
      out.push({ op: "add", item: b[j] as T });
      j++;
    }
  }
  while (i < n) out.push({ op: "del", item: a[i++] as T });
  while (j < m) out.push({ op: "add", item: b[j++] as T });
  return out;
}

function words(s: string): string[] {
  return s.match(/\s+|[^\s]+/g) ?? [];
}

function wordParts(before: string, after: string): { del: AgentsDiffPart[]; add: AgentsDiffPart[] } {
  const ops = lcs(words(before), words(after), (x, y) => x === y);
  if (!ops) return { del: [{ op: "del", text: before }], add: [{ op: "add", text: after }] };
  const del: AgentsDiffPart[] = [];
  const add: AgentsDiffPart[] = [];
  const push = (list: AgentsDiffPart[], op: AgentsDiffOp, text: string) => {
    const last = list[list.length - 1];
    if (last && last.op === op) last.text += text;
    else list.push({ op, text });
  };
  for (const o of ops) {
    if (o.op === "same") {
      push(del, "same", o.item);
      push(add, "same", o.item);
    } else if (o.op === "del") push(del, "del", o.item);
    else push(add, "add", o.item);
  }
  return { del, add };
}

/** A changed line is worth marking word by word when at least 40% of it stayed. */
function similar(parts: AgentsDiffPart[]): boolean {
  let same = 0;
  let all = 0;
  for (const p of parts) {
    const n = p.text.trim().length;
    all += n;
    if (p.op === "same") same += n;
  }
  return all > 0 && same / all >= 0.4;
}

/**
 * Line diff of two texts. A run of removed lines followed by added ones is paired line
 * by line; a pair that mostly matches is marked word by word, so a one-word change shows
 * as that word. Other changed lines carry their whole text as one "same" part.
 */
export function agentsDiff(before: string, after: string): AgentsDiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  if (before === "") a.length = 0;
  if (after === "") b.length = 0;
  const ops = lcs(a, b, (x, y) => x === y) ?? [...a.map((item) => ({ op: "del" as const, item })), ...b.map((item) => ({ op: "add" as const, item }))];
  const out: AgentsDiffLine[] = [];
  for (let k = 0; k < ops.length; ) {
    const o = ops[k];
    if (!o) break;
    if (o.op === "same") {
      out.push({ op: "same", parts: [{ op: "same", text: o.item }] });
      k++;
      continue;
    }
    const dels: string[] = [];
    const adds: string[] = [];
    while (k < ops.length && ops[k]?.op === "del") dels.push((ops[k++] as { item: string }).item);
    while (k < ops.length && ops[k]?.op === "add") adds.push((ops[k++] as { item: string }).item);
    const pairs = Math.min(dels.length, adds.length);
    const delLines: AgentsDiffLine[] = [];
    const addLines: AgentsDiffLine[] = [];
    for (let p = 0; p < Math.max(dels.length, adds.length); p++) {
      const words = p < pairs ? wordParts(dels[p] ?? "", adds[p] ?? "") : null;
      if (words && similar(words.del) && similar(words.add)) {
        delLines.push({ op: "del", parts: words.del });
        addLines.push({ op: "add", parts: words.add });
        continue;
      }
      if (p < dels.length) delLines.push({ op: "del", parts: [{ op: "same", text: dels[p] ?? "" }] });
      if (p < adds.length) addLines.push({ op: "add", parts: [{ op: "same", text: adds[p] ?? "" }] });
    }
    out.push(...delLines, ...addLines);
  }
  return out;
}

/** Collapse long unchanged runs: keep `context` lines around changes, mark the rest as hidden counts. */
export function agentsDiffHunks(lines: AgentsDiffLine[], context = 2): (AgentsDiffLine | { hidden: number })[] {
  const keep = lines.map(() => false);
  lines.forEach((l, i) => {
    if (l.op !== "same") for (let j = Math.max(0, i - context); j <= Math.min(lines.length - 1, i + context); j++) keep[j] = true;
  });
  if (!keep.some(Boolean)) return lines;
  const out: (AgentsDiffLine | { hidden: number })[] = [];
  let hidden = 0;
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (hidden) out.push({ hidden });
      hidden = 0;
      out.push(l);
    } else hidden++;
  });
  if (hidden) out.push({ hidden });
  return out;
}

/** Input as readable "key: value" rows, for suggestions without a before/after preview. */
export function agentsInputRows(input: Record<string, unknown>): { key: string; value: string }[] {
  return Object.entries(input).map(([key, v]) => ({
    key: key.replace(/_/g, " "),
    value: typeof v === "string" ? v : JSON.stringify(v, null, 2),
  }));
}
