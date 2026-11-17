import type { DocsPageFont } from "@scaffoldapp/shared";

/**
 * Page typefaces and small text: they set the type tokens' family and body size on the page's
 * content, so every block that uses type-* utilities follows.
 */
export const FONT_CLASS: Record<DocsPageFont, string> = {
  default: "",
  serif:
    "[--app-type-body-family:var(--font-serif)] [--app-type-title-family:var(--font-serif)] [--app-type-title-sm-family:var(--font-serif)] [--app-type-headline-family:var(--font-serif)] [--app-type-display-family:var(--font-serif)] [--app-type-label-family:var(--font-serif)]",
  mono: "[--app-type-body-family:var(--font-mono)] [--app-type-title-family:var(--font-mono)] [--app-type-title-sm-family:var(--font-mono)] [--app-type-headline-family:var(--font-mono)] [--app-type-display-family:var(--font-mono)] [--app-type-label-family:var(--font-mono)]",
};
export const SMALL_TEXT_CLASS = "[--app-type-body-size:var(--app-type-label-size)] [--app-type-body-line:var(--app-type-label-line)]";
