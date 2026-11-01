"use client";

import { motion } from "motion/react";
import { useId, useRef, useState, type KeyboardEvent } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";

const STAR = "M12 2.6l2.83 6.02 6.6.74-4.93 4.47 1.36 6.5L12 17.06 6.14 20.33l1.36-6.5L2.57 9.36l6.6-.74L12 2.6z";
const px = { sm: 16, md: 20, lg: 32 } as const;

export interface RatingStarsProps {
  /** 0–5. Fractions render partial stars in display mode. */
  value: number;
  /** Makes the stars an input (radio group). */
  onChange?: (value: 1 | 2 | 3 | 4 | 5) => void;
  size?: keyof typeof px;
  /** Show the number after the stars (display mode). */
  showValue?: boolean;
  /** Number of ratings, shown as "(128)". */
  count?: number;
  className?: string;
  "aria-label"?: string;
}

function StarShape({ fill, size, uid, i }: { fill: number; size: number; uid: string; i: number }) {
  const id = `${uid}-s${i}`;
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden className="shrink-0">
      <defs>
        <clipPath id={id}>
          <rect x="0" y="0" width={24 * fill} height="24" />
        </clipPath>
      </defs>
      <path d={STAR} fill="var(--app-sunken)" stroke="var(--app-border-strong)" strokeWidth={1.2} strokeLinejoin="round" />
      <path d={STAR} fill="var(--app-warning)" stroke="var(--app-warning)" strokeWidth={1.2} strokeLinejoin="round" clipPath={`url(#${id})`} />
    </svg>
  );
}

/** Star rating: display (fractional) or input (arrow keys, spring fill). */
export function RatingStars({ value, onChange, size = "md", showValue, count, className, ...aria }: RatingStarsProps) {
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const t = useMotionTokens();
  const s = px[size];
  const [hover, setHover] = useState<number | null>(null);
  const refs = useRef<(HTMLButtonElement | null)[]>([]);

  if (!onChange) {
    return (
      <span className={cn("inline-flex items-center gap-1", className)} role="img" aria-label={aria["aria-label"] ?? `${value.toFixed(1)} out of 5 stars`}>
        <span className="inline-flex gap-0.5">
          {[0, 1, 2, 3, 4].map((i) => (
            <StarShape key={i} i={i} uid={uid} size={s} fill={Math.max(0, Math.min(1, value - i))} />
          ))}
        </span>
        {showValue && <span className="ml-1 font-semibold tabular-nums text-fg">{value.toFixed(1)}</span>}
        {count !== undefined && <span className="tabular-nums text-fg-muted">({count})</span>}
      </span>
    );
  }

  const shown = hover ?? value;
  const pick = (n: number) => onChange(Math.min(5, Math.max(1, n)) as 1 | 2 | 3 | 4 | 5);
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const cur = Math.max(1, Math.round(value));
    let next = 0;
    if (e.key === "ArrowRight" || e.key === "ArrowUp") next = Math.min(5, cur + 1);
    if (e.key === "ArrowLeft" || e.key === "ArrowDown") next = Math.max(1, cur - 1);
    if (!next) return;
    e.preventDefault();
    pick(next);
    refs.current[next - 1]?.focus();
  };
  return (
    <div role="radiogroup" aria-label={aria["aria-label"] ?? "Rating"} className={cn("inline-flex gap-1", className)} onKeyDown={onKeyDown} onMouseLeave={() => setHover(null)}>
      {[1, 2, 3, 4, 5].map((n) => (
        <motion.button
          key={n}
          ref={(el) => {
            refs.current[n - 1] = el;
          }}
          type="button"
          role="radio"
          aria-checked={Math.round(value) === n}
          aria-label={`${n} star${n > 1 ? "s" : ""}`}
          tabIndex={Math.round(value) === n || (value === 0 && n === 1) ? 0 : -1}
          onClick={() => pick(n)}
          onMouseEnter={() => setHover(n)}
          whileTap={{ scale: 0.85 }}
          animate={{ scale: shown >= n ? 1.05 : 1 }}
          transition={t.spring.bouncy}
          className="relative inline-flex rounded-sm before:absolute before:-inset-1 before:content-['']"
        >
          <StarShape i={n} uid={uid} size={s} fill={shown >= n ? 1 : 0} />
        </motion.button>
      ))}
    </div>
  );
}
