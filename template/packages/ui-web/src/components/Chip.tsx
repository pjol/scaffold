"use client";

import { Check } from "lucide-react";
import { AnimatePresence, motion, type HTMLMotionProps } from "motion/react";
import { forwardRef, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";

const sizeClasses = {
  sm: "h-control-sm gap-1.5 px-3 text-label [&_svg]:size-4 pointer-coarse:before:-inset-y-1.5",
  md: "h-control gap-2 px-4 text-label [&_svg]:size-4 pointer-coarse:before:-inset-y-0.5",
} as const;

export interface ChipProps extends Omit<HTMLMotionProps<"button">, "children"> {
  icon?: ReactNode;
  /** Trailing count (results). */
  count?: number;
  selected?: boolean;
  size?: keyof typeof sizeClasses;
  children?: ReactNode;
}

/** Selectable filter or quick pick. */
export const Chip = forwardRef<HTMLButtonElement, ChipProps>(function Chip({ icon, count, selected, size = "md", className, children, type, ...rest }, ref) {
  const t = useMotionTokens();
  return (
    <motion.button
      ref={ref}
      type={type ?? "button"}
      whileTap={{ scale: t.pressScale }}
      transition={t.spring.press}
      data-selected={selected || undefined}
      className={cn(
        "relative inline-flex shrink-0 select-none items-center rounded-md border font-semibold transition-colors duration-(--app-duration-fast)",
        "before:absolute before:inset-0 before:content-['']",
        "disabled:cursor-not-allowed disabled:opacity-50",
        selected ? "border-primary bg-primary-soft text-primary-strong" : "border-border bg-surface text-fg hover:border-border-strong",
        sizeClasses[size],
        className,
      )}
      {...rest}
    >
      {icon}
      {children}
      {count !== undefined && <span className={cn("rounded-sm px-1.5 text-caption tabular-nums", selected ? "bg-primary text-primary-fg" : "bg-sunken text-fg-muted")}>{count}</span>}
    </motion.button>
  );
});

export interface ToggleChipProps extends Omit<ChipProps, "selected" | "onChange"> {
  pressed: boolean;
  onPressedChange: (pressed: boolean) => void;
}

/** A chip that toggles (`aria-pressed`); a check springs in when on. */
export const ToggleChip = forwardRef<HTMLButtonElement, ToggleChipProps>(function ToggleChip({ pressed, onPressedChange, icon, children, onClick, ...rest }, ref) {
  const t = useMotionTokens();
  return (
    <Chip
      ref={ref}
      selected={pressed}
      aria-pressed={pressed}
      onClick={(e) => {
        onClick?.(e);
        if (!e.defaultPrevented) onPressedChange(!pressed);
      }}
      icon={
        <AnimatePresence initial={false} mode="popLayout">
          {pressed ? (
            <motion.span key="check" className="inline-flex" initial={{ scale: 0 }} animate={{ scale: 1 }} exit={{ scale: 0 }} transition={t.spring.bouncy}>
              <Check strokeWidth={3} />
            </motion.span>
          ) : icon ? (
            <motion.span key="icon" className="inline-flex" initial={{ scale: 0.6, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} exit={{ scale: 0.6, opacity: 0 }} transition={t.spring.snappy}>
              {icon}
            </motion.span>
          ) : null}
        </AnimatePresence>
      }
      {...rest}
    >
      {children}
    </Chip>
  );
});
