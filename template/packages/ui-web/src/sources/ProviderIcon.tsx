import { cn } from "../lib/cn";
import { sourceMark, sourceProductName } from "./marks";

export interface ProviderIconProps {
  /** Platform id: google, microsoft, github, gitlab, notion, hubspot, salesforce, zoom (others get a monogram). */
  provider: string;
  /** Product within the platform (drive, docs, calendar, meet…): its own mark when it has one. */
  product?: string | null;
  /** Pixels. @default 16 */
  size?: number;
  /** Accessible name. Omit when a visible label sits next to the icon (it is then hidden from screen readers). */
  title?: string;
  className?: string;
}

/**
 * A platform's mark in the current text colour (Simple Icons, CC0), or a neutral monogram tile
 * where no CC0 mark reads at this size (Microsoft, Salesforce, Zoom).
 */
export function ProviderIcon({ provider, product, size = 16, title, className }: ProviderIconProps) {
  const mark = sourceMark(provider, product);
  const a11y = title ? { role: "img" as const, "aria-label": title } : { "aria-hidden": true as const };
  if ("path" in mark) {
    return (
      <svg viewBox="0 0 24 24" width={size} height={size} fill="currentColor" className={cn("shrink-0", className)} {...a11y}>
        {title ? <title>{title}</title> : null}
        <path d={mark.path} />
      </svg>
    );
  }
  return (
    <span
      className={cn("inline-flex shrink-0 select-none items-center justify-center rounded-xs bg-sunken font-semibold leading-none text-fg-muted", className)}
      style={{ width: size, height: size, fontSize: Math.max(9, Math.round(size * 0.62)) }}
      {...a11y}
      title={title ?? sourceProductName(provider, product)}
    >
      {mark.monogram}
    </span>
  );
}
