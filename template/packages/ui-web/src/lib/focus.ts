// Initial focus for dialogs and sheets. Like Radix's own pick this skips links, and it
// also skips ⓘ triggers (`data-info-tip`): focusing one would pop its tooltip on open.

const CONTROLS = 'button, input, select, textarea, [tabindex], [contenteditable="true"]';
const NON_TEXT_INPUTS = new Set(["button", "checkbox", "color", "file", "image", "radio", "range", "reset", "submit"]);

/** The first enabled, visible, tabbable control inside `root`, ⓘ triggers excluded. */
export function firstControl(root: HTMLElement | null): HTMLElement | null {
  if (!root) return null;
  for (const el of root.querySelectorAll<HTMLElement>(CONTROLS)) {
    if (el.tabIndex < 0 || el.matches(':disabled, [data-info-tip], input[type="hidden"]')) continue;
    if (el.getClientRects().length === 0 || getComputedStyle(el).visibility === "hidden") continue;
    return el;
  }
  return null;
}

/** Focusing it would raise an on-screen keyboard on touch devices. */
export function isTextEntry(el: HTMLElement): boolean {
  if (el instanceof HTMLInputElement) return !NON_TEXT_INPUTS.has(el.type);
  return el instanceof HTMLTextAreaElement || el.isContentEditable;
}

/**
 * `onOpenAutoFocus` for a Radix Dialog: focus the first control in the body, then in the
 * footer (never the title's ⓘ or the close button); a text field only on fine pointers;
 * otherwise `fallback`, else the content itself. Radix calls it only when focus isn't
 * already inside, so a child with `autoFocus` keeps it.
 */
export function focusFirstControl(e: Event, body: HTMLElement | null, footer: HTMLElement | null, fallback?: HTMLElement | null): void {
  e.preventDefault();
  const control = firstControl(body) ?? firstControl(footer);
  const touch = window.matchMedia?.("(pointer: coarse)").matches ?? false;
  const target = control && !(touch && isTextEntry(control)) ? control : (fallback ?? null);
  const content = e.currentTarget instanceof HTMLElement ? e.currentTarget : null;
  (target ?? content)?.focus({ preventScroll: true });
  if (content && !content.contains(document.activeElement)) content.focus({ preventScroll: true });
}
