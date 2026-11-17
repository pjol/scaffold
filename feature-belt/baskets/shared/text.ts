/**
 * Edits to a pasted list, matching how the server numbers it: plain lists count every
 * line (blank ones too); CSV/TSV lists count records (blank lines skipped, header = 1).
 */

const HEADER_WORDS = new Set(["symbol", "ticker", "market", "name", "asset", "item", "title"]);

/** Whether the server reads `text` as CSV/TSV (a header row with a market column). */
export function basketsIsCsv(text: string): boolean {
  const first = text.split(/\r\n|\r|\n/).find((l) => l.trim() !== "")?.trim() ?? "";
  const sep = first.includes("\t") ? "\t" : first.includes(",") ? "," : first.includes(";") ? ";" : null;
  if (!sep) return false;
  return first.split(sep).some((c) => HEADER_WORDS.has(c.trim().replace(/^"|"$/g, "").toLowerCase()));
}

/** Replace line `n` (as the preview numbers it) with `replacement`, or drop it when `replacement` is null. */
export function basketsReplaceLine(text: string, n: number, replacement: string | null): string {
  const lines = text.split(/\r\n|\r|\n/);
  let index = n - 1;
  if (basketsIsCsv(text)) {
    index = -1;
    let seen = 0;
    for (let i = 0; i < lines.length; i += 1) {
      if ((lines[i] ?? "").trim() === "") continue;
      seen += 1;
      if (seen === n) {
        index = i;
        break;
      }
    }
  }
  if (index < 0 || index >= lines.length) return text;
  if (replacement === null) lines.splice(index, 1);
  else lines[index] = replacement;
  return lines.join("\n");
}
