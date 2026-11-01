"use client";

import { useState, type ReactNode } from "react";
import { cn } from "../lib/cn";

const sizes = {
  xs: { box: "size-6 text-caption", px: 24 },
  sm: { box: "size-8 text-caption", px: 32 },
  md: { box: "size-10 text-label", px: 40 },
  lg: { box: "size-14 text-title-sm", px: 56 },
  xl: { box: "size-20 text-title", px: 80 },
  "2xl": { box: "size-28 text-headline", px: 112 },
} as const;

export type AvatarSize = keyof typeof sizes;

/** What a custom image renderer receives (e.g. a `next/image` adapter). */
export interface AvatarImageProps {
  src: string;
  alt: string;
  /** Rendered size in CSS px (square). */
  size: number;
  className: string;
  onError: () => void;
}

export interface AvatarProps {
  src?: string | null;
  /** Full name: initials, alt text and the fallback tint. */
  name: string;
  size?: AvatarSize;
  /** Ring in the surface colour (stacked avatars). */
  ring?: boolean;
  className?: string;
  /** Content instead of initials. */
  fallback?: ReactNode;
  /** Render the photo yourself (`next/image`). Defaults to a lazy `<img>`. */
  renderImage?: (props: AvatarImageProps) => ReactNode;
}

function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  const first = parts[0]?.[0] ?? "";
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? "") : "";
  return (first + last).toUpperCase() || "?";
}

const tints = ["bg-primary-soft text-primary-strong", "bg-accent-soft text-accent-strong", "bg-info-soft text-info-strong", "bg-success-soft text-success-strong", "bg-warning-soft text-warning-strong"];
function tintFor(name: string) {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) | 0;
  return tints[Math.abs(h) % tints.length];
}

/** Photo with an initials fallback tinted from the palette's soft colours. */
export function Avatar({ src, name, size = "md", ring, className, fallback, renderImage }: AvatarProps) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showImg = !!src && failedSrc !== src;
  const s = sizes[size];
  const imgProps: AvatarImageProps | null = showImg ? { src, alt: name, size: s.px, className: "size-full object-cover", onError: () => setFailedSrc(src) } : null;
  return (
    <span
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-pill font-semibold",
        s.box,
        !showImg && tintFor(name),
        ring && "ring-2 ring-surface",
        className,
      )}
    >
      {imgProps ? (
        renderImage ? (
          renderImage(imgProps)
        ) : (
          <img src={imgProps.src} alt={imgProps.alt} width={s.px} height={s.px} className={imgProps.className} onError={imgProps.onError} loading="lazy" decoding="async" />
        )
      ) : (
        (fallback ?? (
          <span role="img" aria-label={name}>
            {initials(name)}
          </span>
        ))
      )}
    </span>
  );
}
