import { createElement, isValidElement, type ComponentType, type ReactNode } from "react";

type IconComponent = ComponentType<{ size?: number; color?: string; strokeWidth?: number }>;

/** An icon prop: a lucide-react-native component (`Bell`) or a ready element (`<Bell />`). */
export type IconLike = ReactNode | IconComponent;

function isComponent(x: unknown): x is IconComponent {
  return typeof x === "function" || (typeof x === "object" && x !== null && "$$typeof" in x && !isValidElement(x));
}

/** Render an {@link IconLike} at a size and colour (elements pass through untouched). */
export function renderIcon(icon: IconLike, opts: { size: number; color: string; strokeWidth?: number }): ReactNode {
  if (icon === null || icon === undefined || icon === false) return null;
  if (isComponent(icon)) return createElement(icon, { size: opts.size, color: opts.color, strokeWidth: opts.strokeWidth ?? 2 });
  return icon as ReactNode;
}
