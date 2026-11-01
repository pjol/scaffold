/**
 * Current legal document versions. The backend requires exactly these strings
 * (`backend/internal/legal/legal.go`); bump both sides together and every user accepts
 * again on their next visit. `pnpm check` fails when the two differ.
 */
export const TERMS_VERSION = "2026-01-01";
export const PRIVACY_VERSION = "2026-01-01";
