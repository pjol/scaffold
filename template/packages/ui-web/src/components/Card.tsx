"use client";

import { motion, type HTMLMotionProps } from "motion/react";
import { forwardRef, type KeyboardEvent } from "react";
import { cn } from "../lib/cn";
import { Slot } from "../lib/slot";
import { useMotionTokens } from "../motion/tokens";

const MotionSlot = motion.create(Slot);

export type CardVariant = "surface" | "raised" | "sunken" | "inverse";

// Resting cards separate with a border or a surface step, never a shadow (design.md).
const variants: Record<CardVariant, string> = {
  surface: "border border-border bg-surface",
  raised: "border border-border bg-surface-raised",
  sunken: "bg-sunken",
  inverse: "bg-inverse text-fg-inverse",
};

/** `card` = 16 → 24 (design.md default), `compact` = 16, `none` = 0. */
const paddings = { none: "", compact: "p-4", card: "p-card" } as const;

export interface CardProps extends HTMLMotionProps<"div"> {
  variant?: CardVariant;
  /** @default "card" */
  padding?: keyof typeof paddings;
  /** Hover outline + spring press. Use with `asChild` (a link) or `onClick`. */
  interactive?: boolean;
  /** Selected outline (a chosen option). */
  selected?: boolean;
  asChild?: boolean;
}

/**
 * Surface container. Interactive cards strengthen their border on hover and press down
 * on tap.
 *
 * ```tsx
 * <Card interactive asChild><Link href="/settings">…</Link></Card>
 * ```
 */
export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { variant = "surface", padding = "card", interactive, selected, asChild, className, onClick, onKeyDown, ...rest },
  ref,
) {
  const t = useMotionTokens();
  const classes = cn(
    "relative rounded-lg text-left",
    variants[variant],
    paddings[padding],
    interactive && "cursor-pointer transition-colors duration-(--app-duration-fast) hover:border-border-strong",
    selected && "outline-2 outline-primary outline-solid",
    className,
  );
  const motionProps = interactive ? { whileTap: { scale: 0.985 }, transition: t.spring.snappy } : {};
  if (asChild) return <MotionSlot ref={ref as never} className={classes} {...motionProps} {...(rest as object)} />;
  const clickable = interactive && onClick;
  return (
    <motion.div
      ref={ref}
      className={classes}
      onClick={onClick}
      {...(clickable
        ? {
            role: "button",
            tabIndex: 0,
            onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => {
              onKeyDown?.(e);
              if (!e.defaultPrevented && (e.key === "Enter" || e.key === " ")) {
                e.preventDefault();
                e.currentTarget.click();
              }
            },
          }
        : onKeyDown
          ? { onKeyDown }
          : {})}
      {...motionProps}
      {...rest}
    />
  );
});
