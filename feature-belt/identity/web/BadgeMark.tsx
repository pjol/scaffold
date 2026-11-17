import type { IdentityBadgeKind } from "@scaffoldapp/shared";
import { cn } from "@scaffoldapp/ui-web";

/** What each badge kind means, for tooltips and settings. */
export const IDENTITY_BADGE_MEANING: Record<IdentityBadgeKind, string> = {
  official: "Run by this app's own team.",
  org: "A business or organization our staff checked.",
  notable: "A public figure or creator our staff checked.",
  paid: "Supports the app with a paid plan. Identity isn't checked.",
};

const TONE: Record<IdentityBadgeKind, string> = {
  official: "text-primary-strong",
  org: "text-warning-strong",
  notable: "text-info-strong",
  paid: "text-success-strong",
};

/** A 16-point scalloped seal: official, so it never reads as just another tick. */
const SEAL = Array.from({ length: 16 }, (_, i) => {
  const a = (i / 16) * Math.PI * 2 - Math.PI / 2;
  const r = i % 2 ? 9.6 : 11.6;
  return `${(12 + r * Math.cos(a)).toFixed(2)},${(12 + r * Math.sin(a)).toFixed(2)}`;
}).join(" ");
const ROSETTE =
  "M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z";
const CHECK = "m8.5 12 2.5 2.5 5-5";

/**
 * The mark for a badge kind, distinct in shape as well as colour: filled seal (official),
 * filled square (org), filled rosette (notable), outlined circle (paid). Decorative: label it
 * where it's used (`VerifiedBadge` does).
 */
export function IdentityBadgeMark({ kind, className }: { kind: IdentityBadgeKind; className?: string }) {
  const filled = kind !== "paid";
  return (
    <svg viewBox="0 0 24 24" className={cn("size-5 shrink-0", TONE[kind], className)} aria-hidden focusable="false">
      {kind === "official" ? <polygon points={SEAL} fill="currentColor" /> : null}
      {kind === "org" ? <rect x="2.5" y="2.5" width="19" height="19" rx="5" fill="currentColor" /> : null}
      {kind === "notable" ? <path d={ROSETTE} fill="currentColor" /> : null}
      {kind === "paid" ? <circle cx="12" cy="12" r="9.5" fill="none" stroke="currentColor" strokeWidth="2" /> : null}
      <path d={CHECK} fill="none" stroke={filled ? "var(--color-canvas)" : "currentColor"} strokeWidth="2.25" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/** "Organization: Riverside Bikes", or the kind's label alone when the badge says nothing more. */
export function identityBadgeName(b: { label: string; kind_label: string }): string {
  return !b.label || b.label === b.kind_label ? b.kind_label : `${b.kind_label}: ${b.label}`;
}
