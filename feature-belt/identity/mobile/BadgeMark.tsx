import type { IdentityBadgeKind } from "@scaffoldapp/shared";
import { useTheme } from "@scaffoldapp/ui-native";
import Svg, { Circle, Path, Polygon, Rect } from "react-native-svg";

/** What each badge kind means, for tooltips and settings. */
export const IDENTITY_BADGE_MEANING: Record<IdentityBadgeKind, string> = {
  official: "Run by this app's own team.",
  org: "A business or organization our staff checked.",
  notable: "A public figure or creator our staff checked.",
  paid: "Supports the app with a paid plan. Identity isn't checked.",
};

const SEAL = Array.from({ length: 16 }, (_, i) => {
  const a = (i / 16) * Math.PI * 2 - Math.PI / 2;
  const r = i % 2 ? 9.6 : 11.6;
  return `${(12 + r * Math.cos(a)).toFixed(2)},${(12 + r * Math.sin(a)).toFixed(2)}`;
}).join(" ");
const ROSETTE =
  "M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z";
const CHECK = "m8.5 12 2.5 2.5 5-5";

/** The mark for a badge kind: filled seal (official), square (org), rosette (notable), outlined circle (paid). Decorative. */
export function IdentityBadgeMark({ kind, size = 20 }: { kind: IdentityBadgeKind; size?: number }) {
  const c = useTheme().colors;
  const color = { official: c.primaryStrong, org: c.warningStrong, notable: c.infoStrong, paid: c.successStrong }[kind];
  const filled = kind !== "paid";
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24">
      {kind === "official" ? <Polygon points={SEAL} fill={color} /> : null}
      {kind === "org" ? <Rect x={2.5} y={2.5} width={19} height={19} rx={5} fill={color} /> : null}
      {kind === "notable" ? <Path d={ROSETTE} fill={color} /> : null}
      {kind === "paid" ? <Circle cx={12} cy={12} r={9.5} fill="none" stroke={color} strokeWidth={2} /> : null}
      <Path d={CHECK} fill="none" stroke={filled ? c.canvas : color} strokeWidth={2.25} strokeLinecap="round" strokeLinejoin="round" />
    </Svg>
  );
}

/** "Organization: Riverside Bikes", or the kind's label alone when the badge says nothing more. */
export function identityBadgeName(b: { label: string; kind_label: string }): string {
  return !b.label || b.label === b.kind_label ? b.kind_label : `${b.kind_label}: ${b.label}`;
}
