export interface BackoffOptions {
  /** First delay (attempt 0), ms. */
  initialMs: number;
  /** Cap, ms. */
  maxMs: number;
  /** Growth factor per attempt. Default 2. */
  factor?: number;
  /**
   * Fraction of the delay that is randomized, 0…1. Default 0.5 ("equal
   * jitter": delay ∈ [d/2, d]). 0 = deterministic.
   */
  jitter?: number;
}

/**
 * Exponential backoff with jitter and a cap:
 * `d = min(maxMs, initialMs × factor^attempt)`, `delay = d × (1 − jitter + jitter × random)`.
 */
export function computeBackoff(attempt: number, options: BackoffOptions, random: () => number = Math.random): number {
  const factor = options.factor ?? 2;
  const jitter = Math.min(1, Math.max(0, options.jitter ?? 0.5));
  const exp = options.initialMs * Math.pow(factor, Math.max(0, attempt));
  const capped = Math.min(options.maxMs, exp);
  return Math.round(capped * (1 - jitter + jitter * random()));
}
