"use client";

import { motionSpring, type MotionTokens, type Palette, type SpringName } from "@scaffoldapp/tokens";
import { useMemo } from "react";
import { usePalette } from "../theme/store";

export type { SpringName };

type Bezier = [number, number, number, number];

/** Palette motion tokens in `motion` units (seconds, ready-to-spread spring transitions). */
export interface MotionTokensWeb {
  spring: Record<SpringName, { type: "spring"; stiffness: number; damping: number; mass: number }>;
  /** Seconds. */
  duration: Record<keyof MotionTokens["duration"], number>;
  ease: Record<keyof MotionTokens["easing"], Bezier>;
  /** Seconds between staggered children. */
  stagger: number;
  pressScale: number;
}

/** Convert a palette's motion tokens into `motion` (web) units. */
export function motionTokens(palette: Palette): MotionTokensWeb {
  const m = palette.motion;
  const spring = Object.fromEntries(Object.entries(m.spring).map(([k, v]) => [k, motionSpring(v)])) as MotionTokensWeb["spring"];
  const duration = Object.fromEntries(Object.entries(m.duration).map(([k, v]) => [k, v / 1000])) as MotionTokensWeb["duration"];
  const ease = Object.fromEntries(Object.entries(m.easing).map(([k, v]) => [k, [...v] as Bezier])) as MotionTokensWeb["ease"];
  return { spring, duration, ease, stagger: m.stagger / 1000, pressScale: m.pressScale };
}

/** Motion tokens of the palette on screen. */
export function useMotionTokens(): MotionTokensWeb {
  const { palette } = usePalette();
  return useMemo(() => motionTokens(palette), [palette]);
}
