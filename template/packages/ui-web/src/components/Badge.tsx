"use client";

import { motion } from "motion/react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";

export type BadgeTone = "neutral" | "primary" | "accent" | "success" | "warning" | "danger" | "info";
export type BadgeVariant = "soft" | "solid" | "outline";

const toneClasses: Record<BadgeTone, Record<BadgeVariant, string>> = {
  neutral: { soft: "bg-sunken text-fg-muted", solid: "bg-inverse text-fg-inverse", outline: "border-border-strong text-fg-muted" },
  primary: { soft: "bg-primary-soft text-primary-strong", solid: "bg-primary text-primary-fg", outline: "border-primary-strong text-primary-strong" },
  accent: { soft: "bg-accent-soft text-accent-strong", solid: "bg-accent text-accent-fg", outline: "border-accent-strong text-accent-strong" },
  success: { soft: "bg-success-soft text-success-strong", solid: "bg-success text-success-fg", outline: "border-success-strong text-success-strong" },
  warning: { soft: "bg-warning-soft text-warning-strong", solid: "bg-warning text-warning-fg", outline: "border-warning-strong text-warning-strong" },
  danger: { soft: "bg-danger-soft text-danger-strong", solid: "bg-danger text-danger-fg", outline: "border-danger-strong text-danger-strong" },
  info: { soft: "bg-info-soft text-info-strong", solid: "bg-info text-info-fg", outline: "border-info-strong text-info-strong" },
};
const toneDot: Record<BadgeTone, string> = {
  neutral: "bg-fg-subtle",
  primary: "bg-primary",
  accent: "bg-accent",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
  info: "bg-info",
};

const sizeClasses = {
  sm: "h-6 gap-1 px-2 text-caption [&_svg]:size-3",
  md: "h-8 gap-1.5 px-3 text-label [&_svg]:size-4",
} as const;

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: BadgeTone;
  variant?: BadgeVariant;
  size?: keyof typeof sizeClasses;
  icon?: ReactNode;
  /** Leading dot (status). Pair it with text: never information by colour alone. */
  dot?: boolean;
  /** Pop in on a bouncy spring on mount (pass a changing `key` to replay). */
  pop?: boolean;
}

/** Compact state label: tones × soft / solid / outline. */
export function Badge({ tone = "neutral", variant = "soft", size = "sm", icon, dot, pop, className, children, ...rest }: BadgeProps) {
  const t = useMotionTokens();
  const content = (
    <>
      {dot && <span aria-hidden className={cn("inline-flex size-2 shrink-0 rounded-pill", variant === "solid" ? "bg-current" : toneDot[tone])} />}
      {icon}
      {children}
    </>
  );
  const classes = cn(
    "inline-flex shrink-0 items-center whitespace-nowrap rounded-sm font-semibold leading-none",
    variant === "outline" && "border",
    toneClasses[tone][variant],
    sizeClasses[size],
    className,
  );
  if (pop) {
    return (
      <motion.span className={classes} initial={{ scale: 0.6, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} transition={t.spring.bouncy} {...(rest as object)}>
        {content}
      </motion.span>
    );
  }
  return (
    <span className={classes} {...rest}>
      {content}
    </span>
  );
}
