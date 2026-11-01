/**
 * Fractional indexing: positions are base-62 strings that sort in byte order, and a new
 * one always fits between two others, so inserting or moving a block rewrites only that
 * block. Same algorithm as the backend (`docs/position.go`, after rocicorp's
 * fractional-indexing).
 */
const DIGITS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
const ZERO_INT = "A" + "0".repeat(26);

function intLength(head: string): number {
  if (head >= "a" && head <= "z") return head.charCodeAt(0) - 97 + 2;
  if (head >= "A" && head <= "Z") return 90 - head.charCodeAt(0) + 2;
  return -1;
}

function intPart(key: string): string | null {
  const n = intLength(key[0] ?? "");
  return n > 0 && n <= key.length ? key.slice(0, n) : null;
}

/** Whether `key` is a well-formed position. */
export function docsValidKey(key: string): boolean {
  if (!key || key.length > 64 || key === ZERO_INT) return false;
  for (const c of key) if (!DIGITS.includes(c)) return false;
  const i = intPart(key);
  if (i === null) return false;
  const f = key.slice(i.length);
  return f === "" || !f.endsWith("0");
}

function midpoint(a: string, b: string | null): string {
  if (b !== null) {
    let n = 0;
    while (n < b.length && (a[n] ?? "0") === b[n]) n++;
    if (n > 0) return b.slice(0, n) + midpoint(a.slice(n), b.slice(n));
  }
  const da = a ? DIGITS.indexOf(a[0]!) : 0;
  const db = b !== null && b !== "" ? DIGITS.indexOf(b[0]!) : DIGITS.length;
  if (db - da > 1) return DIGITS[Math.round(0.5 * (da + db))]!;
  if (b !== null && b.length > 1) return b.slice(0, 1);
  return DIGITS[da]! + midpoint(a.slice(1), null);
}

function increment(x: string): string | null {
  const head = x[0]!;
  const digs = x.slice(1).split("");
  let carry = true;
  for (let i = digs.length - 1; carry && i >= 0; i--) {
    const d = DIGITS.indexOf(digs[i]!) + 1;
    if (d === DIGITS.length) digs[i] = "0";
    else {
      digs[i] = DIGITS[d]!;
      carry = false;
    }
  }
  if (!carry) return head + digs.join("");
  if (head === "Z") return "a0";
  if (head === "z") return null;
  const h = String.fromCharCode(head.charCodeAt(0) + 1);
  if (h > "a") digs.push("0");
  else digs.pop();
  return h + digs.join("");
}

function decrement(x: string): string | null {
  const head = x[0]!;
  const digs = x.slice(1).split("");
  let borrow = true;
  for (let i = digs.length - 1; borrow && i >= 0; i--) {
    const d = DIGITS.indexOf(digs[i]!) - 1;
    if (d === -1) digs[i] = "z";
    else {
      digs[i] = DIGITS[d]!;
      borrow = false;
    }
  }
  if (!borrow) return head + digs.join("");
  if (head === "a") return "Zz";
  if (head === "A") return null;
  const h = String.fromCharCode(head.charCodeAt(0) - 1);
  if (h < "Z") digs.push("z");
  else digs.pop();
  return h + digs.join("");
}

/**
 * A position strictly between `a` and `b` (null = open on that side). When `a >= b`
 * (two siblings share a key) it falls back to a key after `a`.
 */
export function docsKeyBetween(a: string | null | undefined, b: string | null | undefined): string {
  let lo = a && docsValidKey(a) ? a : null;
  let hi = b && docsValidKey(b) ? b : null;
  if (lo !== null && hi !== null && lo >= hi) hi = null;
  if (lo === null && hi === null) return "a0";
  if (lo === null) {
    const ib = intPart(hi!)!;
    const fb = hi!.slice(ib.length);
    if (ib === ZERO_INT) return ib + midpoint("", fb);
    if (ib < hi!) return ib;
    return decrement(ib) ?? ib + midpoint("", fb);
  }
  if (hi === null) {
    const ia = intPart(lo)!;
    return increment(ia) ?? ia + midpoint(lo.slice(ia.length), null);
  }
  const ia = intPart(lo)!;
  const fa = lo.slice(ia.length);
  const ib = intPart(hi)!;
  const fb = hi.slice(ib.length);
  if (ia === ib) return ia + midpoint(fa, fb);
  const i = increment(ia);
  if (i !== null && i < hi) return i;
  lo = ia + midpoint(fa, null);
  return lo;
}

/** `n` increasing positions after `a` (and before `b` when set). */
export function docsKeysBetween(a: string | null | undefined, b: string | null | undefined, n: number): string[] {
  const out: string[] = [];
  let prev = a ?? null;
  for (let i = 0; i < n; i++) {
    const k = docsKeyBetween(prev, b);
    out.push(k);
    prev = k;
  }
  return out;
}

/** Byte-order comparison of positions, then ids (the server's ORDER BY position, id). */
export function docsComparePosition(a: { position: string; id: string }, b: { position: string; id: string }): number {
  if (a.position !== b.position) return a.position < b.position ? -1 : 1;
  return a.id < b.id ? -1 : a.id > b.id ? 1 : 0;
}
