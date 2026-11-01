"use client";

import * as TabsPrimitive from "@radix-ui/react-tabs";
import { motion } from "motion/react";
import { createContext, useContext, useId, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { useControllableState } from "../lib/hooks";
import { useMotionTokens } from "../motion/tokens";

interface TabsCtx {
  value: string;
  variant: "underline" | "pill";
  layoutId: string;
}
const Ctx = createContext<TabsCtx | null>(null);

export interface TabsProps {
  /** Keep the active tab in the URL (route or `?tab=`) and pass it here, so refresh, back and share keep it. */
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  /** `underline` (page sections) or `pill` (compact switchers). @default "underline" */
  variant?: "underline" | "pill";
  children: ReactNode;
  className?: string;
}

/**
 * Radix Tabs (arrow keys move between tabs) with one indicator that glides between
 * them on a shared layout transition.
 */
export function Tabs({ value, defaultValue = "", onValueChange, variant = "underline", children, className }: TabsProps) {
  const [current, setCurrent] = useControllableState({ value, defaultValue, onChange: onValueChange });
  const layoutId = useId();
  return (
    <Ctx.Provider value={{ value: current, variant, layoutId }}>
      <TabsPrimitive.Root value={current} onValueChange={setCurrent} className={className}>
        {children}
      </TabsPrimitive.Root>
    </Ctx.Provider>
  );
}

export function TabsList({ children, className, "aria-label": ariaLabel }: { children: ReactNode; className?: string; "aria-label"?: string }) {
  const ctx = useContext(Ctx);
  return (
    <TabsPrimitive.List
      aria-label={ariaLabel}
      className={cn("relative flex", ctx?.variant === "pill" ? "inline-flex gap-1 rounded-md bg-sunken p-1" : "gap-6 border-b border-border", className)}
    >
      {children}
    </TabsPrimitive.List>
  );
}

export interface TabProps {
  value: string;
  children: ReactNode;
  icon?: ReactNode;
  /** Count after the label. */
  count?: number;
  disabled?: boolean;
  className?: string;
}

export function Tab({ value, children, icon, count, disabled, className }: TabProps) {
  const ctx = useContext(Ctx);
  const t = useMotionTokens();
  const active = ctx?.value === value;
  const pill = ctx?.variant === "pill";
  return (
    <TabsPrimitive.Trigger
      value={value}
      disabled={disabled}
      className={cn(
        "relative inline-flex shrink-0 items-center gap-2 whitespace-nowrap text-label font-semibold transition-colors duration-(--app-duration-fast) disabled:opacity-40 [&_svg]:size-4",
        pill ? "z-0 h-control-sm rounded-sm px-3 pointer-coarse:h-control" : "h-control pointer-coarse:h-tap",
        active ? "text-fg" : "text-fg-muted hover:text-fg",
        className,
      )}
    >
      {active && ctx && (
        <motion.span
          layoutId={ctx.layoutId}
          transition={t.spring.snappy}
          className={pill ? "absolute inset-0 -z-10 rounded-sm border border-border bg-surface" : "absolute inset-x-0 -bottom-px h-0.5 rounded-pill bg-primary"}
        />
      )}
      {icon}
      {children}
      {count !== undefined && <span className={cn("rounded-sm px-1.5 text-caption tabular-nums", active ? "bg-primary text-primary-fg" : "bg-sunken text-fg-muted")}>{count}</span>}
    </TabsPrimitive.Trigger>
  );
}

export function TabPanel({ value, children, className }: { value: string; children: ReactNode; className?: string }) {
  const t = useMotionTokens();
  return (
    <TabsPrimitive.Content value={value} className={cn("outline-none", className)}>
      <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ ...t.spring.gentle, opacity: { duration: t.duration.fast } }}>
        {children}
      </motion.div>
    </TabsPrimitive.Content>
  );
}
