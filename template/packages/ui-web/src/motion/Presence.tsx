"use client";

import { AnimatePresence, motion, type HTMLMotionProps } from "motion/react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "./tokens";

export interface PresenceProps extends Omit<HTMLMotionProps<"div">, "children"> {
  /** Mount (fade + slide in) or unmount (fade + slide out). */
  show: boolean;
  children: ReactNode;
  /** Vertical offset in px while entering and leaving. @default 8 */
  y?: number;
  /** Animate the first render too. @default false */
  initial?: boolean;
}

/**
 * Enter/exit for anything that appears on a state change (banners, inline errors,
 * results): fade plus an 8 px slide on the gentle spring. Reduced motion: opacity only.
 */
export function Presence({ show, children, y = 8, initial = false, transition, ...rest }: PresenceProps) {
  const t = useMotionTokens();
  return (
    <AnimatePresence initial={initial}>
      {show && (
        <motion.div
          key="presence"
          initial={{ opacity: 0, y }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -y / 2, transition: { duration: t.duration.fast, ease: t.ease.in } }}
          transition={{ ...t.spring.gentle, opacity: { duration: t.duration.base, ease: t.ease.out }, ...transition }}
          {...rest}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

export interface CollapseProps {
  open: boolean;
  children: ReactNode;
  className?: string;
}

/** Animates height 0 ↔ auto (field errors, expandable sections). */
export function Collapse({ open, children, className }: CollapseProps) {
  const t = useMotionTokens();
  return (
    <AnimatePresence initial={false}>
      {open && (
        <motion.div
          key="collapse"
          className={cn("overflow-hidden", className)}
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: "auto", opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          transition={{ ...t.spring.snappy, opacity: { duration: t.duration.fast } }}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

export interface SwapTextProps {
  /** The text to show; a new value cross-fades and slides in. */
  children: string | number;
  className?: string;
  /** Direction the old text leaves. @default "up" */
  direction?: "up" | "down";
}

/**
 * Animated text swap for live labels ("Saving" → "Saved"). The slot for OriginKit's
 * `textmorph` / `scrambletext` (see `motion/originkit/README.md`).
 */
export function SwapText({ children, className, direction = "up" }: SwapTextProps) {
  const t = useMotionTokens();
  const d = direction === "up" ? 1 : -1;
  return (
    <span className={cn("relative inline-grid overflow-hidden align-bottom", className)}>
      <AnimatePresence mode="popLayout" initial={false}>
        <motion.span
          key={String(children)}
          className="col-start-1 row-start-1"
          initial={{ y: `${60 * d}%`, opacity: 0, filter: "blur(4px)" }}
          animate={{ y: "0%", opacity: 1, filter: "blur(0px)" }}
          exit={{ y: `${-60 * d}%`, opacity: 0, filter: "blur(4px)" }}
          transition={{ ...t.spring.snappy, opacity: { duration: t.duration.fast } }}
        >
          {children}
        </motion.span>
      </AnimatePresence>
    </span>
  );
}
