/** JSON for previews: pretty-printed, or the parser's complaint. Pure and portable. */

/** `{ ok: true, text }` with two-space indentation, or `{ ok: false, error }` when it isn't valid JSON. */
export function filePreviewFormatJson(text: string): { ok: true; text: string } | { ok: false; error: string } {
  const src = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
  try {
    return { ok: true, text: JSON.stringify(JSON.parse(src), null, 2) };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : "Not valid JSON" };
  }
}
