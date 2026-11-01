"use client";

import { CloudOff, Inbox, RotateCw } from "lucide-react";
import { motion } from "motion/react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { useMotionTokens } from "../motion/tokens";
import { Button } from "./Button";
import { InfoTip } from "./InfoTip";

export interface EmptyStateProps {
  /** A lucide icon element. */
  icon?: ReactNode;
  /** Short title: "No messages yet". No description: use `info`. */
  title: ReactNode;
  info?: ReactNode;
  /** One action (a Button). */
  action?: ReactNode;
  /** `primary` tints the icon tile. */
  tone?: "neutral" | "primary";
  compact?: boolean;
  className?: string;
}

/** Empty state: icon tile that springs in, a title, one action. */
export function EmptyState({ icon, title, info, action, tone = "neutral", compact, className }: EmptyStateProps) {
  const t = useMotionTokens();
  return (
    <div className={cn("flex flex-col items-center justify-center gap-4 text-center", compact ? "py-8" : "py-16", className)}>
      <motion.div
        initial={{ scale: 0.6, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={t.spring.bouncy}
        className={cn(
          "flex items-center justify-center rounded-xl",
          compact ? "size-12 [&_svg]:size-6" : "size-16 [&_svg]:size-8",
          tone === "primary" ? "bg-primary-soft text-primary-strong" : "bg-sunken text-fg-muted",
        )}
      >
        {icon ?? <Inbox strokeWidth={2} />}
      </motion.div>
      <h2 className={cn("flex items-center gap-2 text-fg", compact ? "type-body font-semibold" : "type-title-sm")}>
        {title}
        {info && <InfoTip content={info} size={compact ? "sm" : "md"} />}
      </h2>
      {action && <div className="flex">{action}</div>}
    </div>
  );
}

export interface ErrorStateProps {
  /** What failed: "Couldn't load your profile". @default "Couldn't load this" */
  title?: ReactNode;
  /** Detail behind the ⓘ (the API message, a reference). */
  info?: ReactNode;
  /** Shows a Retry button. */
  onRetry?: () => void;
  /** @default "Retry" */
  retryLabel?: ReactNode;
  retrying?: boolean;
  icon?: ReactNode;
  compact?: boolean;
  className?: string;
}

/** Error state: what failed + Retry; details live in the ⓘ. */
export function ErrorState({ title = "Couldn't load this", info, onRetry, retryLabel = "Retry", retrying, icon, compact, className }: ErrorStateProps) {
  const t = useMotionTokens();
  return (
    <div role="alert" className={cn("flex flex-col items-center justify-center gap-4 text-center", compact ? "py-8" : "py-16", className)}>
      <motion.div
        initial={{ scale: 0.6, opacity: 0 }}
        animate={{ scale: 1, opacity: 1 }}
        transition={t.spring.snappy}
        className={cn("flex items-center justify-center rounded-xl bg-danger-soft text-danger-strong", compact ? "size-12 [&_svg]:size-6" : "size-16 [&_svg]:size-8")}
      >
        {icon ?? <CloudOff strokeWidth={2} />}
      </motion.div>
      <h2 className={cn("flex items-center gap-2 text-fg", compact ? "type-body font-semibold" : "type-title-sm")}>
        {title}
        {info && <InfoTip content={info} size={compact ? "sm" : "md"} label="Error details" />}
      </h2>
      {onRetry && (
        <Button variant="secondary" leftIcon={<RotateCw />} onClick={onRetry} loading={retrying}>
          {retryLabel}
        </Button>
      )}
    </div>
  );
}
