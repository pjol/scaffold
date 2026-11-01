/**
 * Platform-neutral motion helpers. These convert motion tokens into the shapes
 * each animation runtime expects — without importing either runtime.
 */
import type { BezierToken, SpringToken } from "./types";

/** `[0.2, 0, 0, 1]` → `"cubic-bezier(0.2, 0, 0, 1)"`. */
export function cubicBezierCss(b: BezierToken): string {
  return `cubic-bezier(${b.join(", ")})`;
}

/**
 * `motion` (web) transition object: `{ type: "spring", stiffness, damping, mass }`.
 * Spread it into `transition={...}`.
 */
export function motionSpring(s: SpringToken): {
  type: "spring";
  stiffness: number;
  damping: number;
  mass: number;
} {
  return { type: "spring", stiffness: s.stiffness, damping: s.damping, mass: s.mass };
}

/** Reanimated `withSpring` config. `mass` always explicit (Reanimated 4 defaults it to 4). */
export function reanimatedSpring(s: SpringToken): { stiffness: number; damping: number; mass: number } {
  return { stiffness: s.stiffness, damping: s.damping, mass: s.mass };
}

/** Damping ratio ζ (1 = critically damped, < 1 bounces). */
export function dampingRatio(s: SpringToken): number {
  return s.damping / (2 * Math.sqrt(s.stiffness * s.mass));
}

interface SpringSample {
  /** Settle time in ms. */
  duration: number;
  /** Position samples 0..~1 at even time steps. */
  points: number[];
}

/** Integrate a unit spring (0 → 1) until it settles. */
export function sampleSpring(s: SpringToken, samples = 48): SpringSample {
  const dt = 1 / 600; // seconds
  let x = 0;
  let v = 0;
  let t = 0;
  const trace: number[] = [0];
  // semi-implicit Euler; settles when both displacement and velocity are tiny
  while (t < 4) {
    const a = (-s.stiffness * (x - 1) - s.damping * v) / s.mass;
    v += a * dt;
    x += v * dt;
    t += dt;
    trace.push(x);
    if (Math.abs(x - 1) < 0.0015 && Math.abs(v) < 0.02) break;
  }
  const points: number[] = [];
  for (let i = 0; i <= samples; i++) {
    const idx = Math.round((i / samples) * (trace.length - 1));
    points.push(i === samples ? 1 : (trace[idx] ?? 1));
  }
  return { duration: Math.round(t * 1000), points };
}

/**
 * A CSS `linear()` easing that reproduces a spring, plus the duration to pair it with.
 * Lets plain CSS transitions (and Tailwind utilities) feel like the JS springs.
 */
export function springToCssLinear(s: SpringToken, samples = 40): { easing: string; duration: number } {
  const { duration, points } = sampleSpring(s, samples);
  const easing = `linear(${points.map((p) => Math.round(p * 1000) / 1000).join(", ")})`;
  return { easing, duration };
}
