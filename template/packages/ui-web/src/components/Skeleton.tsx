import type { CSSProperties, ReactNode } from "react";
import { cn } from "../lib/cn";

export interface SkeletonProps {
  className?: string;
  /** `rect` (default), `circle`, or `text` (one line at the current font size). */
  shape?: "rect" | "circle" | "text";
  width?: number | string;
  height?: number | string;
  style?: CSSProperties;
}

/** Shimmering placeholder shaped like the final content. Server-safe; static under reduced motion. */
export function Skeleton({ className, shape = "rect", width, height, style }: SkeletonProps) {
  return (
    <span
      aria-hidden
      className={cn("block shimmer animate-shimmer", shape === "circle" ? "rounded-pill" : shape === "text" ? "h-[1em] rounded-xs" : "rounded-md", className)}
      style={{ width, height, ...style }}
    />
  );
}

/** Lines of text (the last one shorter). */
export function SkeletonText({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <span className={cn("flex flex-col gap-2", className)} aria-hidden>
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} shape="text" className={i === lines - 1 && lines > 1 ? "w-3/5" : "w-full"} />
      ))}
    </span>
  );
}

/** A list row: leading circle, title, trailing value. */
export function SkeletonRow({ className, leading = true }: { className?: string; leading?: boolean }) {
  return (
    <span className={cn("flex items-center gap-3 px-4 py-2", leading ? "min-h-tap-lg" : "min-h-row", className)} aria-hidden>
      {leading && <Skeleton shape="circle" className="size-10 shrink-0" />}
      <Skeleton shape="text" className="h-4 flex-1" />
      <Skeleton shape="text" className="h-4 w-16" />
    </span>
  );
}

/** Wraps skeletons so screen readers hear one "Loading". */
export function SkeletonGroup({ children, label = "Loading", className }: { children: ReactNode; label?: string; className?: string }) {
  return (
    <div role="status" aria-label={label} aria-busy className={className}>
      {children}
    </div>
  );
}
