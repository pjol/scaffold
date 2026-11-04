import type { CSSProperties } from "react";
import { cn } from "../lib/cn";

export interface BrandMarkProps {
  /** The logo file the app serves, e.g. `/brand/github.svg` (web/public/brand/; registry in src/lib/brands.ts). */
  src: string;
  /** A one-colour mark: drawn as a mask and painted with `fill`. Leave false for a full-colour logo, shown as it is. */
  mask?: boolean;
  /** Paint for a mask: a token colour (`var(--app-primary)`), a gradient, any CSS background. @default currentColor */
  fill?: string;
  /** Pixels; omit to size with `className`. */
  size?: number;
  /** Accessible name. Omit when the name is written next to the mark (it is then hidden from screen readers). */
  label?: string;
  className?: string;
}

/**
 * A real organisation's or product's logo. One-colour marks are CSS masks, so the surface decides
 * the colour (text colour on a chip, a metal gradient on a badge) and they follow palette and mode;
 * full-colour logos are plain images. A sheen over a mark is a second BrandMark with the same `src`,
 * a gradient `fill` and a blend mode.
 */
export function BrandMark({ src, mask = false, fill, size, label, className }: BrandMarkProps) {
  const box = size ? { width: size, height: size } : undefined;
  const a11y = label ? { role: "img" as const, "aria-label": label } : { "aria-hidden": true as const };
  if (mask) {
    const url = `url("${src}")`;
    const style: CSSProperties = {
      ...box,
      background: fill ?? "currentColor",
      WebkitMask: `${url} center / contain no-repeat`,
      mask: `${url} center / contain no-repeat`,
    };
    return <span className={cn("block shrink-0", className)} style={style} {...a11y} />;
  }
  return (
    // eslint-disable-next-line @next/next/no-img-element -- local files at their final size; next/image adds nothing here
    <img src={src} alt={label ?? ""} className={cn("block shrink-0 object-contain", className)} style={box} loading="lazy" decoding="async" draggable={false} />
  );
}
