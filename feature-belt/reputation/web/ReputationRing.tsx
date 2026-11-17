"use client";

import { REPUTATION_STATUS_LABEL, reputationTierLabel, useReputationSummary, type ReputationStatus } from "@scaffoldapp/shared";
import { Skeleton, cn, motion, useReducedMotion } from "@scaffoldapp/ui-web";

const TIER_STROKE: Record<string, string> = {
  building: "stroke-fg-muted",
  steady: "stroke-info",
  strong: "stroke-success",
  elite: "stroke-primary",
};

function numberClass(size: number): string {
  if (size >= 72) return "type-headline";
  if (size >= 48) return "type-title-sm";
  return "type-caption";
}

export interface ReputationRingProps {
  /** Null unless rated. */
  score: number | null | undefined;
  status: ReputationStatus;
  tier: string | null | undefined;
  /** Diameter in px. @default 40 */
  size?: number;
  /** The tier (or status) under the ring. */
  showLabel?: boolean;
  className?: string;
}

/**
 * A score as a ring filled to score/100, coloured by tier. Unrated and under-review people get
 * a dashed ring with "–" (under review in the warning colour).
 */
export function ReputationRing({ score, status, tier, size = 40, showLabel, className }: ReputationRingProps) {
  const reduce = useReducedMotion();
  const stroke = Math.max(3, Math.round(size / 12));
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const rated = status === "rated" && typeof score === "number";
  const frac = rated ? Math.max(0, Math.min(100, score)) / 100 : 0;
  const label = rated ? reputationTierLabel(tier) : REPUTATION_STATUS_LABEL[status];
  const aria = rated ? `Reputation ${score} of 100, ${label}` : `Reputation: ${label.toLowerCase()}`;
  return (
    <span role="img" aria-label={aria} className={cn("inline-flex shrink-0 flex-col items-center gap-1", className)}>
      <span className="relative inline-grid place-items-center" style={{ width: size, height: size }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="-rotate-90" aria-hidden>
          <circle
            cx={size / 2}
            cy={size / 2}
            r={r}
            fill="none"
            strokeWidth={stroke}
            className={status === "under_review" ? "stroke-warning" : "stroke-border-strong"}
            strokeDasharray={rated ? undefined : `${stroke} ${stroke}`}
          />
          {rated && (
            <motion.circle
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              strokeWidth={stroke}
              strokeLinecap="round"
              className={TIER_STROKE[tier ?? ""] ?? "stroke-primary"}
              strokeDasharray={c}
              initial={reduce ? false : { strokeDashoffset: c }}
              animate={{ strokeDashoffset: c * (1 - frac) }}
              transition={reduce ? { duration: 0 } : { duration: 0.56, ease: [0.2, 0.8, 0.2, 1] }}
            />
          )}
        </svg>
        <span aria-hidden className={cn("absolute font-semibold tabular-nums", numberClass(size), rated ? "text-fg" : "text-fg-muted")}>
          {rated ? score : "–"}
        </span>
      </span>
      {showLabel && (
        <span aria-hidden className={cn("type-caption", status === "under_review" ? "text-warning-strong" : "text-fg-muted")}>
          {label}
        </span>
      )}
    </span>
  );
}

/**
 * A person's ring, fetched by id: place it on profiles and list rows (lists prime the cache with
 * `useReputationSummaries`). Renders nothing when it can't load.
 */
export function ReputationBadge({ userId, size = 32, showLabel, className }: { userId: string; size?: number; showLabel?: boolean; className?: string }) {
  const q = useReputationSummary(userId);
  if (q.isPending) return <Skeleton shape="circle" className={className} style={{ width: size, height: size }} />;
  if (q.isError) return null;
  return <ReputationRing score={q.data.score} status={q.data.status} tier={q.data.tier} size={size} {...(showLabel ? { showLabel } : {})} {...(className ? { className } : {})} />;
}
