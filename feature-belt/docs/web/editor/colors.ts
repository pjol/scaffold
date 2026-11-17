/**
 * Block and text colours as token classes (swatches in @scaffoldapp/tokens). Literal class names so Tailwind finds
 * them: "red" → text, "red_background" → highlight.
 */
import { docsColorParts, type DocsColor, type DocsColorHue } from "@scaffoldapp/shared";

const TEXT: Record<DocsColorHue, string> = {
  gray: "text-swatch-gray",
  brown: "text-swatch-brown",
  orange: "text-swatch-orange",
  yellow: "text-swatch-yellow",
  green: "text-swatch-green",
  blue: "text-swatch-blue",
  purple: "text-swatch-purple",
  pink: "text-swatch-pink",
  red: "text-swatch-red",
};

const BG: Record<DocsColorHue, string> = {
  gray: "bg-swatch-gray-bg",
  brown: "bg-swatch-brown-bg",
  orange: "bg-swatch-orange-bg",
  yellow: "bg-swatch-yellow-bg",
  green: "bg-swatch-green-bg",
  blue: "bg-swatch-blue-bg",
  purple: "bg-swatch-purple-bg",
  pink: "bg-swatch-pink-bg",
  red: "bg-swatch-red-bg",
};

/** Classes for a colour: text colour, or a background (`inline`: a highlight with a little padding). */
export function colorClass(c: DocsColor | null | undefined, inline = false): string {
  if (!c) return "";
  const [hue, bg] = docsColorParts(c);
  if (!bg) return TEXT[hue];
  return inline ? `${BG[hue]} rounded-xs` : `${BG[hue]} rounded-sm`;
}

/** The swatch dot a colour picker shows (text colours as a letter, backgrounds as a filled square). */
export function swatchClasses(hue: DocsColorHue): { text: string; bg: string } {
  return { text: TEXT[hue], bg: BG[hue] };
}
