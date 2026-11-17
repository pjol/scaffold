/**
 * CSV and TSV for previews (RFC 4180: quoted fields, doubled quotes, newlines inside quotes).
 * Pure and portable.
 */

/** Rows parsed from `text`, stopping after `maxRows`; `truncated` says rows were left out. */
export function filePreviewParseCsv(text: string, maxRows = 100_000, delimiter = ","): { rows: string[][]; truncated: boolean } {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  const src = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
  for (let i = 0; i < src.length; i++) {
    const c = src[i];
    if (quoted) {
      if (c === '"') {
        if (src[i + 1] === '"') {
          field += '"';
          i++;
        } else quoted = false;
      } else field += c;
      continue;
    }
    if (c === '"' && field === "") quoted = true;
    else if (c === delimiter) {
      row.push(field);
      field = "";
    } else if (c === "\n" || c === "\r") {
      if (c === "\r" && src[i + 1] === "\n") i++;
      row.push(field);
      rows.push(row);
      row = [];
      field = "";
      if (rows.length >= maxRows) return { rows, truncated: i < src.length - 1 };
    } else field += c;
  }
  if (field !== "" || row.length > 0) {
    row.push(field);
    rows.push(row);
  }
  return { rows, truncated: false };
}

/** The delimiter for a file: tab for .tsv/.tab or when the first line has more tabs than commas. */
export function filePreviewCsvDelimiter(name: string, text: string): string {
  if (/\.(tsv|tab)$/i.test(name)) return "\t";
  const nl = text.indexOf("\n");
  const first = text.slice(0, nl < 0 ? 4000 : Math.min(nl, 4000));
  const tabs = first.split("\t").length;
  const commas = first.split(",").length;
  const semis = first.split(";").length;
  if (tabs > commas && tabs > semis) return "\t";
  if (semis > commas) return ";";
  return ",";
}
