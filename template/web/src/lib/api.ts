import { createApiClient, createRealtimeClient, type ApiClient, type RealtimeClient, type TokenGetter } from "@scaffoldapp/shared";
import { API_URL, WS_URL } from "./env";

/** The shared API client for the browser, pointed at NEXT_PUBLIC_API_URL. Create one per app (see providers.tsx). */
export function createWebApiClient(getToken: TokenGetter): ApiClient {
  return createApiClient({ baseUrl: API_URL, platform: "web", getToken });
}

/** A realtime connection for one signed-in identity (`/v1/ws`). */
export function createWebRealtimeClient(getToken: TokenGetter): RealtimeClient {
  return createRealtimeClient({ url: WS_URL, getToken });
}
