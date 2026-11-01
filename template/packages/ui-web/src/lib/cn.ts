import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/**
 * tailwind-merge taught the token names from `@scaffoldapp/tokens/theme.css`, so
 * `text-caption` (a size) is never merged away against `text-fg` (a colour) and
 * `p-gutter` overrides `p-4`.
 */
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ["caption", "label", "body", "body-lg", "title-sm", "title", "headline", "display", "hero"],
      spacing: [
        "tight",
        "related",
        "group",
        "subsection",
        "section",
        "band",
        "gutter",
        "card",
        "control-sm",
        "control",
        "control-lg",
        "row",
        "topbar",
        "tap-sm",
        "tap",
        "tap-lg",
        "tap-xl",
      ],
      container: ["content", "form"],
      radius: ["pill"],
      ease: ["standard", "emphasized", "spring-snappy", "spring-gentle", "spring-bouncy", "spring-sheet", "spring-press"],
      animate: ["shimmer", "fade-in", "fade-out", "pop-in", "pop-out", "slide-up", "slide-down"],
    },
  },
});

/** Compose class names: clsx semantics plus Tailwind conflict resolution. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
