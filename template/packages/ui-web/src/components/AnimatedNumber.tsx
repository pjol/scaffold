"use client";

import { motion } from "motion/react";
import { memo } from "react";
import { cn } from "../lib/cn";
import { formatInteger } from "../lib/format";
import { useMotionTokens } from "../motion/tokens";

export interface AnimatedNumberProps {
  value: number;
  /** Number → text. Digits roll; everything else ($ , . %) stays put. */
  format?: (value: number) => string;
  /** Turn the roll off (plain text). @default true */
  animate?: boolean;
  className?: string;
  /** Spring for the roll. @default "snappy" */
  spring?: "snappy" | "gentle" | "bouncy";
  "aria-label"?: string;
}

const DIGITS = ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9"];
const H = 1.2; // em per digit row

const Digit = memo(function Digit({ d, transition }: { d: number; transition: object }) {
  return (
    <span className="relative inline-block overflow-hidden align-bottom" style={{ height: `${H}em` }}>
      <span className="invisible block" style={{ lineHeight: `${H}em` }}>
        0
      </span>
      <motion.span className="absolute inset-x-0 top-0 flex flex-col" initial={false} animate={{ y: `${-d * H}em` }} transition={transition}>
        {DIGITS.map((n) => (
          <span key={n} className="block text-center" style={{ lineHeight: `${H}em`, height: `${H}em` }}>
            {n}
          </span>
        ))}
      </motion.span>
    </span>
  );
});

/**
 * Live number: each digit rolls to its new value on a spring (counts, totals). Tabular
 * figures, so widths don't jitter. Reduced motion: instant.
 */
export function AnimatedNumber({ value, format = formatInteger, animate = true, className, spring = "snappy", ...aria }: AnimatedNumberProps) {
  const t = useMotionTokens();
  const text = format(value);
  if (!animate) return <span className={cn("tabular-nums", className)}>{text}</span>;
  const chars = text.split("");
  return (
    <span className={cn("inline-flex items-end whitespace-nowrap tabular-nums", className)}>
      <span className="sr-only">{aria["aria-label"] ?? text}</span>
      {chars.map((ch, i) => {
        const key = chars.length - i; // align from the right so units keep their column
        return DIGITS.includes(ch) ? (
          <span key={`d${key}`} aria-hidden>
            <Digit d={Number(ch)} transition={t.spring[spring]} />
          </span>
        ) : (
          <span key={`c${key}-${ch}`} aria-hidden className="inline-block whitespace-pre" style={{ lineHeight: `${H}em`, height: `${H}em` }}>
            {ch}
          </span>
        );
      })}
    </span>
  );
}
