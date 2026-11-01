"use client";

import { motion, type HTMLMotionProps } from "motion/react";
import { forwardRef, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { Spinner } from "./Spinner";
import { Tooltip } from "./Tooltip";

export type IconButtonVariant = "primary" | "secondary" | "outline" | "ghost" | "soft" | "danger" | "inverse";
export type IconButtonSize = "sm" | "md" | "lg";

const variants: Record<IconButtonVariant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-[color-mix(in_oklab,var(--app-primary),var(--app-primary-fg)_12%)]",
  secondary: "bg-fg/8 text-fg hover:bg-fg/12",
  outline: "border border-border-strong text-fg hover:bg-sunken",
  ghost: "text-fg hover:bg-sunken",
  soft: "bg-primary-soft text-primary-strong hover:bg-[color-mix(in_oklab,var(--app-primary-soft),var(--app-primary)_12%)]",
  danger: "text-danger-strong hover:bg-danger-soft",
  inverse: "bg-inverse text-fg-inverse hover:opacity-90",
};

// Square, 32 / 40 / 48. sm and md extend their hit area to 44 px on coarse pointers.
const sizes: Record<IconButtonSize, string> = {
  sm: "size-control-sm [&_svg]:size-4 pointer-coarse:before:-inset-1.5",
  md: "size-control [&_svg]:size-5 pointer-coarse:before:-inset-0.5",
  lg: "size-control-lg [&_svg]:size-5",
};

export interface IconButtonProps extends Omit<HTMLMotionProps<"button">, "children"> {
  /** Accessible name (required); also the hover tooltip when `tooltip` is set. */
  label: string;
  /** The icon element, e.g. `<X />`. */
  icon: ReactNode;
  variant?: IconButtonVariant;
  size?: IconButtonSize;
  shape?: "circle" | "square";
  loading?: boolean;
  /** Show `label` in a tooltip on hover. */
  tooltip?: boolean;
}

/** Icon-only button with a required label and spring press. */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, icon, variant = "ghost", size = "md", shape = "circle", loading, tooltip, disabled, className, type, ...rest },
  ref,
) {
  const t = useMotionTokens();
  const isDisabled = disabled || loading;
  const btn = (
    <motion.button
      ref={ref}
      type={type ?? "button"}
      aria-label={label}
      aria-busy={loading || undefined}
      disabled={isDisabled}
      whileTap={isDisabled ? undefined : { scale: 0.92 }}
      transition={t.spring.press}
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center transition-colors duration-(--app-duration-fast)",
        "before:absolute before:inset-0 before:content-['']",
        "disabled:cursor-not-allowed disabled:opacity-50",
        shape === "circle" ? "rounded-pill" : "rounded-md",
        variants[variant],
        sizes[size],
        className,
      )}
      {...rest}
    >
      {loading ? <Spinner size={16} /> : icon}
    </motion.button>
  );
  return tooltip ? <Tooltip content={label}>{btn}</Tooltip> : btn;
});
