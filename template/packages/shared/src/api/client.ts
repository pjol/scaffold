import { adminEndpoints } from "./endpoints/admin";
import { coreEndpoints } from "./endpoints/core";
import { createHttpClient, type HttpClient, type HttpClientOptions } from "./http";

/** Options for {@link createApiClient}. */
export type ApiClientOptions = HttpClientOptions;

/**
 * Typed client for the core API. Every method takes an optional trailing `CallOptions`
 * (`signal`, `timeoutMs`, `headers`) and throws `ApiError` on failure.
 *
 * Features don't edit this file: they export an endpoint factory and reach it through
 * `endpointsFor(api.http, chatEndpoints)` (or `useEndpoints(chatEndpoints)` in hooks).
 *
 * @example
 * const api = createApiClient({
 *   baseUrl: process.env.NEXT_PUBLIC_API_URL!,
 *   platform: "web", // Expo: clientPlatform(Platform.OS)
 *   getToken: (o) => session.getToken({ forceRefresh: o?.forceRefresh }),
 * });
 * const me = await api.me();
 */
export function createApiClient(options: ApiClientOptions) {
  const http = createHttpClient(options);
  return {
    /** Low-level client: features build their endpoints on it. */
    http,
    platform: options.platform,
    ...coreEndpoints(http),
    admin: adminEndpoints(http),
  };
}

/** The object returned by {@link createApiClient}. */
export type ApiClient = ReturnType<typeof createApiClient>;

/** A feature's endpoint group: `export const chatEndpoints = (http: HttpClient) => ({ … })`. */
export type EndpointFactory<T> = (http: HttpClient) => T;

const groups = new WeakMap<HttpClient, Map<EndpointFactory<unknown>, unknown>>();

/** The endpoint group `factory` builds for `http`, created once per client and reused. */
export function endpointsFor<T>(http: HttpClient, factory: EndpointFactory<T>): T {
  let byFactory = groups.get(http);
  if (!byFactory) {
    byFactory = new Map();
    groups.set(http, byFactory);
  }
  if (!byFactory.has(factory)) byFactory.set(factory, factory(http));
  return byFactory.get(factory) as T;
}
