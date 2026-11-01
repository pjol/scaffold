import { QueryClient, type QueryClientConfig } from "@tanstack/react-query";
import { isApiError } from "../api/errors";

/**
 * Query retry policy layered on top of the HTTP client's own retries:
 * never retry 4xx / validation / aborts; network errors were already retried
 * by the client; timeouts and 5xx get up to 2 more tries.
 */
export function shouldRetryQuery(failureCount: number, error: unknown): boolean {
  if (isApiError(error)) {
    if (error.code === "aborted" || error.code === "network_error" || error.code === "invalid_request") return false;
    if (error.status >= 400 && error.status < 500) return false;
  }
  return failureCount < 2;
}

/** QueryClient with the house defaults: 30 s staleTime, the retry policy above, no mutation retries. */
export function createQueryClient(config: QueryClientConfig = {}): QueryClient {
  return new QueryClient({
    ...config,
    defaultOptions: {
      ...config.defaultOptions,
      queries: {
        staleTime: 30_000,
        gcTime: 5 * 60_000,
        retry: shouldRetryQuery,
        retryDelay: (attempt) => Math.min(1_000 * 2 ** attempt, 8_000),
        refetchOnWindowFocus: true,
        ...config.defaultOptions?.queries,
      },
      mutations: {
        retry: false,
        ...config.defaultOptions?.mutations,
      },
    },
  });
}
