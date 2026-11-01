interface CryptoLike {
  randomUUID?: () => string;
  getRandomValues?: <T extends ArrayBufferView | null>(array: T) => T;
}

function hex(byte: number): string {
  return (byte < 16 ? "0" : "") + byte.toString(16);
}

/**
 * RFC 4122 v4 UUID that works everywhere: `crypto.randomUUID` when present,
 * else `crypto.getRandomValues` (browsers, RN with expo-crypto /
 * react-native-get-random-values), else `Math.random` (Hermes without a
 * polyfill — fine for idempotency keys and optimistic ids, not for secrets).
 */
export function generateId(): string {
  const c = (globalThis as { crypto?: CryptoLike }).crypto;
  if (c && typeof c.randomUUID === "function") {
    try {
      return c.randomUUID();
    } catch {
      // fall through (e.g. insecure context)
    }
  }
  const bytes = new Uint8Array(16);
  let filled = false;
  if (c && typeof c.getRandomValues === "function") {
    try {
      c.getRandomValues(bytes);
      filled = true;
    } catch {
      filled = false;
    }
  }
  if (!filled) for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  bytes[6] = (bytes[6]! & 0x0f) | 0x40; // version 4
  bytes[8] = (bytes[8]! & 0x3f) | 0x80; // variant 10
  let out = "";
  for (let i = 0; i < 16; i++) {
    if (i === 4 || i === 6 || i === 8 || i === 10) out += "-";
    out += hex(bytes[i]!);
  }
  return out;
}

/** True for a canonical UUID string (any version). */
export function isUuid(value: unknown): value is string {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value);
}
