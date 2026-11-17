import type { CatalogIndex, CatalogIndexItem } from "./types";

/**
 * Search on the device over the compact client index (`GET /v1/catalog/index`), ranked as the
 * server ranks: the exact name, names starting with the query, a later word starting with it,
 * names containing it, then aliases starting with it, then aliases containing it. Ties keep
 * name order. Names are folded as the server folds them (`catalog.NameKey`).
 */

const FOLD: Record<string, string> = {
  æ: "ae", œ: "oe", ø: "o", ł: "l", đ: "d", ð: "d", þ: "th", ß: "ss", ı: "i",
  "’": "'", "‘": "'", "ʼ": "'", "′": "'", "`": "'", "´": "'",
  "“": '"', "”": '"', "„": '"', "″": '"',
  "–": "-", "—": "-", "‐": "-", "‑": "-", "‒": "-", "―": "-", "−": "-",
  " ": " ", " ": " ", " ": " ", " ": " ",
};
const FOLD_RE = new RegExp(`[${Object.keys(FOLD).join("")}]`, "gu");
const MARKS_RE = /[\p{Mn}​‌‍﻿]/gu;
const SEP_RE = /[ \t\n\f\r]*(?:\/\/|\/|\|)[ \t\n\f\r]*/g;
const SPACE_RE = /[ \t\n\f\r]+/g;
const WORD_CHAR_RE = /[\p{L}\p{N}']/u;

/** A name folded for matching: accents dropped, lower case, æ → ae, curly quotes and dashes plain, "A/B" and "A | B" → "a / b", spaces collapsed. */
export function catalogNameKey(s: string): string {
  return s
    .toLowerCase()
    .normalize("NFKD")
    .replace(MARKS_RE, "")
    .replace(FOLD_RE, (c) => FOLD[c] ?? c)
    .toLowerCase()
    .replace(SEP_RE, " / ")
    .replace(SPACE_RE, " ")
    .trim();
}

/** The client index, ready to search. */
export interface CatalogClientIndex {
  version: number;
  /** In name order. */
  items: CatalogIndexItem[];
  nameKeys: string[];
  aliasKeys: string[][];
}

export function buildCatalogClientIndex(ix: CatalogIndex): CatalogClientIndex {
  const keyed = ix.items.map((item) => ({ item, key: catalogNameKey(item.name) }));
  keyed.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : a.item.key < b.item.key ? -1 : a.item.key > b.item.key ? 1 : 0));
  return {
    version: ix.version,
    items: keyed.map((k) => k.item),
    nameKeys: keyed.map((k) => k.key),
    aliasKeys: keyed.map((k) => k.item.aliases.map(catalogNameKey).filter((a) => a !== "")),
  };
}

function wordStart(key: string, q: string): boolean {
  for (let from = 0; ; ) {
    const i = key.indexOf(q, from);
    if (i < 0) return false;
    if (i === 0 || !WORD_CHAR_RE.test(key.charAt(i - 1))) return true;
    from = i + 1;
  }
}

/** Rank of an item for q (0 best), or -1 when nothing matches. */
function rankOf(nameKey: string, aliasKeys: readonly string[], q: string): number {
  if (nameKey === q) return 0;
  if (nameKey.startsWith(q)) return 1;
  if (nameKey.includes(q)) return wordStart(nameKey, q) ? 2 : 3;
  if (aliasKeys.some((a) => a.startsWith(q))) return 4;
  if (aliasKeys.some((a) => a.includes(q))) return 5;
  return -1;
}

export interface CatalogClientSearchOptions {
  category?: string | undefined;
  /** Include retired items. */
  retired?: boolean | undefined;
  /** Default 20, at most 50. */
  limit?: number | undefined;
}

/** Ranked matches for `q` (2 characters at least, else none). */
export function searchCatalogClientIndex(ix: CatalogClientIndex, q: string, opts: CatalogClientSearchOptions = {}): CatalogIndexItem[] {
  const k = catalogNameKey(q);
  if ([...k].length < 2) return [];
  const limit = Math.min(50, opts.limit && opts.limit > 0 ? opts.limit : 20);
  const tiers: CatalogIndexItem[][] = [[], [], [], [], [], []];
  for (let i = 0; i < ix.items.length; i++) {
    const item = ix.items[i] as CatalogIndexItem;
    if ((item.retired && !opts.retired) || (opts.category && item.category !== opts.category)) continue;
    const r = rankOf(ix.nameKeys[i] ?? "", ix.aliasKeys[i] ?? [], k);
    const tier = r >= 0 ? tiers[r] : undefined;
    if (tier && tier.length < limit) tier.push(item);
  }
  return tiers.flat().slice(0, limit);
}
