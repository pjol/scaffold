import { cn } from "../lib/cn";

export interface SpinnerProps {
  /** px. @default 20 */
  size?: number;
  className?: string;
  /** Accessible label. Omit when a parent already announces loading (a busy button). */
  label?: string;
}

/** Ring spinner in `currentColor`. Reduced motion slows it to a stop (global CSS). */
export function Spinner({ size = 20, className, label }: SpinnerProps) {
  const sw = Math.max(2, size / 8);
  const r = (size - sw) / 2;
  const c = 2 * Math.PI * r;
  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      className={cn("shrink-0 animate-spin", className)}
      role={label ? "status" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="currentColor" strokeOpacity={0.22} strokeWidth={sw} />
      <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="currentColor" strokeWidth={sw} strokeLinecap="round" strokeDasharray={`${c * 0.28} ${c}`} />
    </svg>
  );
}
