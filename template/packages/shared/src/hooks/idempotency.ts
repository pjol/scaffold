import { isApiError } from "../api/errors";
import { generateId } from "../utils/id";
import { stableStringify } from "../utils/stable";

/**
 * Keeps one `Idempotency-Key` per submit *attempt* (payments, anything that must
 * not happen twice):
 *
 * - the same body retried after a network error / timeout / 5xx reuses the key
 *   (the server returns the original result if the first request got through);
 * - a different body, a success, or a definitive 4xx starts a new key.
 */
export class IdempotencyKeyManager {
  private key: string | null = null;
  private fingerprint: string | null = null;
  private readonly makeId: () => string;

  constructor(makeId: () => string = generateId) {
    this.makeId = makeId;
  }

  /** Key to send with `body`. */
  keyFor(body: unknown): string {
    const fp = stableStringify(body);
    if (this.key === null || this.fingerprint !== fp) {
      this.key = this.makeId();
      this.fingerprint = fp;
    }
    return this.key;
  }

  /** Record the outcome of the request that used the current key. */
  settle(outcome: { ok: true } | { ok: false; error: unknown }): void {
    if (outcome.ok) {
      this.reset();
      return;
    }
    const err = outcome.error;
    // Ambiguous failures: the write may have happened server-side, so keep the key for the retry.
    const ambiguous = !isApiError(err) || err.code === "network_error" || err.code === "timeout" || err.status >= 500;
    if (!ambiguous) this.reset();
  }

  /** Forget the current key (next call generates a new one). */
  reset(): void {
    this.key = null;
    this.fingerprint = null;
  }

  /** The key currently held, if any. */
  get current(): string | null {
    return this.key;
  }
}
