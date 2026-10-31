import { CircleAlert, CircleCheck, Info, TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type BannerTone = "info" | "success" | "warning" | "danger" | "neutral";

const tones: Record<BannerTone, string> = {
  info: "bg-info-soft text-info-strong",
  success: "bg-success-soft text-success-strong",
  warning: "bg-warning-soft text-warning-strong",
  danger: "bg-danger-soft text-danger-strong",
  neutral: "bg-sunken text-fg",
};

const icons: Record<BannerTone, ReactNode> = {
  info: <Info aria-hidden />,
  success: <CircleCheck aria-hidden />,
  warning: <TriangleAlert aria-hidden />,
  danger: <CircleAlert aria-hidden />,
  neutral: <Info aria-hidden />,
};

export interface BannerProps {
  tone?: BannerTone;
  /** One sentence: what is true and what to do. */
  children: ReactNode;
  /** One action (a small Button or link). */
  action?: ReactNode;
  icon?: ReactNode;
  className?: string;
}

/** Inline status message (suspended account, outdated build). Server-safe. */
export function Banner({ tone = "info", children, action, icon, className }: BannerProps) {
  return (
    <div role={tone === "danger" || tone === "warning" ? "alert" : "status"} className={cn("flex items-center gap-3 rounded-lg px-4 py-3 type-label", tones[tone], className)}>
      <span className="flex shrink-0 [&_svg]:size-5">{icon ?? icons[tone]}</span>
      <span className="min-w-0 flex-1 font-medium">{children}</span>
      {action && <span className="flex shrink-0">{action}</span>}
    </div>
  );
}
