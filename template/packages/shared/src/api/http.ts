import type { ClientPlatform } from "../types";
import { computeBackoff, type BackoffOptions } from "../utils/backoff";
import { generateId } from "../utils/id";
import { ApiError, clientErrorMessage, isApiError, parseErrorResponse } from "./errors";
import { buildQueryString, type QueryParams } from "./query-string";

/** `fetch`-compatible function (global fetch, React Native fetch, or a test double). */
export type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

/**
 * Returns the current session token, or null when signed out. Called before **every**
 * request. `forceRefresh` is set when retrying a 401: wire it to Clerk's
 * `getToken({ skipCache: true })`.
 */
export type TokenGetter = (options?: { forceRefresh?: boolean }) => Promise<string | null | undefined> | string | null | undefined;

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE" | "HEAD";

/**
 * Map an OS name to a {@link ClientPlatform}: `"ios"` / `"android"` pass through,
 * anything else is `"web"`.
 *
 * @example createApiClient({ …, platform: clientPlatform(Platform.OS) }) // Expo
 */
export function clientPlatform(os: string | null | undefined): ClientPlatform {
  return os === "ios" || os === "android" ? os : "web";
}

export interface RetryOptions extends Partial<BackoffOptions> {
  /** Extra attempts after the first. Default 2. */
  retries?: number;
}

export interface HttpClientOptions {
  /** API origin, e.g. `https://api.example.com` (a trailing `/` or `/v1` is tolerated). */
  baseUrl: string;
  getToken: TokenGetter;
  /** Sent as `X-Client-Platform` (`web` for Next; `clientPlatform(Platform.OS)` for Expo). */
  platform: ClientPlatform;
  /** App version, sent as `X-Client-Version` (native builds: `expo-application` version). */
  version?: string;
  /** Custom fetch; defaults to the global fetch, resolved at call time. */
  fetch?: FetchFn;
  /** Per-attempt timeout. Default 15 000 ms. */
  timeoutMs?: number;
  /** Called on a final 401 for a request that carried a token. Sign the user out here. */
  onUnauthorized?: (error: ApiError) => void;
  /** Retry policy for idempotent requests, or `false` to disable. */
  retry?: RetryOptions | false;
  /** Extra headers on every request. */
  headers?: Record<string, string> | (() => Record<string, string>);
  /** Randomness source for backoff jitter (tests). */
  random?: () => number;
}

/** Per-call options accepted by every API method. */
export interface CallOptions {
  /** Abort signal (react-query passes one to queryFn). */
  signal?: AbortSignal | undefined;
  /** Override the client timeout for this call. */
  timeoutMs?: number | undefined;
  /** Extra headers for this call. */
  headers?: Record<string, string> | undefined;
}

export interface RequestOptions extends CallOptions {
  query?: QueryParams | undefined;
  body?: unknown;
  /** Sets `Idempotency-Key`; such requests are also safe to retry. */
  idempotencyKey?: string | undefined;
  /** Send the bearer token. Default true. */
  auth?: boolean | undefined;
  /** Force-enable or disable automatic retry for this call. */
  retry?: boolean | undefined;
}

export interface TextRequestOptions extends RequestOptions {
  /** `Accept` header. Default `text/csv, text/plain;q=0.9, *\/*;q=0.1`. */
  accept?: string | undefined;
}

export interface HttpClient {
  readonly baseUrl: string;
  readonly platform: ClientPlatform;
  /** Performs a request and returns the parsed JSON body (`undefined` for 204 / empty). */
  request<T>(method: HttpMethod, path: string, options?: RequestOptions): Promise<T>;
  /** Same auth, retry and error handling as `request`, resolving to the raw body text (CSV). */
  text(method: HttpMethod, path: string, options?: TextRequestOptions): Promise<string>;
  get<T>(path: string, options?: RequestOptions): Promise<T>;
  post<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T>;
  put<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T>;
  patch<T>(path: string, body?: unknown, options?: RequestOptions): Promise<T>;
  delete<T>(path: string, options?: RequestOptions): Promise<T>;
  /** The fetch implementation in use (uploads PUT straight to storage with it). */
  fetch: FetchFn;
}

const DEFAULT_TIMEOUT_MS = 15_000;
const DEFAULT_RETRY = { retries: 2, initialMs: 300, maxMs: 3_000, factor: 2, jitter: 0.5 } satisfies Required<RetryOptions>;
const RETRYABLE_STATUS = new Set([502, 503, 504]);

/** Normalize the API origin: strip trailing slashes and a trailing `/v1`. */
export function normalizeBaseUrl(baseUrl: string): string {
  let url = baseUrl.trim().replace(/\/+$/, "");
  if (url.endsWith("/v1")) url = url.slice(0, -3);
  return url;
}

function globalFetch(): FetchFn {
  const f = (globalThis as { fetch?: FetchFn }).fetch;
  if (typeof f !== "function") throw new Error("No global fetch available; pass `fetch` to createApiClient.");
  return (input, init) => f(input, init); // unbound call avoids "Illegal invocation"
}

interface RawResponse {
  status: number;
  text: string;
  header: (name: string) => string | null;
}

const aborted = (cause?: unknown) => new ApiError({ status: 0, code: "aborted", message: clientErrorMessage("aborted"), cause });

/** Sleep that rejects with an `aborted` ApiError if the signal fires. */
function sleep(ms: number, signal: AbortSignal | undefined): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(aborted());
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(aborted());
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener?.("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener?.("abort", onAbort);
  });
}

/**
 * One network attempt with a timeout that also covers reading the body. Throws
 * ApiError(network_error | timeout | aborted); never treats non-2xx as an error.
 */
async function sendOnce(fetchFn: FetchFn, url: string, init: RequestInit, timeoutMs: number, signal: AbortSignal | undefined): Promise<RawResponse> {
  if (signal?.aborted) throw aborted();
  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  const onAbort = () => controller.abort();
  signal?.addEventListener?.("abort", onAbort);
  try {
    const res = await fetchFn(url, { ...init, signal: controller.signal });
    const text = res.status === 204 || init.method === "HEAD" ? "" : await res.text();
    return { status: res.status, text, header: (name) => res.headers?.get?.(name) ?? null };
  } catch (cause) {
    if (timedOut) throw new ApiError({ status: 0, code: "timeout", message: clientErrorMessage("timeout"), cause });
    if (signal?.aborted) throw aborted(cause);
    throw new ApiError({ status: 0, code: "network_error", message: clientErrorMessage("network_error"), cause });
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener?.("abort", onAbort);
  }
}

function parseSuccess<T>(raw: RawResponse): T {
  if (raw.status === 204 || raw.text.trim() === "") return undefined as T;
  try {
    return JSON.parse(raw.text) as T;
  } catch (cause) {
    throw new ApiError({
      status: raw.status,
      code: "invalid_response",
      message: clientErrorMessage("invalid_response"),
      requestId: raw.header("x-request-id") ?? undefined,
      cause,
    });
  }
}

/**
 * Low-level HTTP client: bearer token per request, `X-Client-Platform` and
 * `X-Client-Version`, JSON in/out, timeouts, error envelope parsing, one forced token
 * refresh on 401, and backoff retries for idempotent requests (GET/HEAD, or anything
 * carrying an `Idempotency-Key`) on network errors and 502/503/504 only, honouring
 * `Retry-After`.
 */
export function createHttpClient(options: HttpClientOptions): HttpClient {
  const baseUrl = normalizeBaseUrl(options.baseUrl);
  const retryPolicy = options.retry === false ? null : { ...DEFAULT_RETRY, ...(options.retry ?? {}) };
  const random = options.random ?? Math.random;
  const custom = options.fetch;
  const fetchFn: FetchFn = custom ? (input, init) => custom(input, init) : (input, init) => globalFetch()(input, init);

  async function resolveToken(forceRefresh: boolean): Promise<string | null> {
    try {
      const token = await options.getToken(forceRefresh ? { forceRefresh: true } : undefined);
      return token ? token : null;
    } catch {
      return null; // token provider failure: send unauthenticated, the server answers 401
    }
  }

  async function send(method: HttpMethod, path: string, opts: RequestOptions, accept: string): Promise<RawResponse> {
    const url = `${baseUrl}${path.startsWith("/") ? path : `/${path}`}${buildQueryString(opts.query)}`;
    const extra = typeof options.headers === "function" ? options.headers() : options.headers;
    const hasBody = opts.body !== undefined;
    const isIdempotent = method === "GET" || method === "HEAD" || !!opts.idempotencyKey;
    const canRetry = retryPolicy !== null && (opts.retry ?? isIdempotent);
    const timeoutMs = opts.timeoutMs ?? options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
    const useAuth = opts.auth ?? true;

    let retriesUsed = 0;
    let refreshedAuth = false;
    for (;;) {
      if (opts.signal?.aborted) throw aborted();
      const token = useAuth ? await resolveToken(refreshedAuth) : null;
      const headers: Record<string, string> = {
        Accept: accept,
        "X-Client-Platform": options.platform,
        ...(options.version ? { "X-Client-Version": options.version } : {}),
        ...extra,
        ...opts.headers,
      };
      if (hasBody) headers["Content-Type"] = "application/json";
      if (token) headers["Authorization"] = `Bearer ${token}`;
      if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;
      const init: RequestInit = { method, headers };
      if (hasBody) init.body = JSON.stringify(opts.body);

      let raw: RawResponse;
      try {
        raw = await sendOnce(fetchFn, url, init, timeoutMs, opts.signal);
      } catch (err) {
        if (retryPolicy && canRetry && isApiError(err) && err.code === "network_error" && retriesUsed < retryPolicy.retries) {
          await sleep(computeBackoff(retriesUsed, retryPolicy, random), opts.signal);
          retriesUsed++;
          continue;
        }
        throw err;
      }

      if (raw.status >= 200 && raw.status < 300) return raw;

      if (raw.status === 401 && useAuth && token && !refreshedAuth) {
        // The cached token may have just expired: refresh once (not counted as a retry).
        refreshedAuth = true;
        continue;
      }
      const error = parseErrorResponse(raw.status, raw.text, raw.header);
      if (retryPolicy && canRetry && RETRYABLE_STATUS.has(raw.status) && retriesUsed < retryPolicy.retries) {
        const hinted = error.retryAfterSeconds !== undefined ? Math.min(error.retryAfterSeconds * 1000, retryPolicy.maxMs) : 0;
        await sleep(Math.max(hinted, computeBackoff(retriesUsed, retryPolicy, random)), opts.signal);
        retriesUsed++;
        continue;
      }
      // Only a token the server rejected means the session is gone. A request sent without
      // one (signed out, or racing a sign-in) must not trigger a sign-out.
      if (raw.status === 401 && token) options.onUnauthorized?.(error);
      throw error;
    }
  }

  async function request<T>(method: HttpMethod, path: string, opts: RequestOptions = {}): Promise<T> {
    return parseSuccess<T>(await send(method, path, opts, "application/json"));
  }

  async function text(method: HttpMethod, path: string, opts: TextRequestOptions = {}): Promise<string> {
    const raw = await send(method, path, opts, opts.accept ?? "text/csv, text/plain;q=0.9, */*;q=0.1");
    return raw.text;
  }

  return {
    baseUrl,
    platform: options.platform,
    fetch: fetchFn,
    request,
    text,
    get: (path, o) => request("GET", path, o),
    post: (path, body, o) => request("POST", path, { ...o, body }),
    put: (path, body, o) => request("PUT", path, { ...o, body }),
    patch: (path, body, o) => request("PATCH", path, { ...o, body }),
    delete: (path, o) => request("DELETE", path, o),
  };
}

/** A fresh UUID for `Idempotency-Key`. */
export function newIdempotencyKey(): string {
  return generateId();
}
