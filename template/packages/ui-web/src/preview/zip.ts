/**
 * Lists a zip's contents from its central directory without unpacking anything. With the size
 * known, only the end of the file is read (two ranged requests), so a 4 GB archive lists from a
 * few kilobytes; servers that ignore Range fall back to reading the whole file under a cap.
 */
import { fetchBytes, TooLarge } from "./load";

export interface ZipEntry {
  path: string;
  size: number;
  compressed: number;
  modified: string;
  dir: boolean;
}

const WHOLE_MAX = 64 * 1024 * 1024;
const DIR_MAX = 32 * 1024 * 1024;
const MAX_ENTRIES = 20_000;

const dec = new TextDecoder("utf-8", { fatal: false });

function u16(b: Uint8Array, o: number) {
  return b[o]! | (b[o + 1]! << 8);
}
function u32(b: Uint8Array, o: number) {
  return (b[o]! | (b[o + 1]! << 8) | (b[o + 2]! << 16) | (b[o + 3]! << 24)) >>> 0;
}
function u64(b: Uint8Array, o: number) {
  return u32(b, o) + u32(b, o + 4) * 2 ** 32;
}

function dosDate(date: number, time: number): string {
  const y = ((date >> 9) & 0x7f) + 1980;
  const mo = (date >> 5) & 0xf;
  const d = date & 0x1f;
  if (!mo || !d) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${y}-${pad(mo)}-${pad(d)} ${pad(time >> 11)}:${pad((time >> 5) & 0x3f)}`;
}

/** Where the central directory is, from the bytes at the end of the file (`tailStart` = their offset). */
function locate(tail: Uint8Array, tailStart: number): { offset: number; size: number; count: number } {
  for (let i = tail.length - 22; i >= 0; i--) {
    if (u32(tail, i) !== 0x06054b50) continue;
    let count = u16(tail, i + 10);
    let size = u32(tail, i + 12);
    let offset = u32(tail, i + 16);
    if ((offset === 0xffffffff || size === 0xffffffff || count === 0xffff) && i >= 20 && u32(tail, i - 20) === 0x07064b50) {
      const rec = u64(tail, i - 20 + 8) - tailStart;
      if (rec < 0 || rec + 56 > tail.length || u32(tail, rec) !== 0x06064b50) throw new Error("zip64 record out of reach");
      count = u64(tail, rec + 32);
      size = u64(tail, rec + 40);
      offset = u64(tail, rec + 48);
    }
    return { offset, size, count };
  }
  throw new Error("not a zip file");
}

function parseDirectory(cd: Uint8Array): { entries: ZipEntry[]; truncated: boolean } {
  const entries: ZipEntry[] = [];
  let o = 0;
  while (o + 46 <= cd.length && u32(cd, o) === 0x02014b50) {
    if (entries.length >= MAX_ENTRIES) return { entries, truncated: true };
    const nameLen = u16(cd, o + 28);
    const extraLen = u16(cd, o + 30);
    const commentLen = u16(cd, o + 32);
    let compressed = u32(cd, o + 20);
    let size = u32(cd, o + 24);
    const path = dec.decode(cd.subarray(o + 46, o + 46 + nameLen));
    // Zip64 sizes live in extra field 0x0001.
    let e = o + 46 + nameLen;
    const end = e + extraLen;
    while (e + 4 <= end) {
      const id = u16(cd, e);
      const len = u16(cd, e + 2);
      if (id === 0x0001) {
        let p = e + 4;
        if (size === 0xffffffff) (size = u64(cd, p)), (p += 8);
        if (compressed === 0xffffffff) compressed = u64(cd, p);
      }
      e += 4 + len;
    }
    entries.push({ path, size, compressed, modified: dosDate(u16(cd, o + 14), u16(cd, o + 12)), dir: path.endsWith("/") });
    o = end + commentLen;
  }
  return { entries, truncated: false };
}

/** The zip's entries (at most 20,000). Throws `TooLarge` when the server can't serve ranges and the file is over 64 MB. */
export async function listZip(url: string, size: number | undefined, signal: AbortSignal): Promise<{ entries: ZipEntry[]; truncated: boolean }> {
  if (size && size > 4 * 1024 * 1024) {
    const start = Math.max(0, size - 65557);
    const tailRes = await fetchBytes(url, WHOLE_MAX, signal, { headers: { Range: `bytes=${start}-${size - 1}` } });
    if (tailRes.status === 206) {
      const loc = locate(tailRes.bytes, start);
      if (loc.size > DIR_MAX) throw new TooLarge();
      if (loc.offset >= start) return parseDirectory(tailRes.bytes.subarray(loc.offset - start, loc.offset - start + loc.size));
      const cd = await fetchBytes(url, DIR_MAX + 1, signal, { headers: { Range: `bytes=${loc.offset}-${loc.offset + loc.size - 1}` } });
      if (cd.status !== 206) throw new TooLarge();
      return parseDirectory(cd.bytes);
    }
    const loc = locate(tailRes.bytes, 0);
    return parseDirectory(tailRes.bytes.subarray(loc.offset, loc.offset + loc.size));
  }
  const { bytes } = await fetchBytes(url, WHOLE_MAX, signal);
  const loc = locate(bytes, 0);
  return parseDirectory(bytes.subarray(loc.offset, loc.offset + loc.size));
}
