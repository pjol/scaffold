import type { ApiErrorCode } from "../types";

/** Errors produced on the client side (no server envelope). `status` is 0 for these. */
export type ClientErrorCode =
  /** fetch rejected: offline, DNS, CORS, connection reset. */
  | "network_error"
  /** No response within `timeoutMs`. */
  | "timeout"
  /** The caller's AbortSignal fired (e.g. react-query cancelled the query). */
  | "aborted"
  /** 2xx with a body that isn't JSON. */
  | "invalid_response";

/** Core, client-side, or a feature's own code (features branch on their codes with `hasErrorCode`). */
export type AnyErrorCode = ApiErrorCode | ClientErrorCode | (string & {});

export interface ApiErrorInit {
  status: number;
  code: AnyErrorCode;
  message: string;
  details?: Record<string, unknown> | undefined;
  /** `X-Request-ID` response header, for support tickets and logs. */
  requestId?: string | undefined;
  /** Seconds from a `Retry-After` header (429 / 503). */
  retryAfterSeconds?: number | undefined;
  cause?: unknown;
}

/**
 * Every failure thrown by the API client: server errors parsed from the
 * `{"error":{code,message,details}}` envelope, plus client-side network, timeout and
 * abort failures (status 0).
 */
export class ApiError extends Error {
  override readonly name = "ApiError";
  /** HTTP status, or 0 for client-side failures. */
  readonly status: number;
  readonly code: AnyErrorCode;
  readonly details: Record<string, unknown> | undefined;
  readonly requestId: string | undefined;
  readonly retryAfterSeconds: number | undefined;

  constructor(init: ApiErrorInit) {
    super(init.message);
    this.status = init.status;
    this.code = init.code;
    this.details = init.details;
    this.requestId = init.requestId;
    this.retryAfterSeconds = init.retryAfterSeconds;
    if (init.cause !== undefined) (this as { cause?: unknown }).cause = init.cause;
    // Keep instanceof working when compiled to ES5-style classes.
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/**
 * Type guard. `instanceof` plus a structural fallback, so it still works when two copies
 * of this package end up in a bundle (Fast Refresh, duplicated deps).
 */
export function isApiError(error: unknown): error is ApiError {
  if (error instanceof ApiError) return true;
  if (typeof error !== "object" || error === null) return false;
  const e = error as { name?: unknown; status?: unknown; code?: unknown };
  return e.name === "ApiError" && typeof e.status === "number" && typeof e.code === "string";
}

/** True when `error` is an ApiError with the given code. */
export function hasErrorCode(error: unknown, code: AnyErrorCode): error is ApiError {
  return isApiError(error) && error.code === code;
}

export const isUnauthenticated = (error: unknown): error is ApiError => hasErrorCode(error, "unauthenticated");
export const isForbidden = (error: unknown): error is ApiError => hasErrorCode(error, "forbidden");
export const isNotFound = (error: unknown): error is ApiError => hasErrorCode(error, "not_found");
export const isConflict = (error: unknown): error is ApiError => hasErrorCode(error, "conflict");
export const isInvalidRequest = (error: unknown): error is ApiError => hasErrorCode(error, "invalid_request");
export const isRateLimited = (error: unknown): error is ApiError => hasErrorCode(error, "rate_limited");
export const isAccountSuspended = (error: unknown): error is ApiError => hasErrorCode(error, "account_suspended");
export const isEmailUnverified = (error: unknown): error is ApiError => hasErrorCode(error, "email_unverified");
/**
 * `403 consent_required`: the user hasn't accepted the current Terms / Privacy versions.
 * `ApiProvider` refetches `me` when this happens, so `useConsentGate()` flips to
 * `required` and the app shows its consent screen.
 */
export const isConsentRequired = (error: unknown): error is ApiError => hasErrorCode(error, "consent_required");
export const isAborted = (error: unknown): error is ApiError => hasErrorCode(error, "aborted");

/** Offline, unreachable or timed out: show a connectivity message and offer retry. */
export function isNetworkError(error: unknown): error is ApiError {
  return isApiError(error) && (error.code === "network_error" || error.code === "timeout");
}

const CLIENT_MESSAGES: Record<ClientErrorCode, string> = {
  network_error: "Can't connect right now. Check your connection and try again.",
  timeout: "This is taking too long. Try again.",
  aborted: "Request canceled.",
  invalid_response: "Something went wrong. Try again.",
};

/** Default message for a client-side code. */
export function clientErrorMessage(code: ClientErrorCode): string {
  return CLIENT_MESSAGES[code];
}

/** A user-presentable message for any thrown value (toasts, inline errors). Server messages are already sentences. */
export function errorMessage(error: unknown, fallback = "Something went wrong. Try again."): string {
  if (isApiError(error)) return error.message || fallback;
  return fallback;
}

const STATUS_CODES: Record<number, ApiErrorCode> = {
  400: "invalid_request",
  401: "unauthenticated",
  403: "forbidden",
  404: "not_found",
  405: "invalid_request",
  409: "conflict",
  413: "invalid_request",
  422: "invalid_request",
  429: "rate_limited",
};

/** Fallback code when a non-2xx response has no parseable envelope (e.g. a proxy's HTML 502). */
export function codeForStatus(status: number): ApiErrorCode {
  return STATUS_CODES[status] ?? (status >= 500 ? "internal" : "invalid_request");
}

const STATUS_MESSAGES: Partial<Record<ApiErrorCode, string>> = {
  unauthenticated: "Please sign in.",
  forbidden: "You don't have access to that.",
  not_found: "Not found.",
  rate_limited: "Too many requests. Try again shortly.",
  consent_required: "Accept the latest Terms of Service and Privacy Policy to continue.",
  internal: "Something went wrong. Try again.",
};

/** Parse a non-2xx response body into an ApiError. Never throws. */
export function parseErrorResponse(status: number, bodyText: string, getHeader: (name: string) => string | null): ApiError {
  let code: string | undefined;
  let message: string | undefined;
  let details: Record<string, unknown> | undefined;
  if (bodyText) {
    try {
      const parsed: unknown = JSON.parse(bodyText);
      const env = typeof parsed === "object" && parsed !== null ? (parsed as { error?: unknown }).error : undefined;
      if (typeof env === "object" && env !== null) {
        const e = env as { code?: unknown; message?: unknown; details?: unknown };
        if (typeof e.code === "string" && e.code) code = e.code;
        if (typeof e.message === "string" && e.message) message = e.message;
        if (typeof e.details === "object" && e.details !== null && !Array.isArray(e.details)) details = e.details as Record<string, unknown>;
      }
    } catch {
      // Not JSON (proxy error page): fall back to the status mapping.
    }
  }
  const finalCode: AnyErrorCode = code ?? codeForStatus(status);
  const retryAfter = getHeader("retry-after");
  const retryAfterSeconds = retryAfter && /^\d+$/.test(retryAfter.trim()) ? Number(retryAfter.trim()) : undefined;
  return new ApiError({
    status,
    code: finalCode,
    message: message ?? STATUS_MESSAGES[finalCode as ApiErrorCode] ?? `Request failed (${status}).`,
    details,
    requestId: getHeader("x-request-id") ?? undefined,
    retryAfterSeconds,
  });
}
